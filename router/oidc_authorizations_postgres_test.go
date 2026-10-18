package router

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func authorizedRequestFixture(t *testing.T, db *gorm.DB, clientID, accountID string) *oidcAuthRequest {
	t.Helper()
	row := model.OIDCAuthRequest{ClientID: clientID, AccountID: &accountID, Approved: true,
		RequestJSON: []byte(`{"scope":"openid email profile offline_access"}`), ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	return &oidcAuthRequest{OIDCAuthRequest: row, req: oidc.AuthRequest{Scopes: []string{"openid", "email", "offline_access"}}}
}

func TestPostgresOIDCAuthorizationRevocationAndClientDeletion(t *testing.T) {
	for _, operation := range []string{"revoke", "delete", "disable"} {
		t.Run(operation, func(t *testing.T) {
			db, admin, reader := auditTestDB(t)
			client := model.OIDCClient{ID: "owned", OwnerID: &reader.ID, Name: "Owned", RedirectURIsJSON: "[]"}
			other := model.OIDCClient{ID: "other", OwnerID: &admin.ID, Name: "Other", RedirectURIsJSON: "[]"}
			if err := db.Create(&[]model.OIDCClient{client, other}).Error; err != nil {
				t.Fatal(err)
			}
			for _, grant := range []model.OIDCConsent{
				{AccountID: reader.ID, ClientID: client.ID, Scopes: "openid email"},
				{AccountID: admin.ID, ClientID: client.ID, Scopes: "openid profile"},
				{AccountID: reader.ID, ClientID: other.ID, Scopes: "openid"},
			} {
				if err := db.Create(&grant).Error; err != nil {
					t.Fatal(err)
				}
			}
			storage := &oidcStorage{db: db}
			request := authorizedRequestFixture(t, db, client.ID, reader.ID)
			access, refresh, _, err := storage.CreateAccessAndRefreshTokens(t.Context(), request, "")
			if err != nil {
				t.Fatal(err)
			}
			cachedRefresh, err := storage.TokenRequestByRefreshToken(t.Context(), refresh)
			if err != nil {
				t.Fatal(err)
			}
			adminRequest := authorizedRequestFixture(t, db, client.ID, admin.ID)
			if _, _, err := storage.CreateAccessToken(t.Context(), adminRequest); err != nil {
				t.Fatal(err)
			}
			authorizedRequestFixture(t, db, other.ID, reader.ID)
			var actor *model.Account = &reader
			app := fiber.New()
			app.Use(func(c *fiber.Ctx) error { c.Locals(solitudes.CtxAccount, actor); return c.Next() })
			app.Post("/account/oidc/authorizations/:id/revoke", requireAccount, revokeApplicationGrant)
			app.Post("/account/oidc/clients/:id/delete", requireAccount, deleteOIDCClient)
			app.Post("/account/oidc/clients/:id/disable", requireAccount, disableOIDCClient)
			post := func(path string, status int) {
				t.Helper()
				resp, err := app.Test(httptest.NewRequest("POST", path, nil), -1)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != status {
					body, _ := io.ReadAll(resp.Body)
					t.Fatalf("%s: %d %s", path, resp.StatusCode, body)
				}
			}
			actor = nil
			post("/account/oidc/clients/owned/delete", 302)
			actor = &reader
			post("/account/oidc/clients/other/delete", 404)
			actor = &admin
			// Even an administrator revokes only their own grant here.
			post("/account/oidc/authorizations/other/revoke", 404)
			actor = &reader
			path := "/account/oidc/clients/owned/" + operation
			if operation == "delete" {
				actor = &admin // administrators can also remove another owner's app
			}
			if operation == "revoke" {
				path = "/account/oidc/authorizations/owned/revoke"
			}
			post(path, 303)
			if _, err := storage.TokenRequestByRefreshToken(t.Context(), refresh); err == nil {
				t.Fatal("revoked refresh still valid")
			}
			if _, _, _, err := storage.CreateAccessAndRefreshTokens(t.Context(), cachedRefresh, refresh); err == nil {
				t.Fatal("cached refresh resurrected grant")
			}
			if _, _, err := storage.CreateAccessToken(t.Context(), request); err == nil {
				t.Fatal("cached code resurrected grant")
			}
			var count int64
			if err := db.Model(&model.OIDCAccessToken{}).Where("id = ?", access).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("access survived: %d %v", count, err)
			}
			for _, table := range []interface{}{&model.OIDCConsent{}, &model.OIDCAuthRequest{}, &model.OIDCAccessToken{}, &model.OIDCRefreshToken{}} {
				query := db.Model(table).Where("client_id = ?", client.ID)
				if operation == "revoke" {
					query = query.Where("account_id = ?", reader.ID)
				}
				if err := query.Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("orphan %T: %d %v", table, count, err)
				}
			}
			if err := db.Model(&model.OIDCAuthRequest{}).Where("client_id = ?", other.ID).Count(&count).Error; err != nil || count != 1 {
				t.Fatal("unrelated app changed", err)
			}
			if err := db.Model(&model.OIDCConsent{}).Where("client_id = ?", other.ID).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("unrelated consent changed: %d %v", count, err)
			}
			if operation == "revoke" {
				if err := db.Model(&model.OIDCConsent{}).Where("client_id = ? AND account_id = ?", client.ID, admin.ID).Count(&count).Error; err != nil || count != 1 {
					t.Fatalf("other user's consent changed: %d %v", count, err)
				}
				if err := db.Model(&model.OIDCAuthRequest{}).Where("client_id = ? AND account_id = ?", client.ID, admin.ID).Count(&count).Error; err != nil || count != 1 {
					t.Fatal("other user's grant changed", err)
				}
				post(path, 404)
				authorizedRequestFixture(t, db, client.ID, reader.ID) // fresh explicit consent restores access
			}
			if err := db.Model(&model.AuditEvent{}).Where("action = 'oidc.login' AND client_id = ?", client.ID).Count(&count).Error; err != nil || count != 2 {
				t.Fatalf("audit erased: %d %v", count, err)
			}
			if operation == "disable" {
				post("/account/oidc/clients/owned/delete", 303)
			}
			actor = &admin
			post("/account/oidc/clients/other/delete", 303)
		})
	}
}

