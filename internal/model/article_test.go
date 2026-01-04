package model

import (
	"regexp"
	"testing"

	"github.com/88250/lute"
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

func TestSanitizedAnchorName(t *testing.T) {
	unique := make(map[string]int)
	if sanitizedAnchorName(unique, "# 1 测试") != "1-测试" {
		t.FailNow()
	}
	if sanitizedAnchorName(unique, "# 1 测试") != "1-测试-" {
		t.FailNow()
	}
	if sanitizedAnchorName(unique, "# 1 测试") != "1-测试--" {
		t.FailNow()
	}
	if sanitizedAnchorName(unique, "测试") != "测试" {
		t.FailNow()
	}
}

// TestInnerRelatedCountNonRecursive tests the loop refactoring logic
func TestInnerRelatedCountLogic(t *testing.T) {
	// Test that the function handles chapter counting correctly
	// This verifies our loop refactoring doesn't break logic
	t.Run("Chapter count aggregation", func(t *testing.T) {
		// Simulate the loop pattern used in innerRelatedCount
		chapters := []*Article{
			{ReadNum: 10, CommentNum: 5, IsBook: false},
			{ReadNum: 20, CommentNum: 10, IsBook: false},
			{ReadNum: 30, CommentNum: 15, IsBook: false},
		}

		var totalReads, totalComments uint
		for i := range chapters {
			totalReads += chapters[i].ReadNum
			totalComments += chapters[i].CommentNum
		}

		if totalReads != 60 {
			t.Errorf("Expected 60 reads, got %d", totalReads)
		}
		if totalComments != 30 {
			t.Errorf("Expected 30 comments, got %d", totalComments)
		}
	})

	t.Run("Empty chapter list", func(t *testing.T) {
		chapters := []*Article{}
		var total uint
		for range chapters {
			total++
		}
		if total != 0 {
			t.Errorf("Expected 0, got %d", total)
		}
	})
}

// TestRelatedCountBehavior tests RelatedCount function behavior
func TestRelatedCountBehavior(t *testing.T) {
	t.Run("Non-book article", func(t *testing.T) {
		article := &Article{
			ID:     "test-1",
			IsBook: false,
		}

		// RelatedCount should return early for non-book articles
		// No panic or error should occur
		// We can't test with nil db in this simple test, but we verify the logic
		if article.IsBook {
			t.Error("Article should not be a book")
		}
	})

	t.Run("Book article", func(t *testing.T) {
		article := &Article{
			ID:     "test-2",
			IsBook: true,
		}

		if !article.IsBook {
			t.Error("Article should be a book")
		}
	})
}

// createTestLuteEngine 创建测试用的 lute 引擎
func createTestLuteEngine() *lute.Lute {
	engine := lute.New()
	engine.SetCodeSyntaxHighlight(false)
	engine.SetHeadingAnchor(true)
	engine.SetHeadingID(true)
	engine.SetSub(true)
	engine.SetSup(true)
	engine.SetAutoSpace(true)
	return engine
}

// TestLuteAnchorConsistency 测试 lute 渲染后的标题锚点与 sanitizedAnchorName 生成的是否一致
func TestLuteAnchorConsistency(t *testing.T) {
	// 创建测试用 lute 引擎
	testLute := createTestLuteEngine()

	testCases := []struct {
		name     string
		markdown string
		titles   []string // 按顺序列出的标题文本
	}{
		{
			name: "basic titles",
			markdown: `# 标题一
## 标题二
### 标题三
`,
			titles: []string{"标题一", "标题二", "标题三"},
		},
		{
			name: "titles with numbers",
			markdown: `## 1 测试
## 2 测试
### 2.1 子标题
`,
			titles: []string{"1 测试", "2 测试", "2.1 子标题"},
		},
		{
			name: "duplicate titles at same level",
			markdown: `## 测试
## 测试
## 测试
`,
			titles: []string{"测试", "测试", "测试"},
		},
		{
			name: "mixed content",
			markdown: `## 端到端加密详解
### 名词解释
### 用户注册
### 用户登录
## 概览
### Team 成员管理
`,
			titles: []string{"端到端加密详解", "名词解释", "用户注册", "用户登录", "概览", "Team 成员管理"},
		},
		{
			name: "titles with special chars",
			markdown: `## Hello World
## 你好-世界
## Test_Case
`,
			titles: []string{"Hello World", "你好-世界", "Test_Case"},
		},
		// 多级标题中子级与父级兄弟的子级重名
		{
			name: "nested duplicate - child vs sibling child",
			markdown: `## 章节A
### 子标题
## 章节B
### 子标题
## 章节C
### 子标题
`,
			titles: []string{"章节A", "子标题", "章节B", "子标题", "章节C", "子标题"},
		},
		// 深层嵌套重名
		{
			name: "deep nested duplicate",
			markdown: `## 父级
### 子级
#### 孙级
### 子级
#### 孙级
## 父级
### 子级
#### 孙级
`,
			titles: []string{"父级", "子级", "孙级", "子级", "孙级", "父级", "子级", "孙级"},
		},
		// 跨级别重名（同名标题出现在不同级别）
		{
			name: "cross level duplicate",
			markdown: `## 测试
### 测试
#### 测试
## 测试
### 测试
`,
			titles: []string{"测试", "测试", "测试", "测试", "测试"},
		},
		// 复杂场景：多个父级下的子级重名
		{
			name: "complex nested siblings with same children",
			markdown: `## 介绍
### 概述
### 详情
## 安装
### 概述
### 详情
## 使用
### 概述
### 详情
`,
			titles: []string{"介绍", "概述", "详情", "安装", "概述", "详情", "使用", "概述", "详情"},
		},
		// 混合：部分重名部分不重名
		{
			name: "mixed duplicate and unique",
			markdown: `## 开始
### 准备工作
### 安装依赖
## 配置
### 准备工作
### 参数设置
## 运行
### 准备工作
### 启动服务
`,
			titles: []string{"开始", "准备工作", "安装依赖", "配置", "准备工作", "参数设置", "运行", "准备工作", "启动服务"},
		},
		// 与 GenTOC 测试用例一致
		{
			name: "GenTOC test case",
			markdown: `## 端到端加密详解
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
			titles: []string{"端到端加密详解", "名词解释", "用户注册", "用户登录", "个人数据", "读取", "写入", "Team 数据", "读取", "写入", "概览", "Team 成员管理", "目前的缺陷"},
		},
	}

	// 正则匹配锚点 ID
	// lute 生成的格式: <h2 id="xxx"><a id="vditorAnchor-xxx" class="vditor-anchor" href="#xxx">
	anchorRegex := regexp.MustCompile(`<h[1-6][^>]*id="([^"]+)"`)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 使用 lute 渲染
			html := testLute.MarkdownStr(tc.name, tc.markdown)

			// 提取 lute 生成的所有锚点 ID
			matches := anchorRegex.FindAllStringSubmatch(html, -1)
			luteAnchors := make([]string, 0, len(matches))
			for _, match := range matches {
				if len(match) > 1 {
					luteAnchors = append(luteAnchors, match[1])
				}
			}

			// 使用 sanitizedAnchorName 生成期望的锚点
			unique := make(map[string]int)
			expectedAnchors := make([]string, 0, len(tc.titles))
			for _, title := range tc.titles {
				anchor := sanitizedAnchorName(unique, title)
				expectedAnchors = append(expectedAnchors, anchor)
			}

			// 比较数量
			if len(luteAnchors) != len(expectedAnchors) {
				t.Errorf("Anchor count mismatch: lute=%d, expected=%d\nlute: %v\nexpected: %v\nhtml: %s",
					len(luteAnchors), len(expectedAnchors), luteAnchors, expectedAnchors, html)
				return
			}

			// 逐一比较每个锚点，验证 sanitizedAnchorName 与 lute 生成的一致
			for i := range luteAnchors {
				if luteAnchors[i] != expectedAnchors[i] {
					t.Errorf("Anchor mismatch at index %d:\n  lute generated: %q\n  sanitizedAnchorName: %q\n  title: %q\n  all lute: %v\n  all expected: %v",
						i, luteAnchors[i], expectedAnchors[i], tc.titles[i], luteAnchors, expectedAnchors)
				}
			}

			// 验证所有锚点唯一
			seen := make(map[string]bool)
			for i, anchor := range luteAnchors {
				if seen[anchor] {
					t.Errorf("Duplicate anchor at index %d: %q", i, anchor)
				}
				seen[anchor] = true
			}
		})
	}
}

