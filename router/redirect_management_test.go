package router

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func TestRedirectManagementRejectsOversizedEncodedPath(t *testing.T) {
	app, _ := redirectTestApplication(t)
	// A short Unicode input can exceed the database's URL limit after escaping.
	body := fmt.Sprintf(`{"source_path":%q,"target_path":"/new"}`, "/"+strings.Repeat("中", 230))
	status, _, _ := redirectRequest(t, app, "POST", "/admin/redirects", body, "admin")
	if status != 400 {
		t.Fatalf("oversized encoded source accepted: %d", status)
	}
}

func TestRedirectManagementNormalizesUnicodeAndRejectsEquivalentSources(t *testing.T) {
	app, _ := redirectTestApplication(t)
	status, _, body := redirectRequest(t, app, "POST", "/admin/redirects", `{"source_path":"/旧文章/","target_path":"/posts"}`, "admin")
	if status != 201 {
		t.Fatalf("Unicode rule: %d %s", status, body)
	}
	status, header, _ := redirectRequest(t, app, "GET", "/%E6%97%A7%E6%96%87%E7%AB%A0", "", "")
	if status != 301 || header.Get("Location") != "/posts/" {
		t.Fatalf("normalized redirect: %d %s", status, header.Get("Location"))
	}
	status, _, _ = redirectRequest(t, app, "POST", "/admin/redirects", `{"source_path":"/%E6%97%A7%E6%96%87%E7%AB%A0","target_path":"/books/"}`, "admin")
	if status != 409 {
		t.Fatalf("equivalent source accepted: %d", status)
	}
}

func TestRedirectManagementCSRFAndAuditFailureDoNotMutateRules(t *testing.T) {
	app, db := redirectTestApplication(t)
	for _, origin := range []string{"", "http://outside.example"} {
		req := httptest.NewRequest("POST", "/admin/redirects", strings.NewReader(`{"source_path":"/old","target_path":"/new"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", solitudes.AuthCookie+"=admin")
		req.Header.Set("Origin", origin)
		response, err := testRequest(app, req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 403 {
			t.Fatalf("cross-origin rule mutation: %d", response.StatusCode)
		}
	}
	if err := db.Migrator().DropTable(&model.AuditEvent{}); err != nil {
		t.Fatal(err)
	}
	status, _, _ := redirectRequest(t, app, "POST", "/admin/redirects", `{"source_path":"/old","target_path":"/new"}`, "admin")
	if status != 500 {
		t.Fatalf("audit failure ignored: %d", status)
	}
	var count int64
	if err := db.Model(&model.RedirectRule{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed mutation committed: %d %v", count, err)
	}
}

func TestRedirectManagementListsCurrentArticleTargetsAndPaginates(t *testing.T) {
	app, db := redirectTestApplication(t)
	id := "article-for-redirect"
	if err := db.Exec("INSERT INTO articles(id,slug,title,visibility) VALUES (?,?,?,?)", id, "latest-name", "Associated article", model.VisibilityPublic).Error; err != nil {
		t.Fatal(err)
	}
	automatic := model.RedirectRule{SourcePath: "/old-article", ArticleID: &id}
	if err := db.Create(&automatic).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 26; i++ {
		row := model.RedirectRule{SourcePath: fmt.Sprintf("/archive-%02d", i), TargetPath: "/posts/"}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		path  string
		count int
	}{
		{"/admin/redirects?q=Associated", 1},
		{"/admin/redirects?q=archive", 25},
		{"/admin/redirects?q=archive&page=2", 1},
	} {
		status, _, body := redirectRequest(t, app, "GET", tc.path, "", "admin")
		if status != 200 {
			t.Fatalf("list %s: %d %s", tc.path, status, body)
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if count := doc.Find(`[data-testid="redirect-rule-row"]`).Length(); count != tc.count {
			t.Fatalf("list %s rows %d", tc.path, count)
		}
		if tc.path == "/admin/redirects?q=Associated" {
			if doc.Find(`[data-testid="redirect-target"]`).Text() != "/latest-name" || doc.Find(`[data-testid="redirect-article"]`).Text() != "Associated article" {
				t.Fatal("automatic rule does not show its current article")
			}
		}
	}
	status, _, _ := redirectRequest(t, app, "GET", "/admin/redirects?page=3", "", "admin")
	if status != 404 {
		t.Fatalf("out-of-range listing: %d", status)
	}
}

func TestRedirectManagementEditPreservesRuleAndRejectsCycles(t *testing.T) {
	app, db := redirectTestApplication(t)
	rule := model.RedirectRule{SourcePath: "/old", TargetPath: "/destination"}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"id":%d,"source_path":"/renamed-source","target_path":"/posts/"}`, rule.ID)
	status, _, response := redirectRequest(t, app, "POST", "/admin/redirects", body, "admin")
	if status != 200 {
		t.Fatalf("edit: %d %s", status, response)
	}
	if err := db.First(&rule, rule.ID).Error; err != nil || rule.SourcePath != "/renamed-source" || rule.TargetPath != "/posts/" {
		t.Fatalf("edit not persisted: %+v %v", rule, err)
	}
	other := model.RedirectRule{SourcePath: "/other", TargetPath: "/renamed-source"}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	body = fmt.Sprintf(`{"id":%d,"source_path":"/renamed-source","target_path":"/other"}`, rule.ID)
	status, _, _ = redirectRequest(t, app, "POST", "/admin/redirects", body, "admin")
	if status != 409 {
		t.Fatalf("cyclic edit accepted: %d", status)
	}
	if err := db.First(&rule, rule.ID).Error; err != nil || rule.TargetPath != "/posts/" {
		t.Fatal("cyclic edit changed the original destination")
	}
}
