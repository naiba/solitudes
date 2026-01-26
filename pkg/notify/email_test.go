package notify

import (
	"strings"
	"testing"
)

// TestHasChineseCharacters tests Chinese character detection
func TestHasChineseCharacters(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "Simple Chinese",
			content:  "你好",
			expected: true,
		},
		{
			name:     "Traditional Chinese",
			content:  "繁體中文",
			expected: true,
		},
		{
			name:     "Mixed Chinese and English",
			content:  "Hello 世界",
			expected: true,
		},
		{
			name:     "Pure English",
			content:  "Hello World",
			expected: false,
		},
		{
			name:     "Numbers and symbols",
			content:  "123 !@# $%^",
			expected: false,
		},
		{
			name:     "Empty string",
			content:  "",
			expected: false,
		},
		{
			name:     "Chinese at the end",
			content:  "This is 测试",
			expected: true,
		},
		{
			name:     "Multiple Chinese characters",
			content:  "春风十里扬州路",
			expected: true,
		},
		{
			name:     "Simplified Chinese",
			content:  "简体中文",
			expected: true,
		},
		{
			name:     "Japanese characters",
			content:  "こんにちは",
			expected: false,
		},
		{
			name:     "Korean characters",
			content:  "안녕하세요",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasChineseCharacters(tt.content)
			if result != tt.expected {
				t.Errorf("hasChineseCharacters(%q) = %v, want %v", tt.content, result, tt.expected)
			}
		})
	}
}

// TestHasChineseIndicators tests UserAgent detection
func TestHasChineseIndicators(t *testing.T) {
	tests := []struct {
		name      string
		userAgent string
		expected  bool
	}{
		{
			name:      "zh-CN",
			userAgent: "Mozilla/5.0 (Windows NT 10.0; zh-CN; rv:91.0)",
			expected:  true,
		},
		{
			name:      "zh_CN",
			userAgent: "Mozilla/5.0 (Windows NT 10.0; zh_CN; rv:91.0)",
			expected:  true,
		},
		{
			name:      "zh-Hans",
			userAgent: "Mozilla/5.0 (zh-Hans) Gecko",
			expected:  true,
		},
		{
			name:      "zh-Hant",
			userAgent: "Mozilla/5.0 (zh-Hant) Gecko",
			expected:  true,
		},
		{
			name:      "zh-TW",
			userAgent: "Mozilla/5.0 (zh-TW) Gecko",
			expected:  true,
		},
		{
			name:      "zh_TW",
			userAgent: "Mozilla/5.0 (zh_TW) Gecko",
			expected:  true,
		},
		{
			name:      "Chinese keyword",
			userAgent: "Mozilla/5.0 (Chinese) Gecko",
			expected:  true,
		},
		{
			name:      "en-US",
			userAgent: "Mozilla/5.0 (Windows NT 10.0; en-US; rv:91.0)",
			expected:  false,
		},
		{
			name:      "ja-JP",
			userAgent: "Mozilla/5.0 (ja-JP) Gecko",
			expected:  false,
		},
		{
			name:      "Empty string",
			userAgent: "",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasChineseIndicators(tt.userAgent)
			if result != tt.expected {
				t.Errorf("hasChineseIndicators(%q) = %v, want %v", tt.userAgent, result, tt.expected)
			}
		})
	}
}

// TestReplaceString tests string placeholder replacement
func TestReplaceString(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		value    string
		expected string
	}{
		{
			name:     "Simple replacement",
			text:     "Hello {0}",
			value:    "World",
			expected: "Hello World",
		},
		{
			name:     "Multiple placeholders",
			text:     "{0} loves {0}",
			value:    "Go",
			expected: "Go loves Go",
		},
		{
			name:     "No placeholder",
			text:     "Hello World",
			value:    "Test",
			expected: "Hello World",
		},
		{
			name:     "Empty value",
			text:     "Title: {0}",
			value:    "",
			expected: "Title: ",
		},
		{
			name:     "Chinese value",
			text:     "《{0}》",
			value:    "标题",
			expected: "《标题》",
		},
		{
			name:     "Article title replacement",
			text:     "New reply on \"{0}\"",
			value:    "Go Best Practices",
			expected: "New reply on \"Go Best Practices\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := replaceString(tt.text, tt.value)
			if result != tt.expected {
				t.Errorf("replaceString(%q, %q) = %q, want %q", tt.text, tt.value, result, tt.expected)
			}
		})
	}
}

