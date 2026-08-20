package router

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/blevesearch/bleve/v2"
	blevesearch "github.com/blevesearch/bleve/v2/search"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

const (
	maxSearchCandidates = 100
	maxSearchResults    = 10
)

type searchResp struct {
	model.ArticleIndex
	Content string
}

func buildSearchQuery(keywords string) blevequery.Query {
	publicFilter := bleve.NewBoolFieldQuery(false)
	publicFilter.SetField("IsPrivate")
	publicQuery := bleve.NewConjunctionQuery(
		bleve.NewMatchQuery(keywords),
		publicFilter,
	)

	privateFilter := bleve.NewBoolFieldQuery(true)
	privateFilter.SetField("IsPrivate")
	privateTitleQuery := bleve.NewMatchQuery(keywords)
	privateTitleQuery.SetField("Title")
	privateQuery := bleve.NewConjunctionQuery(privateTitleQuery, privateFilter)

	legacyPublicFilter := bleve.NewBoolFieldQuery(false)
	legacyPublicFilter.SetField("IsPrivate")
	legacyPrivateFilter := bleve.NewBoolFieldQuery(true)
	legacyPrivateFilter.SetField("IsPrivate")
	privacyFieldPresent := bleve.NewDisjunctionQuery(legacyPublicFilter, legacyPrivateFilter)
	legacyQuery := bleve.NewBooleanQuery()
	legacyQuery.AddMust(bleve.NewMatchQuery(keywords))
	legacyQuery.AddMustNot(privacyFieldPresent)

	return bleve.NewDisjunctionQuery(publicQuery, privateQuery, legacyQuery)
}

func articleIDFromIndexID(indexID string) string {
	separator := strings.LastIndexByte(indexID, '.')
	if separator <= 0 || separator == len(indexID)-1 {
		return ""
	}
	if _, err := strconv.ParseUint(indexID[separator+1:], 10, 64); err != nil {
		return ""
	}
	return indexID[:separator]
}

func buildSearchResponses(hits blevesearch.DocumentMatchCollection, articles map[string]model.Article) []searchResp {
	results := make([]searchResp, 0, maxSearchResults)
	seenArticleIDs := make(map[string]struct{})
	for _, hit := range hits {
		articleID := articleIDFromIndexID(hit.ID)
		article, found := articles[articleID]
		if !found {
			continue
		}
		if _, seen := seenArticleIDs[articleID]; seen {
			continue
		}

		content := ""
		if article.IsPrivate {
			// Old indexes may still contain a private body. Only accept a hit
			// that actually matched the title, and never return its fragments.
			if len(hit.Fragments["Title"]) == 0 {
				continue
			}
		} else {
			content = strings.Join(hit.Fragments["Content"], "\n")
		}

		seenArticleIDs[articleID] = struct{}{}
		results = append(results, searchResp{
			ArticleIndex: model.ArticleIndex{
				Slug:    article.Slug,
				Version: float64(article.Version),
				Title:   article.Title,
			},
			Content: content,
		})
		if len(results) == maxSearchResults {
			break
		}
	}
	return results
}

func search(c *fiber.Ctx) error {
	keywords := strings.TrimSpace(c.Query("w"))
	var result []searchResp
	if keywords != "" {
		query := buildSearchQuery(keywords)
		searchRequest := bleve.NewSearchRequestOptions(query, maxSearchCandidates, 0, false)
		searchRequest.Highlight = bleve.NewHighlight()
		searchResult, err := solitudes.System.Search.Search(searchRequest)
		if err != nil {
			log.Printf("failed to perform search: %v", err)
			return fiber.ErrInternalServerError
		}

		articleIDs := make([]string, 0, len(searchResult.Hits))
		seenArticleIDs := make(map[string]struct{})
		for _, hit := range searchResult.Hits {
			articleID := articleIDFromIndexID(hit.ID)
			if articleID == "" {
				continue
			}
			if _, seen := seenArticleIDs[articleID]; seen {
				continue
			}
			seenArticleIDs[articleID] = struct{}{}
			articleIDs = append(articleIDs, articleID)
		}

		var articles []model.Article
		if len(articleIDs) > 0 {
			if err := solitudes.System.DB.Select("id", "slug", "version", "title", "is_private").
				Where("id IN ?", articleIDs).Find(&articles).Error; err != nil {
				return fmt.Errorf("failed to validate search result visibility: %w", err)
			}
		}
		articlesByID := make(map[string]model.Article, len(articles))
		for i := range articles {
			articlesByID[articles[i].ID] = articles[i]
		}
		result = buildSearchResponses(searchResult.Hits, articlesByID)
	}

	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	return c.Status(http.StatusOK).Render("site/search", injectSiteData(c, fiber.Map{
		"title":   tr.T("search_result_title", "#SOL.9527.WORD#"),
		"results": result,
		"noindex": true,
	}))
}
