package solitudes

import (
	"github.com/google/wire"
	"github.com/jinzhu/gorm"
	"github.com/patrickmn/go-cache"
)

// Solitudes 依赖注入
type Solitudes struct {
	CacheService *cache.Cache
	Database     *gorm.DB
	Config       *Config
}

// NewSolitudes 依赖注入
func NewSolitudes(set wire.ProviderSet) *Solitudes {
	wire.Build(set, Solitudes{})
	return &Solitudes{}
}
