package router

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
			for _, table := range []interface{}{&model.OIDCAuthRequest{}, &model.OIDCAccessToken{}, &model.OIDCRefreshToken{}} {
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
			if operation == "revoke" {
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

func TestPostgresOIDCAuthorizationsDerivedOnlyFromEffectiveCredentials(t *testing.T) {
	db, admin, reader := auditTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	valid := now.Add(time.Hour)
	later := now.Add(24 * time.Hour)
	create := func(row interface{}) {
		t.Helper()
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"access", "refresh", "pending", "combined", "expired", "unapproved", "consumed", "other-user", "disabled", "audit-only"} {
		client := model.OIDCClient{ID: id, Name: "Same name", OwnerID: &reader.ID, RedirectURIsJSON: "[]"}
		if id == "disabled" {
			client.DisabledAt = &now
		}
		create(&client)
	}
	for _, id := range []string{"access", "combined", "disabled"} {
		create(&model.OIDCAccessToken{ClientID: id, AccountID: reader.ID, Scopes: "openid email", ExpiresAt: valid})
	}
	for _, id := range []string{"refresh", "combined"} {
		create(&model.OIDCRefreshToken{ClientID: id, AccountID: reader.ID, AccessID: reader.ID, TokenHash: "refresh-" + id, Scopes: "openid profile", ExpiresAt: later})
	}
	create(&model.OIDCAccessToken{ClientID: "combined", AccountID: reader.ID, Scopes: "openid email", ExpiresAt: valid})
	create(&model.OIDCAccessToken{ClientID: "combined", AccountID: reader.ID, Scopes: "expired-scope", ExpiresAt: now})
	create(&model.OIDCRefreshToken{ClientID: "combined", AccountID: reader.ID, AccessID: reader.ID, TokenHash: "expired-combined", Scopes: "expired-refresh-scope", ExpiresAt: now})
	for _, id := range []string{"pending", "combined", "unapproved", "consumed", "expired"} {
		row := model.OIDCAuthRequest{ClientID: id, AccountID: &reader.ID, Approved: true, ExpiresAt: valid, RequestJSON: []byte(`{"scope":"openid offline_access"}`)}
		if id == "unapproved" {
			row.Approved = false
		}
		if id == "consumed" {
			row.CodeUsedAt = &now
		}
		if id == "expired" {
			row.ExpiresAt = now
		}
		create(&row)
	}
	create(&model.OIDCAccessToken{ClientID: "expired", AccountID: reader.ID, Scopes: "openid", ExpiresAt: now})
	create(&model.OIDCRefreshToken{ClientID: "expired", AccountID: reader.ID, AccessID: reader.ID, TokenHash: "expired", Scopes: "openid", ExpiresAt: now})
	create(&model.OIDCAccessToken{ClientID: "other-user", AccountID: admin.ID, Scopes: "openid", ExpiresAt: valid})
	create(&model.AuditEvent{ID: "audit-only", ClientID: "audit-only", ActorID: reader.ID, Action: "oidc.login", Outcome: "success"})
	var apps []oidcAuthorization
	if err := authorizedApplicationsQuery(db, reader.ID, now).Scan(&apps).Error; err != nil {
		t.Fatal(err)
	}
	if len(apps) != 4 {
		t.Fatalf("unexpected active applications: %+v", apps)
	}
	for i, id := range []string{"access", "combined", "pending", "refresh"} {
		if apps[i].ClientID != id {
			t.Fatalf("duplicate rows or unstable order: %+v", apps)
		}
		if strings.Contains(apps[i].Scopes, "expired") {
			t.Fatalf("expired scopes leaked: %+v", apps[i])
		}
	}
	for _, scope := range []string{"openid", "email", "profile", "offline_access"} {
		if !strings.Contains(apps[1].Scopes, scope) {
			t.Fatalf("missing effective scope %q", scope)
		}
	}
	if !apps[1].ExpiresAt.Equal(later) {
		t.Fatalf("wrong latest expiry: %+v", apps[1])
	}
	// Access-token expiry must not conceal a still-usable refresh token.
	apps = nil
	if err := authorizedApplicationsQuery(db, reader.ID, valid).Scan(&apps).Error; err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 || apps[0].ClientID != "combined" || apps[1].ClientID != "refresh" {
		t.Fatalf("refresh-only access lost: %+v", apps)
	}
	for _, app := range apps {
		if app.Scopes != "openid profile" {
			t.Fatalf("expired permissions retained: %+v", app)
		}
	}
	apps = nil
	if err := authorizedApplicationsQuery(db, reader.ID, later).Scan(&apps).Error; err != nil || len(apps) != 0 {
		t.Fatalf("expired entries retained: %+v %v", apps, err)
	}

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error { c.Locals(solitudes.CtxAccount, &reader); return c.Next() })
	app.Post("/account/oidc/authorizations/:id/revoke", requireAccount, revokeApplicationGrant)
	// Exercise access-only, refresh-only, pending-only and in-flight consumed codes.
	for _, id := range []string{"access", "refresh", "pending", "consumed", "expired"} {
		resp, err := app.Test(httptest.NewRequest("POST", "/account/oidc/authorizations/"+id+"/revoke", nil), -1)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 303 {
			t.Fatalf("revoke %s: %d", id, resp.StatusCode)
		}
		for _, table := range []interface{}{&model.OIDCAccessToken{}, &model.OIDCRefreshToken{}, &model.OIDCAuthRequest{}} {
			var count int64
			if err := db.Model(table).Where("client_id = ? AND account_id = ?", id, reader.ID).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("revoke left %T: %d %v", table, count, err)
			}
		}
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
