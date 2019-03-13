package solitudes

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/lib/pq"
)

// ArticleTOC 文章标题
type ArticleTOC struct {
	Title     string
	Slug      string
	SubTitles []*ArticleTOC
	Parent    *ArticleTOC `gorm:"-"`
	Level     int         `gorm:"-"`
	ShowLevel int         `gorm:"-"`
}

// SibilingArticle 相邻文章
type SibilingArticle struct {
	Next Article
	Prev Article
}

// Article 文章表
type Article struct {
	ID        string `gorm:"type:uuid;primary_key;default:uuid_generate_v4()"`
	CreatedAt time.Time
	UpdatedAt time.Time

	Slug       string         `form:"slug" binding:"required" gorm:"unique_index"`
	Title      string         `form:"title" binding:"required"`
	Content    string         `form:"content" binding:"required" gorm:"text"`
	TemplateID byte           `form:"template" binding:"required"`
	IsBook     bool           `form:"is_book"`
	RawTags    string         `form:"tags" gorm:"-"`
	Tags       pq.StringArray `gorm:"index;type:varchar(255)[]"`
	ReadNum    uint           `gorm:"default:0;"`
	CommentNum uint           `gorm:"default:0;"`
	Version    uint           `gorm:"default:1;"`
	BookRefer  *string        `form:"book_refer" binding:"omitempty,uuid4" gorm:"type:uuid;index;default:NULL"`

	Comments         []*Comment
	ArticleHistories []*ArticleHistory
	Toc              []*ArticleTOC    `gorm:"-"`
	Chapters         []*Article       `gorm:"foreignkey:BookRefer" form:"-" binding:"-"`
	Book             *Article         `gorm:"-" binding:"-" form:"-"`
	SibilingArticle  *SibilingArticle `gorm:"-" binding:"-" form:"-"`

	// for form
	NewVersion bool `gorm:"-" form:"new_version"`
}

// ArticleIndex index data
type ArticleIndex struct {
	Slug    string
	Version string
	Title   string
	Content string
}

// ToIndexData to index data
func (t *Article) ToIndexData() ArticleIndex {
	return ArticleIndex{
		Slug:    t.Slug,
		Version: fmt.Sprintf("%d", t.Version),
		Content: t.Content,
		Title:   t.Title,
	}
}

// GetIndexID get index data id
func (t *Article) GetIndexID() string {
	return fmt.Sprintf("%s.%d", t.ID, t.Version)
}

// BeforeSave hook
func (t *Article) BeforeSave() {
	t.Tags = strings.Split(t.RawTags, ",")
}

// AfterFind hook
func (t *Article) AfterFind() {
	t.RawTags = strings.Join(t.Tags, ",")
}

var titleRegex = regexp.MustCompile(`^\s{0,2}(#{1,6})\s(.*)$`)

// GenTOC 生成标题树
func (t *Article) GenTOC() {
	lines := strings.Split(t.Content, "\n")
	var matches []string
	var currentToc *ArticleTOC
	for j := 0; j < len(lines); j++ {
		matches = titleRegex.FindStringSubmatch(lines[j])
		if len(matches) != 3 {
			continue
		}
		var toc ArticleTOC
		toc.Level = len(matches[1])
		toc.ShowLevel = 2
		toc.Title = string(matches[2])
		toc.Slug = sanitizedAnchorName(string(matches[2]))
		if currentToc == nil {
			t.Toc = append(t.Toc, &toc)
			currentToc = &toc
			continue
		}
		parent := currentToc
		if currentToc.Level > toc.Level {
			// 父节点
			for i := -1; i < currentToc.Level-toc.Level; i++ {
				parent = parent.Parent
				if parent == nil || parent.Level < toc.Level {
					break
				}
			}
			if parent == nil {
				t.Toc = append(t.Toc, &toc)
			} else {
				toc.Parent = parent
				toc.ShowLevel = parent.ShowLevel + 1
				parent.SubTitles = append(parent.SubTitles, &toc)
			}
		} else if currentToc.Level == toc.Level {
			// 兄弟节点
			if parent.Parent == nil {
				t.Toc = append(t.Toc, &toc)
			} else {
				toc.Parent = parent.Parent
				toc.ShowLevel = parent.ShowLevel + 1
				parent.Parent.SubTitles = append(parent.Parent.SubTitles, &toc)
			}
		} else {
			// 子节点
			toc.Parent = parent
			toc.ShowLevel = parent.ShowLevel + 1
			parent.SubTitles = append(parent.SubTitles, &toc)
		}
		currentToc = &toc
	}
	ensureUniqueIDs(make(map[string]int), t.Toc)
}

// 确保标题 ID 唯一
func ensureUniqueIDs(ids map[string]int, ts []*ArticleTOC) {
	for i := 0; i < len(ts); i++ {
		if count, has := ids[ts[i].Slug]; has {
			ts[i].Slug = fmt.Sprintf("%s-%d", ts[i].Slug, count+1)
		} else {
			ids[ts[i].Slug] = 0
		}
		ensureUniqueIDs(ids, ts[i].SubTitles)
	}
}

// 生成标题 ID
func sanitizedAnchorName(text string) string {
	var anchorName []rune
	futureDash := false
	for _, r := range text {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			if futureDash && len(anchorName) > 0 {
				anchorName = append(anchorName, '-')
			}
			futureDash = false
			anchorName = append(anchorName, unicode.ToLower(r))
		default:
			futureDash = true
		}
	}
	return string(anchorName)
}
