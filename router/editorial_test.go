package router

import (
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func TestFolioEditorialRendersLeadSixRecentAndIndependentRecommendations(t *testing.T) {
	t.Chdir("..")
	previousSystem, previousEngine, previousTranslations := solitudes.System, globalDynamicEngine.engine, translator.Trans
	t.Cleanup(func() {
		solitudes.System, globalDynamicEngine.engine, translator.Trans = previousSystem, previousEngine, previousTranslations
	})
	config := &model.Config{}
	config.Site.Theme, config.Admin.Theme = "folio", "default"
	config.Site.SpaceName, config.Site.Domain = "Test Journal", "example.test"
	intro, err := os.ReadFile("e2e/fixtures/lifelonglearn-hometop.html")
	if err != nil {
		t.Fatal(err)
	}
	config.Site.ThemeConfig = map[string]interface{}{"folio.hometopcontent": string(intro)}
	solitudes.System = &solitudes.SysVariable{Config: config}
	if err := LoadTemplates(); err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{Views: globalDynamicEngine})
	var articles []*model.Article
	app.Use(trans)
	app.Get("/", func(c *fiber.Ctx) error {
		var recommendations []*model.Article
		if len(articles) > 1 {
			recommendations = articles[1:min(3, len(articles))]
		}
		binding := injectSiteData(c, fiber.Map{})
		binding["Queries"] = editorialFixtureQueries{articles: articles, pool: recommendations}
		return c.Render("site/index", binding)
	})
	for _, count := range []int{0, 1, 2, 3, 4, 5, 6, 8, 9} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			articles = nil
			for i := 0; i < count; i++ {
				articles = append(articles, &model.Article{Slug: fmt.Sprintf("story-%d", i), Title: fmt.Sprintf("Story %d", i)})
			}
			resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != 200 {
				t.Fatalf("render: %d %s", resp.StatusCode, body)
			}
			html := string(body)
			masthead, custom, stories := strings.Index(html, `data-testid="folio-masthead"`), strings.Index(html, `data-testid="home-top-content"`), strings.Index(html, `data-testid="folio-frontpage"`)
			if masthead < 0 || custom <= masthead || (count > 0 && stories <= custom) {
				t.Error("expected masthead, custom introduction, then stories")
			}
			if !strings.Contains(html, "function calculateAge(startDate)") || !strings.Contains(html, `href="https://nai.ba"`) {
				t.Error("custom script/link should render unchanged")
			}
			if got := strings.Count(html, `data-testid="folio-story"`); got != min(7, count) {
				t.Fatalf("rendered %d chronological stories, want %d", got, min(7, count))
			}
			for i, article := range articles {
				want := 0
				if i < 7 {
					want++
				}
				if i > 0 && i < 3 {
					want++
				}
				if strings.Count(html, `data-story-slug="`+article.Slug+`"`) != want {
					t.Errorf("%s occurrence count, want %d", article.Slug, want)
				}
			}
			if got := strings.Count(html, `data-testid="folio-recommendation"`); got != max(0, min(2, count-1)) {
				t.Errorf("recommendation count = %d", got)
			}
			if count == 0 && strings.Contains(html, `data-testid="folio-frontpage"`) {
				t.Error("empty front page should not reserve columns")
			}
			if count == 1 && !strings.Contains(html, "journal-front-single") {
				t.Error("single story should span the front page")
			}
		})
	}
}

type editorialFixtureQueries struct{ articles, pool []*model.Article }

func (q editorialFixtureQueries) Articles(order string, count int, filters ...string) []*model.Article {
	if len(filters) > 0 && filters[0] == "tag" {
		return nil
	}
	if order == "reads" {
		return q.pool
	}
	return q.articles[:min(count, len(q.articles))]
}

func (q editorialFixtureQueries) Users(_ string, _ int) []readerCircleMember { return nil }
func (q editorialFixtureQueries) RecentComments(_ int) []publicComment       { return nil }

func TestTOCHeadingCount(t *testing.T) {
	root := &model.ArticleTOC{Title: "Root"}
	child := &model.ArticleTOC{Title: "Child", Parent: root}
	root.SubTitles = []*model.ArticleTOC{nil, child}
	child.SubTitles = []*model.ArticleTOC{{Title: "Grandchild", Parent: child}}
	if got := tocHeadingCount([]*model.ArticleTOC{nil, root}); got != 3 {
		t.Fatalf("nested headings including parent pointers = %d, want 3", got)
	}
	if got := tocHeadingCount(nil); got != 0 {
		t.Fatalf("empty headings = %d, want 0", got)
	}
}
