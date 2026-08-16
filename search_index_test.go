package solitudes

import (
	"testing"

	"github.com/blevesearch/bleve/v2"

	"github.com/naiba/solitudes/internal/model"
)

func TestIndexArticleVersionOmitsPrivateContent(t *testing.T) {
	index, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		t.Fatalf("failed to create test index: %v", err)
	}
	defer index.Close()

	article := model.Article{
		ID:        "private-article",
		Slug:      "private-roadmap",
		Version:   1,
		Title:     "Confidential Roadmap",
		Content:   "privatebodytoken must never be indexed",
		IsPrivate: true,
	}
	if err := indexArticleVersion(index, &article, article.Version, article.Content); err != nil {
		t.Fatalf("failed to index private article: %v", err)
	}

	contentResult, err := index.Search(bleve.NewSearchRequest(bleve.NewMatchQuery("privatebodytoken")))
	if err != nil {
		t.Fatalf("failed to search private body: %v", err)
	}
	if contentResult.Total != 0 {
		t.Fatalf("private body matched %d document(s), want 0", contentResult.Total)
	}

	titleQuery := bleve.NewMatchQuery("confidential")
	titleQuery.SetField("Title")
	titleResult, err := index.Search(bleve.NewSearchRequest(titleQuery))
	if err != nil {
		t.Fatalf("failed to search private title: %v", err)
	}
	if titleResult.Total != 1 {
		t.Fatalf("private title matched %d document(s), want 1", titleResult.Total)
	}
}

func TestIndexArticleVersionKeepsPublicContentSearchable(t *testing.T) {
	index, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		t.Fatalf("failed to create test index: %v", err)
	}
	defer index.Close()

	article := model.Article{
		ID:      "public-article",
		Slug:    "public-roadmap",
		Version: 1,
		Title:   "Public Roadmap",
		Content: "publicbodytoken remains searchable",
	}
	if err := indexArticleVersion(index, &article, article.Version, article.Content); err != nil {
		t.Fatalf("failed to index public article: %v", err)
	}

	result, err := index.Search(bleve.NewSearchRequest(bleve.NewMatchQuery("publicbodytoken")))
	if err != nil {
		t.Fatalf("failed to search public body: %v", err)
	}
	if result.Total != 1 {
		t.Fatalf("public body matched %d document(s), want 1", result.Total)
	}
}

func TestIndexArticleRewritesAllPrivateVersionsWithoutContent(t *testing.T) {
	index, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		t.Fatalf("failed to create test index: %v", err)
	}
	defer index.Close()

	legacyDocument := articleSearchDocument{
		Slug:      "private-roadmap",
		Version:   1,
		Title:     "Confidential Roadmap",
		Content:   "legacyprivatebodytoken",
		IsPrivate: false,
	}
	if err := index.Index("private-article.1", legacyDocument); err != nil {
		t.Fatalf("failed to seed legacy document: %v", err)
	}

	article := model.Article{
		ID:        "private-article",
		Slug:      "private-roadmap",
		Version:   3,
		Title:     "Confidential Roadmap",
		Content:   "currentprivatebodytoken",
		IsPrivate: true,
	}
	if err := indexArticle(index, &article); err != nil {
		t.Fatalf("failed to update private article index: %v", err)
	}

	for _, token := range []string{"legacyprivatebodytoken", "currentprivatebodytoken"} {
		result, err := index.Search(bleve.NewSearchRequest(bleve.NewMatchQuery(token)))
		if err != nil {
			t.Fatalf("failed to search %q: %v", token, err)
		}
		if result.Total != 0 {
			t.Fatalf("private token %q matched %d document(s), want 0", token, result.Total)
		}
	}

	titleQuery := bleve.NewMatchQuery("confidential")
	titleQuery.SetField("Title")
	titleResult, err := index.Search(bleve.NewSearchRequest(titleQuery))
	if err != nil {
		t.Fatalf("failed to search private versions by title: %v", err)
	}
	if titleResult.Total != 3 {
		t.Fatalf("private title matched %d document(s), want 3", titleResult.Total)
	}
}
