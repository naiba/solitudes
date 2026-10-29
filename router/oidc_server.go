package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	googleuuid "github.com/google/uuid"
	"github.com/hashicorp/go-uuid"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

// oidcProvider is initialized once after database migration. Its storage is
// database-backed so authorization and token state survives server restarts.
func newOIDCProvider() (*op.Provider, error) {
	issuer := publicBaseURL()
	if !validSiteDomain() {
		return nil, errors.New("valid site domain required for OIDC issuer")
	}
	cryptoKey, keyID, err := ensureOIDCKeys(solitudes.System.DB)
	if err != nil {
		return nil, err
	}
	config := &op.Config{
		CryptoKey: cryptoKey, CryptoKeyId: keyID,
		CodeMethodS256: true, GrantTypeRefreshToken: true,
		SupportedScopes:          []string{"openid", "email", "profile", "offline_access"},
		DefaultLogoutRedirectURI: issuer + "/",
	}
	var options []op.Option
	if strings.HasPrefix(issuer, "http://localhost") || strings.HasPrefix(issuer, "http://127.0.0.1") {
		options = append(options, op.WithAllowInsecure())
	}
	return op.NewOpenIDProvider(issuer, config, &oidcStorage{db: solitudes.System.DB}, options...)
}

func oidcHTTPHandler(provider http.Handler) endpointHandler {
	return func(writer http.ResponseWriter, httpRequest *http.Request) error {
		// Keep a replayable, bounded body because protocol audit also reads client_id.
		body, err := io.ReadAll(http.MaxBytesReader(writer, httpRequest.Body, 1<<20))
		if err != nil {
			return newHTTPError(http.StatusRequestEntityTooLarge)
		}
		httpRequest.Body = io.NopCloser(bytes.NewReader(body))
		// The provider owns another chi router. Isolate its route context so
		// aliases and protocol patterns cannot reuse or overwrite the outer route.
		protocolContext := context.WithValue(oidcBrowserContext(writer, httpRequest), chi.RouteCtxKey, (*chi.Context)(nil))
		r := httpRequest.Clone(protocolContext)
		r.Body = io.NopCloser(bytes.NewReader(body))
		metadata := requestPath(httpRequest) == "/.well-known/openid-configuration" || requestPath(httpRequest) == "/.well-known/oauth-authorization-server"
		if requestPath(httpRequest) == "/.well-known/oauth-authorization-server" {
			r.URL.Path = "/.well-known/openid-configuration"
			r.URL.RawPath = ""
		}
		var revocationFormError error
		if httpRequest.Method == http.MethodPost && requestPath(httpRequest) == "/revoke" {
			revocationFormError = r.ParseForm()
			if revocationFormError == nil {
				// RFC 7009 hints only optimize lookup; a wrong hint must not
				// prevent revocation. Always let the SDK authenticate the client
				// and check its refresh tokens before attempting access-token
				// decryption, which can misidentify an opaque refresh value.
				r.Form.Del("token_type_hint")
				r.PostForm.Del("token_type_hint")
			}
		}
		response := httptest.NewRecorder()
		if revocationFormError != nil {
			op.RevocationRequestError(response, r, oidc.ErrInvalidRequest().WithDescription("unable to parse request").WithParent(revocationFormError))
		} else {
			provider.ServeHTTP(response, r)
		}
		result := response.Result()
		defer result.Body.Close()
		// Capture only the standardized error code, never error descriptions or
		// the protocol body (which may contain access/refresh tokens).
		var protocolError struct {
			Error string `json:"error"`
		}
		if result.StatusCode >= 400 {
			_ = json.Unmarshal(response.Body.Bytes(), &protocolError)
		}
		if result.StatusCode >= 300 && result.StatusCode < 400 {
			if location, err := url.Parse(result.Header.Get("Location")); err == nil {
				protocolError.Error = location.Query().Get("error")
			}
		}
		switch protocolError.Error {
		case "login_required", "consent_required", "interaction_required":
			// These authorization responses ask the browser to interact, not report
			// failed credentials. Mark even HTTP 400 responses; status alone cannot
			// distinguish a normal guest login prompt from a security failure.
			if httpRequest.Method == http.MethodGet && (requestPath(httpRequest) == "/authorize" || requestPath(httpRequest) == "/authorize/callback") &&
				(result.StatusCode == http.StatusBadRequest || (result.StatusCode >= 300 && result.StatusCode < 400)) {
				requestValue(httpRequest, "audit_interaction_required", true)
			}
		case "invalid_request", "invalid_client", "invalid_grant", "unauthorized_client", "invalid_scope", "unsupported_grant_type", "access_denied", "server_error", "temporarily_unavailable", "invalid_token", "insufficient_scope", "unsupported_response_type":
			requestValue(httpRequest, "audit_reason", protocolError.Error)
			requestValue(httpRequest, "audit_outcome", "failure")
			// This is the targeted application, not proof of authentication.
			// The library may reject a code before looking up its client.
			claimed := formValue(httpRequest, "client_id")
			if httpRequest.Method == http.MethodGet {
				claimed = queryValue(httpRequest, "client_id")
			}
			if id, _, ok := r.BasicAuth(); ok {
				claimed = id
			}
			if claimed != "" && auditIdentifier(claimed) != "" {
				var target model.OIDCClient
				if err := solitudes.System.DB.Select("id").Where("id = ?", claimed).Take(&target).Error; err == nil {
					requestValue(httpRequest, "audit_client", target.ID)
				}
			}
		}
		if metadata && result.StatusCode == http.StatusOK {
			var document map[string]interface{}
			if err := json.NewDecoder(io.LimitReader(result.Body, 1<<20)).Decode(&document); err != nil {
				return err
			}
			// The upstream library advertises implicit flow and device grants even
			// when they are disabled. Publish only flows this server actually handles.
			document["response_types_supported"] = []string{"code"}
			document["grant_types_supported"] = []string{"authorization_code", "refresh_token"}
			document["token_endpoint_auth_methods_supported"] = []string{"none", "client_secret_basic"}
			document["revocation_endpoint_auth_methods_supported"] = []string{"none", "client_secret_basic"}
			document["introspection_endpoint_auth_methods_supported"] = []string{"client_secret_basic"}
			document["request_parameter_supported"] = false
			delete(document, "request_object_signing_alg_values_supported")
			delete(document, "token_endpoint_auth_signing_alg_values_supported")
			delete(document, "revocation_endpoint_auth_signing_alg_values_supported")
			delete(document, "introspection_endpoint_auth_signing_alg_values_supported")
			delete(document, "device_authorization_endpoint")
			writer.Header().Set("Content-Type", "application/json")
			writer.Header().Set("Cache-Control", "public, max-age=300")
			writer.Header().Set("Access-Control-Allow-Origin", "*")
			return writeJSON(writer, http.StatusOK, document)
		}
		for header, values := range result.Header {
			// Preserve the provider's Content-Type and
			// retain separate values such as Set-Cookie instead of comma-joining.
			writer.Header().Del(header)
			for _, value := range values {
				writer.Header().Add(header, value)
			}
		}
		writer.Header().Set("Cache-Control", "no-store")
		writer.WriteHeader(result.StatusCode)
		_, err = io.Copy(writer, result.Body)
		return err
	}
}

