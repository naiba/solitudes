package router

import (
	"time"

	"gorm.io/gorm"
)

// publicComment contains only publicly displayable fields. Never fetch comment
// emails, IP addresses, or private-article content for the homepage.
type publicComment struct {
	ID           string
	AccountID    string
	Nickname     string
	Role         string
	Content      string
	ArticleSlug  string
	ArticleTitle string
	CreatedAt    time.Time
}

func recentPublicComments(db *gorm.DB, limit int) ([]publicComment, error) {
	var comments []publicComment
	err := db.Raw(`
		SELECT c.id, COALESCE(a.id::text, '') AS account_id,
		       COALESCE(NULLIF(a.nickname, ''), c.nickname) AS nickname,
		       CASE WHEN a.id IS NOT NULL THEN a.role::text
		            ELSE 'guest' END AS role,
		       c.content,
		       article.slug AS article_slug, article.title AS article_title,
		       c.created_at
		FROM comments AS c
		JOIN articles AS article ON article.id = c.article_id
		LEFT JOIN accounts AS a ON a.id = c.account_id
		WHERE c.is_spam = false AND article.visibility = 'public'
		  AND (a.id IS NULL OR (a.directory_hidden = false
		       AND a.email_verified_at IS NOT NULL AND a.disabled_at IS NULL))
		  AND NOT EXISTS (
			WITH RECURSIVE ancestors AS (
				SELECT parent.id, parent.reply_to, parent.is_spam
				FROM comments AS parent WHERE parent.id = c.reply_to
				UNION ALL
				SELECT parent.id, parent.reply_to, parent.is_spam
				FROM comments AS parent JOIN ancestors ON ancestors.reply_to = parent.id
			)
			SELECT 1 FROM ancestors WHERE ancestors.is_spam = true
		  )
		ORDER BY c.created_at DESC, c.id DESC
		LIMIT ?`, limit).Scan(&comments).Error
	return comments, err
}
