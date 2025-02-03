package pagination

import (
	"math"

	"gorm.io/gorm"
)

// Param 分页参数
type Param struct {
	DB      *gorm.DB
	Page    int
	Limit   int
	OrderBy []string
	ShowSQL bool
}

// Paginator 分页返回结果
type Paginator struct {
	TotalRecord int         `json:"total_record"`
	TotalPage   int         `json:"total_page"`
	Offset      int         `json:"offset"`
	Limit       int         `json:"limit"`
	Page        int         `json:"page"`
	PrevPage    int         `json:"prev_page"`
	NextPage    int         `json:"next_page"`
	Records     interface{} `json:"records"`
}

// Paginate 分页查询
func Paging(p *Param, result interface{}) *Paginator {
	db := p.DB
	if p.ShowSQL {
		db = db.Debug()
	}

	// 设置默认值
	if p.Page < 1 {
		p.Page = 1
	}
	if p.Limit == 0 {
		p.Limit = 10
	}

	// 添加排序
	if len(p.OrderBy) > 0 {
		for _, order := range p.OrderBy {
			db = db.Order(order)
		}
	}

	// 计算总记录数
	var count int64
	db.Model(result).Count(&count)

	// 计算偏移量
	offset := 0
	if p.Page > 1 {
		offset = (p.Page - 1) * p.Limit
	}

	// 查询记录
	db.Limit(p.Limit).Offset(offset).Find(result)

	// 计算总页数
	totalPage := int(math.Ceil(float64(count) / float64(p.Limit)))

	// 计算上一页和下一页
	prevPage := p.Page
	if p.Page > 1 {
		prevPage = p.Page - 1
	}

	nextPage := p.Page
	if p.Page < totalPage {
		nextPage = p.Page + 1
	}

	return &Paginator{
		TotalRecord: int(count),
		TotalPage:   totalPage,
		Offset:      offset,
		Limit:       p.Limit,
		Page:        p.Page,
		PrevPage:    prevPage,
		NextPage:    nextPage,
		Records:     result,
	}
}