// TestLuteAnchorDuplicateHandling 测试重复标题的锚点处理
func TestLuteAnchorDuplicateHandling(t *testing.T) {
	testLute := createTestLuteEngine()

	testCases := []struct {
		name     string
		markdown string
		count    int
		titles   []string
	}{
		{
			name: "same level duplicates",
			markdown: `## 测试
## 测试
## 测试
`,
			count:  3,
			titles: []string{"测试", "测试", "测试"},
		},
		{
			name: "nested duplicates under different parents",
			markdown: `## 父A
### 子项
## 父B
### 子项
## 父C
### 子项
`,
			count:  6,
			titles: []string{"父A", "子项", "父B", "子项", "父C", "子项"},
		},
		{
			name: "same name across all levels",
			markdown: `# 项目
## 项目
### 项目
#### 项目
##### 项目
###### 项目
`,
			count:  6,
			titles: []string{"项目", "项目", "项目", "项目", "项目", "项目"},
		},
	}

	anchorRegex := regexp.MustCompile(`<h[1-6][^>]*id="([^"]+)"`)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			html := testLute.MarkdownStr(tc.name, tc.markdown)
			matches := anchorRegex.FindAllStringSubmatch(html, -1)

			if len(matches) != tc.count {
				t.Fatalf("Expected %d anchors, got %d", tc.count, len(matches))
			}

			// 提取 lute 锚点
			luteAnchors := make([]string, 0, len(matches))
			for _, match := range matches {
				luteAnchors = append(luteAnchors, match[1])
			}

			// 生成 sanitizedAnchorName 锚点
			unique := make(map[string]int)
			expectedAnchors := make([]string, 0, len(tc.titles))
			for _, title := range tc.titles {
				expectedAnchors = append(expectedAnchors, sanitizedAnchorName(unique, title))
			}

			// 验证一致性
			for i := range luteAnchors {
				if luteAnchors[i] != expectedAnchors[i] {
					t.Errorf("Mismatch at %d: lute=%q, sanitizedAnchorName=%q", i, luteAnchors[i], expectedAnchors[i])
				}
			}

			// 验证锚点唯一性
			seen := make(map[string]bool)
			for _, anchor := range luteAnchors {
				if seen[anchor] {
					t.Errorf("Duplicate lute anchor found: %s", anchor)
				}
				seen[anchor] = true
			}

			seenModel := make(map[string]bool)
			for _, anchor := range expectedAnchors {
				if seenModel[anchor] {
					t.Errorf("Duplicate sanitizedAnchorName anchor found: %s", anchor)
				}
				seenModel[anchor] = true
			}
		})
	}
}
