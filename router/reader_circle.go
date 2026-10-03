package router

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

const readerCirclePageSize = 12

type readerCircleMember struct {
	ID            string
	Nickname      string
	Bio           string
	Role          model.Role
	CreatedAt     time.Time
	ActivityCount int64
}

type readerCircleActivity struct {
	AccountID    string
	Nickname     string
	Role         model.Role
	Kind         string
	ArticleSlug  string
	ArticleTitle string
	CommentID    string
	Excerpt      string
	HappenedAt   time.Time
}

func eligibleReaders(db *gorm.DB) *gorm.DB {
	return db.Model(&model.Account{}).
		Where("directory_hidden = false AND email_verified_at IS NOT NULL AND disabled_at IS NULL")
}

func latestReaders(page, limit int) ([]readerCircleMember, bool, error) {
	var readers []readerCircleMember
	err := eligibleReaders(solitudes.System.DB).
		Select("id, nickname, bio, role, created_at").
		Order("created_at DESC, id DESC").Offset((page - 1) * limit).
		Limit(limit + 1).Scan(&readers).Error
	if err != nil {
		return nil, false, err
	}
	more := len(readers) > limit
	if more {
		readers = readers[:limit]
	}
	return readers, more, nil
}

// Count only recent, public contributions. Never derive popularity from
// private posts, spam, replies under spam, or guest comments.
func activeReaders() ([]readerCircleMember, error) {
	var readers []readerCircleMember
	cutoff := time.Now().AddDate(0, 0, -30)
	err := solitudes.System.DB.Raw(`
		SELECT a.id, a.nickname, a.bio, a.role, a.created_at,
		       SUM(activity.weight) AS activity_count
		FROM accounts AS a
		JOIN (
			SELECT author_id AS account_id, 3 AS weight, created_at AS happened_at
			FROM articles
			WHERE author_id IS NOT NULL AND visibility = 'public' AND created_at >= ?
			UNION ALL
			SELECT comments.account_id, 1 AS weight, comments.created_at AS happened_at
			FROM comments JOIN articles ON articles.id = comments.article_id
			WHERE comments.account_id IS NOT NULL AND comments.is_spam = false
			  AND articles.visibility = 'public' AND comments.created_at >= ?
			  AND NOT EXISTS (
				WITH RECURSIVE ancestors AS (
					SELECT parent.id, parent.reply_to, parent.is_spam
					FROM comments AS parent WHERE parent.id = comments.reply_to
					UNION ALL
					SELECT parent.id, parent.reply_to, parent.is_spam
					FROM comments AS parent JOIN ancestors ON ancestors.reply_to = parent.id
				)
				SELECT 1 FROM ancestors WHERE ancestors.is_spam = true
			  )
		) AS activity ON activity.account_id = a.id
		WHERE a.directory_hidden = false AND a.email_verified_at IS NOT NULL AND a.disabled_at IS NULL
		GROUP BY a.id, a.nickname, a.bio, a.role, a.created_at
		ORDER BY activity_count DESC, MAX(activity.happened_at) DESC, a.id DESC
		LIMIT 8`, cutoff, cutoff).Scan(&readers).Error
	return readers, err
}

// The feed links each public contribution to its author and the source post.
// Comments underneath spam are excluded even when the reply itself is clean.
func recentReaderActivity() ([]readerCircleActivity, error) {
	var activity []readerCircleActivity
	cutoff := time.Now().AddDate(0, 0, -30)
	err := solitudes.System.DB.Raw(`
		SELECT a.id AS account_id, a.nickname, a.role, feed.kind,
		       feed.article_slug, feed.article_title, feed.comment_id,
		       feed.excerpt, feed.happened_at
		FROM (
			SELECT articles.author_id AS account_id, articles.id AS event_id,
			       'article' AS kind, articles.slug AS article_slug,
			       articles.title AS article_title, '' AS comment_id,
			       '' AS excerpt, articles.created_at AS happened_at
			FROM articles
			WHERE articles.author_id IS NOT NULL AND articles.visibility = 'public'
			  AND articles.created_at >= ?
			UNION ALL
			SELECT comments.account_id, comments.id, 'comment',
			       articles.slug, articles.title, comments.id::text,
			       LEFT(regexp_replace(comments.content, '[[:space:]]+', ' ', 'g'), 160),
			       comments.created_at
			FROM comments JOIN articles ON articles.id = comments.article_id
			WHERE comments.account_id IS NOT NULL AND comments.is_spam = false
			  AND articles.visibility = 'public' AND comments.created_at >= ?
			  AND NOT EXISTS (
				WITH RECURSIVE ancestors AS (
					SELECT parent.id, parent.reply_to, parent.is_spam
					FROM comments AS parent WHERE parent.id = comments.reply_to
					UNION ALL
					SELECT parent.id, parent.reply_to, parent.is_spam
					FROM comments AS parent JOIN ancestors ON ancestors.reply_to = parent.id
				)
				SELECT 1 FROM ancestors WHERE ancestors.is_spam = true
			  )
		) AS feed JOIN accounts AS a ON a.id = feed.account_id
		WHERE a.directory_hidden = false AND a.email_verified_at IS NOT NULL AND a.disabled_at IS NULL
		ORDER BY feed.happened_at DESC, feed.event_id DESC
		LIMIT 12`, cutoff, cutoff).Scan(&activity).Error
	return activity, err
}

func readerCircle(c *fiber.Ctx) error {
	page, err := profilePageNumber(c, "page")
	if err != nil {
		return err
	}
	latest, more, err := latestReaders(page, readerCirclePageSize)
	if err != nil {
		return fmt.Errorf("load latest readers: %w", err)
	}
	var active []readerCircleMember
	var activity []readerCircleActivity
	if page == 1 {
		active, err = activeReaders()
		if err != nil {
			return fmt.Errorf("load active readers: %w", err)
		}
		activity, err = recentReaderActivity()
		if err != nil {
			return fmt.Errorf("load reader circle activity: %w", err)
		}
	}
	noindex := len(latest) == 0 || page > 1
	if noindex {
		c.Set("X-Robots-Tag", "noindex, follow")
	}
	return c.Status(http.StatusOK).Render("site/reader_circle", injectSiteData(c, fiber.Map{
		"title": "Reader circle", "noindex": noindex,
		"active": active, "latest": latest, "activity": activity, "page": page,
		"previous": page > 1, "more": more,
	}))
}