// TestGetEmailTexts tests email text retrieval for different languages
func TestGetEmailTexts(t *testing.T) {
	tests := []struct {
		name     string
		lang     string
		key      string
		notEmpty bool
	}{
		{
			name:     "English greeting",
			lang:     "en",
			key:      "greeting",
			notEmpty: true,
		},
		{
			name:     "English new_reply",
			lang:     "en",
			key:      "new_reply",
			notEmpty: true,
		},
		{
			name:     "Chinese greeting",
			lang:     "zh",
			key:      "greeting",
			notEmpty: true,
		},
		{
			name:     "Chinese subject",
			lang:     "zh",
			key:      "subject",
			notEmpty: true,
		},
		{
			name:     "English subject",
			lang:     "en",
			key:      "subject",
			notEmpty: true,
		},
		{
			name:     "Both languages have button text",
			lang:     "en",
			key:      "button_text",
			notEmpty: true,
		},
		{
			name:     "Chinese button text",
			lang:     "zh",
			key:      "button_text",
			notEmpty: true,
		},
		{
			name:     "All languages have copyright",
			lang:     "en",
			key:      "copyright",
			notEmpty: true,
		},
		{
			name:     "Chinese has copyright too",
			lang:     "zh",
			key:      "copyright",
			notEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			texts := getEmailTexts(tt.lang)
			value, ok := texts[tt.key]
			if !ok {
				t.Errorf("getEmailTexts(%q) missing key %q", tt.lang, tt.key)
				return
			}
			if tt.notEmpty && value == "" {
				t.Errorf("getEmailTexts(%q)[%q] is empty", tt.lang, tt.key)
			}
		})
	}
}

// TestBuildArticleURL tests article URL construction
func TestBuildArticleURL(t *testing.T) {
	tests := []struct {
		name   string
		slug   string
		domain string
	}{
		{
			name:   "Simple slug",
			slug:   "hello-world",
			domain: "example.com",
		},
		{
			name:   "Slug with numbers",
			slug:   "post-123",
			domain: "blog.example.com",
		},
		{
			name:   "Complex slug",
			slug:   "go-best-practices-2024",
			domain: "myblog.com",
		},
		{
			name:   "Chinese domain",
			slug:   "test-article",
			domain: "博客.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := buildArticleURL(tt.slug, tt.domain)
			if url == "" {
				t.Error("buildArticleURL() returned empty string")
			}
			if !strings.Contains(url, tt.slug) {
				t.Errorf("buildArticleURL(%q, %q) = %q, should contain slug", tt.slug, tt.domain, url)
			}
			if !strings.Contains(url, tt.domain) {
				t.Errorf("buildArticleURL(%q, %q) = %q, should contain domain", tt.slug, tt.domain, url)
			}
			if !strings.HasPrefix(url, "https://") {
				t.Errorf("buildArticleURL(%q, %q) = %q, should start with 'https://'", tt.slug, tt.domain, url)
			}
		})
	}
}

// TestGetDefaultLogoURL tests the default logo URL helper
func TestGetDefaultLogoURL(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		want   string
	}{
		{
			name:   "Simple domain",
			domain: "example.com",
			want:   "https://example.com/logo.png",
		},
		{
			name:   "Subdomain",
			domain: "blog.example.com",
			want:   "https://blog.example.com/logo.png",
		},
		{
			name:   "Chinese domain",
			domain: "博客.cn",
			want:   "https://博客.cn/logo.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getDefaultLogoURL(tt.domain)
			if got != tt.want {
				t.Errorf("getDefaultLogoURL(%q) = %q, want %q", tt.domain, got, tt.want)
			}
		})
	}
}
