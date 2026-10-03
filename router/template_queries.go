package router

import (
	"fmt"
	"math/rand/v2"
	"strconv"

	"gorm.io/gorm"

	"github.com/naiba/solitudes/internal/model"
)

// TemplateQueries is bound to one request. It exposes no raw DB or SQL and
// deliberately knows nothing about theme names, layouts or recommendations.
type TemplateQueries struct {
	db      *gorm.DB
	account *model.Account
	notice  func(string) string
}

func (q *TemplateQueries) Articles(order string, limit int, filters ...string) ([]model.Article, error) {
	orders := map[string]string{
		"newest": "created_at DESC, id DESC", "oldest": "created_at ASC, id ASC",
		"updated": "updated_at DESC, id DESC", "reads": "read_num DESC, created_at DESC, id DESC",
		"comments": "comment_num DESC, created_at DESC, id DESC",
	}
	sort, ok := orders[order]
	if !ok || limit < 0 || limit > 100 || len(filters)%2 != 0 {
		return nil, fmt.Errorf("invalid article query")
	}
	if limit == 0 {
		return []model.Article{}, nil
	}
	db := readableArticles(q.db, q.account, q.notice).Preload("Author", publicCommentAuthor)
	for i := 0; i < len(filters); i += 2 {
		key, value := filters[i], filters[i+1]
		switch key {
		case "author":
			db = db.Where("author_id = ?", value)
		case "exclude":
			db = db.Where("id <> ?", value)
		case "tag":
			db = db.Where("tags @> ARRAY[?]::varchar[]", value)
		case "without_tag":
			db = db.Where("(array_length(tags, 1) is null OR NOT tags @> ARRAY[?]::varchar[])", value)
		case "template":
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 || n > 255 {
				return nil, fmt.Errorf("invalid template filter")
			}
			db = db.Where("template_id = ?", n)
		case "offset":
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 || n > 10000 {
				return nil, fmt.Errorf("invalid query offset")
			}
			db = db.Offset(n)
		default:
			return nil, fmt.Errorf("unknown article filter %q", key)
		}
	}
	var articles []model.Article
	if err := db.Order(sort).Limit(limit).Find(&articles).Error; err != nil {
		return nil, err
	}
	return articles, nil
}

func (q *TemplateQueries) Comments(articleID string, limit int) ([]*model.Comment, error) {
	if limit < 0 || limit > 100 {
		return nil, fmt.Errorf("invalid comment limit")
	}
	if limit == 0 {
		return []*model.Comment{}, nil
	}
	var article model.Article
	if err := readableArticles(q.db, q.account).Select("id").Take(&article, "id = ?", articleID).Error; err != nil {
		return nil, err
	}
	var comments []*model.Comment
	err := visibleComments(q.db).Preload("Account", publicCommentAuthor).
		Where("reply_to IS NULL AND article_id = ?", articleID).Order("created_at DESC, id DESC").Limit(limit).Find(&comments).Error
	return comments, err
}

// Users returns only directory-visible public profile fields, never credentials
// or email addresses. Account privacy preferences are enforced in the query.
func (q *TemplateQueries) Users(order string, limit int) ([]readerCircleMember, error) {
	if (order != "newest" && order != "oldest") || limit < 0 || limit > 100 {
		return nil, fmt.Errorf("invalid user query")
	}
	if limit == 0 {
		return []readerCircleMember{}, nil
	}
	sort := "created_at DESC, id DESC"
	if order == "oldest" {
		sort = "created_at ASC, id ASC"
	}
	var users []readerCircleMember
	err := eligibleReaders(q.db).Select("id, nickname, bio, role, created_at").Order(sort).Limit(limit).Scan(&users).Error
	return users, err
}

func (q *TemplateQueries) RecentComments(limit int) ([]publicComment, error) {
	if limit < 0 || limit > 100 {
		return nil, fmt.Errorf("invalid comment limit")
	}
	if limit == 0 {
		return []publicComment{}, nil
	}
	return recentPublicComments(q.db, limit)
}

// RandomIndices samples without replacement. The pool is bounded so a template
// cannot accidentally request an unbounded allocation; zero/empty is valid.
func randomIndices(length, count int) ([]int, error) {
	if length < 0 || length > 10000 || count < 0 || count > 10000 {
		return nil, fmt.Errorf("invalid random sample size")
	}
	return rand.Perm(length)[:min(length, count)], nil
}

// dict replaces presentation-specific articleData/commentsData wrappers.
func templateDict(values ...interface{}) (map[string]interface{}, error) {
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("dict expects key/value pairs")
	}
	result := make(map[string]interface{}, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict keys must be strings")
		}
		result[key] = values[i+1]
	}
	return result, nil
}
