package notify

import (
	"errors"

	"github.com/matcornic/hermes"
	"github.com/naiba/solitudes"
	"gopkg.in/gomail.v2"
)

var h = hermes.Hermes{
	Product: hermes.Product{
		Name: solitudes.System.Config.SpaceName,
		Link: solitudes.System.Config.Web.Domain,
		Logo: "https://" + solitudes.System.Config.Web.Domain + "/static/cactus/images/logo.png",
	},
}
var sender = gomail.NewPlainDialer(solitudes.System.Config.Email.Host,
	solitudes.System.Config.Email.Port, solitudes.System.Config.Email.User,
	solitudes.System.Config.Email.Pass)

//Email notify
func Email(src, dist *solitudes.Comment, article *solitudes.Article) error {
	if src.ReplyTo == nil {
		return errors.New("不是回复评论，无需邮件通知")
	}
	email := hermes.Email{
		Body: hermes.Body{
			Name: dist.Nickname,
			Actions: []hermes.Action{
				{
					Instructions: "To view the article click this:",
					Button: hermes.Button{
						Color: "#22BC66", // Optional action button color
						Text:  "View comment",
						Link:  "https://" + solitudes.System.Config.Web.Domain + "/" + article.Slug,
					},
				},
			},
		},
	}
	emailBody, err := h.GenerateHTML(email)
	if err != nil {
		return err
	}
	m := gomail.NewMessage()
	m.SetHeader("From", solitudes.System.Config.Email.User)
	m.SetHeader("To", dist.Email)
	m.SetHeader("Subject", "Your comment in "+article.Title+" got a reply")
	m.SetBody("text/html", emailBody)
	return sender.DialAndSend(m)
}
