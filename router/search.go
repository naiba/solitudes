package router

import (
	"bytes"
	"net/http"

	"github.com/blevesearch/bleve/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

type searchResp struct {
	model.ArticleIndex
	Content string
}

func search(c *fiber.Ctx) error {
	keywords := c.Query("w")

	query := bleve.NewQueryStringQuery(keywords)
	searchRequest := bleve.NewSearchRequest(query)
	searchRequest.Highlight = bleve.NewHighlight()
	searchRequest.Fields = []string{"Title", "Version", "Slug"}
	searchRequest.Explain = true
	searchResult, _ := solitudes.System.Search.Search(searchRequest)

	var result []searchResp
	for _, hit := range searchResult.Hits {
		item := model.ArticleIndex{
			Slug:    hit.Fields["Slug"].(string),
			Version: hit.Fields["Version"].(float64),
			Title:   hit.Fields["Title"].(string),
		}
		content := bytes.NewBufferString("")
		for _, fragments := range hit.Fragments {
			for _, fragment := range fragments {
				content.WriteString(fragment + "\n")
			}
		}
		result = append(result, searchResp{
			item, content.String(),
		})
	}

	c.Status(http.StatusOK).Render("default/search", injectSiteData(c, fiber.Map{
		"title":   c.Locals(solitudes.CtxTranslator).(*translator.Translator).T("search_result_title", c.Query("w")),
		"word":    c.Query("w"),
		"results": result,
	}))
	return nil
}
