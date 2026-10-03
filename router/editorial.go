package router

import "github.com/naiba/solitudes/internal/model"

func tocHeadingCount(items []*model.ArticleTOC) int {
	count := 0
	for _, item := range items {
		if item != nil {
			count += 1 + tocHeadingCount(item.SubTitles)
		}
	}
	return count
}
