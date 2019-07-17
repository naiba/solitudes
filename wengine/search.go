package wengine

import (
	"net/http"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"

	"github.com/gin-gonic/gin"
	"github.com/go-ego/riot/types"
)

type searchResp struct {
	solitudes.ArticleIndex
	Content string
}

func search(c *gin.Context) {
	keywords := c.Query("w")
	sea := solitudes.System.Search.Search(types.SearchReq{
		Text: keywords,
		RankOpts: &types.RankOpts{
			OutputOffset: 0,
			MaxOutputs:   20,
		}})

	var articles []types.ScoredDoc
	if sea.Docs != nil {
		articles = sea.Docs.(types.ScoredDocs)
	}
	var result []searchResp
	for _, v := range articles {
		item := v.Attri.(solitudes.ArticleIndex)
		if len([]rune(v.Content)) > 200 {
			v.Content = string([]rune(v.Content)[:200])
		}
		result = append(result, searchResp{
			item, v.Content,
		})
	}

	c.HTML(http.StatusOK, "default/search", soligin.Soli(c, true, gin.H{
		"title":   c.MustGet(solitudes.CtxTranslator).(*solitudes.Translator).T("search_result_title", c.Query("w")),
		"results": result,
	}))
}
