package notify

import (
	"errors"

	"github.com/matcornic/hermes/v2"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"gopkg.in/gomail.v2"
)

//Email notify
func Email(src, dist *model.Comment, article *model.Article) error {
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
						Link:  "https://" + solitudes.System.Config.Site.Domain + "/" + article.Slug,
					},
				},
			},
		},
	}

	var h = hermes.Hermes{
		Product: hermes.Product{
			Name:      solitudes.System.Config.Site.SpaceName,
			Link:      solitudes.System.Config.Site.Domain,
			Logo:      "https://" + solitudes.System.Config.Site.Domain + "/static/cactus/images/logo.png",
			Copyright: "Copyright © " + solitudes.System.Config.Site.SpaceName + ". All rights reserved.",
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

	var sender = gomail.NewDialer(solitudes.System.Config.Email.Host,
		solitudes.System.Config.Email.Port, solitudes.System.Config.Email.User,
		solitudes.System.Config.Email.Pass)
	sender.SSL = solitudes.System.Config.Email.SSL

	return sender.DialAndSend(m)
}
