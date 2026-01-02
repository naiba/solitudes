package notify

import (
	"errors"
	"strings"
	"unicode"

	"github.com/matcornic/hermes/v2"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"gopkg.in/gomail.v2"
)

// getDefaultLogoURL returns the default logo URL with cache busting
func getDefaultLogoURL(domain string) string {
	return "https://" + domain + "/static/cactus/images/logo.png?20211213"
}

// getEmailTexts returns email texts based on language
func getEmailTexts(lang string) map[string]string {
	texts := map[string]string{
		"greeting":     "Hi",
		"new_reply":    "💬 Your comment got a new reply",
		"original":     "Your comment:",
		"reply":        "Reply:",
		"view_article": "Click to view and reply:",
		"button_text":  "View Article",
		"copyright":    "Copyright © {0}. All rights reserved.",
		"subject":      "Your comment on \"{0}\" got a reply",
	}

	// Chinese texts (enhanced with emojis)
	if lang == "zh" {
		texts["greeting"] = "你好"
		texts["new_reply"] = "💬 你的评论收到了新回复"
		texts["original"] = "你的评论："
		texts["reply"] = "回复内容："
		texts["view_article"] = "点击查看并回复："
		texts["button_text"] = "查看文章"
		texts["subject"] = "你在「{0}」的评论有新回复"
	}

	return texts
}

// detectLanguage detects user language preference from comment content
func detectLanguage(dist *model.Comment) string {
	// First, try to detect from comment content
	if hasChineseCharacters(dist.Content) {
		return "zh"
	}
	// Fall back to UserAgent detection
	if len(dist.UserAgent) > 0 {
		if hasChineseIndicators(dist.UserAgent) {
			return "zh"
		}
	}
	// Default to English
	return "en"
}

// hasChineseCharacters checks if content contains Chinese characters using unicode.Han
func hasChineseCharacters(content string) bool {
	for _, r := range content {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// hasChineseIndicators checks if UserAgent indicates Chinese locale
func hasChineseIndicators(ua string) bool {
	chineseKeywords := []string{
		"zh-CN",
		"zh_CN",
		"zh-Hans",
		"zh-Hant",
		"zh-TW",
		"zh_TW",
		"Chinese",
	}
	for _, keyword := range chineseKeywords {
		if strings.Contains(ua, keyword) {
			return true
		}
	}
	return false
}

// Email notify with language support
func Email(src, dist *model.Comment, article *model.Article) error {
	if dist == nil || dist.Email == "" {
		return errors.New("recipient comment or email not found")
	}
	if dist.Email == src.Email {
		return errors.New("cannot notify: same email address")
	}
	if dist.IsAdmin {
		return errors.New("skip notification for admin replies")
	}

	// Detect user language
	lang := detectLanguage(dist)
	texts := getEmailTexts(lang)

	domain := solitudes.System.Config.Site.Domain
	articleURL := buildArticleURL(article.Slug, domain)
	email := hermes.Email{
		Body: hermes.Body{
			Name: dist.Nickname,
			Intros: []string{
				texts["new_reply"],
				"",
				texts["original"],
				dist.Nickname + ": " + dist.Content,
				"",
				texts["reply"],
				src.Nickname + ": " + src.Content,
			},
			Actions: []hermes.Action{
				{
					Instructions: texts["view_article"],
					Button: hermes.Button{
						Color: "#22BC66",
						Text:  texts["button_text"],
						Link:  articleURL,
					},
				},
			},
		},
	}

	h := hermes.Hermes{
		Product: hermes.Product{
			Name:      solitudes.System.Config.Site.SpaceName,
			Link:      "https://" + domain,
			Logo:      getDefaultLogoURL(domain),
			Copyright: texts["copyright"],
		},
	}

	emailBody, err := h.GenerateHTML(email)
	if err != nil {
		return err
	}

	m := gomail.NewMessage()
	m.SetHeader("From", solitudes.System.Config.Email.User)
	m.SetHeader("To", dist.Email)
	m.SetHeader("Subject", replaceString(texts["subject"], article.Title))
	m.SetHeader("Content-Language", lang)
	m.SetBody("text/html", emailBody)

	return sendEmail(m)
}

// buildArticleURL constructs the article URL
func buildArticleURL(slug, domain string) string {
	return "https://" + domain + "/" + slug
}

// sendEmail sends the email message
func sendEmail(m *gomail.Message) error {
	sender := gomail.NewDialer(
		solitudes.System.Config.Email.Host,
		solitudes.System.Config.Email.Port,
		solitudes.System.Config.Email.User,
		solitudes.System.Config.Email.Pass,
	)
	sender.SSL = solitudes.System.Config.Email.SSL
	return sender.DialAndSend(m)
}

// replaceString replaces {0} placeholder with value
func replaceString(text, value string) string {
	return strings.ReplaceAll(text, "{0}", value)
}
