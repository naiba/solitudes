package router

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	html "github.com/gofiber/template/html/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

// In a small circle the newest members are the active ones. Listing a person
// under both headings filled the page with duplicates, so the newcomer list
// leaves out anyone already shown as active and keeps its order otherwise.
func TestReadersNotListedDropsMembersAlreadyShown(t *testing.T) {
	member := func(id string) readerCircleMember { return readerCircleMember{ID: id, Nickname: id} }
	latest := []readerCircleMember{member("newest"), member("active-b"), member("quiet"), member("active-a")}
	active := []readerCircleMember{member("active-a"), member("active-b"), member("veteran")}
	got := readersNotListed(latest, active)
	if len(got) != 2 || got[0].ID != "newest" || got[1].ID != "quiet" {
		t.Fatalf("newcomers = %+v, want newest then quiet", got)
	}
	if got := readersNotListed(latest, nil); len(got) != len(latest) {
		t.Fatalf("without active members every newcomer stays, got %d", len(got))
	}
	if got := readersNotListed(nil, active); len(got) != 0 {
		t.Fatalf("no newcomers in, none out, got %d", len(got))
	}
}

// communityFixtureQueries serves as many rows as a template asks for, up to
// what the fixture holds.
type communityFixtureQueries struct{ articles, topics, comments int }

func (q communityFixtureQueries) Articles(order string, count int, filters ...string) []*model.Article {
	available, tags := q.articles, ""
	if len(filters) > 0 && filters[0] == "tag" {
		available, tags, order = q.topics, "Topic", "topic"
	}
	rows := make([]*model.Article, 0, count)
	for i := 0; i < min(count, available); i++ {
		rows = append(rows, &model.Article{ID: fmt.Sprintf("%s-%d", order, i), Slug: fmt.Sprintf("%s-%d", order, i), Title: fmt.Sprintf("%s %d", order, i), Content: "Body", RawTags: tags})
	}
	return rows
}
func (q communityFixtureQueries) Comments(_ string, _ int) []*model.Comment { return nil }
func (q communityFixtureQueries) Users(_ string, count int) []readerCircleMember {
	rows := make([]readerCircleMember, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, readerCircleMember{ID: fmt.Sprintf("member-%d", i), Nickname: fmt.Sprintf("Member %d", i), Role: model.RoleUser})
	}
	return rows
}
func (q communityFixtureQueries) RecentComments(count int) []publicComment {
	rows := make([]publicComment, 0, count)
	for i := 0; i < min(count, q.comments); i++ {
		rows = append(rows, publicComment{ID: fmt.Sprintf("comment-%d", i), Nickname: "Reader", Content: "Comment", ArticleSlug: "newest-0", ArticleTitle: "newest 0"})
	}
	return rows
}

