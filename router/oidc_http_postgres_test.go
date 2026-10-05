package router

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"

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
