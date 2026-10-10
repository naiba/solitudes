package router

import (
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func TestPostgresMachineContentPrivacyAndPagination(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}); err != nil {
		t.Fatal(err)
	}
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	config := &model.Config{}
	config.Site.Domain = "example.test"
	config.Site.SpaceName = "Research"
	config.Site.SpaceDesc = "Public articles"
	solitudes.System = &solitudes.SysVariable{DB: db, Config: config}
	public := model.Article{Title: "Public article", Slug: "story.md", Content: "Public evidence\n\n[Source](https://outside.test/report)\n\n```access:members\nHIDDEN FRAGMENT\n```", Version: 2}
	private := model.Article{Title: "PRIVATE TITLE", Slug: "private-story", Content: "PRIVATE BODY", Visibility: model.VisibilityPrivate}
	for _, a := range []*model.Article{&public, &private} {
		if err := db.Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, visibility := range []model.ArticleVisibility{model.VisibilityMembers, model.VisibilityEditors} {
		restricted := model.Article{Title: "RESTRICTED " + string(visibility), Slug: string(visibility), Content: "RESTRICTED BODY", Visibility: visibility}
		if err := db.Create(&restricted).Error; err != nil {
			t.Fatal(err)
		}
	}
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		if c.Get("X-Test-Admin") == "yes" {
			c.Locals(solitudes.CtxAccount, &model.Account{Role: model.RoleAdmin})
		}
		return c.Next()
	})
	app.Get("/llms.txt", llmsDirectory)
	app.Get("/read/:id/article.md", articleMachineContent)
	request := func(path string, admin bool) (int, string) {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		if admin {
			req.Header.Set("X-Test-Admin", "yes")
		}
		req.Header.Set("If-None-Match", `"old-public-body"`)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("cache can leak restricted content")
		}
		if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/markdown") {
			t.Fatal("incorrect machine content type")
		}
		return resp.StatusCode, string(body)
	}
	for _, admin := range []bool{false, true} {
		for _, path := range []string{"/llms.txt", "/read/" + public.ID + "/article.md"} {
			status, body := request(path, admin)
			if status != 200 || !strings.Contains(body, "Public") || strings.Contains(body, "HIDDEN FRAGMENT") || strings.Contains(body, "PRIVATE") || strings.Contains(body, "RESTRICTED") {
				t.Fatalf("privacy/status: %d %s", status, body)
			}
			if !strings.Contains(body, "https://example.test/story.md") {
				t.Fatal("missing canonical source")
			}
		}
		status, body := request("/read/"+private.ID+"/article.md", admin)
		if status != 404 || strings.Contains(body, "PRIVATE") {
			t.Fatal("private export")
		}
	}
	if err := db.Model(&public).Update("slug", "renamed-story").Error; err != nil {
		t.Fatal(err)
	}
	_, renamed := request("/read/"+public.ID+"/article.md", false)
	if !strings.Contains(renamed, "https://example.test/renamed-story") || strings.Contains(renamed, "https://example.test/story.md") {
		t.Fatal("stable export retains old canonical")
	}
	for i := 0; i < 51; i++ {
		a := model.Article{Title: fmt.Sprintf("Archive %d", i), Slug: fmt.Sprintf("archive-%d", i), Content: "Public", CreatedAt: time.Now().Add(-time.Duration(i+1) * time.Hour)}
		if err := db.Create(&a).Error; err != nil {
			t.Fatal(err)
		}
	}
	_, first := request("/llms.txt", false)
	status, second := request("/llms.txt?page=2", false)
	if !strings.Contains(first, "Next page") || status != 200 || !strings.Contains(second, "Previous page") || strings.Contains(second, "Next page") || strings.Count(first, " — [Original]") != 50 || strings.Count(second, " — [Original]") != 2 {
		t.Fatalf("pagination: %s\n%s", first, second)
	}
	if status, _ := request("/llms.txt?page=3", false); status != 404 {
		t.Fatal("empty archive not 404")
	}
	if status, _ := request("/llms.txt?page=1001", false); status != 400 {
		t.Fatal("unbounded archive")
	}
	if err := db.Model(&public).Update("visibility", model.VisibilityPrivate).Error; err != nil {
		t.Fatal(err)
	}
	if status, _ := request("/read/"+public.ID+"/article.md", true); status != 404 {
		t.Fatal("visibility change bypassed by session/cache")
	}
	_, directory := request("/llms.txt", false)
	if strings.Contains(directory, "- [Public article]") {
		t.Fatal("private title still listed")
	}
	if err := db.Delete(&private).Error; err != nil {
		t.Fatal(err)
	}
	if status, _ := request("/read/"+private.ID+"/article.md", false); status != 404 {
		t.Fatal("deleted export")
	}
	if status, _ := request("/read/not-a-uuid/article.md", false); status != 404 {
		t.Fatal("invalid ID")
	}
}
