package router

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/pagination"
)

// Page direct replies, not an unbounded recursive tree. Every thread remains
// navigable, including deep replies; ancestors must belong to this article and
// be visible before any reply content is returned.
func articleComments(c *fiber.Ctx, article *model.Article) (*pagination.Paginator, pageNavigation, string, error) {
	page, err := listPage(c.Query("comment_page"))
	if err != nil {
		return nil, pageNavigation{}, "", err
	}
	thread := c.Query("thread")
	key := "comment_page"
	query := visibleComments(solitudes.System.DB).Preload("Account", publicCommentAuthor).Where("article_id = ?", article.ID)
	var parent model.Comment
	if thread != "" {
		if _, err := uuid.Parse(thread); err != nil {
			return nil, pageNavigation{}, "", fiber.ErrBadRequest
		}
		page, err = listPage(c.Query("replies_page"))
		if err != nil {
			return nil, pageNavigation{}, "", err
		}
		key = "replies_page"
		if err := query.Session(&gorm.Session{}).Where(`id = ? AND NOT EXISTS (
			WITH RECURSIVE ancestors AS (
			 SELECT id, reply_to, is_spam, article_id FROM comments WHERE id = ?
			 UNION SELECT p.id, p.reply_to, p.is_spam, p.article_id FROM comments p JOIN ancestors a ON p.id = a.reply_to
			) SELECT 1 FROM ancestors WHERE is_spam = true OR article_id <> ?
		)`, thread, thread, article.ID).Take(&parent).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, pageNavigation{}, "", fiber.ErrNotFound
			}
			return nil, pageNavigation{}, "", err
		}
		query = query.Where("reply_to = ?", thread)
	} else {
		query = query.Where("reply_to IS NULL")
	}
	var comments []*model.Comment
	order := "created_at DESC, id DESC"
	if thread != "" {
		order = "created_at ASC, id ASC"
	}
	pg, err := pagination.Paging(&pagination.Param{DB: query, Page: page, Limit: 20, OrderBy: []string{order}}, &comments)
	if err != nil {
		return nil, pageNavigation{}, "", err
	}
	if thread == "" {
		// Preview up to three direct replies per root using one bounded query.
		ids := make([]string, 0, len(comments))
		parents := make(map[string]*model.Comment)
		for _, comment := range comments {
			ids = append(ids, comment.ID)
			parents[comment.ID] = comment
		}
		if len(ids) > 0 {
			var previews []*model.Comment
			if err := solitudes.System.DB.Raw(`SELECT * FROM (
			 SELECT comments.*, ROW_NUMBER() OVER (PARTITION BY reply_to ORDER BY created_at, id) AS position
			 FROM comments WHERE article_id = ? AND is_spam = false AND reply_to IN ?
			) AS ranked WHERE position <= 3 ORDER BY created_at, id`, article.ID, ids).Scan(&previews).Error; err != nil {
				return nil, pageNavigation{}, "", err
			}
			if err := loadCommentAccounts(previews); err != nil {
				return nil, pageNavigation{}, "", err
			}
			for _, reply := range previews {
				parents[*reply.ReplyTo].ChildComments = append(parents[*reply.ReplyTo].ChildComments, reply)
			}
		}
	} else {
		parent.ChildComments = comments
		comments = []*model.Comment{&parent}
	}
	var all []*model.Comment
	for _, comment := range comments {
		all = append(all, comment)
		all = append(all, comment.ChildComments...)
	}
	ids := make([]string, 0, len(all))
	for _, comment := range all {
		comment.Article = article
		ids = append(ids, comment.ID)
	}
	if len(ids) > 0 {
		var counts []struct {
			ReplyTo string
			Count   int64
		}
		if err := solitudes.System.DB.Model(&model.Comment{}).Select("reply_to, count(*) AS count").Where("article_id = ? AND is_spam = false AND reply_to IN ?", article.ID, ids).Group("reply_to").Scan(&counts).Error; err != nil {
			return nil, pageNavigation{}, "", err
		}
		countMap := make(map[string]int64)
		for _, count := range counts {
			countMap[count.ReplyTo] = count.Count
		}
		for _, comment := range all {
			comment.ReplyCount = countMap[comment.ID]
		}
	}
	article.Comments = comments
	return pg, pageNavigationFor(c, key, page, page < pg.TotalPage, "comments"), thread, nil
}
