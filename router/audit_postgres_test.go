package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/hashicorp/go-uuid"
	"github.com/patrickmn/go-cache"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func auditTestDB(t *testing.T) (*gorm.DB, model.Account, model.Account) {
	t.Helper()
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.OIDCClient{}, &model.OIDCAuthRequest{}, &model.OIDCAccessToken{}, &model.OIDCRefreshToken{}); err != nil {
		t.Fatal(err)
	}
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	cfg := &model.Config{}
	cfg.Site.Domain = "localhost:8080"
	solitudes.System = &solitudes.SysVariable{DB: db, Config: cfg}
	now := time.Now()
	admin := model.Account{Email: "admin@audit.test", Nickname: "Audit Admin", Role: model.RoleAdmin, EmailVerifiedAt: &now}
	reader := model.Account{Email: "reader@audit.test", Nickname: "Audit Reader", Role: model.RoleUser, EmailVerifiedAt: &now}
	for _, account := range []*model.Account{&admin, &reader} {
		if err := db.Create(account).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db, admin, reader
}

func TestPostgresAuditStatisticsSurviveRefreshAndRevocation(t *testing.T) {
	db, admin, reader := auditTestDB(t)
	client := model.OIDCClient{ID: "audit-client", OwnerID: &reader.ID, Name: "Reader app", Public: true, RedirectURIsJSON: `["http://localhost/callback"]`}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	storage := &oidcStorage{db: db}
	ctx := context.WithValue(t.Context(), auditContextKey{}, &auditRequest{ID: "request-1", IP: "127.0.0.1"})
	newRequest := func(accountID string) *oidcAuthRequest {
		id, err := uuid.GenerateUUID()
		if err != nil {
			t.Fatal(err)
		}
		row := model.OIDCAuthRequest{ID: id, ClientID: client.ID, AccountID: &accountID, Approved: true, RequestJSON: []byte(`{}`), ExpiresAt: time.Now().Add(time.Hour)}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		return &oidcAuthRequest{OIDCAuthRequest: row}
	}
	request := newRequest(reader.ID)
	if _, _, err := storage.CreateAccessToken(ctx, request); err != nil {
		t.Fatal(err)
	}
	if _, _, err := storage.CreateAccessToken(ctx, request); err == nil {
		t.Fatal("duplicate authorization grant counted")
	}
	var tokens int64
	db.Model(&model.OIDCAccessToken{}).Count(&tokens)
	if tokens != 1 {
		t.Fatalf("duplicate grant did not roll back token: %d", tokens)
	}
	_, refresh, _, err := storage.CreateAccessAndRefreshTokens(ctx, newRequest(reader.ID), "")
	if err != nil {
		t.Fatal(err)
	}
	r, err := storage.TokenRequestByRefreshToken(ctx, refresh)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = storage.CreateAccessAndRefreshTokens(ctx, r, refresh); err != nil {
		t.Fatal(err)
	}
	if _, _, err = storage.CreateAccessToken(ctx, newRequest(admin.ID)); err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		var view adminClientView
		if err := adminClientsQuery(db).Where("c.id = ?", client.ID).Take(&view).Error; err != nil {
			t.Fatal(err)
		}
		if view.LoginCount != 3 || view.LoginUsers != 2 || view.LastLoginAt == nil || view.OwnerName != reader.Nickname {
			t.Fatalf("incorrect metrics: %+v", view)
		}
	}
	check()
	if err := storage.TerminateSession(ctx, reader.ID, client.ID); err != nil {
		t.Fatal(err)
	}
	check()
	app := fiber.New()
	app.Use(auditMiddleware, func(c *fiber.Ctx) error { c.Locals(solitudes.CtxAccount, &admin); return c.Next() })
	app.Post("/admin/oidc/clients/:id/disable", requireAdmin, disableOIDCClient)
	resp, err := app.Test(httptest.NewRequest("POST", "/admin/oidc/clients/"+client.ID+"/disable", nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("disable: %d", resp.StatusCode)
	}
	check()
	var user adminUserView
	if err := adminUsersQuery(db).Where("a.id = ?", reader.ID).Take(&user).Error; err != nil {
		t.Fatal(err)
	}
	if user.AppCount != 1 {
		t.Fatalf("disabled apps missing from ownership count: %+v", user)
	}
	total, err := identityOverview(db)
	if err != nil {
		t.Fatal(err)
	}
	if total.Users != 2 || total.Clients != 1 || total.ActiveClients != 0 || total.Logins != 3 {
		t.Fatalf("totals: %+v", total)
	}
}

