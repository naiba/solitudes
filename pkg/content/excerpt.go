package content

import (
	"strings"

	"github.com/88250/lute"
	"github.com/88250/lute/ast"
	luteHTML "github.com/88250/lute/html"
	"github.com/88250/lute/parse"
)

// Excerpt returns plain prose, excluding headings, code samples, images and
// raw HTML (including access notices). It never exposes Markdown/HTML tokens
// in description metadata. Already-authorized content may be passed in.
func Excerpt(markdown string, limit int) string {
	if limit <= 0 {
		return ""
	}
	return cachedExcerpt(markdown, limit)
}

func excerpt(markdown string, limit int) string {
	engine := lute.New()
	tree := parse.Parse("", []byte(Public(markdown)), engine.ParseOptions)
	var text strings.Builder
	ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			if n.IsBlock() {
				text.WriteByte(' ')
			}
			return ast.WalkContinue
		}
		switch n.Type {
		case ast.NodeHeading, ast.NodeCodeBlock, ast.NodeImage, ast.NodeHTMLBlock, ast.NodeLinkRefDefBlock:
			return ast.WalkSkipChildren
		case ast.NodeText, ast.NodeLinkText, ast.NodeCodeSpanContent, ast.NodeHTMLEntity, ast.NodeBackslashContent:
			text.WriteString(luteHTML.UnescapeString(string(n.Tokens)))
		case ast.NodeSoftBreak, ast.NodeHardBreak:
			text.WriteByte(' ')
		}
		return ast.WalkContinue
	})
	words := strings.Fields(text.String())
	prose := words[:0]
	for _, word := range words {
		if !strings.HasPrefix(word, "https://") && !strings.HasPrefix(word, "http://") {
			prose = append(prose, word)
		}
	}
	runes := []rune(strings.Join(prose, " "))
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return string(runes)
}
