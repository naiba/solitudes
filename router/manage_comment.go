package router

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/adtac/go-akismet/akismet"
	"github.com/gofiber/fiber/v2"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/pagination"
	"github.com/naiba/solitudes/pkg/translator"
	"gorm.io/gorm"
)

func comments(c *fiber.Ctx) error {
	rawPage := c.Query("page")
	var page int64
	page, _ = strconv.ParseInt(rawPage, 10, 32)
	var cs []model.Comment
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.DB.Preload("Article"),
		Page:    int(page),
		Limit:   20,
		OrderBy: []string{"created_at DESC"},
	}, &cs)
	c.Status(http.StatusOK).Render("admin/comments", injectSiteData(c, fiber.Map{
		"title":    c.Locals(solitudes.CtxTranslator).(*translator.Translator).T("manage_comments"),
		"comments": cs,
		"page":     pg,
	}))
	return nil
}

func deleteComment(c *fiber.Ctx) error {
	id := c.Query("id")
	articleID := c.Query("aid")

	if len(id) < 10 || len(articleID) < 10 {
		return errors.New("error id")
	}

	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		// 删除评论
		if err := tx.Delete(&model.Comment{}, "id = ?", id).Error; err != nil {
			return err
		}
		// 更新回复关系
		if err := tx.Model(&model.Comment{}).Where("reply_to = ?", id).Update("reply_to", nil).Error; err != nil {
			return err
		}
		// 更新文章评论数
		if err := tx.Model(&model.Article{}).Where("id = ?", articleID).
			UpdateColumn("comment_num", gorm.Expr("comment_num - ?", 1)).Error; err != nil {
			return err
		}
		return nil
	})

	return err
}

func reportSpam(c *fiber.Ctx) error {
	id := c.Query("id")
	articleID := c.Query("aid")

	if len(id) < 10 || len(articleID) < 10 {
		return errors.New("error id")
	}

	var cm model.Comment
	if err := solitudes.System.DB.Take(&cm, "id = ?", id).Error; err != nil {
		return err
	}

	var article model.Article
	if err := solitudes.System.DB.Take(&article, "id = ?", articleID).Error; err != nil {
		return err
	}

	cmType, _, err := getCommentType(&commentForm{
		Nickname: cm.Nickname,
		Email:    cm.Email,
		Website:  cm.Website,
		Content:  cm.Content,
		ReplyTo:  cm.ReplyTo,
	})
	if err != nil {
		return err
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
		return err
	}

	err = solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		// 删除评论
		if err := tx.Delete(&model.Comment{}, "id = ?", id).Error; err != nil {
			return err
		}
		// 更新回复关系
		if err := tx.Model(&model.Comment{}).Where("reply_to = ?", id).Update("reply_to", nil).Error; err != nil {
			return err
		}
		// 更新文章评论数
		if err := tx.Model(&model.Article{}).Where("id = ?", articleID).
			UpdateColumn("comment_num", gorm.Expr("comment_num - ?", 1)).Error; err != nil {
			return err
		}
		return nil
	})

	return err
}