// The home page, reader circle and member pages are composed so that nothing
// sits in a side column that can trail off beside a longer one. This pins the
// parts of that composition the templates decide; the browser contract
// "community pages are composed of level sections" checks the geometry.
func TestSiteThemeCommunityComposition(t *testing.T) {
	t.Chdir("..")
	previousTranslations, previousSystem := translator.Trans, solitudes.System
	t.Cleanup(func() { translator.Trans, solitudes.System = previousTranslations, previousSystem })
	conf := &model.Config{}
	conf.Site.Domain = "example.test"
	solitudes.System = &solitudes.SysVariable{Config: conf}

	count := func(t *testing.T, doc *goquery.Document, selector string, want int) {
		t.Helper()
		if got := doc.Find(selector).Length(); got != want {
			t.Errorf("%s matched %d nodes, want %d", selector, got, want)
		}
	}
	members := func(prefix string, n int) []readerCircleMember {
		rows := make([]readerCircleMember, 0, n)
		for i := 0; i < n; i++ {
			rows = append(rows, readerCircleMember{ID: fmt.Sprintf("%s-%d", prefix, i), Nickname: fmt.Sprintf("%s %d", prefix, i), Role: model.RoleUser, CreatedAt: time.Unix(int64(i), 0)})
		}
		return rows
	}
	for _, theme := range []string{"cactus", "folio"} {
		t.Run(theme, func(t *testing.T) {
			translator.Reload(theme, "default")
			catalog, _ := translator.Trans.GetTranslator("en")
			tr := &translator.Translator{Translator: catalog, Trans: catalog}
			engine := html.New("resource/themes/site/"+theme+"/templates", ".html")
			setFuncMap(engine)
			if err := engine.Load(); err != nil {
				t.Fatal(err)
			}
			render := func(t *testing.T, name string, queries communityFixtureQueries, data viewData) *goquery.Document {
				t.Helper()
				var output bytes.Buffer
				binding := viewData{"Tr": tr, "Conf": conf, "Theme": viewData{}, "Path": "/", "Data": data, "Queries": queries, "tr": tr}
				if err := engine.Render(&output, "site/"+name, binding); err != nil {
					t.Fatal(err)
				}
				doc, err := goquery.NewDocumentFromReader(&output)
				if err != nil {
					t.Fatal(err)
				}
				return doc
			}

			t.Run("home", func(t *testing.T) {
				busy := render(t, "index", communityFixtureQueries{articles: 40, topics: 9, comments: 40}, viewData{})
				quiet := render(t, "index", communityFixtureQueries{articles: 40, comments: 40}, viewData{})
				if theme == "cactus" {
					// The comments are the elastic list of the side column: one list,
					// more rows than usually fit, shown as far as the main column reaches.
					count(t, busy, `.home-secondary .home-comments-list.fill-rows`, 1)
					count(t, busy, `.home-comments-list.fill-rows > [data-testid="home-recent-comment"]`, 12)
					count(t, busy, `.home-primary .fill-rows`, 0)
					count(t, busy, `.home-articles-section [data-testid="article-list-link"]`, 6)
					// Without topics the writing log takes their place.
					count(t, quiet, `.home-articles-section [data-testid="article-list-link"]`, 10)
					count(t, quiet, `.home-topics-section`, 0)
					return
				}
				// Departments stack down the page; each fills whole rows of its own grid.
				count(t, busy, `.journal-community > .journal-department`, 3)
				count(t, busy, `.journal-community [class*="side"]`, 0)
				count(t, busy, `.journal-topic-grid [data-testid="topic-entry"]`, 4)
				count(t, busy, `.journal-department [data-testid="home-recent-comment"]`, 6)
				count(t, busy, `.journal-department [data-testid="reader-circle-member"]`, 6)
				count(t, busy, `[data-testid="folio-recommendation"]`, 2)
				count(t, busy, `.fill-rows`, 0)
				count(t, quiet, `.journal-community > .journal-department`, 2)
			})

			t.Run("reader circle", func(t *testing.T) {
				events := make([]readerCircleActivity, 0, readerCircleFeedSize)
				for i := 0; i < readerCircleFeedSize; i++ {
					events = append(events, readerCircleActivity{AccountID: "member", Nickname: "Member", Role: model.RoleUser, Kind: "article",
						ArticleSlug: fmt.Sprintf("story-%d", i), ArticleTitle: fmt.Sprintf("Story %d", i), HappenedAt: time.Unix(int64(i), 0)})
				}
				full := render(t, "reader_circle", communityFixtureQueries{}, viewData{"page": 1, "activity": events, "active": members("Active", 3), "latest": members("New", 4)})
				count(t, full, `[data-testid="reader-circle-activity"]`, readerCircleFeedSize)
				count(t, full, `[data-testid="reader-circle-active"]`, 3)
				count(t, full, `[data-testid="reader-circle-latest"]`, 4)
				count(t, full, `.fill-rows`, 0)
				// Everyone is already listed as active: no empty "new faces" heading.
				onlyActive := render(t, "reader_circle", communityFixtureQueries{}, viewData{"page": 1, "activity": events, "active": members("Active", 3)})
				count(t, onlyActive, `#circle-new-title`, 0)
				count(t, onlyActive, `#circle-active-title`, 1)
				// ...unless there are further pages of newcomers to reach.
				paged := render(t, "reader_circle", communityFixtureQueries{}, viewData{"page": 1, "activity": events, "active": members("Active", 3), "more": true})
				count(t, paged, `#circle-new-title`, 1)
				count(t, paged, `[data-testid="pagination-next"]`, 1)
				// An empty circle still invites the first member.
				empty := render(t, "reader_circle", communityFixtureQueries{}, viewData{"page": 1})
				count(t, empty, `#circle-new-title`, 1)
				count(t, empty, `[data-testid="reader-circle-latest"]`, 0)
			})

			t.Run("member page", func(t *testing.T) {
				story := &model.Article{ID: "story", Slug: "story", Title: "Story"}
				comment := &model.Comment{ID: "comment", Article: story, Content: "What the member wrote"}
				doc := render(t, "user_profile", communityFixtureQueries{}, viewData{"profile": model.Account{Nickname: "Reader"},
					"articles": []*model.Article{story}, "comments": []*model.Comment{comment}, "articles_page": 1, "comments_page": 1})
				// On a member's page their own words come before the article they answered.
				reply := doc.Find(`[data-testid="public-profile-comment"]`)
				if reply.Length() != 1 || reply.NextAllFiltered(`:has([data-testid="public-profile-comment-article"]), [data-testid="public-profile-comment-article"]`).Length() != 1 {
					t.Errorf("the comment must precede the link to its article")
				}
				count(t, doc, `#profile-articles [data-testid="public-profile-article"]`, 1)
				count(t, doc, `#profile-comments [data-testid="public-profile-comment-article"]`, 1)
			})
		})
	}
}