func consentPage(writer http.ResponseWriter, httpRequest *http.Request) error {
	writer.Header().Set("Cache-Control", "private, no-store")
	provider := activeOIDCProvider
	if provider == nil {
		return newHTTPError(http.StatusServiceUnavailable)
	}
	id := queryValue(httpRequest, "authRequestID")
	if _, err := googleuuid.Parse(id); err != nil {
		return newHTTPError(http.StatusBadRequest)
	}
	request, err := provider.Storage().AuthRequestByID(httpRequest.Context(), id)
	if err != nil {
		return newHTTPError(http.StatusBadRequest)
	}
	client, err := provider.Storage().GetClientByClientID(httpRequest.Context(), request.GetClientID())
	if err != nil {
		return newHTTPError(http.StatusBadRequest)
	}
	pending, ok := request.(*oidcAuthRequest)
	if !ok {
		return newHTTPError(http.StatusBadRequest)
	}
	resolved, decision, err := completeOIDCConsent(writer, httpRequest, pending, false)
	if err != nil {
		return err
	}
	if decision == oidcNeedsLogin || (decision == oidcNeedsConsent && slices.Contains(resolved.req.Prompt, oidc.PromptNone)) {
		return oidcConsentResponse(writer, httpRequest, resolved, decision)
	}
	if decision == oidcConsentReady {
		return redirectResponse(writer, httpRequest, "/authorize/callback?id="+url.QueryEscape(id), http.StatusFound)
	}
	info, err := oidcClientInfo(httpRequest.Context(), client)
	if err != nil {
		return err
	}
	return renderPage(writer, httpRequest, http.StatusOK, "site/consent", injectSiteData(writer, httpRequest, viewData{
		"title": "Authorize application", "noindex": true, "oidc_client": info, "scopes": request.GetScopes(), "request_id": id,
	}))
}

