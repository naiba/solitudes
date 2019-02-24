package wengine

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/biezhi/gorm-paginator/pagination"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"
)

func publish(c *gin.Context) {
	id := c.Query("id")
	var article solitudes.Article
	if id != "" {
		solitudes.System.D.Where("id = ?", id).First(&article)
	}
	c.HTML(http.StatusOK, "admin/publish", soligin.Soli(c, true, gin.H{
		"templates": solitudes.Templates,
		"article":   article,
	}))
}

var titleRegex = regexp.MustCompile(`^\s{0,2}(#{1,6})\s(.*)$`)
var whitespaces = regexp.MustCompile(`[\s|\.]{1,}`)

func genTOC(post *solitudes.Article) {
	lines := strings.Split(post.Content, "\n")
	var matches []string
	var currentToc *solitudes.ArticleTOC
	post.Toc = make([]*solitudes.ArticleTOC, 0)
	for j := 0; j < len(lines); j++ {
		matches = titleRegex.FindStringSubmatch(lines[j])
		if len(matches) == 3 {
			var toc solitudes.ArticleTOC
			toc.Level = len(matches[1])
			toc.Title = string(matches[2])
			toc.Slug = string(whitespaces.ReplaceAllString(matches[2], "-"))
			toc.SubTitles = make([]*solitudes.ArticleTOC, 0)
			if currentToc == nil {
				post.Toc = append(post.Toc, &toc)
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
						post.Toc = append(post.Toc, &toc)
					} else {
						toc.Parent = parent
						parent.SubTitles = append(parent.SubTitles, &toc)
					}
				} else if currentToc.Level == toc.Level {
					// 兄弟节点
					if parent.Parent == nil {
						post.Toc = append(post.Toc, &toc)
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

func publishHandler(c *gin.Context) {
	var err error
	if c.Query("action") == "delete" && c.Query("id") != "" {
		// delete article
		if err = solitudes.System.D.Unscoped().Delete(solitudes.Article{}, "id = ?", c.Query("id")).Error; err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.Redirect(http.StatusFound, "/admin/")
	} else {
		// new or edit article
		var newArticle solitudes.Article
		if err = c.ShouldBind(&newArticle); err != nil {
			c.String(http.StatusForbidden, err.Error())
			return
		}
		genTOC(&newArticle)
		if newArticle.ID != 0 {
			// get article in db
			var originArticle solitudes.Article
			if err := solitudes.System.D.First(&originArticle, "id = ?", newArticle.ID).Error; err != nil {
				c.String(http.StatusInternalServerError, err.Error())
				return
			}
			originArticle.Title = newArticle.Title
			originArticle.Slug = newArticle.Slug
			originArticle.Content = newArticle.Content
			originArticle.TemplateID = newArticle.TemplateID
			originArticle.RawTags = newArticle.RawTags
			originArticle.CollectionID = newArticle.CollectionID
			originArticle.IsCollection = newArticle.IsCollection
			newArticle = originArticle
		} else {
			newArticle.DeletedAt = nil
		}

		// update article version
		newArticle.Version = newArticle.Version + 1

		// save edit history && article
		tx := solitudes.System.D.Begin()
		err = tx.Save(&newArticle).Error
		if err == nil {
			var history solitudes.ArticleHistory
			history.Content = newArticle.Content
			history.Version = newArticle.Version
			history.ArticleID = newArticle.ID
			err = tx.Save(&history).Error
		}
		if err != nil {
			tx.Rollback()
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		if err = tx.Commit().Error; err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
	}
}

func article(c *gin.Context) {
	slug := c.MustGet(solitudes.CtxRequestParams).([]string)
	var a solitudes.Article

	if err := solitudes.System.D.Where("slug = ?", slug[1]).First(&a).Error; err == gorm.ErrRecordNotFound {
		c.HTML(http.StatusNotFound, "default/error", soligin.Soli(c, true, gin.H{
			"title": "404 Page Not Found",
			"msg":   "Wow ... This page may fly to Mars.",
		}))
		return
	} else if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	var prevPost, nextPost solitudes.Article
	if a.CollectionID == 0 {
		solitudes.System.D.Select("id,slug").Where("id > ?", a.ID).First(&nextPost)
		solitudes.System.D.Select("id,slug").Where("id < ?", a.ID).Order("id DESC").First(&prevPost)
	} else {
		solitudes.System.D.Select("id,slug").Where("collection_id = ? and  id > ?", a.CollectionID, a.ID).First(&nextPost)
		solitudes.System.D.Select("id,slug").Where("collection_id = ? and  id < ?", a.CollectionID, a.ID).Order("id DESC").First(&prevPost)
	}

	if len(a.Tags) == 0 {
		a.Tags = nil
	}

	c.HTML(http.StatusOK, "default/"+solitudes.TemplateIndex[a.TemplateID], soligin.Soli(c, false, gin.H{
		"article": a,
		"next":    nextPost.Slug,
		"prev":    prevPost.Slug,
	}))
}

func archive(c *gin.Context) {
	pageSlice := c.MustGet(solitudes.CtxRequestParams).([]string)
	var page int64
	if len(pageSlice) == 2 {
		page, _ = strconv.ParseInt(pageSlice[1], 10, 32)
	}
	var articles []solitudes.Article
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.D,
		Page:    int(page),
		Limit:   15,
		OrderBy: []string{"id desc"},
	}, &articles)
	c.HTML(http.StatusOK, "default/archive", soligin.Soli(c, false, gin.H{
		"articles": listArticleByYear(articles),
		"page":     pg,
	}))
}

func listArticleByYear(as []solitudes.Article) [][]solitudes.Article {
	var listed = make([][]solitudes.Article, 0)
	var lastYear int
	var listItem []solitudes.Article
	for i := 0; i < len(as); i++ {
		currentYear := as[i].UpdatedAt.Year()
		if currentYear != lastYear {
			if len(listItem) > 0 {
				listed = append(listed, listItem)
			}
			listItem = make([]solitudes.Article, 0)
			lastYear = currentYear
		}
		listItem = append(listItem, as[i])
	}
	if len(listItem) > 0 {
		listed = append(listed, listItem)
	}
	return listed
}
