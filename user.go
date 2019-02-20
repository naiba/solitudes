package solitudes

import (
	"github.com/jinzhu/gorm"
)

// User 用户表
type User struct {
	gorm.Model
	Email    string `gorm:"unique_index"`
	Password string
	Name     string
}