type oidcApplicationInfo struct {
	ID          string
	Name        string
	Description string
	HomepageURL string
	OwnerID     string
	OwnerName   string
}

func clientMetadataComplete(client *model.OIDCClient) bool {
	return client.OwnerID != nil && *client.OwnerID != "" && strings.TrimSpace(client.Name) != "" && validClientHomepage(client.HomepageURL)
}

func oidcClientInfo(ctx context.Context, client op.Client) (oidcApplicationInfo, error) {
	entry, ok := client.(*oidcClient)
	if !ok || !clientMetadataComplete(&entry.OIDCClient) {
		return oidcApplicationInfo{}, errors.New("invalid application metadata")
	}
	var owner model.Account
	if err := solitudes.System.DB.WithContext(ctx).Select("id, nickname").Take(&owner, "id = ?", *entry.OwnerID).Error; err != nil {
		return oidcApplicationInfo{}, fmt.Errorf("load OIDC client owner: %w", err)
	}
	return oidcApplicationInfo{ID: entry.ID, Name: entry.Name, Description: entry.Description, HomepageURL: entry.HomepageURL, OwnerID: owner.ID, OwnerName: owner.Nickname}, nil
}

// Only a server-created authorization request can supply application details
// to the sign-in page. A user-supplied return_to cannot assert an app name.
func loginOIDCApplication(writer http.ResponseWriter, httpRequest *http.Request, returnTo string) (*oidcApplicationInfo, error) {
	if activeOIDCProvider == nil || returnTo == "" {
		return nil, nil
	}
	target, err := url.Parse(returnTo)
	if err != nil || target.Path != "/oidc/consent" || len(target.Query()["authRequestID"]) != 1 {
		return nil, nil
	}
	id := target.Query().Get("authRequestID")
	if _, err := googleuuid.Parse(id); err != nil {
		return nil, nil
	}
	request, err := activeOIDCProvider.Storage().AuthRequestByID(httpRequest.Context(), id)
	if err != nil || request.Done() {
		return nil, nil
	}
	client, err := activeOIDCProvider.Storage().GetClientByClientID(httpRequest.Context(), request.GetClientID())
	if err != nil {
		return nil, nil
	}
	info, err := oidcClientInfo(httpRequest.Context(), client)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func consentHandler(writer http.ResponseWriter, httpRequest *http.Request) error {
	if activeOIDCProvider == nil {
		return newHTTPError(http.StatusServiceUnavailable)
	}
	account := currentAccount(writer, httpRequest)
	if account == nil {
		return newHTTPError(http.StatusUnauthorized)
	}
	id := formValue(httpRequest, "request_id")
	request, err := activeOIDCProvider.Storage().AuthRequestByID(httpRequest.Context(), id)
	if err != nil || request.Done() {
		return newHTTPError(http.StatusBadRequest)
	}
	if _, err := activeOIDCProvider.Storage().GetClientByClientID(httpRequest.Context(), request.GetClientID()); err != nil {
		return newHTTPError(http.StatusBadRequest)
	}
	if formValue(httpRequest, "allow") != "yes" {
		requestValue(httpRequest, "audit_reason", "consent_declined")
		requestValue(httpRequest, "audit_client", request.GetClientID())
		// The redirect URI was checked against the registered client before the
		// authorization request was saved by the OIDC library.
		target, err := url.Parse(request.GetRedirectURI())
		if err != nil {
			return newHTTPError(http.StatusBadRequest)
		}
		query := target.Query()
		query.Set("error", "access_denied")
		query.Set("state", request.GetState())
		target.RawQuery = query.Encode()
		_ = activeOIDCProvider.Storage().DeleteAuthRequest(httpRequest.Context(), id)
		return redirectResponse(writer, httpRequest, target.String(), http.StatusFound)
	}
	pending, ok := request.(*oidcAuthRequest)
	if !ok {
		return newHTTPError(http.StatusBadRequest)
	}
	resolved, decision, err := completeOIDCConsent(writer, httpRequest, pending, true)
	if err != nil {
		return err
	}
	if decision != oidcConsentReady {
		return oidcConsentResponse(writer, httpRequest, resolved, decision)
	}
	return redirectResponse(writer, httpRequest, "/authorize/callback?id="+url.QueryEscape(id), http.StatusFound)
}

var activeOIDCProvider *op.Provider

func oidcClientsPage(writer http.ResponseWriter, httpRequest *http.Request) error {
	if !strings.HasPrefix(requestPath(httpRequest), "/account/") {
		return adminClientsPage(writer, httpRequest)
	}
	var clients []model.OIDCClient
	page, err := listPage(queryValue(httpRequest, "page"))
	if err != nil {
		return err
	}
	query := solitudes.System.DB.Where("owner_id = ?", currentAccount(writer, httpRequest).ID).
		Order("created_at DESC, id DESC").Limit(21).Offset((page - 1) * 20)
	if err := query.Find(&clients).Error; err != nil {
		return err
	}
	more := len(clients) > 20
	if more {
		clients = clients[:20]
	}
	type clientView struct {
		model.OIDCClient
		RedirectURIs string
		LogoutURIs   string
		LoginUsers   int64
		ActiveUsers  int64
	}
	ids := make([]string, len(clients))
	for i := range clients {
		ids[i] = clients[i].ID
	}
	usage, err := ownedClientUsage(solitudes.System.DB.WithContext(httpRequest.Context()), currentAccount(writer, httpRequest).ID, ids, time.Now())
	if err != nil {
		return err
	}
	views := make([]clientView, 0, len(clients))
	for _, client := range clients {
		if !validClientHomepage(client.HomepageURL) {
			client.HomepageURL = ""
		}
		var redirects []string
		_ = json.Unmarshal([]byte(client.RedirectURIsJSON), &redirects)
		var logoutURIs []string
		_ = json.Unmarshal([]byte(client.PostLogoutURIsJSON), &logoutURIs)
		views = append(views, clientView{OIDCClient: client, RedirectURIs: strings.Join(redirects, "\n"), LogoutURIs: strings.Join(logoutURIs, "\n"), LoginUsers: usage[client.ID].LoginUsers, ActiveUsers: usage[client.ID].ActiveUsers})
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	return renderPage(writer, httpRequest, http.StatusOK, "site/account_oidc_clients", injectSiteData(writer, httpRequest, viewData{
		"title": "OIDC clients", "noindex": true, "clients": views, "issuer": publicBaseURL(),
		"navigation": pageNavigationFor(writer, httpRequest, "page", page, more, "clients"),
	}))
}

func validClientRedirect(raw string) bool {
	if raw == "" || strings.ContainsAny(raw, "*\\\r\n") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	return u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1")
}

func validClientHomepage(raw string) bool {
	if raw == "" || len(raw) > 2048 || strings.ContainsAny(raw, "*\\\r\n\t") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	return u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1")
}

type oidcClientMetadata struct {
	name        string
	description string
	homepage    string
	redirects   string
	logoutURIs  string
}

func oidcClientMetadataFromForm(writer http.ResponseWriter, httpRequest *http.Request) (oidcClientMetadata, error) {
	var metadata oidcClientMetadata
	name := strings.TrimSpace(formValue(httpRequest, "name"))
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 100 || strings.ContainsAny(name, "\r\n\x00") {
		return metadata, newHTTPError(http.StatusBadRequest)
	}
	description := strings.TrimSpace(formValue(httpRequest, "description"))
	homepage := strings.TrimSpace(formValue(httpRequest, "homepage_url"))
	if !utf8.ValidString(description) || utf8.RuneCountInString(description) > 300 || strings.ContainsAny(description, "\x00") || !validClientHomepage(homepage) {
		return metadata, newHTTPError(http.StatusBadRequest, "invalid application description or homepage URL")
	}
	redirects := strings.Fields(formValue(httpRequest, "redirect_uris"))
	if len(redirects) == 0 || len(redirects) > 10 {
		return metadata, newHTTPError(http.StatusBadRequest)
	}
	seen := make(map[string]bool, len(redirects))
	for _, redirect := range redirects {
		if !validClientRedirect(redirect) || seen[redirect] {
			return metadata, newHTTPError(http.StatusBadRequest, "invalid or duplicate redirect URI")
		}
		seen[redirect] = true
	}
	encoded, err := json.Marshal(redirects)
	if err != nil {
		return metadata, err
	}
	logoutURIs := strings.Fields(formValue(httpRequest, "post_logout_uris"))
	if len(logoutURIs) > 10 {
		return metadata, newHTTPError(http.StatusBadRequest)
	}
	seen = make(map[string]bool, len(logoutURIs))
	for _, redirect := range logoutURIs {
		if !validClientRedirect(redirect) || seen[redirect] {
			return metadata, newHTTPError(http.StatusBadRequest, "invalid or duplicate post-logout URI")
		}
		seen[redirect] = true
	}
	logoutEncoded, err := json.Marshal(logoutURIs)
	if err != nil {
		return metadata, err
	}
	return oidcClientMetadata{name: name, description: description, homepage: homepage,
		redirects: string(encoded), logoutURIs: string(logoutEncoded)}, nil
}

func createOIDCClient(writer http.ResponseWriter, httpRequest *http.Request) error {
	metadata, err := oidcClientMetadataFromForm(writer, httpRequest)
	if err != nil {
		return err
	}
	clientID, err := uuid.GenerateUUID()
	if err != nil {
		return err
	}
	public := formValue(httpRequest, "public") == "on"
	secret := ""
	hash := ""
	if !public {
		secret, err = newSecret()
		if err != nil {
			return err
		}
		bytes, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		hash = string(bytes)
	}
	ownerID := currentAccount(writer, httpRequest).ID
	client := model.OIDCClient{ID: clientID, OwnerID: &ownerID, Name: metadata.name, Description: metadata.description, HomepageURL: metadata.homepage, SecretHash: hash,
		Public: public, RedirectURIsJSON: metadata.redirects, PostLogoutURIsJSON: metadata.logoutURIs}
	requestValue(httpRequest, "audit_client", clientID)
	if err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&client).Error; err != nil {
			return err
		}
		return auditMutation(writer, httpRequest, tx, "client.create", clientID, clientID, "")
	}); err != nil {
		return err
	}
	requestValue(httpRequest, "audit_recorded", true)
	writer.Header().Set("Cache-Control", "private, no-store")
	templateName := "admin/oidc_client_created"
	if strings.HasPrefix(requestPath(httpRequest), "/account/") {
		templateName = "site/account_oidc_client_created"
	}
	return renderPage(writer, httpRequest, http.StatusCreated, templateName, injectSiteData(writer, httpRequest, viewData{
		"title": "OIDC client created", "noindex": true, "client_id": clientID, "secret": secret,
	}))
}

