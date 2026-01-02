package notify

import (
	"errors"
	"fmt"
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
		"greeting":     "Hi there! 👋",
		"new_reply":    "💬 Someone replied to your comment!",
		"view_article": "Want to continue the conversation? 👇",
		"button_text":  "View & Reply",
		"copyright":    "Copyright © {0}. All rights reserved.",
		"subject":      "💬 New reply to your comment on \"{0}\"",
	}

	// Chinese texts (enhanced with emojis)
	if lang == "zh" {
		texts["greeting"] = "Hi~ 👋"
		texts["new_reply"] = "💬 有人回复了你的评论！"
		texts["view_article"] = "想要继续交流？👇"
		texts["button_text"] = "查看并回复"
		texts["subject"] = "💬 你在「{0}」收到新回复啦"
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
	// Use tracking redirect URL with only comment ID
	articleURL := buildTrackingRedirectURL(src.ID, domain)
	email := hermes.Email{
		Body: hermes.Body{
			Name: dist.Nickname,
			Intros: []string{
				texts["new_reply"],
				"",
				dist.Nickname + ": " + dist.Content,
				"",
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

	// Add tracking pixel to email body (disguised as spacer image)
	trackingPixelURL := buildTrackingPixelURL(src.ID, domain)
	// Use more natural HTML that doesn't look like tracking
	trackingPixel := fmt.Sprintf(`<img src="%s" alt="" style="width:1px;height:1px;border:0;" />`, trackingPixelURL)
	// Insert near the end but not at </body> to look more natural
	emailBody = strings.Replace(emailBody, "</body>", trackingPixel+"</body>", 1)

	m := gomail.NewMessage()
	m.SetHeader("From", solitudes.System.Config.Email.User)
	m.SetHeader("To", dist.Email)
	m.SetHeader("Subject", replaceString(texts["subject"], article.Title))
	m.SetHeader("Content-Language", lang)
	m.SetBody("text/html", emailBody)

	return sendEmail(m)
}

// buildArticleURL constructs the article URL with optional tracking pixel
func buildArticleURL(slug, domain string) string {
	return "https://" + domain + "/" + slug
}

// buildTrackingRedirectURL constructs a tracking redirect URL
// This provides more reliable tracking than pixel-only approach
// Only requires comment ID, the article slug will be looked up from database
func buildTrackingRedirectURL(commentID, domain string) string {
	return fmt.Sprintf("https://%s/r/%s", domain, commentID)
}

// buildTrackingPixelURL constructs the tracking pixel URL for email read tracking
// Disguised as a static resource to avoid being blocked by email clients
func buildTrackingPixelURL(commentID, domain string) string {
	// Use a less obvious path that looks like a regular static resource
	return fmt.Sprintf("https://%s/static/i/%s.gif", domain, commentID)
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
