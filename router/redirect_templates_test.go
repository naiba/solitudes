package router

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	html "github.com/gofiber/template/html/v2"

	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

type redirectTemplateRule struct {
	ID           uint64
	SourcePath   string
	TargetPath   string
	ArticleID    *string
	ArticleTitle string
	CreatedAt    time.Time
}

func TestRedirectTemplates(t *testing.T) {
	t.Chdir("..")
	previous := translator.Trans
	t.Cleanup(func() { translator.Trans = previous })
	translator.Reload("cactus", "default")
	engine := html.New("resource/themes/admin/default/templates", ".html")
	setFuncMap(engine)
	if err := engine.Load(); err != nil {
		t.Fatal(err)
	}
	articleID := "linked-article"
	maliciousPath := `/old"><img src=x onerror=alert(1)>`
	maliciousTitle := `<script>alert("article")</script>`
	createdAt := time.Date(2026, time.January, 2, 3, 4, 0, 0, time.UTC)
	rules := []redirectTemplateRule{
		{ID: 18446744073709551615, SourcePath: maliciousPath, TargetPath: `/new?q="quoted"&page=1#section`, CreatedAt: createdAt},
		{ID: 2, SourcePath: "/previous-slug", TargetPath: "/latest-slug", ArticleID: &articleID, ArticleTitle: maliciousTitle, CreatedAt: createdAt},
	}
	for _, locale := range []string{"en", "zh"} {
		tr, _ := translator.Trans.GetTranslator(locale)
		translated := &translator.Translator{Translator: tr, Trans: tr}
		render := func(t *testing.T, rows []redirectTemplateRule, role model.Role) *goquery.Document {
			t.Helper()
			data := viewData{"Tr": translated, "Conf": &model.Config{}, "Account": &model.Account{ID: "admin", Role: role}, "Path": "/admin/redirects", "Data": viewData{
				"rules": rows, "q": maliciousPath, "total": int64(len(rows)),
				"navigation": pageNavigation{Page: 2, Previous: "/admin/redirects?q=old&page=1", Next: "/admin/redirects?q=old&page=3"},
			}}
			var output bytes.Buffer
			if err := engine.Render(&output, "admin/redirects", data); err != nil {
				t.Fatal(err)
			}
			doc, err := goquery.NewDocumentFromReader(&output)
			if err != nil {
				t.Fatal(err)
			}
			return doc
		}
		t.Run(locale+"/rules", func(t *testing.T) {
			doc := render(t, rules, model.RoleAdmin)
			rows := doc.Find(`[data-testid="redirect-rule-row"]`)
			if rows.Length() != 2 {
				t.Fatalf("rule rows = %d, want 2", rows.Length())
			}
			manual, automatic := rows.Eq(0), rows.Eq(1)
			if manual.Find(`[data-testid="redirect-rule-kind"]`).Text() != translated.T("redirect_manual") || automatic.Find(`[data-testid="redirect-rule-kind"]`).Text() != translated.T("redirect_automatic") {
				t.Fatal("manual and automatic rule labels are missing or reversed")
			}
			if manual.Find(`[data-testid="redirect-edit"]`).Length() != 1 || automatic.Find(`[data-testid="redirect-edit"]`).Length() != 0 || doc.Find(`[data-testid="redirect-delete"]`).Length() != 2 {
				t.Fatal("only manual rules should be editable; both kinds must be deletable")
			}
			if manual.Find(`[data-testid="redirect-source"]`).Text() != maliciousPath || manual.Find(`[data-testid="redirect-target"]`).Text() != rules[0].TargetPath {
				t.Fatal("paths were altered or rendered as markup")
			}
			if automatic.Find(`[data-testid="redirect-target"]`).Text() != "/latest-slug" || automatic.Find(`[data-testid="redirect-article"]`).Text() != maliciousTitle || automatic.Find(`[data-testid="redirect-article"]`).AttrOr("href", "") != "/admin/publish?id="+articleID {
				t.Fatal("automatic rule does not show its associated article and resolved current target")
			}
			if !strings.Contains(automatic.Text(), translated.T("redirect_automatic_hint")) || !strings.Contains(doc.Find(`[data-testid="redirect-priority-hint"]`).Text(), translated.T("redirect_priority_hint")) {
				t.Fatal("missing automatic tracking or route precedence explanation")
			}
			if doc.Find(`img[src="x"], script:not([src])`).FilterFunction(func(_ int, node *goquery.Selection) bool {
				return node.Is("img") || strings.TrimSpace(node.Text()) == `alert("article")`
			}).Length() != 0 {
				t.Fatal("untrusted path or article title became executable markup")
			}
			for i, rule := range rules {
				checkbox := rows.Eq(i).Find(`[data-testid="redirect-select"]`)
				if checkbox.AttrOr("name", "") != "redirect_ids" || checkbox.AttrOr("type", "") != "checkbox" || checkbox.AttrOr("value", "") != fmt.Sprint(rule.ID) {
					t.Fatal("missing exact rule ID in row selection")
				}
				if strings.TrimSpace(rows.Eq(i).Find(`[data-testid="redirect-status"]`).Text()) != "301" {
					t.Fatal("redirect status must always be 301")
				}
			}
			if doc.Find(`[data-testid="redirect-select-page"]`).Length() != 1 || doc.Find(`[data-testid="redirect-select-page"]`).ParentsFiltered("thead").Length() != 0 || doc.Find(`[data-testid="redirect-delete-selected"][disabled]`).Length() != 1 {
				t.Fatal("missing mobile-visible current-page select-all or initially disabled batch delete")
			}
			if doc.Find(`[data-testid="redirect-total"]`).Text() != "2" || doc.Find(`[data-testid="pagination-next"]`).AttrOr("href", "") != "/admin/redirects?q=old&page=3" || doc.Find(`[data-testid="pagination-previous"]`).AttrOr("href", "") != "/admin/redirects?q=old&page=1" {
				t.Fatal("total or search-preserving pagination is incorrect")
			}
			form := doc.Find(`[data-testid="redirect-form"]`)
			if form.Length() != 1 || form.Find(`input[name="id"][type="hidden"][value="0"]`).Length() != 1 || form.Find(`input[name="source_path"][required]`).Length() != 1 || form.Find(`input[name="target_path"][required]`).Length() != 1 || form.Find(`button[type="submit"]`).Length() != 1 {
				t.Fatal("manual rule form is missing its ID, required paths or submit action")
			}
			search := doc.Find(`[data-testid="redirect-search-form"]`)
			if search.AttrOr("method", "") != "get" || search.AttrOr("action", "") != "/admin/redirects" || search.Find(`input[name="q"]`).AttrOr("value", "") != maliciousPath {
				t.Fatal("search must use GET and safely retain the current query")
			}
			if doc.Find(`[data-testid="admin-nav-redirects"][aria-current="page"]`).Length() != 1 {
				t.Fatal("administrator navigation entry is missing")
			}
		})
		t.Run(locale+"/empty", func(t *testing.T) {
			doc := render(t, nil, model.RoleAdmin)
			if doc.Find(`[data-testid="redirect-rule-row"]`).Length() != 0 || doc.Find(`[data-testid="redirect-empty"]`).Text() != translated.T("no_data") || doc.Find(`[data-testid="redirect-form"]`).Length() != 1 {
				t.Fatal("empty listing must retain the create form and show its empty state")
			}
		})
		t.Run(locale+"/editor-navigation", func(t *testing.T) {
			if render(t, nil, model.RoleEditor).Find(`[data-testid="admin-nav-redirects"]`).Length() != 0 {
				t.Fatal("redirect navigation must not be exposed to editors")
			}
		})
	}
}
