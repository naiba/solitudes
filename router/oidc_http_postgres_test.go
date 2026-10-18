package router

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

// Exercise the public HTTP bridge, not provider.ServeHTTP directly. Standard
// OAuth clients select the token response parser using Content-Type.
func TestPostgresOIDCHTTPStandardClient(t *testing.T) {
	db, owner, visitor := auditTestDB(t)
	if err := db.AutoMigrate(&model.OIDCSigningKey{}, &model.OIDCCryptoKey{}); err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	server := httptest.NewServer(adaptor.FiberApp(app))
	t.Cleanup(server.Close)
	solitudes.System.Config.Site.Domain = strings.TrimPrefix(server.URL, "http://")
	provider, err := newOIDCProvider()
	if err != nil {
		t.Fatal(err)
	}
	app.Use(oidcHTTPHandler(provider))
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	decodeJSON := func(t *testing.T, resp *http.Response, dest interface{}) {
		t.Helper()
		defer resp.Body.Close()
		mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" || resp.StatusCode != http.StatusOK {
			t.Fatalf("invalid JSON HTTP response: status=%d type=%q error=%v", resp.StatusCode, resp.Header.Get("Content-Type"), err)
		}
		if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
			t.Fatal(err)
		}
	}
	resp, err := client.Get(server.URL + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatal(err)
	}
	var discovery struct {
		Issuer    string `json:"issuer"`
		Authorize string `json:"authorization_endpoint"`
		Token     string `json:"token_endpoint"`
		Userinfo  string `json:"userinfo_endpoint"`
		Keys      string `json:"jwks_uri"`
	}
	decodeJSON(t, resp, &discovery)
	if discovery.Issuer != server.URL {
		t.Fatal("incorrect discovery issuer")
	}
	for _, public := range []bool{false, true} {
		name := "confidential"
		if public {
			name = "public"
		}
		t.Run(name, func(t *testing.T) {
			secret := "test-client-secret"
			hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.MinCost)
			if err != nil {
				t.Fatal(err)
			}
			registered := model.OIDCClient{ID: name, OwnerID: &owner.ID, Name: name, Public: public,
				HomepageURL: "http://localhost:9090/", RedirectURIsJSON: `["http://localhost:9090/callback"]`, SecretHash: string(hash)}
			if err := db.Create(&registered).Error; err != nil {
				t.Fatal(err)
			}
			config := oauth2.Config{ClientID: registered.ID, ClientSecret: secret, RedirectURL: "http://localhost:9090/callback",
				Scopes:   []string{"openid", "email", "profile", "offline_access"},
				Endpoint: oauth2.Endpoint{AuthURL: discovery.Authorize, TokenURL: discovery.Token}}
			if public {
				config.ClientSecret = ""
				config.Endpoint.AuthStyle = oauth2.AuthStyleInParams
			}
			verifier := oauth2.GenerateVerifier()
			getRedirect := func(target string) *url.URL {
				t.Helper()
				resp, err := client.Get(target)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				location, err := resp.Location()
				if err != nil || resp.StatusCode != http.StatusFound {
					t.Fatalf("authorization redirect: status=%d error=%v", resp.StatusCode, err)
				}
				return location
			}
			consent := getRedirect(config.AuthCodeURL("test-state", oauth2.S256ChallengeOption(verifier)))
			id := consent.Query().Get("authRequestID")
			if consent.Path != "/oidc/consent" || id == "" {
				t.Fatal("missing consent request")
			}
			// Consent UI/authentication are covered by E2E; approve this fixture as
			// a different user to assert the client receives the visitor's identity.
			if err := db.Model(&model.OIDCAuthRequest{}).Where("id = ?", id).
				Updates(map[string]interface{}{"approved": true, "account_id": visitor.ID, "auth_time": time.Now()}).Error; err != nil {
				t.Fatal(err)
			}
			callback := getRedirect(server.URL + "/authorize/callback?id=" + url.QueryEscape(id))
			code := callback.Query().Get("code")
			if code == "" || callback.Query().Get("state") != "test-state" {
				t.Fatal("missing code or incorrect state")
			}
			token, err := config.Exchange(t.Context(), code, oauth2.VerifierOption(verifier))
			if err != nil {
				t.Fatalf("standard OAuth client exchange: %v", err)
			}
			if !token.Valid() || token.RefreshToken == "" || token.Extra("id_token") == nil {
				t.Fatal("missing access, refresh or ID token")
			}
			resp, err := config.Client(t.Context(), token).Get(discovery.Userinfo)
			if err != nil {
				t.Fatal(err)
			}
			var user struct {
				Subject string `json:"sub"`
				Email   string `json:"email"`
				Name    string `json:"name"`
			}
			decodeJSON(t, resp, &user)
			if user.Subject != visitor.ID || user.Email != visitor.Email || user.Name != visitor.Nickname {
				t.Fatal("userinfo did not preserve the visiting user's identity")
			}
			rotated, err := config.TokenSource(t.Context(), &oauth2.Token{RefreshToken: token.RefreshToken}).Token()
			if err != nil || !rotated.Valid() || rotated.RefreshToken == token.RefreshToken {
				t.Fatalf("standard OAuth client refresh: %v", err)
			}
			_, err = config.Exchange(t.Context(), code, oauth2.VerifierOption(verifier))
			var protocolError *oauth2.RetrieveError
			if !errors.As(err, &protocolError) || protocolError.ErrorCode != "invalid_grant" {
				t.Fatalf("code replay must return a parseable invalid_grant: %v", err)
			}
		})
	}
	resp, err = client.Get(discovery.Keys)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]interface{}
	decodeJSON(t, resp, &keys)
}

