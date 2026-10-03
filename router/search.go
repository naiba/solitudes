package router

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

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
		if !article.Public() {
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
	}
	return results
}

func search(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.UserContext(), 5*time.Second)
	defer cancel()
	page, err := listPage(c.Query("page"))
	if err != nil {
		return err
	}
	keywords := strings.TrimSpace(c.Query("w"))
	if len(keywords) > 300 {
		return fiber.ErrBadRequest
	}
	more := false
	var result []searchResp
	if keywords != "" {
		query := buildSearchQuery(keywords)
		seen := make(map[string]bool)
		skip := (page - 1) * maxSearchResults
		batchSize := min(maxSearchCandidates, skip+maxSearchResults+1)
		for offset := 0; ; {
			if ctx.Err() != nil {
				return fiber.ErrServiceUnavailable
			}
			searchRequest := bleve.NewSearchRequestOptions(query, batchSize, offset, false)
			searchRequest.SortBy([]string{"-_score", "_id"})
			// Persisted snippets are never trusted; only build highlights from
			// the live, permission-checked candidate index below.
			searchResult, err := solitudes.System.Search.SearchInContext(ctx, searchRequest)
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
				if err := solitudes.System.DB.WithContext(ctx).Select("id", "slug", "version", "title", "content", "visibility", "author_id").
					Where("id IN ?", articleIDs).Find(&articles).Error; err != nil {
					return fmt.Errorf("failed to validate search result visibility: %w", err)
				}
			}
			// Never trust persisted snippets after a policy edit or failed index
			// update. Re-match the bounded candidate set against current public
			// bodies; old revisions and restricted blocks cannot cause a hit.
			articlesByID := make(map[string]model.Article, len(articles))
			live, err := func() (*bleve.SearchResult, error) {
				liveIndex, err := bleve.NewMemOnly(solitudes.System.Search.Mapping())
				if err != nil {
					return nil, err
				}
				defer liveIndex.Close()
				batch := liveIndex.NewBatch()
				for i := range articles {
					if !articles[i].CanRead(currentAccount(c)) {
						continue
					}
					articlesByID[articles[i].ID] = articles[i]
					if err := batch.Index(articles[i].GetIndexID(), map[string]interface{}{
						"Title": articles[i].Title, "Content": articles[i].ContentFor(nil, nil), "IsPrivate": !articles[i].Public(),
					}); err != nil {
						return nil, err
					}
				}
				if err := liveIndex.Batch(batch); err != nil {
					return nil, err
				}
				liveRequest := bleve.NewSearchRequestOptions(query, maxSearchCandidates, 0, false)
				liveRequest.Highlight = bleve.NewHighlight()
				return liveIndex.SearchInContext(ctx, liveRequest)
			}()
			if err != nil {
				return err
			}
			valid := make(map[string]searchResp)
			for _, item := range buildSearchResponses(live.Hits, articlesByID) {
				valid[item.Slug] = item
			}
			// Keep the stable original ranking, but only return currently readable,
			// re-matched articles. Skip duplicates across revision/candidate batches.
			for _, hit := range searchResult.Hits {
				id := articleIDFromIndexID(hit.ID)
				article, exists := articlesByID[id]
				item, matches := valid[article.Slug]
				if !exists || !matches || seen[id] {
					continue
				}
				seen[id] = true
				if skip > 0 {
					skip--
					continue
				}
				result = append(result, item)
				if len(result) > maxSearchResults {
					break
				}
			}
			if len(result) > maxSearchResults {
				more = true
				result = result[:maxSearchResults]
				break
			}
			offset += len(searchResult.Hits)
			if offset >= int(searchResult.Total) || len(searchResult.Hits) == 0 {
				break
			}
			// Start with just enough for the page and its next-page sentinel;
			// expand only when permission checks or stale hits require more work.
			batchSize = min(maxSearchCandidates, batchSize*2)
		}
	}

	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	return c.Status(http.StatusOK).Render("site/search", injectSiteData(c, fiber.Map{
		"title":      tr.T("search_result_title", "#SOL.9527.WORD#"),
		"results":    result,
		"noindex":    true,
		"navigation": pageNavigationFor(c, "page", page, more, "search"),
	}))
}
