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
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	googleuuid "github.com/google/uuid"
	"github.com/hashicorp/go-uuid"
	"github.com/zitadel/oidc/v3/pkg/op"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

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

func oidcHTTPHandler(provider *op.Provider) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if len(c.Body()) > 1<<20 {
			return fiber.ErrRequestEntityTooLarge
		}
		address := publicBaseURL() + c.OriginalURL()
		metadata := c.Path() == "/.well-known/openid-configuration" || c.Path() == "/.well-known/oauth-authorization-server"
		if c.Path() == "/.well-known/oauth-authorization-server" {
			address = publicBaseURL() + "/.well-known/openid-configuration"
		}
		r, err := http.NewRequestWithContext(c.UserContext(), c.Method(), address, bytes.NewReader(c.Body()))
		if err != nil {
			return err
		}
		c.Request().Header.VisitAll(func(key, value []byte) { r.Header.Add(string(key), string(value)) })
		r.Host = c.Hostname()
		response := httptest.NewRecorder()
		provider.ServeHTTP(response, r)
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
		case "invalid_request", "invalid_client", "invalid_grant", "unauthorized_client", "invalid_scope", "unsupported_grant_type", "access_denied", "login_required", "consent_required", "server_error", "temporarily_unavailable", "invalid_token", "insufficient_scope", "unsupported_response_type", "interaction_required":
			c.Locals("audit_reason", protocolError.Error)
			c.Locals("audit_outcome", "failure")
			// This is the targeted application, not proof of authentication.
			// The library may reject a code before looking up its client.
			claimed := c.FormValue("client_id")
			if c.Method() == http.MethodGet {
				claimed = c.Query("client_id")
			}
			if id, _, ok := r.BasicAuth(); ok {
				claimed = id
			}
			if claimed != "" && auditIdentifier(claimed) != "" {
				var target model.OIDCClient
				if err := solitudes.System.DB.Select("id").Where("id = ?", claimed).Take(&target).Error; err == nil {
					c.Locals("audit_client", target.ID)
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
			c.Set("Content-Type", "application/json")
			c.Set("Cache-Control", "public, max-age=300")
			c.Set("Access-Control-Allow-Origin", "*")
			return c.Status(http.StatusOK).JSON(document)
		}
		for header, values := range result.Header {
			for _, value := range values {
				c.Append(header, value)
			}
		}
		c.Set(fiber.HeaderCacheControl, "no-store")
		c.Status(result.StatusCode)
		_, err = io.Copy(c, result.Body)
		return err
	}
}

