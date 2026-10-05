package router

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func TestPostgresCactusRecommendationsUsePopularVisiblePool(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.Comment{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 15; i++ {
		a := model.Article{Title: fmt.Sprintf("Story %d", i), Slug: fmt.Sprintf("story-%d", i), ReadNum: uint(i + 1), TemplateID: solitudes.ArticleTemplateID}
		switch i {
		case 12:
			a.Visibility = model.VisibilityPrivate
		case 13:
			a.RawTags = "Topic"
		case 14:
			a.TemplateID = solitudes.PageTemplateID
		}
		if err := db.Create(&a).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir("..")
	previous, engine, translations := solitudes.System, globalDynamicEngine.engine, translator.Trans
	t.Cleanup(func() {
		solitudes.System, globalDynamicEngine.engine, translator.Trans = previous, engine, translations
	})
	conf := &model.Config{}
	conf.Site.Theme, conf.Admin.Theme = "cactus", "default"
	solitudes.System = &solitudes.SysVariable{Config: conf, DB: db}
	if err := LoadTemplates(); err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{Views: globalDynamicEngine})
	app.Use(trans)
	app.Get("/", index)
	check := func(count int, allowed map[string]bool) {
		t.Helper()
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Accept-Language", "en")
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := goquery.NewDocumentFromReader(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("render: %d %v", resp.StatusCode, err)
		}
		section := doc.Find(".home-most-read-section")
		links := section.Find(`[data-testid="article-list-link"]`)
		if links.Length() != count {
			t.Fatalf("got %d picks, want %d", links.Length(), count)
		}
		if count == 0 && section.Length() != 0 {
			t.Fatal("empty recommendation column left a heading")
		}
		if count > 0 && section.Find("h2").Text() != "Recommended" {
			t.Fatal("old most-read label remains")
		}
		seen := map[string]bool{}
		links.Each(func(_ int, link *goquery.Selection) {
			href, _ := link.Attr("href")
			if !allowed[href] || seen[href] {
				t.Errorf("out-of-pool or duplicate pick %s", href)
			}
			seen[href] = true
		})
		if strings.Contains(section.Text(), "Story 12") {
			t.Fatal("private article recommended")
		}
	}
	allowed := map[string]bool{}
	for i := 2; i < 12; i++ {
		allowed[fmt.Sprintf("/story-%d", i)] = true
	}
	for i := 0; i < 8; i++ {
		check(3, allowed)
	}
	for _, count := range []int{2, 1, 0} {
		if err := db.Model(&model.Article{}).Where("read_num > ?", count).Update("visibility", model.VisibilityPrivate).Error; err != nil {
			t.Fatal(err)
		}
		check(count, map[string]bool{"/story-0": true, "/story-1": true})
	}
}
