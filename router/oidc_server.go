package router

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
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
		CodeMethodS256: true, GrantTypeRefreshToken: true, AuthMethodPost: true,
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
		for header, values := range result.Header {
			for _, value := range values {
				c.Append(header, value)
			}
		}
		c.Status(result.StatusCode)
		_, err = io.Copy(c, result.Body)
		return err
	}
}

func consentPage(c *fiber.Ctx) error {
	provider := activeOIDCProvider
	if provider == nil {
		return fiber.ErrServiceUnavailable
	}
	id := c.Query("authRequestID")
	request, err := provider.Storage().AuthRequestByID(c.UserContext(), id)
	if err != nil {
		return fiber.ErrBadRequest
	}
	client, err := provider.Storage().GetClientByClientID(c.UserContext(), request.GetClientID())
	if err != nil {
		return fiber.ErrBadRequest
	}
	if currentAccount(c) == nil {
		return c.Redirect("/admin/login?return_to="+url.QueryEscape("/oidc/consent?authRequestID="+id), http.StatusFound)
	}
	if request.Done() {
		return c.Redirect("/authorize/callback?id="+url.QueryEscape(id), http.StatusFound)
	}
	var clientName string
	if entry, ok := client.(*oidcClient); ok {
		clientName = entry.Name
	}
	if clientName == "" {
		clientName = client.GetID()
	}
	return c.Status(http.StatusOK).Render("admin/consent", injectSiteData(c, fiber.Map{
		"title": "Authorize application", "client": clientName, "scopes": request.GetScopes(), "request_id": id,
	}))
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
	if c.FormValue("allow") != "yes" {
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
	var clients []model.OIDCClient
	query := solitudes.System.DB.Order("created_at DESC")
	accountPage := strings.HasPrefix(c.Path(), "/account/")
	if accountPage {
		query = query.Where("owner_id = ?", currentAccount(c).ID)
	}
	if err := query.Find(&clients).Error; err != nil {
		return err
	}
	templateName := "admin/oidc_clients"
	if accountPage {
		templateName = "admin/account_oidc_clients"
	}
	return c.Status(http.StatusOK).Render(templateName, injectSiteData(c, fiber.Map{
		"title": "OIDC clients", "clients": clients, "issuer": publicBaseURL(),
	}))
}

func validClientRedirect(raw string) bool {
	if raw == "" || strings.ContainsAny(raw, "*\\\r\n") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	return u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1")
}

func createOIDCClient(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" || len(name) > 100 {
		return fiber.ErrBadRequest
	}
	redirects := strings.Fields(c.FormValue("redirect_uris"))
	if len(redirects) == 0 || len(redirects) > 10 {
		return fiber.ErrBadRequest
	}
	seen := make(map[string]bool, len(redirects))
	for _, redirect := range redirects {
		if !validClientRedirect(redirect) || seen[redirect] {
			return fiber.NewError(http.StatusBadRequest, "invalid or duplicate redirect URI")
		}
		seen[redirect] = true
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
	encoded, err := json.Marshal(redirects)
	if err != nil {
		return err
	}
	ownerID := currentAccount(c).ID
	client := model.OIDCClient{ID: clientID, OwnerID: &ownerID, Name: name, SecretHash: hash,
		Public: public, RedirectURIsJSON: string(encoded)}
	if err := solitudes.System.DB.Create(&client).Error; err != nil {
		return err
	}
	c.Set("Cache-Control", "private, no-store")
	templateName := "admin/oidc_client_created"
	if strings.HasPrefix(c.Path(), "/account/") {
		templateName = "admin/account_oidc_client_created"
	}
	return c.Status(http.StatusCreated).Render(templateName, injectSiteData(c, fiber.Map{
		"title": "OIDC client created", "client_id": clientID, "secret": secret,
	}))
}

func disableOIDCClient(c *fiber.Ctx) error {
	id := c.Params("id")
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
		return tx.Where("client_id = ?", id).Delete(&model.OIDCAccessToken{}).Error
	})
	if err != nil {
		return err
	}
	if strings.HasPrefix(c.Path(), "/account/") {
		return c.Redirect("/account/oidc/clients", http.StatusSeeOther)
	}
	return c.Redirect("/admin/oidc/clients", http.StatusSeeOther)
}

func rotateOIDCKeys(c *fiber.Ctx) error {
	if err := generateSigningKey(solitudes.System.DB); err != nil {
		return fmt.Errorf("rotate OIDC signing keys: %w", err)
	}
	return c.Redirect("/admin/oidc/clients", http.StatusSeeOther)
}