func TestPostgresOIDCAuthorizationScopesPaginationAndRollback(t *testing.T) {
	db, admin, reader := auditTestDB(t)
	for i := 0; i < 23; i++ {
		id := fmt.Sprintf("client-%02d", i)
		if err := db.Create(&model.OIDCClient{ID: id, OwnerID: &reader.ID, Name: id, HomepageURL: "https://example.test/", RedirectURIsJSON: "[]"}).Error; err != nil {
			t.Fatal(err)
		}
		authorizedRequestFixture(t, db, id, reader.ID)
		if err := db.Create(&model.OIDCConsent{ClientID: id, AccountID: reader.ID, Scopes: "openid email profile offline_access"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	authorizedRequestFixture(t, db, "client-00", admin.ID)
	t.Chdir("..")
	previousEngine, translations := globalDynamicEngine.engine, translator.Trans
	t.Cleanup(func() { globalDynamicEngine.engine, translator.Trans = previousEngine, translations })
	for _, theme := range []string{"cactus", "folio"} {
		solitudes.System.Config.Site.Theme, solitudes.System.Config.Admin.Theme = theme, "default"
		if err := LoadTemplates(); err != nil {
			t.Fatal(err)
		}
		app := fiber.New(fiber.Config{Views: globalDynamicEngine})
		app.Use(trans, func(c *fiber.Ctx) error { c.Locals(solitudes.CtxAccount, &reader); return c.Next() })
		app.Get("/account/oidc/authorizations", requireAccount, authorizedApplicationsPage)
		app.Post("/account/oidc/clients/:id/delete", requireAccount, deleteOIDCClient)
		for page, expected := range []int{20, 3, 0} {
			resp, err := app.Test(httptest.NewRequest("GET", fmt.Sprintf("/account/oidc/authorizations?page=%d", page+1), nil), -1)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 200 || strings.Count(string(body), `data-testid="authorization-row"`) != expected || resp.Header.Get("Cache-Control") != "private, no-store" {
				t.Fatalf("%s page %d: %d %s", theme, page+1, resp.StatusCode, body)
			}
			if expected > 0 && (!strings.Contains(string(body), "Authorized until you revoke access.") || strings.Contains(string(body), "Latest credential expiry") || strings.Contains(string(body), "<time")) {
				t.Fatalf("%s still describes expiring credentials: %s", theme, body)
			}
			if page == 1 && !strings.Contains(string(body), `data-client-id="client-20"`) {
				t.Fatal("unstable pagination")
			}
		}
		if err := db.Callback().Create().Before("gorm:create").Register("grant_audit_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "audit_events" {
				tx.AddError(errors.New("audit offline"))
			}
		}); err != nil {
			t.Fatal(err)
		}
		resp, err := app.Test(httptest.NewRequest("POST", "/account/oidc/clients/client-00/delete", nil), -1)
		db.Callback().Create().Remove("grant_audit_failure")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 500 {
			t.Fatalf("audit failure status %d", resp.StatusCode)
		}
		var count int64
		if err := db.Model(&model.OIDCAuthRequest{}).Where("client_id = ?", "client-00").Count(&count).Error; err != nil || count != 2 {
			t.Fatalf("failed deletion not rolled back: %d %v", count, err)
		}
	}
}

func TestPostgresOIDCAuthorizationsDerivedOnlyFromDurableConsent(t *testing.T) {
	db, admin, reader := auditTestDB(t)
	now := time.Now()
	create := func(row interface{}) {
		t.Helper()
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"consent-only", "combined", "expired", "tokens-only", "request-only", "other-user", "disabled", "audit-only"} {
		client := model.OIDCClient{ID: id, Name: "Same name", Description: "Application description", HomepageURL: "https://example.test/", OwnerID: &admin.ID, RedirectURIsJSON: "[]"}
		if id == "disabled" {
			client.DisabledAt = &now
		}
		create(&client)
	}
	for _, id := range []string{"consent-only", "combined", "expired", "disabled"} {
		create(&model.OIDCConsent{ClientID: id, AccountID: reader.ID, Scopes: "email offline_access openid profile"})
	}
	create(&model.OIDCConsent{ClientID: "other-user", AccountID: admin.ID, Scopes: "openid"})
	create(&model.OIDCConsent{ClientID: "combined", AccountID: admin.ID, Scopes: "admin-only-scope"})
	for _, id := range []string{"combined", "expired", "tokens-only"} {
		expiry := now.Add(time.Hour)
		if id == "expired" {
			expiry = now.Add(-time.Hour)
		}
		access := model.OIDCAccessToken{ClientID: id, AccountID: reader.ID, Scopes: "openid token-only-scope", ExpiresAt: expiry}
		create(&access)
		create(&model.OIDCRefreshToken{ClientID: id, AccountID: reader.ID, AccessID: access.ID, TokenHash: "refresh-" + id, Scopes: "openid", ExpiresAt: expiry})
	}
	authorizedRequestFixture(t, db, "request-only", reader.ID)
	create(&model.AuditEvent{ID: "audit-only", ClientID: "audit-only", ActorID: reader.ID, Action: "oidc.login", Outcome: "success"})
	assertListing := func() {
		t.Helper()
		var apps []oidcAuthorization
		if err := authorizedApplicationsQuery(db, reader.ID).Scan(&apps).Error; err != nil {
			t.Fatal(err)
		}
		if len(apps) != 3 {
			t.Fatalf("expected three durable authorizations, got %+v", apps)
		}
		for i, id := range []string{"combined", "consent-only", "expired"} {
			app := apps[i]
			if app.ClientID != id || app.Scopes != "email offline_access openid profile" || app.OwnerID != admin.ID || app.OwnerName != admin.Nickname || app.Name != "Same name" || app.Description != "Application description" || app.HomepageURL != "https://example.test/" {
				t.Fatalf("wrong grant, metadata, scopes, or order: %+v", app)
			}
		}
		var others []oidcAuthorization
		if err := authorizedApplicationsQuery(db, admin.ID).Scan(&others).Error; err != nil {
			t.Fatal(err)
		}
		if len(others) != 2 || others[0].ClientID != "combined" || others[0].Scopes != "admin-only-scope" || others[1].ClientID != "other-user" || others[1].Scopes != "openid" {
			t.Fatalf("account isolation lost: %+v", others)
		}
	}
	assertListing()
	// Removing all credentials must neither hide grants nor shrink remembered scopes.
	for _, table := range []interface{}{&model.OIDCAccessToken{}, &model.OIDCRefreshToken{}, &model.OIDCAuthRequest{}} {
		if err := db.Where("account_id = ?", reader.ID).Delete(table).Error; err != nil {
			t.Fatal(err)
		}
	}
	assertListing()
}

func TestPostgresOIDCDurableAuthorizationAuditRollback(t *testing.T) {
	for _, operation := range []string{"revoke", "delete"} {
		for _, withCredentials := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/credentials=%t", operation, withCredentials), func(t *testing.T) {
				db, admin, reader := auditTestDB(t)
				client := model.OIDCClient{ID: "durable-grant", OwnerID: &reader.ID, Name: "Durable", RedirectURIsJSON: "[]"}
				if err := db.Create(&client).Error; err != nil {
					t.Fatal(err)
				}
				grants := []model.OIDCConsent{{ClientID: client.ID, AccountID: reader.ID, Scopes: "openid email"}, {ClientID: client.ID, AccountID: admin.ID, Scopes: "openid profile"}}
				if err := db.Create(&grants).Error; err != nil {
					t.Fatal(err)
				}
				if withCredentials {
					request := authorizedRequestFixture(t, db, client.ID, reader.ID)
					if _, _, _, err := (&oidcStorage{db: db}).CreateAccessAndRefreshTokens(t.Context(), request, ""); err != nil {
						t.Fatal(err)
					}
				}
				var before []model.OIDCConsent
				if err := db.Order("account_id").Find(&before).Error; err != nil {
					t.Fatal(err)
				}
				app := fiber.New()
				app.Use(func(c *fiber.Ctx) error { c.Locals(solitudes.CtxAccount, &reader); return c.Next() })
				app.Post("/revoke/:id", revokeApplicationGrant)
				app.Post("/delete/:id", deleteOIDCClient)
				if err := db.Callback().Create().Before("gorm:create").Register("durable_grant_audit_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "audit_events" {
						tx.AddError(errors.New("audit offline"))
					}
				}); err != nil {
					t.Fatal(err)
				}
				response, err := app.Test(httptest.NewRequest("POST", "/"+operation+"/"+client.ID, nil), -1)
				db.Callback().Create().Remove("durable_grant_audit_failure")
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if response.StatusCode != 500 {
					t.Fatalf("audit failure status: %d", response.StatusCode)
				}
				var after []model.OIDCConsent
				if err := db.Order("account_id").Find(&after).Error; err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, after) {
					t.Fatalf("consent not rolled back: before=%+v after=%+v", before, after)
				}
				var count int64
				if err := db.Model(&model.OIDCClient{}).Where("id = ?", client.ID).Count(&count).Error; err != nil || count != 1 {
					t.Fatalf("client not restored: %d %v", count, err)
				}
				for _, table := range []interface{}{&model.OIDCAuthRequest{}, &model.OIDCAccessToken{}, &model.OIDCRefreshToken{}} {
					expected := int64(0)
					if withCredentials {
						expected = 1
					}
					if err := db.Model(table).Where("client_id = ?", client.ID).Count(&count).Error; err != nil || count != expected {
						t.Fatalf("%T not restored: %d %v", table, count, err)
					}
				}
				if err := db.Model(&model.AuditEvent{}).Where("action IN ?", []string{"grant.revoke", "client.delete"}).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("failed mutation audited: %d %v", count, err)
				}
			})
		}
	}
}

