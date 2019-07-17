package wengine

import (
	"log"

	"github.com/naiba/solitudes"

	"github.com/gin-gonic/gin"
	"github.com/go-ego/riot/types"
)

type searchResp struct {
	solitudes.ArticleIndex
	Match map[string]string
}

func search(c *gin.Context) {
	keywords := c.Query("w")

	sea := solitudes.System.Search.Search(types.SearchReq{
		Text: keywords,
		RankOpts: &types.RankOpts{
			OutputOffset: 0,
			MaxOutputs:   20,
		}})

	log.Println(sea)
	log.Println(solitudes.System.Search.NumIndexed())

	// var articleIndex = make(map[string]struct {
	// 	Version uint64
	// 	Index   int
	// })
	// var result []searchResp
	// for _, v := range sea.Docs {
	// 	d, err := solitudes.System.Search.Document(v.ID)
	// 	if err == nil {
	// 		var r searchResp
	// 		for _, f := range d.Fields {
	// 			switch f.Name() {
	// 			case "Slug":
	// 				r.Slug = string(f.Value())
	// 			case "Title":
	// 				r.Title = string(f.Value())
	// 			case "Version":
	// 				r.Version = string(f.Value())
	// 			}
	// 		}
	// 		r.Match = make(map[string]string)
	// 		for k, v := range v.Fragments {
	// 			var t = ""
	// 			for _, innerV := range v {
	// 				t += innerV + ","
	// 			}
	// 			var l int
	// 			if len(t) > 100 {
	// 				l = 100
	// 			}
	// 			t = t[:l]
	// 			r.Match[k] = t
	// 		}
	// 		intVersion, _ := strconv.ParseUint(r.Version, 10, 64)
	// 		// hide too old version article
	// 		if v, has := articleIndex[r.Slug]; has {
	// 			if intVersion > v.Version {
	// 				result[v.Index] = r
	// 			}
	// 			continue
	// 		}
	// 		articleIndex[r.Slug] = struct {
	// 			Version uint64
	// 			Index   int
	// 		}{
	// 			intVersion,
	// 			len(result),
	// 		}
	// 		result = append(result, r)
	// 	}
	// }
	// c.HTML(http.StatusOK, "default/search", soligin.Soli(c, true, gin.H{
	// 	"title":   c.MustGet(solitudes.CtxTranslator).(*solitudes.Translator).T("search_result_title", c.Query("w")),
	// 	"results": result,
	// }))
}
