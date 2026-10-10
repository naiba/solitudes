package router

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestArticleImageFigures(t *testing.T) {
	for _, tc := range []struct {
		name, markdown, caption string
		figures                 int
	}{
		{"caption", `![Accessible description](/photo.png "Visible caption")`, "Visible caption", 1},
		{"no invented caption", `![Only alt](/photo.png)`, "", 1},
		{"inline image", `Text ![icon](/icon.png "Icon") continues.`, "", 0},
		{"linked image", `[![Photo](/photo.png "Linked caption")](https://example.test/source)`, "Linked caption", 1},
		{"escaped caption", `![Photo](/photo.png "<script>alert('caption')</script>")`, "<script>alert('caption')</script>", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rendered := articleMarkdown("image-story", tc.markdown)
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(rendered))
			if err != nil {
				t.Fatal(err)
			}
			if doc.Find("figure.article-image").Length() != tc.figures {
				t.Fatalf("unexpected figures: %s", rendered)
			}
			if doc.Find("figcaption").Text() != tc.caption {
				t.Fatalf("caption: %s", rendered)
			}
			if tc.caption == "" && doc.Find("figcaption").Length() != 0 {
				t.Fatalf("invented caption: %s", rendered)
			}
			if doc.Find("p figure, script").Length() != 0 {
				t.Fatalf("invalid or unsafe figure: %s", rendered)
			}
			if doc.Find("img").Length() != 1 {
				t.Fatalf("lost image: %s", rendered)
			}
			if tc.name == "linked image" && doc.Find(`figure a[href="/r/go?url=aHR0cHM6Ly9leGFtcGxlLnRlc3Qvc291cmNl"] img`).Length() != 1 {
				t.Fatalf("lost link: %s", rendered)
			}
			if tc.name == "caption" && doc.Find("img").AttrOr("alt", "") != "Accessible description" {
				t.Fatal("lost alt")
			}
			if strings.Contains(mdRender("image-story", tc.markdown), "<figure") {
				t.Fatal("changed non-article Markdown")
			}
		})
	}
}
