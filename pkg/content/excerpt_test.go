package content

import "testing"

func TestExcerptUsesProseNotMarkupOrAccessNotices(t *testing.T) {
	raw := "# Heading\n\nHello **world** &amp; `code` [link](https://example.com)\n\n![alt](https://image.example)\n\n```access:members\nhiddensecret\n```\n\n<aside class=\"access-notice\"><p>Sign in</p></aside>\n\nFinal."
	if got := Excerpt(raw, 200); got != "Hello world & code link Final." {
		t.Fatal(got)
	}
	if got := Excerpt("中文摘要", 2); got != "中文…" {
		t.Fatal(got)
	}
	if got := Excerpt("text", -1); got != "" {
		t.Fatal(got)
	}
}
