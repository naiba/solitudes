package router

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func TestPostgresTemplatesUseModelQueries(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.ArticleHistory{}, &model.Comment{}); err != nil {
		t.Fatal(err)
	}
	for i, tag := range []string{"", "Topic"} {
		a := model.Article{Title: "Render", Slug: []string{"story", "topic"}[i], RawTags: tag, TemplateID: solitudes.ArticleTemplateID, Content: "Public\n\n```access:members\nnever-expose-token\n```"}
		if err := db.Create(&a).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir("..")
	previous, engine, translations := solitudes.System, globalDynamicEngine.engine, translator.Trans
	t.Cleanup(func() {
		solitudes.System, globalDynamicEngine.engine, translator.Trans = previous, engine, translations
	})
	for _, theme := range []string{"cactus", "folio"} {
		t.Run(theme, func(t *testing.T) {
			conf := &model.Config{}
			conf.Site.Theme = theme
			conf.Admin.Theme = "default"
			solitudes.System = &solitudes.SysVariable{Config: conf, DB: db}
			if err := LoadTemplates(); err != nil {
				t.Fatal(err)
			}
			app := fiber.New(fiber.Config{Views: globalDynamicEngine})
			app.Use(trans)
			app.Get("/", index)
			app.Get("/:slug", article)
			for _, url := range []string{"/", "/story", "/topic"} {
				resp, err := app.Test(httptest.NewRequest("GET", url, nil), -1)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != 200 {
					t.Fatalf("%s: %d %s", url, resp.StatusCode, body)
				}
				if strings.Contains(string(body), "never-expose-token") {
					t.Fatalf("template got raw restricted body: %s", url)
				}
			}
		})
	}
}

func TestPostgresSearchRevalidatesStaleRestrictedSnippets(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}); err != nil {
		t.Fatal(err)
	}
	a := model.Article{Title: "Current story", Slug: "current-story", Version: 1, Content: "visiblecurrenttoken\n\n```access:members\nrestrictedstaletoken\n```"}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	index, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	if err := index.Index(a.GetIndexID(), map[string]interface{}{"Title": a.Title, "Content": "visiblecurrenttoken restrictedstaletoken", "IsPrivate": false}); err != nil {
		t.Fatal(err)
	}
	t.Chdir("..")
	previous, engine, translations := solitudes.System, globalDynamicEngine.engine, translator.Trans
	t.Cleanup(func() {
		solitudes.System, globalDynamicEngine.engine, translator.Trans = previous, engine, translations
	})
	conf := &model.Config{}
	conf.Site.Theme = "cactus"
	conf.Admin.Theme = "default"
	solitudes.System = &solitudes.SysVariable{DB: db, Config: conf, Search: index}
	if err := LoadTemplates(); err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{Views: globalDynamicEngine})
	app.Use(trans)
	app.Get("/search", search)
	for _, term := range []string{"restrictedstaletoken", "visiblecurrenttoken"} {
		resp, err := app.Test(httptest.NewRequest("GET", "/search?w="+term, nil), -1)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("search error: %d %s", resp.StatusCode, body)
		}
		if strings.Contains(string(body), "Current story") != (term == "visiblecurrenttoken") {
			t.Fatalf("stale/private query leaked or public query lost: %s", body)
		}
	}
}
