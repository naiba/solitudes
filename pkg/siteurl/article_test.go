package siteurl

import (
	"strings"
	"testing"
)

func TestArticleSlugValidationKeepsRoutableUnicodeAndReservesSystemPaths(t *testing.T) {
	for _, slug := range []string{"中文文章", "Mixed-Case", "C#? +% &", "%2F-literal", "Poſts", "BOOKS"} {
		if err := ValidateArticleSlug(slug); err != nil {
			t.Errorf("valid slug %q: %v", slug, err)
		}
	}
	for _, slug := range []string{"", " ", " leading", "trailing ", "posts", "POSTS", "admin", ".well-known", "robots.txt", "a/b", "a\\b", ".", "..", "a\x00b", string([]byte{0xff}), strings.Repeat("中", 230)} {
		if err := ValidateArticleSlug(slug); err == nil {
			t.Errorf("invalid slug accepted: %q", slug)
		}
	}
}

func TestArticleSlugDecodeIsSinglePathDecoding(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"%E4%B8%AD%E6%96%87", "中文"}, {"C%23%3F%20+%25%20&", "C#? +% &"}, {"%252F-literal", "%2F-literal"}, {"A+B", "A+B"},
	} {
		got, err := DecodeArticleSlug(tc.raw)
		if err != nil || got != tc.want {
			t.Errorf("decode %q = %q %v", tc.raw, got, err)
		}
	}
	for _, raw := range []string{"%", "%ZZ", "a%2Fb", "a%5Cb", "%00", "%ff", "%2e", "%2e%2e"} {
		if got, err := DecodeArticleSlug(raw); err == nil {
			t.Errorf("unsafe segment %q became %q", raw, got)
		}
	}
}
