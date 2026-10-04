package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func TestPostgresHistoryInheritsLatestAudienceAndHistoricalFragments(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.ArticleHistory{}, &model.Comment{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	owner := model.Account{Email: "history-owner@example.test", Role: model.RoleUser, EmailVerifiedAt: &now}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	unverifiedOwner := owner
	unverifiedOwner.EmailVerifiedAt = nil
	readers := []struct {
		name    string
		account *model.Account
		allowed [4]bool // public, members, editors, private (independent of implementation)
	}{
		{"guest", nil, [4]bool{true, false, false, false}},
		{"unverified", &model.Account{Role: model.RoleUser}, [4]bool{true, false, false, false}},
		{"member", &model.Account{Role: model.RoleUser, EmailVerifiedAt: &now}, [4]bool{true, true, false, false}},
		{"editor", &model.Account{Role: model.RoleEditor, EmailVerifiedAt: &now}, [4]bool{true, true, true, false}},
		{"demoted-author", &owner, [4]bool{true, true, false, true}},
		{"unverified-author", &unverifiedOwner, [4]bool{true, false, false, true}},
		{"admin", &model.Account{Role: model.RoleAdmin}, [4]bool{true, true, true, true}},
		{"disabled-admin", &model.Account{Role: model.RoleAdmin, DisabledAt: &now}, [4]bool{true, false, false, false}},
	}
	a := model.Article{AuthorID: &owner.ID, Title: "Revision access", Slug: "revision-access", Version: 2,
		TemplateID: solitudes.ArticleTemplateID,
		Content:    "current-body-token\n\n```access:private\npreviously-open-token\n```\n\nhistorically-private-token"}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	history := model.ArticleHistory{ArticleID: a.ID, Version: 1, Content: "## Historical public heading\n\npreviously-open-token\n\n" +
		"```access:members\n## Historical member heading\n\nmember-history-token\n```\n\n" +
		"```access:editors\n## Historical editor heading\n\neditor-history-token\n```\n\n" +
		"```access:private\n## Historical private heading\n\nhistorically-private-token\n![secret](https://private-history.example/image.png)\n```\n\n" +
		"````access:members\n```access:private\nnested-history-token\n```\n````"}
	if err := db.Create(&history).Error; err != nil {
		t.Fatal(err)
	}
	t.Chdir("..")
	previous, engine, translations := solitudes.System, globalDynamicEngine.engine, translator.Trans
	t.Cleanup(func() {
		solitudes.System, globalDynamicEngine.engine, translator.Trans = previous, engine, translations
	})
	for _, theme := range []string{"cactus", "folio"} {
		t.Run(theme, func(t *testing.T) {
			conf := &model.Config{}
			conf.Site.Theme, conf.Admin.Theme = theme, "default"
			solitudes.System = &solitudes.SysVariable{Config: conf, DB: db}
			if err := LoadTemplates(); err != nil {
				t.Fatal(err)
			}
			var viewer *model.Account
			app := fiber.New(fiber.Config{Views: globalDynamicEngine})
			app.Use(trans)
			app.Use(func(c *fiber.Ctx) error {
				c.Locals(solitudes.CtxAccount, viewer)
				return c.Next()
			})
			app.Get("/:slug/:version?", article)
			request := func(t *testing.T, suffix string, status int) (string, http.Header) {
				t.Helper()
				resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/"+a.Slug+suffix, nil), -1)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != status {
					t.Fatalf("%s: status %d, want %d: %s", suffix, resp.StatusCode, status, body)
				}
				return string(body), resp.Header
			}
			for _, templateID := range []byte{solitudes.ArticleTemplateID, solitudes.PageTemplateID} {
				// Change only the latest row: history must immediately follow both
				// tightening and reopening its audience, without rewriting revisions.
				for vi, visibility := range []model.ArticleVisibility{model.VisibilityPublic, model.VisibilityMembers, model.VisibilityEditors, model.VisibilityPrivate, model.VisibilityPublic} {
					if err := db.Model(&a).Updates(map[string]interface{}{"visibility": visibility, "template_id": templateID}).Error; err != nil {
						t.Fatal(err)
					}
					for _, reader := range readers {
						t.Run(string(visibility)+"/"+reader.name, func(t *testing.T) {
							viewer = reader.account
							canRead := reader.allowed[vi%4]
							if !canRead {
								for _, suffix := range []string{"", "/v1", "/v2", "/v999"} {
									body, headers := request(t, suffix, http.StatusNotFound)
									for _, token := range []string{a.Title, "current-body-token", "previously-open-token", "historically-private-token"} {
										if strings.Contains(body, token) {
											t.Fatalf("unauthorized response leaked %q", token)
										}
									}
									if headers.Get("Location") != "" || !strings.Contains(headers.Get("Cache-Control"), "no-store") {
										t.Fatal("unauthorized response redirected or was cacheable")
									}
								}
								return
							}
							body, headers := request(t, "/v1", http.StatusOK)
							for token, visible := range map[string]bool{
								"previously-open-token": true, "current-body-token": false,
								"member-history-token": reader.allowed[1], "Historical member heading": reader.allowed[1],
								"editor-history-token": reader.allowed[2], "Historical editor heading": reader.allowed[2],
								"historically-private-token": reader.allowed[3], "Historical private heading": reader.allowed[3],
								"private-history.example": reader.allowed[3], "nested-history-token": reader.allowed[1] && reader.allowed[3],
							} {
								if strings.Contains(body, token) != visible {
									t.Fatalf("historical fragment %q visibility: want %v", token, visible)
								}
							}
							if !strings.Contains(body, "noindex") {
								t.Fatal("historical page is indexable")
							}
							if visibility != model.VisibilityPublic && !strings.Contains(headers.Get("Cache-Control"), "no-store") {
								t.Fatal("restricted history is cacheable")
							}
							current, _ := request(t, "", http.StatusOK)
							if strings.Contains(current, "previously-open-token") != reader.allowed[3] || !strings.Contains(current, "historically-private-token") {
								t.Fatal("latest fragments incorrectly inherited historical policy")
							}
							if templateID == solitudes.ArticleTemplateID && !strings.Contains(current, `href="/revision-access/v1"`) {
								t.Fatal("authorized reader missing history link")
							}
							_, headers = request(t, "/v2", http.StatusMovedPermanently)
							if headers.Get("Location") != "/"+a.Slug {
								t.Fatal("incorrect latest version redirect")
							}
							for _, suffix := range []string{"/v999", "/v0", "/v", "/x1", "/v-1", "/vnope", "/v18446744073709551616"} {
								request(t, suffix, http.StatusNotFound)
							}
						})
					}
				}
			}
		})
	}
	var stored model.ArticleHistory
	if err := db.First(&stored, "article_id = ? AND version = ?", a.ID, 1).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Content != history.Content {
		t.Fatal("reading or changing latest visibility mutated historical source")
	}
}
