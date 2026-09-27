package router

import (
	"gorm.io/gorm"

	"github.com/naiba/solitudes/internal/model"
)

const privateArticleContent = "Private Article"

func maskPrivateArticleContent(article *model.Article, authorized bool) {
	if article == nil || !article.IsPrivate || authorized {
		return
	}
	article.Content = privateArticleContent
	article.Toc = nil
}

func canReadArticle(account *model.Account, article *model.Article) bool {
	if article == nil {
		return false
	}
	if !article.IsPrivate {
		return true
	}
	return account != nil && (account.Role.IsAdmin() || account.Role.CanPublish() && article.AuthorID != nil && *article.AuthorID == account.ID)
}

func readableArticles(db *gorm.DB, account *model.Account) *gorm.DB {
	if account != nil && account.Role.IsAdmin() {
		return db
	}
	if account != nil && account.Role.CanPublish() {
		return db.Where("(is_private = false OR author_id = ?)", account.ID)
	}
	return db.Where("is_private = false")
}
