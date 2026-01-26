package router

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/adtac/go-akismet/akismet"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/pagination"
	"github.com/naiba/solitudes/pkg/translator"
)

func comments(c *fiber.Ctx) error {
	rawPage := c.Query("page")
	var page int64
	if rawPage != "" {
		var err error
		page, err = strconv.ParseInt(rawPage, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid page format: %w", err)
		}
	}
	var cs []model.Comment
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.DB.Preload("Article"),
		Page:    int(page),
		Limit:   20,
		OrderBy: []string{"created_at DESC"},
	}, &cs)
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	return c.Status(http.StatusOK).Render("admin/comments", injectSiteData(c, fiber.Map{
		"title":    tr.T("manage_comments"),
		"comments": cs,
		"page":     pg,
	}))
}

func deleteComment(c *fiber.Ctx) error {
	id := c.Query("id")
	articleID := c.Query("aid")

	if len(id) < 10 || len(articleID) < 10 {
		return errors.New("invalid id")
	}

	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		// 删除评论
		if err := tx.Delete(&model.Comment{}, "id = ?", id).Error; err != nil {
			return fmt.Errorf("failed to delete comment: %w", err)
		}
		// 更新回复关系
		if err := tx.Model(&model.Comment{}).Where("reply_to = ?", id).Update("reply_to", nil).Error; err != nil {
			return fmt.Errorf("failed to update child comments: %w", err)
		}
		// 更新文章评论数
		if err := tx.Model(&model.Article{}).Where("id = ?", articleID).
			UpdateColumn("comment_num", gorm.Expr("comment_num - ?", 1)).Error; err != nil {
			return fmt.Errorf("failed to update article comment count: %w", err)
		}
		return nil
	})

	return err
}

func reportSpam(c *fiber.Ctx) error {
	id := c.Query("id")
	articleID := c.Query("aid")

	if len(id) < 10 || len(articleID) < 10 {
		return errors.New("invalid id")
	}

	var cm model.Comment
	if err := solitudes.System.DB.Take(&cm, "id = ?", id).Error; err != nil {
		return fmt.Errorf("failed to fetch comment for reporting: %w", err)
	}

	var article model.Article
	if err := solitudes.System.DB.Take(&article, "id = ?", articleID).Error; err != nil {
		return fmt.Errorf("failed to fetch article for reporting: %w", err)
	}

	cmType, _, err := getCommentType(&commentForm{
		Nickname: cm.Nickname,
		Email:    cm.Email,
		Website:  cm.Website,
		Content:  cm.Content,
		ReplyTo:  cm.ReplyTo,
	})
	if err != nil {
		return fmt.Errorf("failed to determine comment type: %w", err)
	}

	if err := akismet.SubmitSpam(&akismet.Comment{
		Blog:               "https://" + solitudes.System.Config.Site.Domain, // required
		UserIP:             cm.IP,                                            // required
		UserAgent:          cm.UserAgent,                                     // required
		CommentType:        cmType,
		Referrer:           string(c.Request().Header.Referer()),
		Permalink:          "https://" + solitudes.System.Config.Site.Domain + "/" + article.Slug,
		CommentAuthor:      cm.Nickname,
		CommentAuthorEmail: cm.Email,
		CommentAuthorURL:   cm.Website,
		CommentContent:     cm.Content,
	}, solitudes.System.Config.Akismet); err != nil {
		return fmt.Errorf("akismet reporting failed: %w", err)
	}

	err = solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		// 删除评论
		if err := tx.Delete(&model.Comment{}, "id = ?", id).Error; err != nil {
			return fmt.Errorf("failed to delete spam comment: %w", err)
		}
		// 更新回复关系
		if err := tx.Model(&model.Comment{}).Where("reply_to = ?", id).Update("reply_to", nil).Error; err != nil {
			return fmt.Errorf("failed to update child comments: %w", err)
		}
		// 更新文章评论数
		if err := tx.Model(&model.Article{}).Where("id = ?", articleID).
			UpdateColumn("comment_num", gorm.Expr("comment_num - ?", 1)).Error; err != nil {
			return fmt.Errorf("failed to update article comment count: %w", err)
		}
		return nil
	})

	return err
}
