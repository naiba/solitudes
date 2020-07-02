package model

// User 用户表
type User struct {
	Email    string
	Nickname string
	/*
		Password 用户密码
		* default password: 123456
		* gen password: https://bcrypt-generator.com/
	*/
	Password     string
	Token        string
	TokenExpires int64
}