func TestPostgresOIDCHTTPRevocationTokenHintsAndIsolation(t *testing.T) {
	db, owner, reader := auditTestDB(t)
	if err := db.AutoMigrate(&model.OIDCSigningKey{}, &model.OIDCCryptoKey{}); err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	server := httptest.NewServer(adaptor.FiberApp(app))
	t.Cleanup(server.Close)
	solitudes.System.Config.Site.Domain = strings.TrimPrefix(server.URL, "http://")
	provider, err := newOIDCProvider()
	if err != nil {
		t.Fatal(err)
	}
	app.Use(oidcHTTPHandler(provider))
	for _, id := range []string{"revoker", "unrelated"} {
		if err := db.Create(&model.OIDCClient{ID: id, OwnerID: &owner.ID, Name: id, Public: true, HomepageURL: "http://localhost:9090/", RedirectURIsJSON: `["http://localhost:9090/callback"]`}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, grant := range []model.OIDCConsent{
		{ClientID: "revoker", AccountID: reader.ID, Scopes: "openid email offline_access"},
		{ClientID: "revoker", AccountID: owner.ID, Scopes: "openid profile"},
		{ClientID: "unrelated", AccountID: reader.ID, Scopes: "openid"},
	} {
		if err := db.Create(&grant).Error; err != nil {
			t.Fatal(err)
		}
	}
	storage := &oidcStorage{db: db}
	type credentials struct{ access, refresh string }
	issue := func(t *testing.T, clientID, accountID string) credentials {
		t.Helper()
		access, refresh, _, err := storage.CreateAccessAndRefreshTokens(t.Context(), authorizedRequestFixture(t, db, clientID, accountID), "")
		if err != nil {
			t.Fatal(err)
		}
		return credentials{access, refresh}
	}
	check := func(t *testing.T, pair credentials, accessCount, refreshCount int64) {
		t.Helper()
		var count int64
		if err := db.Model(&model.OIDCAccessToken{}).Where("id = ?", pair.access).Count(&count).Error; err != nil || count != accessCount {
			t.Fatalf("access count=%d want=%d err=%v", count, accessCount, err)
		}
		if err := db.Model(&model.OIDCRefreshToken{}).Where("token_hash = ?", secretHash(pair.refresh)).Count(&count).Error; err != nil || count != refreshCount {
			t.Fatalf("refresh count=%d want=%d err=%v", count, refreshCount, err)
		}
	}
	t.Run("refresh_lookup_failure", func(t *testing.T) {
		pair := issue(t, "revoker", reader.ID)
		raw, err := provider.Crypto().Encrypt("invalid-token-id:invalid-account-id")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&model.OIDCRefreshToken{}).Where("token_hash = ?", secretHash(pair.refresh)).Update("token_hash", secretHash(raw)).Error; err != nil {
			t.Fatal(err)
		}
		pair.refresh = raw
		if err := db.Callback().Query().Before("gorm:query").Register("revocation_refresh_lookup_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "o_id_c_refresh_tokens" {
				tx.AddError(errors.New("refresh storage unavailable"))
			}
		}); err != nil {
			t.Fatal(err)
		}
		response, err := server.Client().PostForm(server.URL+"/revoke", url.Values{"client_id": {"revoker"}, "token": {raw}, "token_type_hint": {"access_token"}})
		db.Callback().Query().Remove("revocation_refresh_lookup_failure")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusInternalServerError {
			t.Fatalf("storage failure hidden: status=%d", response.StatusCode)
		}
		var failure struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(response.Body).Decode(&failure); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 500 || failure.Error != "server_error" {
			t.Fatalf("storage failure hidden: status=%d error=%s", response.StatusCode, failure.Error)
		}
		check(t, pair, 1, 1)
		var auditCount int64
		if err := db.Model(&model.AuditEvent{}).Where("reason = 'token_revoked'").Count(&auditCount).Error; err != nil || auditCount != 0 {
			t.Fatalf("failed lookup audited as revocation: %d %v", auditCount, err)
		}
	})
	for _, kind := range []string{"refresh_token", "access_token", "refresh_decodable_as_access"} {
		for _, hint := range []string{"", "refresh_token", "access_token", "unknown"} {
			t.Run(kind+"/hint="+hint, func(t *testing.T) {
				target := issue(t, "revoker", reader.ID)
				others := []credentials{issue(t, "revoker", reader.ID), issue(t, "revoker", owner.ID), issue(t, "unrelated", reader.ID)}
				var before []model.OIDCConsent
				if err := db.Order("account_id, client_id").Find(&before).Error; err != nil {
					t.Fatal(err)
				}
				// Deterministically exercise the SDK AES decrypt path: this opaque
				// refresh value also decrypts to a non-UUID ID + subject pair.
				if kind == "refresh_decodable_as_access" {
					ambiguous, err := provider.Crypto().Encrypt("invalid-token-id:invalid-account-id")
					if err != nil {
						t.Fatal(err)
					}
					if err := db.Model(&model.OIDCRefreshToken{}).Where("token_hash = ?", secretHash(target.refresh)).Update("token_hash", secretHash(ambiguous)).Error; err != nil {
						t.Fatal(err)
					}
					target.refresh = ambiguous
				}
				raw := target.refresh
				if kind == "access_token" {
					raw, err = provider.Crypto().Encrypt(target.access + ":" + reader.ID)
					if err != nil {
						t.Fatal(err)
					}
				}
				var auditsBefore int64
				if err := db.Model(&model.AuditEvent{}).Where("reason = 'token_revoked'").Count(&auditsBefore).Error; err != nil {
					t.Fatal(err)
				}
				for step, clientID := range []string{"unrelated", "revoker", "revoker"} {
					form := url.Values{"client_id": {clientID}, "token": {raw}}
					if hint != "" {
						form.Set("token_type_hint", hint)
					}
					response, err := server.Client().PostForm(server.URL+"/revoke", form)
					if err != nil {
						t.Fatal(err)
					}
					response.Body.Close()
					if response.StatusCode != http.StatusOK {
						t.Fatalf("revocation status=%d", response.StatusCode)
					}
					accessCount, refreshCount := int64(1), int64(1)
					if step > 0 {
						accessCount = 0
						if kind != "access_token" {
							refreshCount = 0
						}
					}
					check(t, target, accessCount, refreshCount)
					for _, pair := range others {
						check(t, pair, 1, 1)
					}
					var after []model.OIDCConsent
					if err := db.Order("account_id, client_id").Find(&after).Error; err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(before, after) {
						t.Fatal("token revocation changed durable consent")
					}
					var count int64
					want := auditsBefore
					if step > 0 {
						want++
					}
					if err := db.Model(&model.AuditEvent{}).Where("reason = 'token_revoked'").Count(&count).Error; err != nil || count != want {
						t.Fatalf("revocation audit count=%d want=%d err=%v", count, want, err)
					}
				}
				if kind != "access_token" {
					response, err := server.Client().PostForm(server.URL+"/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {"revoker"}, "refresh_token": {target.refresh}})
					if err != nil {
						t.Fatal(err)
					}
					defer response.Body.Close()
					var rejected struct {
						Error   string `json:"error"`
						Access  string `json:"access_token"`
						Refresh string `json:"refresh_token"`
					}
					if err := json.NewDecoder(response.Body).Decode(&rejected); err != nil {
						t.Fatal(err)
					}
					if response.StatusCode != 400 || rejected.Error != "invalid_grant" || rejected.Access != "" || rejected.Refresh != "" {
						t.Fatalf("revoked refresh accepted: status=%d body=%+v", response.StatusCode, rejected)
					}
				}
			})
		}
	}
}

