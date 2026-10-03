package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/88250/lute"
	"github.com/88250/lute/ast"
	"github.com/88250/lute/parse"
	"github.com/88250/lute/render"
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
	ID        string  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()"`
	AuthorID  *string `gorm:"type:uuid"`
	Author    Account `gorm:"foreignKey:AuthorID" json:"-"`
	CreatedAt time.Time
	UpdatedAt time.Time

	Slug           string            `form:"slug" validate:"required" gorm:"uniqueIndex;not null"`
	Title          string            `form:"title" validate:"required"`
	Content        string            `form:"content" validate:"required" gorm:"text"`
	TemplateID     byte              `form:"template" validate:"required"`
	IsBook         bool              `form:"is_book"`
	RawTags        string            `form:"tags" gorm:"-"`
	Tags           pq.StringArray    `gorm:"index:idx_articles_tags_gin,type:gin;type:varchar(255)[]" validate:"-" form:"-"`
	ReadNum        uint              `gorm:"default:0;"`
	CommentNum     uint              `gorm:"default:0;"`
	Version        uint              `gorm:"default:1;"`
	BookRefer      *string           `form:"book_refer" validate:"omitempty,uuid4" gorm:"type:uuid;index;default:NULL"`
	Visibility     ArticleVisibility `form:"visibility" gorm:"type:varchar(16);not null;default:public;index"`
	DisableComment bool              `form:"disable_comment"`

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
func (t Article) GetIndexID() string {
	return fmt.Sprintf("%s.%d", t.ID, t.Version)
}

// BeforeSave hook
func (t *Article) BeforeSave(tx *gorm.DB) (err error) {
	if t.Visibility == "" {
		t.Visibility = VisibilityPublic
	}
	if !t.Visibility.Valid() {
		return fmt.Errorf("invalid article visibility %q", t.Visibility)
	}
	t.RawTags = strings.TrimSpace(t.RawTags)
	t.parseRawTags()
	return nil
}

func (t *Article) PreviousVersions() []uint {
	var versions []uint
	for v := t.Version; v > 1 && len(versions) < 100; v-- {
		versions = append(versions, v-1)
	}
	return versions
}

func (t *Article) parseRawTags() {
	if t.RawTags == "" {
		t.Tags = nil
		return
	}
	// 去重并过滤空标签
	tags := strings.Split(t.RawTags, ",")
	t.Tags = lo.Uniq(lo.Filter(tags, func(item string, index int) bool {
		return strings.TrimSpace(item) != ""
	}))
}

// AfterFind hook
func (t *Article) AfterFind(tx *gorm.DB) (err error) {
	t.RawTags = strings.Join(t.Tags, ",")
	t.redactForQuery(tx)
	return nil
}

// IsTopic 是否是哔哔
func (t Article) IsTopic() bool {
	t.parseRawTags()
	return lo.Contains(t.Tags, "Topic")
}

// GenTOC 生成标题树
func (t *Article) GenTOC() {
	t.Toc = nil
	engine := lute.New()
	engine.SetHeadingID(true)
	tree := parse.Parse(t.ID, []byte(t.Content), engine.ParseOptions)
	var stack []*ArticleTOC
	ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering || n.Type != ast.NodeHeading {
			return ast.WalkContinue
		}
		toc := &ArticleTOC{Title: n.Text(), Slug: render.HeadingID(n), Level: n.HeadingLevel}
		for len(stack) > 0 && stack[len(stack)-1].Level >= toc.Level {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			t.Toc = append(t.Toc, toc)
		} else {
			toc.Parent = stack[len(stack)-1]
			toc.Parent.SubTitles = append(toc.Parent.SubTitles, toc)
		}
		stack = append(stack, toc)
		return ast.WalkSkipChildren
	})
}