func TestPostgresOIDCLegacyCredentialsRemainRevocableWithoutConsent(t *testing.T) {
	for _, kind := range []string{"access", "refresh", "pending", "consumed", "expired"} {
		t.Run(kind, func(t *testing.T) {
			db, _, reader := auditTestDB(t)
			client := model.OIDCClient{ID: "legacy-credentials", Name: "Legacy", RedirectURIsJSON: "[]"}
			if err := db.Create(&client).Error; err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			var row interface{}
			switch kind {
			case "access":
				row = &model.OIDCAccessToken{ClientID: client.ID, AccountID: reader.ID, Scopes: "openid", ExpiresAt: now.Add(time.Hour)}
			case "refresh":
				row = &model.OIDCRefreshToken{ClientID: client.ID, AccountID: reader.ID, AccessID: reader.ID, TokenHash: "legacy-refresh", Scopes: "openid", ExpiresAt: now.Add(time.Hour)}
			default:
				request := &model.OIDCAuthRequest{ClientID: client.ID, AccountID: &reader.ID, Approved: true, RequestJSON: []byte(`{"scope":"openid"}`), ExpiresAt: now.Add(time.Hour)}
				if kind == "consumed" {
					request.CodeUsedAt = &now
				}
				if kind == "expired" {
					request.ExpiresAt = now.Add(-time.Hour)
				}
				row = request
			}
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
			app := fiber.New()
			app.Use(func(c *fiber.Ctx) error { c.Locals(solitudes.CtxAccount, &reader); return c.Next() })
			app.Post("/:id/revoke", requireAccount, revokeApplicationGrant)
			response, err := app.Test(httptest.NewRequest("POST", "/"+client.ID+"/revoke", nil), -1)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != 303 {
				t.Fatalf("legacy %s revoke: %d", kind, response.StatusCode)
			}
			for _, table := range []interface{}{&model.OIDCConsent{}, &model.OIDCAuthRequest{}, &model.OIDCAccessToken{}, &model.OIDCRefreshToken{}} {
				var count int64
				if err := db.Model(table).Where("client_id = ? AND account_id = ?", client.ID, reader.ID).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("legacy revoke left %T: %d %v", table, count, err)
				}
			}
		})
	}
}

