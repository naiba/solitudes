package model

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/jinzhu/gorm"
	"github.com/lib/pq"
	"github.com/panjf2000/ants"
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

	Slug       string         `form:"slug" validate:"required" gorm:"unique_index"`
	Title      string         `form:"title" validate:"required"`
	Content    string         `form:"content" validate:"required" gorm:"text"`
	TemplateID byte           `form:"template" validate:"required"`
	IsBook     bool           `form:"is_book"`
	RawTags    string         `form:"tags" gorm:"-"`
	Tags       pq.StringArray `gorm:"index;type:varchar(255)[]" validate:"-" form:"-"`
	ReadNum    uint           `gorm:"default:0;"`
	CommentNum uint           `gorm:"default:0;"`
	Version    uint           `gorm:"default:1;"`
	BookRefer  *string        `form:"book_refer" validate:"omitempty,uuid4" gorm:"type:uuid;index;default:NULL"`

	Comments         []*Comment
	ArticleHistories []*ArticleHistory
	Toc              []*ArticleTOC
	Chapters         []*Article       `gorm:"foreignkey:BookRefer" form:"-" validate:"-"`
	Book             *Article         `gorm:"-" validate:"-" form:"-"`
	SibilingArticle  *SibilingArticle `gorm:"-" validate:"-" form:"-"`

	// for form
	NewVersion bool `gorm:"-" form:"new_version"`
}

// ArticleIndex index data
type ArticleIndex struct {
	Slug    string
	Version float64
	Title   string
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
				parent.SubTitles = append(parent.SubTitles, &toc)
			}
		} else if currentToc.Level == toc.Level {
			// 兄弟节点
			if parent.Parent == nil {
				t.Toc = append(t.Toc, &toc)
			} else {
				toc.Parent = parent.Parent
				toc.ShowLevel = toc.Parent.Level + 1
				toc.Parent.SubTitles = append(toc.Parent.SubTitles, &toc)
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
			anchorName = append(anchorName, r)
		default:
			futureDash = true
		}
	}
	return string(anchorName)
}

// RelatedCount 合计专栏下文章计数
func (t *Article) RelatedCount(db *gorm.DB, pool *ants.Pool, checkPoolSubmit func(*sync.WaitGroup, error)) {
	if !t.IsBook {
		return
	}
	var wg sync.WaitGroup
	wg.Add(1)
	checkPoolSubmit(&wg, pool.Submit(func() {
		innerRelatedCount(db, t, &wg, true)
	}))
	wg.Wait()
}

func innerRelatedCount(db *gorm.DB, p *Article, wg *sync.WaitGroup, root bool) {
	var chapters []*Article
	db.Select("id,is_book,read_num,comment_num").Where("book_refer = ?", p.ID).Find(&chapters)
	for i := 0; i < len(chapters); i++ {
		if chapters[i].IsBook {
			innerRelatedCount(db, chapters[i], nil, false)
		}
		p.ReadNum += chapters[i].ReadNum
		p.CommentNum += chapters[i].CommentNum
	}
	if root {
		wg.Done()
	}
}
