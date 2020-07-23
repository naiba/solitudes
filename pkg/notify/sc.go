package notify

import (
	"log"
	"net/http"
	"net/url"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

//ServerChan Server酱推送
func ServerChan(comment *model.Comment, article *model.Article, err error) {
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
	_, err = http.PostForm("https://sc.ftqq.com/"+solitudes.System.Config.ServerChan+".send", params)
	if err != nil {
		log.Println("http.PostForm", err)
	}
}
