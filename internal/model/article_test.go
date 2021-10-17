package model

import (
	"testing"
)

func TestGenTOC(t *testing.T) {
	var post = &Article{
		Content: `这个端到端加密的工具
## 端到端加密详解
### 名词解释
### 用户注册
### 用户登录
### 个人数据
#### 读取
#### 写入
### Team 数据
#### 读取
#### 写入
## 概览
### Team 成员管理
### 目前的缺陷
`,
	}
	post.GenTOC()
	index := 0
	validateToc(t, post.Toc, &index, []int{2, 3, 3, 3, 3, 4, 4, 3, 4, 4, 2, 3, 3})
}

func validateToc(t *testing.T, toc []*ArticleTOC, now *int, expect []int) {
	for _, toc_item := range toc {
		if toc_item.Level != expect[*now] {
			t.FailNow()
		}
		*now++
		validateToc(t, toc_item.SubTitles, now, expect)
	}
}
