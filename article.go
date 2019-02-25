package solitudes

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/jinzhu/gorm"
	"github.com/lib/pq"
)

// ArticleTOC 文章标题
type ArticleTOC struct {
	Title     string
	Slug      string
	SubTitles []*ArticleTOC
	Parent    *ArticleTOC `gorm:"-" json:"-"`
	Level     int         `gorm:"-" json:"-"`
}

// Article 文章表
type Article struct {
	gorm.Model
	Slug    string `form:"slug" binding:"required" gorm:"unique_index" json:"slug,omitempty"`
	Title   string `form:"title" binding:"required" json:"title,omitempty"`
	Content string `form:"content" binding:"required" gorm:"text" json:"content,omitempty"`

	TemplateID byte           `form:"template" binding:"required" json:"template_id,omitempty"`
	IsBook     bool           `form:"is_book" json:"is_book,omitempty"`
	RawTags    string         `form:"tags" gorm:"-" json:"-"`
	Tags       pq.StringArray `gorm:"index;type:varchar(255)[]" json:"tags,omitempty"`
	ReadNum    uint           `gorm:"default:0;" json:"read_num,omitempty"`
	CommentNum uint           `gorm:"default:0;"`
	Version    uint           `form:"version" gorm:"default:1;"`
	BookRefer  uint           `form:"book_refer" gorm:"index" json:"book_refer,omitempty"`

	Chapters         []Article     `gorm:"foreignkey:BookRefer" form:"-" binding:"-"`
	Comments         []Comment     `json:"comments,omitempty"`
	Toc              []*ArticleTOC `gorm:"-"`
	ArticleHistories []ArticleHistory
}

// SID string id
func (t *Article) SID() string {
	return fmt.Sprintf("%d", t.ID)
}

// ArticleIndex index data
type ArticleIndex struct {
	ID      string
	Slug    string
	Version uint
	Content string
	Tags    string
}

// ToIndexData to index data
func (t *Article) ToIndexData() ArticleIndex {
	return ArticleIndex{
		ID:      t.SID(),
		Slug:    t.Slug,
		Version: t.Version,
		Tags:    t.RawTags,
		Content: t.Content,
	}
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
var whitespaces = regexp.MustCompile(`[\s|\.]{1,}`)

// GenTOC 生成标题树
func (t *Article) GenTOC() {
	lines := strings.Split(t.Content, "\n")
	var matches []string
	var currentToc *ArticleTOC
	t.Toc = make([]*ArticleTOC, 0)
	for j := 0; j < len(lines); j++ {
		matches = titleRegex.FindStringSubmatch(lines[j])
		if len(matches) == 3 {
			var toc ArticleTOC
			toc.Level = len(matches[1])
			toc.Title = string(matches[2])
			toc.Slug = string(whitespaces.ReplaceAllString(matches[2], "-"))
			toc.SubTitles = make([]*ArticleTOC, 0)
			if currentToc == nil {
				t.Toc = append(t.Toc, &toc)
				currentToc = &toc
			} else {
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
						parent.Parent.SubTitles = append(parent.Parent.SubTitles, &toc)
					}
				} else {
					// 子节点
					toc.Parent = parent
					parent.SubTitles = append(parent.SubTitles, &toc)
				}
				currentToc = &toc
			}
		}
	}
}

// BuildArticleIndex 重建索引
func BuildArticleIndex() {
	var as []Article
	System.D.Find(&as)
	for i := 0; i < len(as); i++ {
		err := System.S.Index(as[i].SID(), as[i].ToIndexData())
		if err != nil {
			panic(err)
		}
	}
}
