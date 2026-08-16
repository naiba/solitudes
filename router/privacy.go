package router

import "github.com/naiba/solitudes/internal/model"

const privateArticleContent = "Private Article"

func maskPrivateArticleContent(article *model.Article, authorized bool) {
	if article == nil || !article.IsPrivate || authorized {
		return
	}
	article.Content = privateArticleContent
	article.Toc = nil
}
