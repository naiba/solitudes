package wengine

import (
	"bytes"
	"net/http"

	"github.com/blevesearch/bleve"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"

	"github.com/gin-gonic/gin"
)

type searchResp struct {
	solitudes.ArticleIndex
	Content string
}

func search(c *gin.Context) {
	keywords := c.Query("w")

	query := bleve.NewQueryStringQuery(keywords)
	searchRequest := bleve.NewSearchRequest(query)
	searchRequest.Highlight = bleve.NewHighlight()
	searchRequest.Fields = []string{"Title", "Version", "Slug"}
	searchRequest.Explain = true
	searchResult, _ := solitudes.System.Search.Search(searchRequest)

	var result []searchResp
	for _, hit := range searchResult.Hits {
		item := solitudes.ArticleIndex{
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

	c.HTML(http.StatusOK, "default/search", soligin.Soli(c, true, gin.H{
		"title":   c.MustGet(solitudes.CtxTranslator).(*solitudes.Translator).T("search_result_title", c.Query("w")),
		"word":    c.Query("w"),
		"results": result,
	}))
}
