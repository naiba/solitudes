package router

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

type baseSearchIndex interface{ bleve.Index }

type trackedSearchIndex struct {
	baseSearchIndex
	requests [][2]int
}

func (i *trackedSearchIndex) SearchInContext(ctx context.Context, request *bleve.SearchRequest) (*bleve.SearchResult, error) {
	i.requests = append(i.requests, [2]int{request.Size, request.From})
	return i.baseSearchIndex.SearchInContext(ctx, request)
}

func TestPostgresSearchAdaptiveBatchesPreservePrivacyAndPagination(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}); err != nil {
		t.Fatal(err)
	}
	idx, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	tracked := &trackedSearchIndex{baseSearchIndex: idx}
	for i := 0; i < 105; i++ {
		a := model.Article{ID: fmt.Sprintf("40000000-0000-4000-8000-%012d", i), Slug: fmt.Sprintf("candidate-%03d", i), Title: "Search case", Content: "searchneedle", Version: 2}
		if i < 60 {
			a.Content = "body no longer matches"
		}
		if i >= 60 && i < 80 {
			a.Visibility = model.VisibilityPrivate
		}
		if err := db.Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		// Deliberately stale content and stale visibility in the durable index.
		for _, version := range []int{1, 2} {
			if err := idx.Index(fmt.Sprintf("%s.%d", a.ID, version), map[string]interface{}{"Title": a.Title, "Content": "searchneedle", "IsPrivate": false}); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Chdir("..")
	previous, engine, translations := solitudes.System, globalDynamicEngine.engine, translator.Trans
	t.Cleanup(func() {
		solitudes.System, globalDynamicEngine.engine, translator.Trans = previous, engine, translations
	})
	conf := &model.Config{}
	conf.Site.Theme, conf.Admin.Theme = "cactus", "default"
	solitudes.System = &solitudes.SysVariable{DB: db, Config: conf, Search: tracked}
	if err := LoadTemplates(); err != nil {
		t.Fatal(err)
	}
	view := &paginationView{}
	app := fiber.New(fiber.Config{Views: view})
	app.Use(trans)
	app.Get("/search", search)
	seen := map[string]bool{}
	for page := 1; page <= 3; page++ {
		tracked.requests = nil
		resp, err := app.Test(httptest.NewRequest("GET", fmt.Sprintf("/search?w=searchneedle&page=%d", page), nil), -1)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status=%d", resp.StatusCode)
		}
		items := view.data["results"].([]searchResp)
		want := 10
		if page == 3 {
			want = 5
		}
		if len(items) != want {
			t.Fatalf("page %d has %d results", page, len(items))
		}
		for j, item := range items {
			if item.Slug != fmt.Sprintf("candidate-%03d", 80+(page-1)*10+j) || seen[item.Slug] {
				t.Fatalf("unstable, duplicate or private result: %s", item.Slug)
			}
			seen[item.Slug] = true
		}
		if (view.data["navigation"].(pageNavigation).Next != "") != (page < 3) {
			t.Fatal("incorrect next-page sentinel")
		}
		if tracked.requests[0] != [2]int{page*10 + 1, 0} || len(tracked.requests) < 3 {
			t.Fatalf("not adaptive: %v", tracked.requests)
		}
		for i, request := range tracked.requests {
			if request[0] > maxSearchCandidates {
				t.Fatal("unbounded batch")
			}
			if i > 0 && request[1] != tracked.requests[i-1][1]+tracked.requests[i-1][0] {
				t.Fatal("candidate offset skipped results")
			}
		}
	}
	if len(seen) != 25 {
		t.Fatal("lost visible articles")
	}
}
