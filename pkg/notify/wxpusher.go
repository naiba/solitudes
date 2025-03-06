package notify

import (
	wxpusher "github.com/wxpusher/wxpusher-sdk-go"
	wxpusherModel "github.com/wxpusher/wxpusher-sdk-go/model"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

// WxpusherNotify 微信推送
func WxpusherNotify(comment *model.Comment, article *model.Article, err error) {
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
	content := `### ` + article.Title + ` got a new comment

- Article:` + article.Title + `  
- Author:` + comment.Nickname + `(` + comment.Email + `)   
- Content:` + comment.Content + errmsg

	msg := wxpusherModel.NewMessage(solitudes.System.Config.WxpusherAppToken)
	msg.SetSummary(solitudes.System.Config.Site.SpaceName + "::<<" + article.Title + ">> got a new reply from [" + comment.Nickname + "]")
	msg.SetContent(content)
	msg.SetContentType(3)
	msg.AddUId(solitudes.System.Config.WxpusherUID)
	wxpusher.SendMessage(msg)
}
