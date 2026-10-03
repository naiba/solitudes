package router

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func TestPostgresHomeArticlesChronologyAndPopularPool(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}); err != nil {
		t.Fatal(err)
	}
	author := model.Account{Email: "editorial@example.test", Nickname: "Editorial author", Role: model.RoleEditor}
	if err := db.Create(&author).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	var stories []model.Article
	for i := 0; i < 20; i++ {
		article := model.Article{AuthorID: &author.ID, Slug: fmt.Sprintf("editorial-%02d", i), Title: "Article",
			Content: "Body", TemplateID: solitudes.ArticleTemplateID, CreatedAt: now.Add(-time.Duration(i) * time.Hour),
			ReadNum: uint(100 - i)}
		if err := db.Create(&article).Error; err != nil {
			t.Fatal(err)
		}
		stories = append(stories, article)
	}
	// Topics neither consume the recent-article budget nor enter the pool.
	for i := 0; i < 5; i++ {
		article := model.Article{Slug: fmt.Sprintf("topic-%d", i), RawTags: "Topic", Title: "Topic", Content: "A note",
			TemplateID: solitudes.ArticleTemplateID, ReadNum: 10000, CreatedAt: now.Add(time.Hour)}
		if err := db.Create(&article).Error; err != nil {
			t.Fatal(err)
		}
	}
	page := model.Article{Slug: "popular-page", Title: "Page", Content: "About", TemplateID: solitudes.PageTemplateID,
		CreatedAt: now.Add(-100 * time.Hour), ReadNum: 10000}
	if err := db.Create(&page).Error; err != nil {
		t.Fatal(err)
	}
	pool := make(map[string]bool)
	for _, article := range stories[1:11] {
		pool[article.ID] = true
	}
	for attempt := 0; attempt < 20; attempt++ {
		data, err := queryArticleFixture(db, nil)
		latest, recommendations := data.Articles, data.Recommendations
		if err != nil {
			t.Fatal(err)
		}
		if len(latest) != 16 || len(recommendations) != 2 {
			t.Fatalf("counts = %d/%d", len(latest), len(recommendations))
		}
		for i, article := range latest {
			if article.ID != stories[i].ID || article.Author.ID != author.ID {
				t.Errorf("wrong recent article or missing author at %d", i)
			}
		}
		if len(data.MostRead) != 3 || data.MostRead[0].ID != stories[0].ID || data.MostRead[1].ID != stories[1].ID || data.MostRead[2].ID != stories[2].ID {
			t.Fatal("popular ranking must not be shuffled or exclude the lead")
		}
		if recommendations[0].ID == recommendations[1].ID {
			t.Fatal("duplicate recommendation")
		}
		for _, article := range recommendations {
			if !pool[article.ID] || article.Author.ID != author.ID {
				t.Fatalf("recommendation outside top-ten eligible pool: %s", article.Slug)
			}
		}
	}
}

// Capture the router-to-template contract without installing a bundled theme.
type homeContractViews struct{}

func (homeContractViews) Load() error { return nil }
func (homeContractViews) Render(w io.Writer, _ string, binding interface{}, _ ...string) error {
	q := binding.(fiber.Map)["Queries"].(*TemplateQueries)
	data, err := queryArticleFixture(q.db, q.account)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(data)
}

func TestPostgresHomeDataContractDoesNotDependOnThemeName(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.Comment{}); err != nil {
		t.Fatal(err)
	}
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	config := &model.Config{}
	solitudes.System = &solitudes.SysVariable{Config: config, DB: db}
	for i := 0; i < 18; i++ {
		article := model.Article{Slug: fmt.Sprintf("universal-%02d", i), Title: "Universal", Content: "Body",
			TemplateID: solitudes.ArticleTemplateID, ReadNum: uint(100 - i), CreatedAt: time.Date(2026, 1, 20-i, 0, 0, 0, 0, time.UTC)}
		if err := db.Create(&article).Error; err != nil {
			t.Fatal(err)
		}
	}
	app := fiber.New(fiber.Config{Views: homeContractViews{}})
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(solitudes.CtxTranslator, &translator.Translator{})
		return c.Next()
	})
	app.Get("/", index)
	for _, name := range []string{"cactus", "folio", "my-independent-theme"} {
		config.Site.Theme = name
		response, err := app.Test(httptest.NewRequest("GET", "/", nil), -1)
		if err != nil {
			t.Fatal(err)
		}
		var data struct {
			Articles        []model.Article
			MostRead        []model.Article
			Recommendations []model.Article
		}
		err = json.NewDecoder(response.Body).Decode(&data)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 {
			t.Fatalf("%s: status=%d err=%v", name, response.StatusCode, err)
		}
		if len(data.Articles) != 16 || len(data.MostRead) != 3 || len(data.Recommendations) != 2 {
			t.Fatalf("%s: unequal home data counts", name)
		}
		for i, article := range data.Articles {
			if article.Slug != fmt.Sprintf("universal-%02d", i) {
				t.Fatalf("%s: different article ordering", name)
			}
		}
		for i, article := range data.MostRead {
			if article.Slug != fmt.Sprintf("universal-%02d", i) {
				t.Fatalf("%s: different popular ordering", name)
			}
		}
		for _, article := range data.Recommendations {
			if article.Slug <= "universal-00" || article.Slug > "universal-10" {
				t.Fatalf("%s: different popular pool", name)
			}
		}
	}
}