func updateOIDCClient(writer http.ResponseWriter, httpRequest *http.Request) error {
	metadata, err := oidcClientMetadataFromForm(writer, httpRequest)
	if err != nil {
		return err
	}
	account := currentAccount(writer, httpRequest)
	requestValue(httpRequest, "audit_client", routeParam(httpRequest, "id"))
	err = solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&model.OIDCClient{}).
			Where("id = ? AND disabled_at IS NULL", routeParam(httpRequest, "id"))
		if !account.Role.IsAdmin() {
			query = query.Where("owner_id = ?", account.ID)
		}
		var stored model.OIDCClient
		if err := query.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&stored).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newHTTPError(http.StatusNotFound)
			}
			return err
		}
		// An unchanged form submission must not claim an application configuration change.
		if stored.Name == metadata.name && stored.Description == metadata.description && stored.HomepageURL == metadata.homepage && stored.RedirectURIsJSON == metadata.redirects && stored.PostLogoutURIsJSON == metadata.logoutURIs {
			return nil
		}
		result := tx.Model(&stored).Updates(map[string]interface{}{
			"name": metadata.name, "description": metadata.description,
			"homepage_url": metadata.homepage, "redirect_uris_json": metadata.redirects,
			"post_logout_uris_json": metadata.logoutURIs,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return newHTTPError(http.StatusNotFound)
		}
		return auditMutation(writer, httpRequest, tx, "client.update", routeParam(httpRequest, "id"), routeParam(httpRequest, "id"), "name,description,homepage,redirects,logout_uris")
	})
	if err != nil {
		return err
	}
	requestValue(httpRequest, "audit_recorded", true)
	if strings.HasPrefix(requestPath(httpRequest), "/account/") {
		return redirectResponse(writer, httpRequest, "/account/oidc/clients", http.StatusSeeOther)
	}
	return redirectResponse(writer, httpRequest, "/admin/oidc/clients", http.StatusSeeOther)
}

