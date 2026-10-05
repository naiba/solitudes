package router

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/blevesearch/bleve/v2"
	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func TestPostgresPublishSnapshotsTitleWithContent(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.ArticleHistory{}); err != nil {
		t.Fatal(err)
	}
	index, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	solitudes.System = &solitudes.SysVariable{DB: db, Config: &model.Config{}, Search: index}
	owner := model.Account{Email: "revision-editor@example.test", Role: model.RoleEditor}
	admin := model.Account{Email: "revision-admin@example.test", Role: model.RoleAdmin}
	reader := model.Account{Email: "revision-reader@example.test", Role: model.RoleUser}
	other := model.Account{Email: "other-editor@example.test", Role: model.RoleEditor}
	for _, account := range []*model.Account{&owner, &admin, &reader, &other} {
		if err := db.Create(account).Error; err != nil {
			t.Fatal(err)
		}
	}
	a := model.Article{AuthorID: &owner.ID, UpdatedByID: &owner.ID, Title: `初稿 <标题> & "引号"`, Slug: "versioned-title", Content: "Original body", Version: 1, TemplateID: solitudes.ArticleTemplateID}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	viewer := &owner
	app := fiber.New()
	app.Post("/admin/publish", func(c *fiber.Ctx) error {
		c.Locals(solitudes.CtxAccount, viewer)
		return publishHandler(c)
	})
	save := func(title, body, slug, newVersion string) int {
		t.Helper()
		form := url.Values{"id": {a.ID}, "title": {title}, "content": {body}, "slug": {slug}, "template": {"1"}, "new_version": {newVersion}}
		form.Set("author_id", reader.ID)
		form.Set("updated_by_id", reader.ID)
		req := httptest.NewRequest(http.MethodPost, "/admin/publish", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", fiber.MIMEApplicationForm)
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if status := save("Second title", a.Content, a.Slug, "1"); status != 200 {
		t.Fatalf("title-only revision: %d", status)
	}
	viewer = &admin
	if status := save("Third title", "Third body", a.Slug, "1"); status != 200 {
		t.Fatalf("administrator revision: %d", status)
	}
	if status := save("Corrected third title", "Corrected body", a.Slug, "0"); status != 200 {
		t.Fatalf("in-place edit: %d", status)
	}
	for _, unauthorized := range []*model.Account{nil, &reader, &other} {
		viewer = unauthorized
		if status := save("Forbidden title", "Forbidden body", a.Slug, "1"); status != 403 {
			t.Fatalf("unauthorized revision: %d", status)
		}
	}
	// A failed article write must roll back its historical title and body too.
	occupied := model.Article{Title: "Occupied", Slug: "occupied", TemplateID: solitudes.ArticleTemplateID}
	if err := db.Create(&occupied).Error; err != nil {
		t.Fatal(err)
	}
	viewer = &owner
	if status := save("Failed title", "Failed body", occupied.Slug, "1"); status != 500 {
		t.Fatalf("duplicate slug unexpectedly saved: %d", status)
	}
	var history []model.ArticleHistory
	if err := db.Where("article_id = ?", a.ID).Order("version").Find(&history).Error; err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Version != 1 || history[1].Version != 2 ||
		history[0].Title != a.Title || history[1].Title != "Second title" || history[0].Content != a.Content || history[1].Content != a.Content ||
		history[0].EditorID == nil || *history[0].EditorID != owner.ID || history[1].EditorID == nil || *history[1].EditorID != admin.ID {
		t.Fatalf("revision snapshots are inconsistent: %+v", history)
	}
	if history[1].UpdatedByID == nil || *history[1].UpdatedByID != owner.ID || history[1].UpdatedAt.IsZero() {
		t.Fatal("snapshot lost the editor of the previous revision")
	}
	var current model.Article
	if err := db.Take(&current, "id = ?", a.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.Version != 3 || current.Title != "Corrected third title" || current.Content != "Corrected body" || *current.AuthorID != owner.ID {
		t.Fatalf("unexpected current article: %+v", current)
	}
	if current.UpdatedByID == nil || *current.UpdatedByID != admin.ID || !current.UpdatedAt.After(history[1].UpdatedAt) {
		t.Fatal("creator and latest editor were not kept separate")
	}
	viewer = &owner
	if status := save(current.Title, "Owner minor edit", a.Slug, "0"); status != 200 {
		t.Fatalf("owner minor edit: %d", status)
	}
	if err := db.Take(&current, "id = ?", a.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.Version != 3 || current.UpdatedByID == nil || *current.UpdatedByID != owner.ID || *current.AuthorID != owner.ID {
		t.Fatal("minor edit did not update the editor independently")
	}
}

func TestPostgresHistoryTitleMigrationDoesNotInventOldTitles(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}); err != nil {
		t.Fatal(err)
	}
	a := model.Article{Title: "Latest title is not historical evidence", Slug: "migrated-history", Version: 2}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	// Reproduce the deployed schema before historical titles were stored.
	if err := db.Exec(`CREATE TABLE article_histories (
		article_id uuid NOT NULL, editor_id uuid, version bigint NOT NULL,
		"desc" text, content text, created_at timestamptz
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO article_histories (article_id, version, content, created_at) VALUES (?, 1, 'Original body', ?)`, a.ID, a.CreatedAt).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := db.AutoMigrate(&model.ArticleHistory{}); err != nil {
			t.Fatal(err)
		}
		var missing bool
		if err := db.Raw(`SELECT title IS NULL FROM article_histories WHERE article_id = ? AND version = 1`, a.ID).Scan(&missing).Error; err != nil || !missing {
			t.Fatalf("migration invented an old title: %v", err)
		}
		var history model.ArticleHistory
		if err := db.Take(&history, "article_id = ? AND version = 1", a.ID).Error; err != nil || history.Title != "" || history.Content != "Original body" {
			t.Fatalf("migration changed the historical source: %+v %v", history, err)
		}
	}
}

