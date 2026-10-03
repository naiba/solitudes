package router

import (
	"net/url"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

const privateArticleContent = "Private Article"

func maskPrivateArticleContent(article *model.Article, authorized bool) {
	if article == nil || article.Public() || authorized {
		return
	}
	article.Content = privateArticleContent
	article.Toc = nil
}

func canReadArticle(account *model.Account, article *model.Article) bool {
	if article == nil {
		return false
	}
	return article.CanRead(account)
}

func readableArticles(db *gorm.DB, account *model.Account, notices ...func(string) string) *gorm.DB {
	notice := accessNotice
	if len(notices) > 0 && notices[0] != nil {
		notice = notices[0]
	}
	return model.ReadableArticles(db, account, notice)
}

func accessNoticeFor(c *fiber.Ctx) func(string) string {
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	returnTo := url.QueryEscape(c.OriginalURL())
	return func(level string) string {
		if level == "members" {
			return renderAccessNotice(tr.T("access_members", "/login?return_to="+returnTo, "/register?return_to="+returnTo))
		}
		return renderAccessNotice(tr.T("access_" + level))
	}
}

func renderAccessNotice(message string) string {
	return "<aside class=\"access-notice\" data-testid=\"access-notice\">" + luteEngine.MarkdownStr("", "🔒 "+message) + "</aside>\n"
}

func accessNotice(level string) string {
	switch level {
	case "members":
		return renderAccessNotice("[Sign in](/login) or [register](/register) with a verified email to read this section.")
	case "editors":
		return renderAccessNotice("This section is available to editors and administrators.")
	default:
		return renderAccessNotice("This section is available to the author and administrators.")
	}
}
