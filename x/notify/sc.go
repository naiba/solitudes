package notify

import (
	"net/http"
	"net/url"

	"github.com/naiba/solitudes"
)

//ServerChain Server酱推送
func ServerChain(comment *solitudes.Comment, article *solitudes.Article, err error) {
	params := url.Values{"text": {article.Title + " got a new comment"}, "desp": {`
	### Comment detail
	
	- Article:` + article.Title + `
	- Author:` + comment.Nickname + `(` + comment.Email + `)
	- Content:` + comment.Content + `

	### Email notify

	` + err.Error()}}
	http.PostForm("https://sc.ftqq.com/"+solitudes.System.Config.ServerChain+".send", params)
}