func TestPostgresHistoricalTitleRenderingAndMissingTitle(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.ArticleHistory{}, &model.Comment{}); err != nil {
		t.Fatal(err)
	}
	a := model.Article{Title: "Latest title", Slug: "history-titles", Content: "Current body", TemplateID: solitudes.ArticleTemplateID, Version: 3}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	historicalTitle := `旧标题 <img src=x onerror=alert(1)> & "引号"`
	for _, row := range []model.ArticleHistory{
		{ArticleID: a.ID, Version: 1, Content: "Title was never recorded"},
		{ArticleID: a.ID, Version: 2, Title: historicalTitle, Content: "Previous body"},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir("..")
	previous, engine, translations := solitudes.System, globalDynamicEngine.engine, translator.Trans
	t.Cleanup(func() {
		solitudes.System, globalDynamicEngine.engine, translator.Trans = previous, engine, translations
	})
	for _, theme := range []string{"cactus", "folio"} {
		cfg := &model.Config{}
		cfg.Site.Theme, cfg.Admin.Theme = theme, "default"
		solitudes.System = &solitudes.SysVariable{DB: db, Config: cfg}
		if err := LoadTemplates(); err != nil {
			t.Fatal(err)
		}
		app := fiber.New(fiber.Config{Views: globalDynamicEngine})
		app.Use(trans)
		app.Get("/:slug/:version?", article)
		for _, template := range []byte{solitudes.ArticleTemplateID, solitudes.PageTemplateID} {
			if err := db.Model(&a).Update("template_id", template).Error; err != nil {
				t.Fatal(err)
			}
			for _, locale := range []struct{ language, missing string }{{"zh-CN", "标题未记录"}, {"en", "Title not recorded"}} {
				for _, version := range []struct{ path, title, suffix string }{
					{"/v1", locale.missing, " v1"}, {"/v2", historicalTitle, " v2"}, {"", a.Title, ""},
				} {
					req := httptest.NewRequest(http.MethodGet, "/"+a.Slug+version.path, nil)
					req.Header.Set("Accept-Language", locale.language)
					resp, err := app.Test(req, -1)
					if err != nil {
						t.Fatal(err)
					}
					doc, err := goquery.NewDocumentFromReader(resp.Body)
					resp.Body.Close()
					if err != nil || resp.StatusCode != 200 {
						t.Fatalf("%s %s: status=%d, %v", theme, version.path, resp.StatusCode, err)
					}
					wantTitle := version.title + version.suffix
					if !strings.HasPrefix(doc.Find("title").Text(), wantTitle) {
						t.Fatalf("%s document title: %s", theme, doc.Find("title").Text())
					}
					for _, selector := range []string{`meta[property="og:title"]`, `meta[name="twitter:title"]`} {
						if value, _ := doc.Find(selector).Attr("content"); !strings.HasPrefix(value, wantTitle) {
							t.Fatalf("%s metadata title: %s", selector, value)
						}
					}
					if template == solitudes.ArticleTemplateID {
						heading := doc.Find(`[data-testid="site-article"] h1`).First()
						if strings.TrimSpace(heading.Text()) != version.title || heading.Find("img,script").Length() != 0 {
							t.Fatalf("%s heading title missing or unescaped: %s", theme, heading.Text())
						}
					} else if label, _ := doc.Find(`[data-testid="site-page"]`).Attr("aria-label"); label != version.title {
						t.Fatalf("%s standalone page title: %s", theme, label)
					}
				}
			}
		}
	}
	var stored model.ArticleHistory
	if err := db.Take(&stored, "article_id = ? AND version = 1", a.ID).Error; err != nil || stored.Title != "" {
		t.Fatalf("missing title was backfilled: %+v %v", stored, err)
	}
}
