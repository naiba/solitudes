package pagination

import (
	"errors"
	"fmt"
	"strconv"

	"gorm.io/gorm"
)

const MaxPage = 1000

var ErrInvalidPage = errors.New("invalid page number")

func Parse(raw string) (int, error) {
	if raw == "" {
		return 1, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > MaxPage {
		return 0, ErrInvalidPage
	}
	return n, nil
}

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
func Paging(p *Param, result interface{}) (*Paginator, error) {
	db := p.DB
	if p.ShowSQL {
		db = db.Debug()
	}

	// 设置默认值
	if p.Page == 0 {
		p.Page = 1
	}
	if p.Limit == 0 {
		p.Limit = 10
	}
	if p.Page < 1 || p.Page > MaxPage || p.Limit < 1 || p.Limit > 100 {
		return nil, ErrInvalidPage
	}

	// 添加排序
	if len(p.OrderBy) > 0 {
		for _, order := range p.OrderBy {
			db = db.Order(order)
		}
	}

	// 计算总记录数
	var count int64
	if err := db.Session(&gorm.Session{}).Model(result).Count(&count).Error; err != nil {
		return nil, fmt.Errorf("count page: %w", err)
	}

	// 计算偏移量
	offset := 0
	if p.Page > 1 {
		offset = (p.Page - 1) * p.Limit
	}

	// 查询记录
	if err := db.Limit(p.Limit).Offset(offset).Find(result).Error; err != nil {
		return nil, fmt.Errorf("query page: %w", err)
	}

	// 计算总页数
	totalPage := max(1, int((count+int64(p.Limit)-1)/int64(p.Limit)))

	// 计算上一页和下一页
	prevPage := p.Page
	if p.Page > 1 {
		prevPage = p.Page - 1
	}

	nextPage := p.Page
	if p.Page < totalPage && p.Page < MaxPage {
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
	}, nil
}
