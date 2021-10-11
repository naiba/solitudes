package router

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/biezhi/gorm-paginator/pagination"
	"github.com/gofiber/fiber/v2"
	"github.com/jinzhu/gorm"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func comments(c *fiber.Ctx) error {
	rawPage := c.Query("page")
	var page int64
	page, _ = strconv.ParseInt(rawPage, 10, 32)
	var cs []model.Comment
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.DB.Preload("Article"),
		Page:    int(page),
		Limit:   15,
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
	rpl := c.Query("rpl")
	articleID := c.Query("aid")

	if len(id) < 10 || len(articleID) < 10 {
		return errors.New("error id")
	}

	tx := solitudes.System.DB.Begin()
	if err := tx.Delete(&model.Comment{}, "id =?", id).Error; err != nil {
		tx.Rollback()
		return err
	}
	if rpl == "" {
		if err := tx.Model(model.Article{}).Where("id = ?", articleID).
			UpdateColumn("comment_num", gorm.Expr("comment_num - ?", 1)).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	return nil
}