func TestPostgresAuditStatisticsSurviveCompaction(t *testing.T) {
	db, admin, reader := auditTestDB(t)
	client := model.OIDCClient{ID: "compact-client", OwnerID: &reader.ID, Name: "Client", RedirectURIsJSON: "[]"}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	old := now.AddDate(0, 0, -100)
	for i, event := range []model.AuditEvent{
		{Action: "oidc.login", Outcome: "success", ClientID: client.ID, ActorID: reader.ID, CreatedAt: old},
		{Action: "oidc.login", Outcome: "success", ClientID: client.ID, ActorID: reader.ID, CreatedAt: now},
		{Action: "oidc.login", Outcome: "success", ClientID: client.ID, ActorID: admin.ID, CreatedAt: old},
		{Action: "oidc.login", Outcome: "failure", ClientID: client.ID, ActorID: reader.ID, CreatedAt: old},
		{Action: "session.login", Outcome: "success", ActorID: reader.ID, CreatedAt: old},
		{Action: "session.login", Outcome: "success", ActorID: reader.ID, CreatedAt: now},
	} {
		event.ID = fmt.Sprintf("compact-%d", i)
		if err := db.Create(&event).Error; err != nil {
			t.Fatal(err)
		}
	}
	check := func() {
		t.Helper()
		var clientView adminClientView
		if err := adminClientsQuery(db).Where("c.id=?", client.ID).Take(&clientView).Error; err != nil {
			t.Fatal(err)
		}
		if clientView.LoginCount != 3 || clientView.LoginUsers != 2 || clientView.LastLoginAt == nil || !clientView.LastLoginAt.Equal(now) {
			t.Fatalf("client totals %+v", clientView)
		}
		var user adminUserView
		if err := adminUsersQuery(db).Where("a.id=?", reader.ID).Take(&user).Error; err != nil {
			t.Fatal(err)
		}
		if user.LoginCount != 2 || user.AppCount != 1 || user.LastLoginAt == nil || !user.LastLoginAt.Equal(now) {
			t.Fatalf("user totals %+v", user)
		}
		overview, err := identityOverview(db)
		if err != nil || overview.Logins != 3 {
			t.Fatalf("overview %+v %v", overview, err)
		}
		view := &paginationView{}
		app := fiber.New(fiber.Config{Views: view})
		app.Use(func(c *fiber.Ctx) error {
			c.Locals(solitudes.CtxAccount, &admin)
			c.Locals(solitudes.CtxTranslator, &translator.Translator{})
			return c.Next()
		})
		app.Get("/admin/oidc/clients/:id", adminClientDetail)
		resp, err := app.Test(httptest.NewRequest("GET", "/admin/oidc/clients/"+client.ID, nil))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("application users after compaction: %d %s", resp.StatusCode, body)
		}
		encoded, err := json.Marshal(view.data["login_users"])
		if err != nil {
			t.Fatal(err)
		}
		var users []struct {
			ID          string
			LoginCount  int64
			LastLoginAt time.Time
		}
		if err := json.Unmarshal(encoded, &users); err != nil {
			t.Fatal(err)
		}
		if len(users) != 2 || users[0].ID != reader.ID || users[0].LoginCount != 2 || users[1].ID != admin.ID || users[1].LoginCount != 1 {
			t.Fatalf("compaction changed distinct application users: %s", encoded)
		}
	}
	check()
	for i := 0; i < 2; i++ {
		if _, err := model.CompactAuditBatch(db, now.AddDate(0, 0, -90)); err != nil {
			t.Fatal(err)
		}
		check()
	}
	// Cache applies only to the overview; detailed/authorization queries stay live.
	solitudes.System.Cache = cache.New(time.Minute, 0)
	solitudes.System.SafeCache = new(singleflight.Group)
	first, err := cachedIdentityOverview()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Account{Email: "new@compact.test", Nickname: "New"}).Error; err != nil {
		t.Fatal(err)
	}
	stale, err := cachedIdentityOverview()
	if err != nil || stale.Users != first.Users {
		t.Fatalf("overview cache not used: %+v %v", stale, err)
	}
	solitudes.System.Cache.Flush()
	fresh, err := cachedIdentityOverview()
	if err != nil || fresh.Users != first.Users+1 {
		t.Fatalf("overview did not refresh: %+v %v", fresh, err)
	}
}