func TestPostgresOIDCRevocationSerializesWithTokenIssuance(t *testing.T) {
	for _, operation := range []string{"revoke", "delete", "disable"} {
		t.Run(operation, func(t *testing.T) {
			db, _, reader := auditTestDB(t)
			if err := db.Create(&model.OIDCClient{ID: "racing", OwnerID: &reader.ID, Name: "Racing", RedirectURIsJSON: "[]"}).Error; err != nil {
				t.Fatal(err)
			}
			request := authorizedRequestFixture(t, db, "racing", reader.ID)
			storage := &oidcStorage{db: db}
			_, refresh, _, err := storage.CreateAccessAndRefreshTokens(t.Context(), request, "")
			if err != nil {
				t.Fatal(err)
			}
			cached, err := storage.TokenRequestByRefreshToken(t.Context(), refresh)
			if err != nil {
				t.Fatal(err)
			}
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			client, err := lockOIDCClient(tx, "racing", false)
			if err != nil {
				t.Fatal(err)
			}
			if err := deleteOIDCCredentials(tx, "racing", reader.ID); err != nil {
				t.Fatal(err)
			}
			if operation == "delete" {
				if err := tx.Delete(client).Error; err != nil {
					t.Fatal(err)
				}
			}
			if operation == "disable" {
				if err := tx.Model(client).Update("disabled_at", time.Now()).Error; err != nil {
					t.Fatal(err)
				}
			}
			results := make(chan error, 2)
			go func() { _, _, err := storage.CreateAccessToken(t.Context(), request); results <- err }()
			go func() {
				_, _, _, err := storage.CreateAccessAndRefreshTokens(t.Context(), cached, refresh)
				results <- err
			}()
			// Both issuers have cached requests but must wait for the lifecycle lock.
			select {
			case err := <-results:
				t.Fatalf("issuance bypassed lifecycle lock: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			if err := tx.Commit().Error; err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				select {
				case err := <-results:
					if err == nil {
						t.Fatal("revoked credentials resurrected")
					}
				case <-time.After(5 * time.Second):
					t.Fatal("issuance deadlocked")
				}
			}
			var count int64
			if err := db.Model(&model.OIDCAccessToken{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("revoked access remains: %d %v", count, err)
			}
		})
	}
}

func TestPostgresOIDCAuthorizationManagementRequiresActiveSessionAndSameOrigin(t *testing.T) {
	db, _, reader := auditTestDB(t)
	if err := db.Create(&model.LoginSession{AccountID: reader.ID, TokenHash: secretHash("grant-session"), ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Use(auth, csrfGuard)
	app.Get("/account/oidc/authorizations", requireAccount, authorizedApplicationsPage)
	app.Post("/account/oidc/authorizations/:id/revoke", requireAccount, revokeApplicationGrant)
	app.Post("/account/oidc/clients/:id/delete", requireAccount, deleteOIDCClient)
	app.Post("/admin/oidc/clients/:id/delete", requireAdmin, deleteOIDCClient)
	for _, tc := range []struct {
		name, token, origin  string
		disabled, unverified bool
		status               int
	}{
		{"guest", "", "http://localhost:8080", false, false, 302},
		{"invalid", "invalid", "http://localhost:8080", false, false, 302},
		{"disabled", "grant-session", "http://localhost:8080", true, false, 302},
		{"unverified", "grant-session", "http://localhost:8080", false, true, 302},
		{"csrf", "grant-session", "https://attacker.test", false, false, 403},
		{"missing-origin", "grant-session", "", false, false, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			var disabled, verified *time.Time
			if tc.disabled {
				disabled = &now
			}
			if !tc.unverified {
				verified = &now
			}
			if err := db.Model(&reader).Updates(map[string]interface{}{"disabled_at": disabled, "email_verified_at": verified}).Error; err != nil {
				t.Fatal(err)
			}
			for _, route := range []string{"/account/oidc/authorizations/missing/revoke", "/account/oidc/clients/missing/delete"} {
				req := httptest.NewRequest("POST", route, nil)
				req.Host = "localhost:8080"
				req.Header.Set("Host", "localhost:8080")
				req.Header.Set("Origin", tc.origin)
				if tc.token != "" {
					req.AddCookie(&http.Cookie{Name: solitudes.AuthCookie, Value: tc.token})
				}
				resp, err := app.Test(req, -1)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != tc.status {
					t.Fatalf("%s: %d", route, resp.StatusCode)
				}
			}
		})
	}
	// A verified member still cannot access the administrative delete endpoint.
	req := httptest.NewRequest("POST", "/admin/oidc/clients/missing/delete", nil)
	req.Host = "localhost:8080"
	req.Header.Set("Host", "localhost:8080")
	req.Header.Set("Origin", "http://localhost:8080")
	req.AddCookie(&http.Cookie{Name: solitudes.AuthCookie, Value: "grant-session"})
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("member reached admin delete: %d", resp.StatusCode)
	}
}
