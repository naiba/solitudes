package notify

import (
	"net/http"
	"net/url"

	"github.com/naiba/solitudes"
)

//ServerChan Server酱推送
func ServerChan(comment *solitudes.Comment, article *solitudes.Article, err error) {
	// when err == nil skip admin
	if comment.IsAdmin && err == nil {
		return
	}
	var errmsg string
	if err != nil {
		errmsg = `

		### Email notify
		
		` + err.Error()
	}
	params := url.Values{"text": {article.Title + " got a new comment"}, "desp": {
		`### Comment detail

- Article:` + article.Title + `
- Author:` + comment.Nickname + `(` + comment.Email + `)
- Content:` + comment.Content + errmsg}}
	http.PostForm("https://sc.ftqq.com/"+solitudes.System.Config.ServerChain+".send", params)
}