func TestPostgresAuditRoleChangeIsAtomicAndSecretFree(t *testing.T) {
	db, admin, reader := auditTestDB(t)
	app := fiber.New()
	app.Use(auditMiddleware, func(c *fiber.Ctx) error { c.Locals(solitudes.CtxAccount, &admin); return c.Next() })
	app.Post("/admin/users/:id/role", requireAdmin, setUserRole)
	post := func(role string) int {
		t.Helper()
		req := httptest.NewRequest("POST", "/admin/users/"+reader.ID+"/role?code=secret-query", strings.NewReader("role="+role+"&password=secret-body"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer secret-header")
		req.Header.Set("Cookie", "other=secret-cookie")
		req.Header.Set("User-Agent", "secret-agent")
		req.Header.Set("X-Request-ID", "secret-untrusted-id")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.Header.Get("X-Request-ID") == "secret-untrusted-id" {
			t.Fatal("trusted attacker request ID")
		}
		return resp.StatusCode
	}
	if status := post("editor"); status != 303 {
		t.Fatalf("role update: %d", status)
	}
	var events []model.AuditEvent
	if err := db.Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ActorID != admin.ID || events[0].TargetID != reader.ID || events[0].Details != "user -> editor" || events[0].RequestID == "" {
		t.Fatalf("role audit: %+v", events)
	}
	encoded, _ := json.Marshal(events)
	if strings.Contains(string(encoded), "secret-") {
		t.Fatalf("credential leak in audit: %s", encoded)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("audit_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_events" {
			tx.AddError(errors.New("simulated audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Create().Remove("audit_failure") })
	if status := post("user"); status != 500 {
		t.Fatalf("audit failure accepted mutation: %d", status)
	}
	var current model.Account
	db.Take(&current, "id = ?", reader.ID)
	if current.Role != model.RoleEditor {
		t.Fatal("role changed without an audit event")
	}
}

func TestPostgresAuditRecordsDeniedAndFailedRequests(t *testing.T) {
	db, _, reader := auditTestDB(t)
	for _, role := range []model.Role{model.RoleUser, model.RoleEditor} {
		reader.Role = role
		app := fiber.New()
		app.Use(auditMiddleware, func(c *fiber.Ctx) error { c.Locals(solitudes.CtxAccount, &reader); return c.Next() })
		for _, route := range []string{"/admin/audit", "/admin/users/:id", "/admin/oidc/clients/:id"} {
			app.Get(route, requireAdmin, func(c *fiber.Ctx) error { t.Fatal("non-admin reached management data"); return nil })
		}
		for _, route := range []string{"/admin/audit", "/admin/users/private", "/admin/oidc/clients/private"} {
			resp, err := app.Test(httptest.NewRequest("GET", route+"?token=never-record", nil))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 403 {
				t.Fatalf("%s: %d", route, resp.StatusCode)
			}
		}
	}
	var events []model.AuditEvent
	db.Find(&events)
	if len(events) != 6 {
		t.Fatalf("missing denied events: %d", len(events))
	}
	for _, e := range events {
		if e.Outcome != "denied" || e.ActorID != reader.ID || e.RequestID == "" || strings.Contains(e.Route, "never-record") {
			t.Fatalf("bad event: %+v", e)
		}
	}
	app := fiber.New()
	app.Use(auditMiddleware)
	app.Post("/login", func(c *fiber.Ctx) error {
		c.Locals("audit_reason", "invalid_credentials")
		return fiber.ErrUnauthorized
	})
	resp, err := app.Test(httptest.NewRequest("POST", "/login", strings.NewReader("password=not-saved")))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var failed model.AuditEvent
	if err := db.Where("action = ?", "session.login").Take(&failed).Error; err != nil {
		t.Fatal(err)
	}
	if failed.Outcome != "denied" || failed.Reason != "invalid_credentials" || failed.ActorID != "" {
		t.Fatalf("login failure: %+v", failed)
	}
}

func TestPostgresAuditManagementPages(t *testing.T) {
	db, admin, reader := auditTestDB(t)
	client := model.OIDCClient{ID: "management-client", OwnerID: &reader.ID, Name: "Reader application", RedirectURIsJSON: `["http://localhost/callback"]`}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 27; i++ {
		id, err := uuid.GenerateUUID()
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.OIDCClient{ID: id, OwnerID: &reader.ID, Name: fmt.Sprintf("Paged app %02d", i), RedirectURIsJSON: "[]"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir("..")
	previousEngine, previousTranslation := globalDynamicEngine.engine, translator.Trans
	t.Cleanup(func() { globalDynamicEngine.engine = previousEngine; translator.Trans = previousTranslation })
	solitudes.System.Config.Site.Theme = "cactus"
	for _, theme := range []string{"default"} {
		t.Run(theme, func(t *testing.T) {
			solitudes.System.Config.Admin.Theme = theme
			if err := LoadTemplates(); err != nil {
				t.Fatal(err)
			}
			app := fiber.New(fiber.Config{Views: globalDynamicEngine, ErrorHandler: func(c *fiber.Ctx, err error) error { t.Errorf("%s: %v", c.Path(), err); return c.SendStatus(500) }})
			app.Use(trans, func(c *fiber.Ctx) error { c.Locals(solitudes.CtxAccount, &admin); return c.Next() })
			app.Get("/admin/users", requireAdmin, usersPage)
			app.Get("/admin/users/:id", requireAdmin, adminUserDetail)
			app.Get("/admin/oidc/clients", requireAdmin, oidcClientsPage)
			app.Get("/admin/oidc/clients/:id", requireAdmin, adminClientDetail)
			app.Get("/admin/audit", requireAdmin, auditPage)
			for _, route := range []string{"/admin/users", "/admin/users?q=Reader", "/admin/users/" + reader.ID, "/admin/oidc/clients?owner_id=" + reader.ID, "/admin/oidc/clients/" + client.ID, "/admin/audit", "/admin/oidc/clients?page=2&owner_id=" + reader.ID} {
				response, err := app.Test(httptest.NewRequest("GET", route, nil))
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(route, "page=2") && strings.Count(string(body), `data-testid="admin-client-row"`) != 3 {
					t.Fatal("application pagination lost or duplicated records")
				}
				if response.StatusCode != 200 {
					t.Fatalf("%s: status %d", route, response.StatusCode)
				}
				if response.Header.Get("Cache-Control") != "private, no-store" {
					t.Fatal("management data is cacheable")
				}
			}
		})
	}
}

func TestPostgresAuditSkipsAnonymousPageRedirectsButKeepsSecurityEvents(t *testing.T) {
	db, admin, reader := auditTestDB(t)
	for _, session := range []struct {
		token   string
		account model.Account
		expires time.Time
	}{
		{"expired-session", admin, time.Now().Add(-time.Hour)},
		{"admin-session", admin, time.Now().Add(time.Hour)},
		{"reader-session", reader, time.Now().Add(time.Hour)},
	} {
		if err := db.Create(&model.LoginSession{AccountID: session.account.ID, TokenHash: secretHash(session.token), ExpiresAt: session.expires}).Error; err != nil {
			t.Fatal(err)
		}
	}
	app := fiber.New()
	app.Use(auditMiddleware, auth)
	ok := func(c *fiber.Ctx) error { return c.SendStatus(http.StatusOK) }
	adminRoutes := app.Group("/admin", loginRequired)
	adminRoutes.Get("/", ok)
	adminRoutes.Get("/audit", requireAdmin, ok)
	adminRoutes.Post("/settings", requireAdmin, ok)
	app.Get("/account", requireAccount, ok)
	app.Post("/account/password", requireAccount, ok)
	app.Get("/account/forbidden", func(c *fiber.Ctx) error { return fiber.ErrForbidden })
	app.Get("/account/unauthorized", func(c *fiber.Ctx) error { return fiber.ErrUnauthorized })
	app.Get("/account/error", func(c *fiber.Ctx) error { return fiber.ErrInternalServerError })
	app.Post("/login", func(c *fiber.Ctx) error {
		c.Locals("audit_reason", "invalid_credentials")
		return fiber.ErrUnauthorized
	})
	app.Get("/authorize", func(c *fiber.Ctx) error { return c.Redirect("/login") })

	for _, token := range []string{"", "invalid-session", "expired-session"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, path := range []string{"/admin/", "/admin/audit", "/account"} {
				req := httptest.NewRequest(method, path+"?private=never-record", nil)
				if token != "" {
					req.AddCookie(&http.Cookie{Name: solitudes.AuthCookie, Value: token})
				}
				resp, err := app.Test(req, -1)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), "/login") || resp.Header.Get("X-Request-ID") == "" {
					t.Fatalf("lost authentication redirect: %s %s: %d", method, path, resp.StatusCode)
				}
			}
		}
	}
	var count int64
	if err := db.Model(&model.AuditEvent{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("anonymous page redirects wrote audit events: %d, %v", count, err)
	}
	for _, tc := range []struct {
		method, path, token, action, outcome, reason string
		status                                       int
	}{
		{"POST", "/admin/settings", "", "settings.update", "denied", "authentication_required", 302},
		{"POST", "/account/password", "expired-session", "account.password.update", "denied", "authentication_required", 302},
		{"POST", "/login", "", "session.login", "denied", "invalid_credentials", 401},
		{"GET", "/admin/", "reader-session", "access.denied", "denied", "authentication_or_permission", 403},
		{"GET", "/admin/audit", "reader-session", "audit.view", "denied", "authentication_or_permission", 403},
		{"GET", "/account/forbidden", "", "access.denied", "denied", "authentication_or_permission", 403},
		{"GET", "/account/unauthorized", "", "access.denied", "denied", "authentication_or_permission", 401},
		{"GET", "/account/error", "", "request.error", "failure", "internal_error", 500},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.token != "" {
			req.AddCookie(&http.Cookie{Name: solitudes.AuthCookie, Value: tc.token})
		}
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		var event model.AuditEvent
		if err := db.Where("request_id = ?", resp.Header.Get("X-Request-ID")).Take(&event).Error; err != nil {
			t.Fatalf("lost security audit for %s %s: %v", tc.method, tc.path, err)
		}
		if resp.StatusCode != tc.status || event.Action != tc.action || event.Outcome != tc.outcome || event.Reason != tc.reason {
			t.Fatalf("incorrect security audit for %s %s: %+v", tc.method, tc.path, event)
		}
	}
}

func TestPostgresAuditFailureRollsBackNewSessionsAndTokens(t *testing.T) {
	db, _, reader := auditTestDB(t)
	if err := db.Callback().Create().Before("gorm:create").Register("fail_audit", func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_events" {
			tx.AddError(errors.New("audit offline"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Create().Remove("fail_audit") })
	storage := &oidcStorage{db: db}
	if err := db.Create(&model.OIDCClient{ID: "app", Name: "Test", RedirectURIsJSON: "[]"}).Error; err != nil {
		t.Fatal(err)
	}
	row := model.OIDCAuthRequest{ClientID: "app", AccountID: &reader.ID, Approved: true, RequestJSON: []byte(`{}`), ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	request := &oidcAuthRequest{OIDCAuthRequest: row}
	if _, _, err := storage.CreateAccessToken(t.Context(), request); err == nil {
		t.Fatal("token issued without audit")
	}
	if _, _, _, err := storage.CreateAccessAndRefreshTokens(t.Context(), request, ""); err == nil {
		t.Fatal("refresh issued without audit")
	}
	app := fiber.New()
	app.Post("/login", func(c *fiber.Ctx) error { return issueLoginSession(c, &reader, false) })
	resp, err := app.Test(httptest.NewRequest("POST", "/login", nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 500 || resp.Header.Get("Set-Cookie") != "" {
		t.Fatal("session issued without audit")
	}
	for _, table := range []interface{}{&model.LoginSession{}, &model.OIDCAccessToken{}, &model.OIDCRefreshToken{}} {
		var count int64
		if err := db.Model(table).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("orphaned credentials after audit failure: %T", table)
		}
	}
}

func TestPostgresOIDCFailureAuditKeepsOnlyStandardErrorCode(t *testing.T) {
	db, _, reader := auditTestDB(t)
	if err := db.AutoMigrate(&model.OIDCAuthRequest{}, &model.OIDCSigningKey{}, &model.OIDCCryptoKey{}); err != nil {
		t.Fatal(err)
	}
	client := model.OIDCClient{ID: "audit-public", OwnerID: &reader.ID, Name: "Audit public", Public: true, RedirectURIsJSON: `["http://localhost/callback"]`}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	provider, err := newOIDCProvider()
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Use(auditMiddleware)
	app.Use("/oauth", oidcHTTPHandler(provider))
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ID}, "code": {"secret-code-not-logged"}, "code_verifier": {strings.Repeat("s", 64)}, "redirect_uri": {"http://localhost/callback"}}
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Host = "localhost:8080"
	req.Header.Set("Host", "localhost:8080")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("bad code: %d", resp.StatusCode)
	}
	var event model.AuditEvent
	if err := db.Take(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Reason != "invalid_grant" || event.Outcome != "failure" || event.ClientID != client.ID || event.RequestID == "" {
		t.Fatalf("protocol failure not traceable: %+v", event)
	}
	encoded, _ := json.Marshal(event)
	if strings.Contains(string(encoded), "secret-code-not-logged") || strings.Contains(string(encoded), strings.Repeat("s", 64)) {
		t.Fatal("credential leaked into protocol audit")
	}
}
