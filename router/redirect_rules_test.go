package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func redirectTestApplication(t *testing.T) (*fiber.App, *gorm.DB) {
	t.Helper()
	t.Chdir("..")
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "redirect-management.sqlite")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, ddl := range []string{
		`CREATE TABLE articles (id TEXT PRIMARY KEY, slug TEXT UNIQUE, title TEXT, content TEXT, visibility TEXT, author_id TEXT, version INTEGER DEFAULT 1, created_at datetime, updated_at datetime)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AutoMigrate(&model.AuditEvent{}, &model.LoginSummary{}, &model.RedirectRule{}); err != nil {
		t.Fatal(err)
	}
	previous, translations, lookup, engine, provider := solitudes.System, translator.Trans, sessionLookup, globalDynamicEngine.engine, activeOIDCProvider
	t.Cleanup(func() {
		solitudes.System, translator.Trans, sessionLookup, globalDynamicEngine.engine, activeOIDCProvider = previous, translations, lookup, engine, provider
	})
	conf := &model.Config{}
	conf.Site.Domain = "example.test"
	conf.Site.Theme = "cactus"
	conf.Admin.Theme = "default"
	solitudes.System = &solitudes.SysVariable{Config: conf, DB: db}
	sessionLookup = func(token string) (*model.Account, error) {
		if token == "admin" {
			return &model.Account{ID: "10000000-0000-4000-8000-000000000001", Role: model.RoleAdmin, Nickname: "Administrator"}, nil
		}
		if token == "editor" {
			return &model.Account{ID: "10000000-0000-4000-8000-000000000002", Role: model.RoleEditor}, nil
		}
		return nil, errNoAccount
	}
	return newAppWithRoutes(func(app *fiber.App) {
		app.Get("/system-health", func(c *fiber.Ctx) error { return c.SendString("system") })
	}), db
}

func redirectRequest(t *testing.T, app *fiber.App, method, path, body, token string) (int, http.Header, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "example.test"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://example.test")
	if token != "" {
		req.AddCookie(&http.Cookie{Name: solitudes.AuthCookie, Value: token})
	}
	response, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, response.Header, string(data)
}

func TestRedirectManagementCreatesAndResolvesRule(t *testing.T) {
	app, db := redirectTestApplication(t)
	status, _, body := redirectRequest(t, app, "POST", "/admin/redirects", `{"source_path":"/old-story","target_path":"/new-story?fixed=target#section"}`, "admin")
	if status != 201 {
		t.Fatalf("create rule: status=%d body=%s", status, body)
	}
	var row struct{ ID uint64 }
	if err := json.Unmarshal([]byte(body), &row); err != nil || row.ID == 0 {
		t.Fatalf("created rule: %s %v", body, err)
	}
	for _, method := range []string{"GET", "HEAD"} {
		status, headers, _ := redirectRequest(t, app, method, "/old-story?utm_source=mail&fixed=request", "", "")
		if status != 301 || headers.Get("Location") != "/new-story?fixed=target&utm_source=mail#section" {
			t.Fatalf("%s redirect: %d %s", method, status, headers.Get("Location"))
		}
		if headers.Get("Cache-Control") != "no-store" {
			t.Fatal("editable permanent redirects must not stay in caches")
		}
	}
	status, _, _ = redirectRequest(t, app, "POST", "/old-story", `{}`, "")
	if status == 301 {
		t.Fatal("redirect changed a write request")
	}
	var count int64
	if err := db.Table("redirect_rules").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("stored rules=%d %v", count, err)
	}
}

func TestRedirectManagementRejectsUnsafeConflictingAndCyclicRules(t *testing.T) {
	app, _ := redirectTestApplication(t)
	for _, body := range []string{
		`{"source_path":"/old","target_path":"https://outside.example/"}`,
		`{"source_path":"//outside.example","target_path":"/new"}`,
		`{"source_path":"/old?query=1","target_path":"/new"}`,
		`{"source_path":"/old","target_path":"/%2foutside.example"}`,
		`{"source_path":"/old","target_path":"/a/../old"}`,
		`{"source_path":"/old","target_path":"/old/"}`,
		`{"source_path":"/admin/redirects","target_path":"/new"}`,
		`{"source_path":"/posts","target_path":"/new"}`,
		`{"source_path":"/old","target_path":"/new%0d%0aLocation:evil"}`,
	} {
		status, _, response := redirectRequest(t, app, "POST", "/admin/redirects", body, "admin")
		if status != 400 {
			t.Errorf("unsafe rule accepted: %s -> %d %s", body, status, response)
		}
	}
	for _, body := range []string{`{"source_path":"/alpha","target_path":"/beta"}`, `{"source_path":"/beta","target_path":"/gamma"}`} {
		status, _, body := redirectRequest(t, app, "POST", "/admin/redirects", body, "admin")
		if status != 201 {
			t.Fatalf("create chain: %d %s", status, body)
		}
	}
	for _, body := range []string{`{"source_path":"/gamma","target_path":"/alpha"}`, `{"source_path":"/alpha","target_path":"/delta"}`} {
		status, _, _ := redirectRequest(t, app, "POST", "/admin/redirects", body, "admin")
		if status != 409 {
			t.Fatalf("conflict/cycle not rejected: %d", status)
		}
	}
	status, header, _ := redirectRequest(t, app, "GET", "/alpha", "", "")
	if status != 301 || header.Get("Location") != "/gamma" {
		t.Fatalf("chain not flattened: %d %s", status, header.Get("Location"))
	}
}

func TestRedirectPriorityPreservesSystemNormalizationAndOverridesArticles(t *testing.T) {
	app, db := redirectTestApplication(t)
	for _, source := range []string{"/robots.txt", "/system-health", "/existing-story", "/old-story"} {
		if err := db.Exec("INSERT INTO redirect_rules(source_path,target_path) VALUES (?,?)", source, "/destination").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("INSERT INTO articles(id,slug,title,visibility) VALUES ('story','existing-story','Existing','public')").Error; err != nil {
		t.Fatal(err)
	}
	status, headers, body := redirectRequest(t, app, "GET", "/robots.txt", "", "")
	if status != 200 || !strings.Contains(body, "User-agent:") || headers.Get("Location") != "" {
		t.Fatal("redirect overrode robots.txt")
	}
	status, _, body = redirectRequest(t, app, "GET", "/system-health", "", "")
	if status != 200 || body != "system" {
		t.Fatal("redirect overrode a system route")
	}
	status, headers, _ = redirectRequest(t, app, "GET", "/old-story/", "", "")
	if status != 301 || headers.Get("Location") != "/old-story" {
		t.Fatal("normalization must precede redirect lookup")
	}
	status, headers, _ = redirectRequest(t, app, "GET", "/existing-story", "", "")
	if status != 301 || headers.Get("Location") != "/destination" {
		t.Fatal("article route ran before redirect")
	}
}

func TestRedirectManagementAuthorizationAndAtomicBulkDelete(t *testing.T) {
	app, db := redirectTestApplication(t)
	for _, token := range []string{"", "editor"} {
		for _, method := range []string{"GET", "POST", "DELETE"} {
			status, _, _ := redirectRequest(t, app, method, "/admin/redirects", `{"source_path":"/old","target_path":"/new","ids":[1]}`, token)
			if status != 302 && status != 403 {
				t.Fatalf("unauthorized %s %q: %d", method, token, status)
			}
		}
	}
	for _, source := range []string{"/one", "/two", "/three"} {
		if err := db.Exec("INSERT INTO redirect_rules(source_path,target_path) VALUES (?,?)", source, "/target").Error; err != nil {
			t.Fatal(err)
		}
	}
	status, _, _ := redirectRequest(t, app, "DELETE", "/admin/redirects", `{"ids":[1,999]}`, "admin")
	if status != 404 {
		t.Fatalf("missing bulk item: %d", status)
	}
	var count int64
	db.Table("redirect_rules").Count(&count)
	if count != 3 {
		t.Fatal("partial bulk deletion")
	}
	status, _, body := redirectRequest(t, app, "DELETE", "/admin/redirects", `{"ids":[1,2]}`, "admin")
	if status != 204 {
		t.Fatalf("bulk delete: %d %s", status, body)
	}
	db.Table("redirect_rules").Count(&count)
	if count != 1 {
		t.Fatalf("remaining rules=%d", count)
	}
	var auditCount int64
	db.Model(&model.AuditEvent{}).Where("action = ? AND outcome = ?", "redirect.delete", "success").Count(&auditCount)
	if auditCount != 2 {
		t.Fatalf("successful deletions must be audited: %d", auditCount)
	}
}
