package router

import (
	"errors"
	"fmt"
	"log"
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

const (
	commentStatusAll     = "all"
	commentStatusVisible = "visible"
	commentStatusSpam    = "spam"
)

func normalizeCommentStatus(status string) string {
	switch status {
	case commentStatusVisible, commentStatusSpam:
		return status
	default:
		return commentStatusAll
	}
}

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
	status := normalizeCommentStatus(c.Query("status"))
	db := solitudes.System.DB.Preload("Article")
	switch status {
	case commentStatusVisible:
		db = db.Where("is_spam = ?", false)
	case commentStatusSpam:
		db = db.Where("is_spam = ?", true)
	}
	var cs []model.Comment
	pg := pagination.Paging(&pagination.Param{
		DB:      db,
		Page:    int(page),
		Limit:   20,
		OrderBy: []string{"created_at DESC"},
	}, &cs)
	var visibleCount, spamCount int64
	if err := solitudes.System.DB.Model(&model.Comment{}).Where("is_spam = ?", false).Count(&visibleCount).Error; err != nil {
		return fmt.Errorf("failed to count visible comments: %w", err)
	}
	if err := solitudes.System.DB.Model(&model.Comment{}).Where("is_spam = ?", true).Count(&spamCount).Error; err != nil {
		return fmt.Errorf("failed to count spam comments: %w", err)
	}
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	return c.Status(http.StatusOK).Render("admin/comments", injectSiteData(c, fiber.Map{
		"title":         tr.T("manage_comments"),
		"comments":      cs,
		"page":          pg,
		"status":        status,
		"total_count":   visibleCount + spamCount,
		"visible_count": visibleCount,
		"spam_count":    spamCount,
	}))
}

func deleteComment(c *fiber.Ctx) error {
	id := c.Query("id")
	articleID := c.Query("aid")

	if len(id) < 10 || len(articleID) < 10 {
		return errors.New("invalid id")
	}

	var cm model.Comment
	if err := solitudes.System.DB.Take(&cm, "id = ? AND article_id = ?", id, articleID).Error; err != nil {
		return fmt.Errorf("failed to fetch comment for deletion: %w", err)
	}

	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		var promotedVisibleChildren int64
		if cm.ReplyTo == nil {
			if err := tx.Model(&model.Comment{}).
				Where("reply_to = ? AND is_spam = ?", id, false).
				Count(&promotedVisibleChildren).Error; err != nil {
				return fmt.Errorf("failed to count promoted child comments: %w", err)
			}
		}

		if err := tx.Delete(&model.Comment{}, "id = ?", id).Error; err != nil {
			return fmt.Errorf("failed to delete comment: %w", err)
		}
		var newReplyTo interface{}
		if cm.ReplyTo != nil {
			newReplyTo = *cm.ReplyTo
		}
		if err := tx.Model(&model.Comment{}).Where("reply_to = ?", id).Update("reply_to", newReplyTo).Error; err != nil {
			return fmt.Errorf("failed to update child comments: %w", err)
		}

		if cm.ReplyTo == nil {
			delta := promotedVisibleChildren
			if cm.CountsTowardArticle() {
				delta--
			}
			if delta != 0 {
				if err := updateArticleCommentCount(tx, articleID, delta); err != nil {
					return err
				}
			}
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
	if err := solitudes.System.DB.Take(&cm, "id = ? AND article_id = ?", id, articleID).Error; err != nil {
		return fmt.Errorf("failed to fetch comment for reporting: %w", err)
	}
	if cm.IsSpam {
		return nil
	}

	var article model.Article
	if err := solitudes.System.DB.Take(&article, "id = ?", articleID).Error; err != nil {
		return fmt.Errorf("failed to fetch article for reporting: %w", err)
	}

	changed := false
	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.Comment{}).Where("id = ? AND is_spam = ?", id, false).Update("is_spam", true)
		if result.Error != nil {
			return fmt.Errorf("failed to mark comment as spam: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return nil
		}
		changed = true
		if cm.CountsTowardArticle() {
			if err := updateArticleCommentCount(tx, articleID, -1); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	submitAkismetFeedback(cm, article, string(c.Request().Header.Referer()), true)
	return nil
}

func restoreSpam(c *fiber.Ctx) error {
	id := c.Query("id")
	articleID := c.Query("aid")
	if len(id) < 10 || len(articleID) < 10 {
		return errors.New("invalid id")
	}

	var cm model.Comment
	if err := solitudes.System.DB.Take(&cm, "id = ? AND article_id = ?", id, articleID).Error; err != nil {
		return fmt.Errorf("failed to fetch spam comment for restoration: %w", err)
	}
	if !cm.IsSpam {
		return nil
	}

	var article model.Article
	if err := solitudes.System.DB.Take(&article, "id = ?", articleID).Error; err != nil {
		return fmt.Errorf("failed to fetch article for spam restoration: %w", err)
	}

	changed := false
	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.Comment{}).Where("id = ? AND is_spam = ?", id, true).Update("is_spam", false)
		if result.Error != nil {
			return fmt.Errorf("failed to restore spam comment: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return nil
		}
		changed = true
		if cm.ReplyTo == nil {
			if err := updateArticleCommentCount(tx, articleID, 1); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	submitAkismetFeedback(cm, article, string(c.Request().Header.Referer()), false)
	return nil
}

func updateArticleCommentCount(tx *gorm.DB, articleID string, delta int64) error {
	if err := tx.Model(&model.Article{}).Where("id = ?", articleID).UpdateColumn(
		"comment_num",
		gorm.Expr("CASE WHEN comment_num + ? < 0 THEN 0 ELSE comment_num + ? END", delta, delta),
	).Error; err != nil {
		return fmt.Errorf("failed to update article comment count: %w", err)
	}
	return nil
}

func submitAkismetFeedback(cm model.Comment, article model.Article, referrer string, spam bool) {
	key := solitudes.System.Config.Akismet
	if key == "" {
		return
	}
	commentType := "comment"
	if cm.ReplyTo != nil {
		commentType = "reply"
	}
	payload := &akismet.Comment{
		Blog:               "https://" + solitudes.System.Config.Site.Domain,
		UserIP:             cm.IP,
		UserAgent:          cm.UserAgent,
		CommentType:        commentType,
		Referrer:           referrer,
		Permalink:          "https://" + solitudes.System.Config.Site.Domain + "/" + article.Slug,
		CommentAuthor:      cm.Nickname,
		CommentAuthorEmail: cm.Email,
		CommentAuthorURL:   cm.Website,
		CommentContent:     cm.Content,
	}
	go func() {
		var err error
		if spam {
			err = akismet.SubmitSpam(payload, key)
		} else {
			err = akismet.SubmitHam(payload, key)
		}
		if err != nil {
			log.Printf("failed to submit Akismet moderation feedback for comment %s: %v", cm.ID, err)
		}
	}()
}
