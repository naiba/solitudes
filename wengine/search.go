package wengine

import (
	"net/http"
	"strconv"

	"github.com/blevesearch/bleve"
	"github.com/gin-gonic/gin"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"
)

type searchResp struct {
	solitudes.ArticleIndex
	Match map[string]string
}

func search(c *gin.Context) {
	keywords := c.Query("w")
	req := bleve.NewSearchRequest(bleve.NewQueryStringQuery(keywords))
	req.Highlight = bleve.NewHighlight()
	res, err := solitudes.System.Search.Search(req)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "default/error", soligin.Soli(c, true, gin.H{
			"title": "Search Engine Error",
			"msg":   err.Error(),
		}))
		return
	}
	var result []searchResp
	for _, v := range res.Hits {
		d, err := solitudes.System.Search.Document(v.ID)
		if err == nil {
			var r searchResp
			for _, f := range d.Fields {
				switch f.Name() {
				case "Slug":
					r.Slug = string(f.Value())
				case "Title":
					r.Title = string(f.Value())
				case "Version":
					t, _ := strconv.ParseUint(string(f.Value()), 10, 64)
					r.Version = uint(t)
				}
			}
			r.Match = make(map[string]string)
			for k, v := range v.Fragments {
				var t = ""
				for _, innerV := range v {
					t += innerV + ","
				}
				var l int
				if len(t) > 100 {
					l = 100
				}
				t = t[:l]
				r.Match[k] = t
			}
			result = append(result, r)
		}
	}
	c.HTML(http.StatusOK, "default/search", soligin.Soli(c, true, gin.H{
		"title":   "Search result for \"" + c.Query("w") + "\"",
		"results": result,
	}))
}
