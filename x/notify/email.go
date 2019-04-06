package notify

import (
	"errors"

	"github.com/matcornic/hermes"
	"github.com/naiba/solitudes"
	"gopkg.in/gomail.v2"
)

var h = hermes.Hermes{
	Product: hermes.Product{
		Name:      solitudes.System.Config.SpaceName,
		Link:      solitudes.System.Config.Web.Domain,
		Logo:      "https://" + solitudes.System.Config.Web.Domain + "/static/cactus/images/logo.png",
		Copyright: "Copyright © " + solitudes.System.Config.SpaceName + ". All rights reserved.",
	},
}
var sender = gomail.NewPlainDialer(solitudes.System.Config.Email.Host,
	solitudes.System.Config.Email.Port, solitudes.System.Config.Email.User,
	solitudes.System.Config.Email.Pass, solitudes.System.Config.Email.SSL)

//Email notify
func Email(src, dist *solitudes.Comment, article *solitudes.Article) error {
	if dist == nil || dist.Email == "" {
		return errors.New("Not replying to a comment or being replied to a person who does not leave a mailbox, without email notification")
	}
	if dist.Email == src.Email {
		return errors.New("Same email from src to dist")
	}
	if dist.IsAdmin {
		return errors.New("Reply to the administrator without notification")
	}
	email := hermes.Email{
		Body: hermes.Body{
			Name: dist.Nickname,
			Intros: []string{
				dist.Nickname + ":" + dist.Content,
				src.Nickname + ":" + src.Content,
			},
			Actions: []hermes.Action{
				{
					Instructions: "View the article:",
					Button: hermes.Button{
						Color: "#22BC66", // Optional action button color
						Text:  "Open",
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
	m.SetHeader("Subject", "Comment in ["+article.Title+"] got new reply")
	m.SetBody("text/html", emailBody)
	return sender.DialAndSend(m)
}
