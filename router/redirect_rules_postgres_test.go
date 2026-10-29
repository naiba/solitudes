package router

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func articleRedirectFixture(t *testing.T) (*gorm.DB, *testHTTPRouter, *model.Article) {
	t.Helper()
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.ArticleHistory{}, &model.Comment{}, &model.RedirectRule{}); err != nil {
		t.Fatal(err)
	}
	index, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	previous, translations, engine := solitudes.System, translator.Trans, globalDynamicEngine.engine
	t.Cleanup(func() {
		solitudes.System, translator.Trans, globalDynamicEngine.engine = previous, translations, engine
	})
	t.Chdir("..")
	conf := &model.Config{}
	conf.Site.Domain = "example.test"
	conf.Site.Theme = "cactus"
	conf.Admin.Theme = "default"
	solitudes.System = &solitudes.SysVariable{DB: db, Config: conf, Search: index}
	if err := LoadTemplates(); err != nil {
		t.Fatal(err)
	}
	owner := model.Account{Email: "redirect-owner@example.test", Role: model.RoleAdmin, Nickname: "Owner"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	article := &model.Article{AuthorID: &owner.ID, UpdatedByID: &owner.ID, Title: "Redirect story", Slug: "original-story", Content: "Public body", TemplateID: solitudes.ArticleTemplateID, Version: 1}
	if err := db.Create(article).Error; err != nil {
		t.Fatal(err)
	}
	app := newTestRouter(testHTTPOptions{Views: globalDynamicEngine})
	app.Use(trans, func(writer http.ResponseWriter, httpRequest *http.Request) error {
		if requestHeader(httpRequest, "X-Test-Admin") == "yes" {
			requestValue(httpRequest, solitudes.CtxAccount, &owner)
		}
		return nil
	})
	app.Post("/admin/publish", publishHandler)
	app.Delete("/admin/articles", deleteArticle)
	app.Use(canonicalURLRedirect, registeredRedirect)
	app.Get("/:slug", func(writer http.ResponseWriter, httpRequest *http.Request) error {
		return writeText(writer, http.StatusOK, routeParam(httpRequest, "slug"))
	})
	return db, app, article
}

func publishRedirectArticle(t *testing.T, app *testHTTPRouter, article *model.Article, slug string) int {
	t.Helper()
	form := url.Values{"id": {article.ID}, "title": {article.Title}, "slug": {slug}, "content": {article.Content}, "template": {fmt.Sprint(article.TemplateID)}, "visibility": {string(article.Visibility)}, "new_version": {fmt.Sprint(article.NewVersion)}}
	req := httptest.NewRequest("POST", "/admin/publish", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Test-Admin", "yes")
	response, err := testRequest(app, req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		body, _ := io.ReadAll(response.Body)
		t.Logf("publish response: %d %s", response.StatusCode, body)
	}
	return response.StatusCode
}

func TestPostgresArticleRedirectLifecycle(t *testing.T) {
	db, app, article := articleRedirectFixture(t)
	for _, slug := range []string{"second-story", "current-story"} {
		if status := publishRedirectArticle(t, app, article, slug); status != 200 {
			t.Fatalf("rename status %d", status)
		}
	}
	for _, old := range []string{"original-story", "second-story"} {
		response, err := testRequest(app, httptest.NewRequest("GET", "/"+old+"?utm_source=archive", nil))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 301 || response.Header.Get("Location") != "/current-story?utm_source=archive" {
			t.Fatalf("old URL %s: %d %s", old, response.StatusCode, response.Header.Get("Location"))
		}
	}
	if status := publishRedirectArticle(t, app, article, "original-story"); status != 200 {
		t.Fatalf("rename back status=%d", status)
	}
	var count int64
	if err := db.Model(&model.RedirectRule{}).Where("source_path = ?", "/original-story").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("self alias retained: %d %v", count, err)
	}
	if err := db.Model(&model.Article{}).Where("id = ?", article.ID).Update("visibility", model.VisibilityPrivate).Error; err != nil {
		t.Fatal(err)
	}
	for _, admin := range []bool{false, true} {
		req := httptest.NewRequest("GET", "/second-story", nil)
		if admin {
			req.Header.Set("X-Test-Admin", "yes")
		}
		response, err := testRequest(app, req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if admin {
			if response.StatusCode != 301 || response.Header.Get("Location") != "/original-story" {
				t.Fatal("authorized automatic redirect failed")
			}
		} else if response.StatusCode != 404 || response.Header.Get("Location") != "" {
			t.Fatal("private article destination leaked")
		}
	}
	req := httptest.NewRequest("DELETE", "/admin/articles?id="+article.ID, nil)
	req.Header.Set("X-Test-Admin", "yes")
	response, err := testRequest(app, req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("delete article %d", response.StatusCode)
	}
	if err := db.Model(&model.RedirectRule{}).Where("article_id = ?", article.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("deleted article retained redirects: %d %v", count, err)
	}
}

func TestPostgresArticleVersionChangeCannotActivateRedirectLoop(t *testing.T) {
	db, app, article := articleRedirectFixture(t)
	if err := db.Create(&model.RedirectRule{SourcePath: "/original-story", TargetPath: "/original-story/v2"}).Error; err != nil {
		t.Fatal(err)
	}
	article.NewVersion = 1
	if status := publishRedirectArticle(t, app, article, article.Slug); status != 409 {
		t.Fatalf("version change activated a redirect loop: %d", status)
	}
	var stored model.Article
	if err := db.Take(&stored, "id = ?", article.ID).Error; err != nil || stored.Version != 1 {
		t.Fatalf("rejected publication changed current revision: %d %v", stored.Version, err)
	}
	var histories int64
	if err := db.Model(&model.ArticleHistory{}).Where("article_id = ?", article.ID).Count(&histories).Error; err != nil || histories != 0 {
		t.Fatalf("rejected publication wrote history: %d %v", histories, err)
	}
}

func TestPostgresArticleRedirectConflictRollsBackPublication(t *testing.T) {
	db, app, article := articleRedirectFixture(t)
	manual := model.RedirectRule{SourcePath: "/occupied-source", TargetPath: "/destination"}
	if err := db.Create(&manual).Error; err != nil {
		t.Fatal(err)
	}
	if status := publishRedirectArticle(t, app, article, "occupied-source"); status != 409 {
		t.Fatalf("conflicting publication status=%d", status)
	}
	var stored model.Article
	if err := db.Take(&stored, "id = ?", article.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Slug != "original-story" {
		t.Fatal("failed redirect registration committed the article rename")
	}
	var count int64
	if err := db.Model(&model.RedirectRule{}).Where("article_id = ?", article.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("partial registration: %d %v", count, err)
	}
	if err := db.First(&manual, manual.ID).Error; err != nil || manual.TargetPath != "/destination" {
		t.Fatal("manual rule overwritten")
	}
}