func consentPage(c *fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "private, no-store")
	provider := activeOIDCProvider
	if provider == nil {
		return fiber.ErrServiceUnavailable
	}
	id := c.Query("authRequestID")
	if _, err := googleuuid.Parse(id); err != nil {
		return fiber.ErrBadRequest
	}
	request, err := provider.Storage().AuthRequestByID(c.UserContext(), id)
	if err != nil {
		return fiber.ErrBadRequest
	}
	client, err := provider.Storage().GetClientByClientID(c.UserContext(), request.GetClientID())
	if err != nil {
		return fiber.ErrBadRequest
	}
	if currentAccount(c) == nil {
		return c.Redirect("/login?return_to="+url.QueryEscape("/oidc/consent?authRequestID="+id), http.StatusFound)
	}
	if request.Done() {
		return c.Redirect("/authorize/callback?id="+url.QueryEscape(id), http.StatusFound)
	}
	info, err := oidcClientInfo(c.UserContext(), client)
	if err != nil {
		return err
	}
	return c.Status(http.StatusOK).Render("site/consent", injectSiteData(c, fiber.Map{
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

func oidcClientInfo(ctx context.Context, client op.Client) (oidcApplicationInfo, error) {
	info := oidcApplicationInfo{ID: client.GetID(), Name: client.GetID()}
	entry, ok := client.(*oidcClient)
	if !ok {
		return info, nil
	}
	if entry.Name != "" {
		info.Name = entry.Name
	}
	info.Description = entry.Description
	// Older client records may predate homepage validation. Never render an
	// unvalidated stored URL as a clickable link, even if it is in the database.
	if validClientHomepage(entry.HomepageURL) {
		info.HomepageURL = entry.HomepageURL
	}
	if entry.OwnerID != nil {
		var owner model.Account
		err := solitudes.System.DB.WithContext(ctx).Select("id, nickname").Take(&owner, "id = ?", *entry.OwnerID).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return info, fmt.Errorf("load OIDC client owner: %w", err)
		}
		if err == nil {
			info.OwnerID, info.OwnerName = owner.ID, owner.Nickname
		}
	}
	return info, nil
}

// Only a server-created authorization request can supply application details
// to the sign-in page. A user-supplied return_to cannot assert an app name.
func loginOIDCApplication(c *fiber.Ctx, returnTo string) (*oidcApplicationInfo, error) {
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
	request, err := activeOIDCProvider.Storage().AuthRequestByID(c.UserContext(), id)
	if err != nil || request.Done() {
		return nil, nil
	}
	client, err := activeOIDCProvider.Storage().GetClientByClientID(c.UserContext(), request.GetClientID())
	if err != nil {
		return nil, nil
	}
	info, err := oidcClientInfo(c.UserContext(), client)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func consentHandler(c *fiber.Ctx) error {
	if activeOIDCProvider == nil {
		return fiber.ErrServiceUnavailable
	}
	account := currentAccount(c)
	if account == nil {
		return fiber.ErrUnauthorized
	}
	id := c.FormValue("request_id")
	request, err := activeOIDCProvider.Storage().AuthRequestByID(c.UserContext(), id)
	if err != nil || request.Done() {
		return fiber.ErrBadRequest
	}
	if _, err := activeOIDCProvider.Storage().GetClientByClientID(c.UserContext(), request.GetClientID()); err != nil {
		return fiber.ErrBadRequest
	}
	if c.FormValue("allow") != "yes" {
		c.Locals("audit_reason", "consent_declined")
		c.Locals("audit_client", request.GetClientID())
		// The redirect URI was checked against the registered client before the
		// authorization request was saved by the OIDC library.
		target, err := url.Parse(request.GetRedirectURI())
		if err != nil {
			return fiber.ErrBadRequest
		}
		query := target.Query()
		query.Set("error", "access_denied")
		query.Set("state", request.GetState())
		target.RawQuery = query.Encode()
		_ = activeOIDCProvider.Storage().DeleteAuthRequest(c.UserContext(), id)
		return c.Redirect(target.String(), http.StatusFound)
	}
	result := solitudes.System.DB.Model(&model.OIDCAuthRequest{}).
		Where("id = ? AND approved = false AND expires_at > ?", id, time.Now()).
		Updates(map[string]interface{}{"approved": true, "account_id": account.ID, "auth_time": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fiber.ErrBadRequest
	}
	return c.Redirect("/authorize/callback?id="+url.QueryEscape(id), http.StatusFound)
}

var activeOIDCProvider *op.Provider

func oidcClientsPage(c *fiber.Ctx) error {
	if !strings.HasPrefix(c.Path(), "/account/") {
		return adminClientsPage(c)
	}
	var clients []model.OIDCClient
	page, err := listPage(c.Query("page"))
	if err != nil {
		return err
	}
	query := solitudes.System.DB.Where("owner_id = ?", currentAccount(c).ID).
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
		views = append(views, clientView{OIDCClient: client, RedirectURIs: strings.Join(redirects, "\n"), LogoutURIs: strings.Join(logoutURIs, "\n")})
	}
	c.Set("Cache-Control", "private, no-store")
	return c.Status(http.StatusOK).Render("site/account_oidc_clients", injectSiteData(c, fiber.Map{
		"title": "OIDC clients", "noindex": true, "clients": views, "issuer": publicBaseURL(),
		"navigation": pageNavigationFor(c, "page", page, more, "clients"),
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

func oidcClientMetadataFromForm(c *fiber.Ctx) (oidcClientMetadata, error) {
	var metadata oidcClientMetadata
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 100 || strings.ContainsAny(name, "\r\n\x00") {
		return metadata, fiber.ErrBadRequest
	}
	description := strings.TrimSpace(c.FormValue("description"))
	homepage := strings.TrimSpace(c.FormValue("homepage_url"))
	if !utf8.ValidString(description) || utf8.RuneCountInString(description) > 300 || strings.ContainsAny(description, "\x00") || !validClientHomepage(homepage) {
		return metadata, fiber.NewError(http.StatusBadRequest, "invalid application description or homepage URL")
	}
	redirects := strings.Fields(c.FormValue("redirect_uris"))
	if len(redirects) == 0 || len(redirects) > 10 {
		return metadata, fiber.ErrBadRequest
	}
	seen := make(map[string]bool, len(redirects))
	for _, redirect := range redirects {
		if !validClientRedirect(redirect) || seen[redirect] {
			return metadata, fiber.NewError(http.StatusBadRequest, "invalid or duplicate redirect URI")
		}
		seen[redirect] = true
	}
	encoded, err := json.Marshal(redirects)
	if err != nil {
		return metadata, err
	}
	logoutURIs := strings.Fields(c.FormValue("post_logout_uris"))
	if len(logoutURIs) > 10 {
		return metadata, fiber.ErrBadRequest
	}
	seen = make(map[string]bool, len(logoutURIs))
	for _, redirect := range logoutURIs {
		if !validClientRedirect(redirect) || seen[redirect] {
			return metadata, fiber.NewError(http.StatusBadRequest, "invalid or duplicate post-logout URI")
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

func createOIDCClient(c *fiber.Ctx) error {
	metadata, err := oidcClientMetadataFromForm(c)
	if err != nil {
		return err
	}
	clientID, err := uuid.GenerateUUID()
	if err != nil {
		return err
	}
	public := c.FormValue("public") == "on"
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
	ownerID := currentAccount(c).ID
	client := model.OIDCClient{ID: clientID, OwnerID: &ownerID, Name: metadata.name, Description: metadata.description, HomepageURL: metadata.homepage, SecretHash: hash,
		Public: public, RedirectURIsJSON: metadata.redirects, PostLogoutURIsJSON: metadata.logoutURIs}
	c.Locals("audit_client", clientID)
	if err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&client).Error; err != nil {
			return err
		}
		return auditMutation(c, tx, "client.create", clientID, clientID, "")
	}); err != nil {
		return err
	}
	c.Locals("audit_recorded", true)
	c.Set("Cache-Control", "private, no-store")
	templateName := "admin/oidc_client_created"
	if strings.HasPrefix(c.Path(), "/account/") {
		templateName = "site/account_oidc_client_created"
	}
	return c.Status(http.StatusCreated).Render(templateName, injectSiteData(c, fiber.Map{
		"title": "OIDC client created", "noindex": true, "client_id": clientID, "secret": secret,
	}))
}

func updateOIDCClient(c *fiber.Ctx) error {
	metadata, err := oidcClientMetadataFromForm(c)
	if err != nil {
		return err
	}
	account := currentAccount(c)
	c.Locals("audit_client", c.Params("id"))
	err = solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&model.OIDCClient{}).
			Where("id = ? AND disabled_at IS NULL", c.Params("id"))
		if !account.Role.IsAdmin() {
			query = query.Where("owner_id = ?", account.ID)
		}
		result := query.Updates(map[string]interface{}{
			"name": metadata.name, "description": metadata.description,
			"homepage_url": metadata.homepage, "redirect_uris_json": metadata.redirects,
			"post_logout_uris_json": metadata.logoutURIs,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fiber.ErrNotFound
		}
		return auditMutation(c, tx, "client.update", c.Params("id"), c.Params("id"), "name,description,homepage,redirects,logout_uris")
	})
	if err != nil {
		return err
	}
	c.Locals("audit_recorded", true)
	if strings.HasPrefix(c.Path(), "/account/") {
		return c.Redirect("/account/oidc/clients", http.StatusSeeOther)
	}
	return c.Redirect("/admin/oidc/clients", http.StatusSeeOther)
}

func disableOIDCClient(c *fiber.Ctx) error {
	id := c.Params("id")
	c.Locals("audit_client", id)
	account := currentAccount(c)
	if account == nil {
		return fiber.ErrUnauthorized
	}
	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		client := tx.Model(&model.OIDCClient{}).Where("id = ? AND disabled_at IS NULL", id)
		if !account.Role.IsAdmin() {
			client = client.Where("owner_id = ?", account.ID)
		}
		result := client.Update("disabled_at", time.Now())
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fiber.ErrNotFound
		}
		if err := tx.Where("client_id = ?", id).Delete(&model.OIDCRefreshToken{}).Error; err != nil {
			return err
		}
		if err := tx.Where("client_id = ?", id).Delete(&model.OIDCAccessToken{}).Error; err != nil {
			return err
		}
		return auditMutation(c, tx, "client.disable", id, id, "")
	})
	if err != nil {
		return err
	}
	c.Locals("audit_recorded", true)
	if strings.HasPrefix(c.Path(), "/account/") {
		return c.Redirect("/account/oidc/clients", http.StatusSeeOther)
	}
	return c.Redirect("/admin/oidc/clients", http.StatusSeeOther)
}

func rotateOIDCKeys(c *fiber.Ctx) error {
	if err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		if err := generateSigningKey(tx); err != nil {
			return err
		}
		return auditMutation(c, tx, "oidc.keys.rotate", "", "", "")
	}); err != nil {
		return fmt.Errorf("rotate OIDC signing keys: %w", err)
	}
	c.Locals("audit_recorded", true)
	return c.Redirect("/admin/oidc/clients", http.StatusSeeOther)
}