func TestPostgresHomeArticlesSparseDataAndPrivacy(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}); err != nil {
		t.Fatal(err)
	}
	data, err := queryArticleFixture(db, nil)
	latest, recommendations := data.Articles, data.Recommendations
	if err != nil || len(latest) != 0 || len(recommendations) != 0 {
		t.Fatalf("empty home: %v %v %v", latest, recommendations, err)
	}
	now := time.Now().UTC()
	editor := model.Account{Email: "editorial-owner@example.test", Nickname: "Owner", Role: model.RoleEditor}
	admin := model.Account{Email: "editorial-admin@example.test", Nickname: "Admin", Role: model.RoleAdmin}
	for _, account := range []*model.Account{&editor, &admin} {
		if err := db.Create(account).Error; err != nil {
			t.Fatal(err)
		}
	}
	lead := model.Article{AuthorID: &editor.ID, Slug: "lead", Title: "Lead", Content: "Body", TemplateID: solitudes.ArticleTemplateID, CreatedAt: now}
	if err := db.Create(&lead).Error; err != nil {
		t.Fatal(err)
	}
	data, err = queryArticleFixture(db, nil)
	latest, recommendations = data.Articles, data.Recommendations
	if err != nil || len(latest) != 1 || len(recommendations) != 0 {
		t.Fatal("single article must not recommend itself", err)
	}
	old := model.Article{AuthorID: &editor.ID, Slug: "old", Title: "Old", Content: "Body", TemplateID: solitudes.ArticleTemplateID, CreatedAt: now.Add(-time.Hour)}
	ownPrivate := model.Article{AuthorID: &editor.ID, Slug: "own-private", Title: "Private", Content: "Private", TemplateID: solitudes.ArticleTemplateID, Visibility: model.VisibilityPrivate, ReadNum: 99999, CreatedAt: now.Add(time.Hour)}
	otherPrivate := model.Article{AuthorID: &admin.ID, Slug: "other-private", Title: "Private", Content: "Private", TemplateID: solitudes.ArticleTemplateID, Visibility: model.VisibilityPrivate, ReadNum: 999999, CreatedAt: now.Add(2 * time.Hour)}
	for _, article := range []*model.Article{&old, &ownPrivate, &otherPrivate} {
		if err := db.Create(article).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, account := range []*model.Account{nil, {Role: model.RoleUser}, &editor, &admin} {
		data, err := queryArticleFixture(db, account)
		latest, recommendations := data.Articles, data.Recommendations
		if err != nil {
			t.Fatal(err)
		}
		wantCount, wantLead := 2, lead.ID
		if account != nil && account.Role == model.RoleEditor {
			wantCount, wantLead = 3, ownPrivate.ID
		}
		if account != nil && account.Role == model.RoleAdmin {
			wantCount, wantLead = 4, otherPrivate.ID
		}
		if len(latest) != wantCount || latest[0].ID != wantLead || len(recommendations) != min(2, wantCount-1) {
			t.Fatalf("unexpected role selection: latest=%d recommendations=%d", len(latest), len(recommendations))
		}
		for _, article := range append(latest, recommendations...) {
			if !article.Public() && (account == nil || (!account.Role.IsAdmin() && (account.Role != model.RoleEditor || article.Author.ID != account.ID))) {
				t.Fatalf("private article leaked: %s", article.Slug)
			}
		}
		if wantCount == 2 && recommendations[0].ID != old.ID {
			t.Fatal("sparse recommendations should reuse a recent entry, not leave an empty column")
		}
	}
}

// Test-only composition matching the templates. There is no home/recommendation
// query in production: template authors choose filters, sizes and random picks.
type articleQueryFixture struct{ Articles, MostRead, Recommendations []model.Article }

func queryArticleFixture(db *gorm.DB, account *model.Account) (articleQueryFixture, error) {
	q := &TemplateQueries{db: db, account: account}
	var d articleQueryFixture
	var err error
	d.Articles, err = q.Articles("newest", 16, "without_tag", "Topic")
	if err != nil || len(d.Articles) == 0 {
		return d, err
	}
	d.MostRead, err = q.Articles("reads", 3, "without_tag", "Topic", "template", "1")
	if err != nil {
		return d, err
	}
	pool, err := q.Articles("reads", 10, "without_tag", "Topic", "template", "1", "exclude", d.Articles[0].ID)
	if err != nil {
		return d, err
	}
	indices, _ := randomIndices(len(pool), 2)
	for _, i := range indices {
		d.Recommendations = append(d.Recommendations, pool[i])
	}
	return d, nil
}