func disableOIDCClient(writer http.ResponseWriter, httpRequest *http.Request) error {
	id := routeParam(httpRequest, "id")
	requestValue(httpRequest, "audit_client", id)
	account := currentAccount(writer, httpRequest)
	if account == nil {
		return newHTTPError(http.StatusUnauthorized)
	}
	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		client, err := lockOIDCClient(tx, id, true)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return newHTTPError(http.StatusNotFound)
		}
		if err != nil {
			return err
		}
		if !account.Role.IsAdmin() && (client.OwnerID == nil || *client.OwnerID != account.ID) {
			return newHTTPError(http.StatusNotFound)
		}
		if err := tx.Model(client).Update("disabled_at", time.Now()).Error; err != nil {
			return err
		}
		if err := deleteOIDCCredentials(tx, id, ""); err != nil {
			return err
		}
		return auditMutation(writer, httpRequest, tx, "client.disable", id, id, "")
	})
	if err != nil {
		return err
	}
	requestValue(httpRequest, "audit_recorded", true)
	if strings.HasPrefix(requestPath(httpRequest), "/account/") {
		return redirectResponse(writer, httpRequest, "/account/oidc/clients", http.StatusSeeOther)
	}
	return redirectResponse(writer, httpRequest, "/admin/oidc/clients", http.StatusSeeOther)
}

func rotateOIDCKeys(writer http.ResponseWriter, httpRequest *http.Request) error {
	if err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		if err := generateSigningKey(tx); err != nil {
			return err
		}
		return auditMutation(writer, httpRequest, tx, "oidc.keys.rotate", "", "", "")
	}); err != nil {
		return fmt.Errorf("rotate OIDC signing keys: %w", err)
	}
	requestValue(httpRequest, "audit_recorded", true)
	return redirectResponse(writer, httpRequest, "/admin/oidc/clients", http.StatusSeeOther)
}