func TestPostgresOIDCHTTPRefreshNarrowsWithoutExpandingConsent(t *testing.T) {
	f := newConsentFixture(t)
	f.app.Use("/userinfo", oidcHTTPHandler(activeOIDCProvider))
	server := httptest.NewServer(adaptor.FiberApp(f.app))
	defer server.Close()
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	requestJSON := func(method, path string, form url.Values, bearer string, status int, dest interface{}) {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(form.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		// The fixture's issuer is localhost:8080; use a real HTTP transport without
		// changing that issuer or bypassing the production Fiber protocol adapter.
		req.Host = "localhost:8080"
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != status {
			t.Fatalf("%s %s: status %d, want %d", method, path, response.StatusCode, status)
		}
		if err := json.NewDecoder(response.Body).Decode(dest); err != nil {
			t.Fatal(err)
		}
	}
	f.approve(t, "openid email profile offline_access")
	callback := f.approve(t, "openid email offline_access")
	consent := f.storedConsent(t)
	location := consentLocation(t, f.request(t, http.MethodGet, callback.RequestURI(), "reader-session", nil))
	type tokenResponse struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}
	var original tokenResponse
	requestJSON(http.MethodPost, "/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {f.client.ID}, "code": {location.Query().Get("code")}, "redirect_uri": {"http://localhost:9090/callback"}, "code_verifier": {strings.Repeat("v", 64)}}, "", http.StatusOK, &original)
	if original.AccessToken == "" || original.RefreshToken == "" {
		t.Fatal("authorization code did not issue both tokens")
	}
	var storedRefresh model.OIDCRefreshToken
	if err := f.db.Take(&storedRefresh, "token_hash = ?", secretHash(original.RefreshToken)).Error; err != nil {
		t.Fatal(err)
	}
	if storedRefresh.Scopes != "openid email offline_access" {
		t.Fatalf("original refresh scope: %s", storedRefresh.Scopes)
	}
	var originalUser map[string]interface{}
	requestJSON(http.MethodGet, "/userinfo", nil, original.AccessToken, http.StatusOK, &originalUser)
	if originalUser["email"] != f.reader.Email {
		t.Fatal("initial access token did not grant email")
	}
	var rejected struct {
		Error string `json:"error"`
	}
	requestJSON(http.MethodPost, "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {f.client.ID}, "refresh_token": {original.RefreshToken}, "scope": {"openid profile"}}, "", http.StatusBadRequest, &rejected)
	if rejected.Error != "invalid_scope" {
		t.Fatalf("historical consent expanded refresh permission: %s", rejected.Error)
	}
	var unchanged model.OIDCRefreshToken
	if err := f.db.Take(&unchanged, "id = ?", storedRefresh.ID).Error; err != nil || !reflect.DeepEqual(unchanged, storedRefresh) {
		t.Fatal("rejected scope consumed or changed original refresh token", err)
	}
	if got := f.storedConsent(t); !reflect.DeepEqual(got, consent) {
		t.Fatal("rejected refresh mutated durable consent")
	}
	originalUser = nil
	requestJSON(http.MethodGet, "/userinfo", nil, original.AccessToken, http.StatusOK, &originalUser)
	if originalUser["email"] != f.reader.Email {
		t.Fatal("rejected refresh invalidated the original access token")
	}
	var narrowed tokenResponse
	requestJSON(http.MethodPost, "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {f.client.ID}, "refresh_token": {original.RefreshToken}, "scope": {"openid"}}, "", http.StatusOK, &narrowed)
	if narrowed.Scope != "openid" || narrowed.AccessToken == "" || narrowed.AccessToken == original.AccessToken || narrowed.RefreshToken == "" || narrowed.RefreshToken == original.RefreshToken {
		t.Fatalf("refresh response did not narrow and rotate: scope=%s", narrowed.Scope)
	}
	var access []model.OIDCAccessToken
	var refresh []model.OIDCRefreshToken
	if err := f.db.Find(&access).Error; err != nil || len(access) != 1 || access[0].Scopes != "openid" {
		t.Fatalf("access token was not narrowed: %+v %v", access, err)
	}
	if err := f.db.Find(&refresh).Error; err != nil || len(refresh) != 1 || refresh[0].Scopes != "openid" || refresh[0].TokenHash != secretHash(narrowed.RefreshToken) || refresh[0].AccessID != access[0].ID {
		t.Fatalf("refresh token was not narrowed/rotated: %+v %v", refresh, err)
	}
	var user map[string]interface{}
	requestJSON(http.MethodGet, "/userinfo", nil, narrowed.AccessToken, http.StatusOK, &user)
	if user["sub"] != f.reader.ID {
		t.Fatal("narrowed token changed subject")
	}
	for _, claim := range []string{"email", "email_verified", "name", "nickname"} {
		if _, ok := user[claim]; ok {
			t.Fatalf("narrowed userinfo leaked %s", claim)
		}
	}
	if got := f.storedConsent(t); !reflect.DeepEqual(got, consent) {
		t.Fatal("refresh narrowed or mutated durable consent")
	}
}
