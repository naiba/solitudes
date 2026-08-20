package router

import (
	"testing"

	"github.com/blevesearch/bleve/v2"
	blevesearch "github.com/blevesearch/bleve/v2/search"

	"github.com/naiba/solitudes/internal/model"
)

func TestMaskPrivateArticleContent(t *testing.T) {
	tests := []struct {
		name        string
		article     model.Article
		authorized  bool
		wantContent string
		wantTOC     bool
	}{
		{
			name: "anonymous private article",
			article: model.Article{
				IsPrivate: true,
				Content:   "secret body",
				Toc:       []*model.ArticleTOC{{Title: "secret heading"}},
			},
			wantContent: privateArticleContent,
		},
		{
			name: "authorized private article",
			article: model.Article{
				IsPrivate: true,
				Content:   "secret body",
				Toc:       []*model.ArticleTOC{{Title: "secret heading"}},
			},
			authorized:  true,
			wantContent: "secret body",
			wantTOC:     true,
		},
		{
			name: "public article",
			article: model.Article{
				Content: "public body",
				Toc:     []*model.ArticleTOC{{Title: "public heading"}},
			},
			wantContent: "public body",
			wantTOC:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			maskPrivateArticleContent(&test.article, test.authorized)
			if test.article.Content != test.wantContent {
				t.Fatalf("content = %q, want %q", test.article.Content, test.wantContent)
			}
			if got := len(test.article.Toc) > 0; got != test.wantTOC {
				t.Fatalf("TOC present = %t, want %t", got, test.wantTOC)
			}
		})
	}
}

func TestBuildSearchQueryLimitsPrivateDocumentsToTitle(t *testing.T) {
	index, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		t.Fatalf("failed to create test index: %v", err)
	}
	defer index.Close()

	documents := map[string]interface{}{
		"private.1": struct {
			Title     string
			Content   string
			IsPrivate bool
		}{Title: "Confidential Roadmap", Content: "legacyprivatebodytoken", IsPrivate: true},
		"public.1": struct {
			Title     string
			Content   string
			IsPrivate bool
		}{Title: "Public Roadmap", Content: "publicbodytoken", IsPrivate: false},
		"legacy-public.1": struct {
			Title   string
			Content string
		}{Title: "Legacy Public", Content: "legacypublicbodytoken"},
	}
	for id, document := range documents {
		if err := index.Index(id, document); err != nil {
			t.Fatalf("failed to index %s: %v", id, err)
		}
	}

	privateBodyResult, err := index.Search(bleve.NewSearchRequest(buildSearchQuery("legacyprivatebodytoken")))
	if err != nil {
		t.Fatalf("failed to search legacy private body: %v", err)
	}
	if privateBodyResult.Total != 0 {
		t.Fatalf("legacy private body matched %d document(s), want 0", privateBodyResult.Total)
	}

	privateTitleResult, err := index.Search(bleve.NewSearchRequest(buildSearchQuery("confidential")))
	if err != nil {
		t.Fatalf("failed to search private title: %v", err)
	}
	if privateTitleResult.Total != 1 || privateTitleResult.Hits[0].ID != "private.1" {
		t.Fatalf("private title result = %+v, want private.1", privateTitleResult.Hits)
	}

	publicBodyResult, err := index.Search(bleve.NewSearchRequest(buildSearchQuery("publicbodytoken")))
	if err != nil {
		t.Fatalf("failed to search public body: %v", err)
	}
	if publicBodyResult.Total != 1 || publicBodyResult.Hits[0].ID != "public.1" {
		t.Fatalf("public body result = %+v, want public.1", publicBodyResult.Hits)
	}

	legacyPublicResult, err := index.Search(bleve.NewSearchRequest(buildSearchQuery("legacypublicbodytoken")))
	if err != nil {
		t.Fatalf("failed to search legacy public body: %v", err)
	}
	if legacyPublicResult.Total != 1 || legacyPublicResult.Hits[0].ID != "legacy-public.1" {
		t.Fatalf("legacy public body result = %+v, want legacy-public.1", legacyPublicResult.Hits)
	}
}

func TestBuildSearchQueryTreatsSpecialCharactersAsText(t *testing.T) {
	index, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		t.Fatalf("failed to create test index: %v", err)
	}
	defer index.Close()

	if err := index.Index("public.1", struct {
		Title     string
		Content   string
		IsPrivate bool
	}{Title: "Search syntax", Content: "ordinary content"}); err != nil {
		t.Fatalf("failed to index document: %v", err)
	}

	for _, keywords := range []string{`"`, `(`, `title:`, `foo AND (`, `[a TO z]`} {
		t.Run(keywords, func(t *testing.T) {
			if _, err := index.Search(bleve.NewSearchRequest(buildSearchQuery(keywords))); err != nil {
				t.Fatalf("special-character search returned an error: %v", err)
			}
		})
	}
}

func TestBuildSearchResponsesRedactsUsingCurrentDatabaseState(t *testing.T) {
	hits := blevesearch.DocumentMatchCollection{
		{
			ID: "private-content.1",
			Fragments: blevesearch.FieldFragmentMap{
				"Content": {"legacy secret body"},
			},
		},
		{
			ID: "private-title.2",
			Fragments: blevesearch.FieldFragmentMap{
				"Title":   {"<mark>Private</mark> title"},
				"Content": {"must not be returned"},
			},
		},
		{
			ID: "public.1",
			Fragments: blevesearch.FieldFragmentMap{
				"Content": {"public <mark>body</mark>"},
			},
		},
	}
	articles := map[string]model.Article{
		"private-content": {ID: "private-content", Title: "Private content", Slug: "private-content", Version: 1, IsPrivate: true},
		"private-title":   {ID: "private-title", Title: "Private title", Slug: "private-title", Version: 2, IsPrivate: true},
		"public":          {ID: "public", Title: "Public", Slug: "public", Version: 1},
	}

	results := buildSearchResponses(hits, articles)
	if len(results) != 2 {
		t.Fatalf("got %d result(s), want 2: %+v", len(results), results)
	}
	if results[0].Title != "Private title" || results[0].Content != "" {
		t.Fatalf("private result was not redacted: %+v", results[0])
	}
	if results[1].Title != "Public" || results[1].Content != "public <mark>body</mark>" {
		t.Fatalf("public result changed unexpectedly: %+v", results[1])
	}
}

func TestArticleIDFromIndexID(t *testing.T) {
	tests := map[string]string{
		"article-id.12": "article-id",
		"missing":       "",
		"article.bad":   "",
		".1":            "",
	}
	for input, want := range tests {
		if got := articleIDFromIndexID(input); got != want {
			t.Errorf("articleIDFromIndexID(%q) = %q, want %q", input, got, want)
		}
	}
}
