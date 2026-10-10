package router

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// articleMarkdown enhances standalone images only; comments, feeds and inline
// icons retain the regular Markdown renderer and links keep their destinations.
func articleMarkdown(id, raw string) string {
	rendered := mdRender(id, raw)
	if !strings.Contains(rendered, "<img") {
		return rendered
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rendered))
	if err != nil {
		return rendered
	}
	doc.Find("p").Each(func(_ int, paragraph *goquery.Selection) {
		images := paragraph.Find("img")
		if images.Length() != 1 {
			return
		}
		standalone := paragraph.Clone()
		standalone.Find("img").Remove()
		if strings.TrimSpace(standalone.Text()) != "" || standalone.Find("br, code, video, iframe").Length() != 0 {
			return
		}
		// A figure inside a paragraph is invalid HTML: replace the paragraph itself
		// so crawlers and browsers see the same image/caption association without JS.
		paragraph.Get(0).Data = "figure"
		paragraph.Get(0).DataAtom = 0
		paragraph.AddClass("article-image")
		if caption := strings.TrimSpace(images.AttrOr("title", "")); caption != "" {
			node := &html.Node{Type: html.ElementNode, Data: "figcaption"}
			node.AppendChild(&html.Node{Type: html.TextNode, Data: caption})
			paragraph.AppendNodes(node)
		}
	})
	result, err := doc.Find("body").Html()
	if err != nil {
		return rendered
	}
	return result
}
