package solitudes

import (
	"fmt"
	"time"

	"github.com/go-ego/riot/types"
)

// ArticleHistory 文章修订历史
type ArticleHistory struct {
	ArticleID string `gorm:"type:uuid;index"`
	Article   Article
	Version   uint   `gorm:"index"`
	Desc      string `gorm:"text"`
	Content   string `gorm:"text"`
	CreatedAt time.Time
}

// GetIndexID get index data id
func (t *ArticleHistory) GetIndexID() string {
	return fmt.Sprintf("%s.%d", t.ArticleID, t.Version)
}

// ToIndexData to index data
func (t *ArticleHistory) ToIndexData() types.DocData {
	return types.DocData{
		Content: t.Content,
		Attri: ArticleIndex{
			Slug:    t.Article.Slug,
			Version: fmt.Sprintf("%d", t.Version),
			Title:   t.Article.Title,
		},
	}
}
