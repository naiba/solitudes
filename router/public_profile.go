package router

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

const profilePageSize = 12

func profilePageNumber(c *fiber.Ctx, name string) (int, error) {
	return listPage(c.Query(name))
}

func publicUserProfile(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.ErrNotFound
	}
	articlePage, err := profilePageNumber(c, "articles_page")
	if err != nil {
		return err
	}
	commentPage, err := profilePageNumber(c, "comments_page")
	if err != nil {
		return err
	}
	var account model.Account
	if err := solitudes.System.DB.Select("id, nickname, bio, role, created_at").Take(&account, "id = ?", id.String()).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		return fmt.Errorf("load public profile: %w", err)
	}
	var articles []model.Article
	if err := solitudes.System.DB.Select("id, title, slug, created_at, is_book").
		Where("author_id = ? AND visibility = 'public'", account.ID).
		Order("created_at DESC, id DESC").Offset((articlePage - 1) * profilePageSize).
		Limit(profilePageSize + 1).Find(&articles).Error; err != nil {
		return fmt.Errorf("load public articles: %w", err)
	}
	articlesMore := len(articles) > profilePageSize
	if articlesMore {
		articles = articles[:profilePageSize]
	}
	var comments []model.Comment
	if err := solitudes.System.DB.Model(&model.Comment{}).
		Select("comments.id, comments.content, comments.article_id, comments.created_at, comments.version").
		Joins("JOIN articles ON articles.id = comments.article_id").
		Where("comments.account_id = ? AND comments.is_spam = false AND articles.visibility = 'public'", account.ID).
		Where(`NOT EXISTS (
			WITH RECURSIVE ancestors AS (
				SELECT parent.id, parent.reply_to, parent.is_spam FROM comments AS parent WHERE parent.id = comments.reply_to
				UNION ALL
				SELECT parent.id, parent.reply_to, parent.is_spam FROM comments AS parent
				JOIN ancestors ON ancestors.reply_to = parent.id
			)
			SELECT 1 FROM ancestors WHERE ancestors.is_spam = true
		)`).
		Preload("Article", func(db *gorm.DB) *gorm.DB { return db.Select("id, slug, title") }).
		Order("comments.created_at DESC, comments.id DESC").Offset((commentPage - 1) * profilePageSize).
		Limit(profilePageSize + 1).Find(&comments).Error; err != nil {
		return fmt.Errorf("load public comments: %w", err)
	}
	commentsMore := len(comments) > profilePageSize
	if commentsMore {
		comments = comments[:profilePageSize]
	}
	// A bio alone should not make a newly registered, inactive profile
	// indexable. Index the canonical first page only when the account has
	// public activity; pagination remains reachable by links.
	noindex := articlePage > 1 || commentPage > 1 ||
		len(articles) == 0 && len(comments) == 0
	if noindex {
		c.Set("X-Robots-Tag", "noindex, follow")
	}
	return c.Status(http.StatusOK).Render("site/user_profile", injectSiteData(c, fiber.Map{
		"title": account.Nickname, "desc": account.Bio, "profile": account, "noindex": noindex,
		"articles": articles, "comments": comments,
		"articles_page": articlePage, "comments_page": commentPage,
		"articles_previous": articlePage > 1, "articles_more": articlesMore,
		"comments_previous": commentPage > 1, "comments_more": commentsMore,
	}))
}
