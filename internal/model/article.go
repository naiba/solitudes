package model

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/lib/pq"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

// ArticleTOC 文章标题
type ArticleTOC struct {
	Title     string
	Slug      string
	SubTitles []*ArticleTOC `gorm:"-"`
	Parent    *ArticleTOC   `gorm:"-"`
	Level     int           `gorm:"-"`
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
	IsPrivate      bool           `form:"is_private"`
	DisableComment bool           `form:"disable_comment"`

	Comments         []*Comment        `gorm:"foreignKey:ArticleID"`
	ArticleHistories []*ArticleHistory `gorm:"foreignKey:ArticleID"`
	Toc              []*ArticleTOC     `gorm:"-"`
	Chapters         []*Article        `gorm:"foreignkey:BookRefer" form:"-" validate:"-"`
	Book             *Article          `gorm:"-" validate:"-" form:"-"`
	SibilingArticle  *SibilingArticle  `gorm:"-" validate:"-" form:"-"`

	// for form
	NewVersion uint `gorm:"-" form:"new_version"`
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
func (t *Article) BeforeSave(tx *gorm.DB) (err error) {
	t.RawTags = strings.TrimSpace(t.RawTags)
	t.parseRawTags()
	return nil
}

func (t *Article) parseRawTags() {
	if t.RawTags == "" {
		return
	}
	t.Tags = strings.Split(t.RawTags, ",")
}

// AfterFind hook
func (t *Article) AfterFind(tx *gorm.DB) (err error) {
	t.RawTags = strings.Join(t.Tags, ",")
	return nil
}

var titleRegex = regexp.MustCompile(`^\s{0,2}(#{1,6})\s(.*)$`)

// IsTopic 是否是哔哔
func (t *Article) IsTopic() bool {
	t.parseRawTags()
	return lo.Contains(t.Tags, "Topic")
}

// GenTOC 生成标题树
func (t *Article) GenTOC() {
	lines := strings.Split(t.Content, "\n")
	uniqueHeadingID := make(map[string]int)
	var matches []string
	var currentToc *ArticleTOC
	for j := 0; j < len(lines); j++ {
		matches = titleRegex.FindStringSubmatch(lines[j])
		if len(matches) != 3 {
			continue
		}
		var toc ArticleTOC
		toc.Level = len(matches[1])
		toc.Title = string(matches[2])
		toc.Slug = sanitizedAnchorName(uniqueHeadingID, string(matches[2]))
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
				toc.Parent.SubTitles = append(toc.Parent.SubTitles, &toc)
			}
		} else {
			// 子节点
			toc.Parent = parent
			parent.SubTitles = append(parent.SubTitles, &toc)
		}
		currentToc = &toc
	}
}

func removeLeadingHashtag(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] != '#' {
			return s[i:]
		}
	}
	return s
}

// 生成标题 ID
func sanitizedAnchorName(unique map[string]int, text string) (ret string) {
	text = strings.TrimSpace(removeLeadingHashtag(strings.TrimSpace(text)))
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			ret += string(r)
		} else {
			ret += "-"
		}
	}
	for 0 < unique[ret] {
		ret += "-"
	}
	unique[ret] = 1
	return
}

// RelatedCount 合计专栏下文章计数
func (t *Article) RelatedCount(db *gorm.DB) {
	if !t.IsBook {
		return
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		innerRelatedCount(db, t, true)
	}()
	wg.Wait()
}

func innerRelatedCount(db *gorm.DB, p *Article, root bool) {
	var chapters []*Article
	db.Model(&Article{}).Select("id", "is_book", "read_num", "comment_num").Where("book_refer = ?", p.ID).Find(&chapters)
	for i := range chapters {
		if chapters[i].IsBook {
			innerRelatedCount(db, chapters[i], false)
		}
		p.ReadNum += chapters[i].ReadNum
		p.CommentNum += chapters[i].CommentNum
	}
}
