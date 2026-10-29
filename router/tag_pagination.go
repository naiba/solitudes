package router

import (
	"net/http"

	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func pagedTags(writer http.ResponseWriter, httpRequest *http.Request, articles *gorm.DB) (viewData, error) {
	page, err := listPage(queryValue(httpRequest, "page"))
	if err != nil {
		return nil, err
	}
	type tagCount struct {
		Tag   string
		Count int
	}
	var rows []tagCount
	grouped := articles.Model(&model.Article{}).Select("count(*) AS count, unnest(articles.tags) AS tag").Group("tag")
	err = solitudes.System.DB.Table("(?) AS tag_counts", grouped).Order("count DESC, tag ASC").Limit(51).Offset((page - 1) * 50).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	more := len(rows) > 50
	if more {
		rows = rows[:50]
	}
	tags, counts := make([]string, 0, len(rows)), make([]int, 0, len(rows))
	for _, row := range rows {
		tags = append(tags, row.Tag)
		counts = append(counts, row.Count)
	}
	return viewData{"tags": tags, "counts": counts, "navigation": pageNavigationFor(writer, httpRequest, "page", page, more, ""), "noindex": page > 1}, nil
}
