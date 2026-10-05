package content

import (
	"errors"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var ErrDiffTooLarge = errors.New("rendered comparison exceeds size limit")

const MaxDiffBytes = 1 << 20
const maxDiffBlocks = 600

// HTMLChange contains sanitized, rendered blocks, never Markdown source.
type HTMLChange struct {
	Before string
	After  string
	Kind   string
}

var diffPolicy = bluemonday.UGCPolicy()

func stripDiffIdentifiers(node *html.Node) {
	attrs := node.Attr[:0]
	for _, attr := range node.Attr {
		if attr.Key != "id" && attr.Key != "name" {
			attrs = append(attrs, attr)
		}
	}
	node.Attr = attrs
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		stripDiffIdentifiers(child)
	}
}

func diffBlocks(source string) ([]string, error) {
	if len(source) > MaxDiffBytes {
		return nil, ErrDiffTooLarge
	}
	// Strip scripts, styles, event handlers, forms, frames and IDs. Comparison
	// is inert prose, even when an administrator's original embeds are executable.
	safe := diffPolicy.Sanitize(source)
	nodes, err := html.ParseFragment(strings.NewReader(safe), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return nil, err
	}
	var blocks []string
	for _, node := range nodes {
		stripDiffIdentifiers(node)
		if node.Type == html.TextNode && strings.TrimSpace(node.Data) == "" {
			continue
		}
		var out strings.Builder
		if err := html.Render(&out, node); err != nil {
			return nil, err
		}
		blocks = append(blocks, out.String())
		if len(blocks) > maxDiffBlocks {
			return nil, ErrDiffTooLarge
		}
	}
	return blocks, nil
}

// CompareHTML aligns top-level rendered blocks. A bounded LCS keeps list/table
// markup intact and avoids invalid HTML produced by inserting word-level tags
// across element boundaries. Memory is bounded to roughly 720 KiB for alignment.
func CompareHTML(before, after string) ([]HTMLChange, error) {
	a, err := diffBlocks(before)
	if err != nil {
		return nil, err
	}
	b, err := diffBlocks(after)
	if err != nil {
		return nil, err
	}
	cols := len(b) + 1
	dp := make([]uint16, (len(a)+1)*cols)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i*cols+j] = 1 + dp[(i+1)*cols+j+1]
			} else {
				dp[i*cols+j] = max(dp[(i+1)*cols+j], dp[i*cols+j+1])
			}
		}
	}
	var rows []HTMLChange
	var removed, added []string
	flush := func() {
		for n := 0; n < max(len(removed), len(added)); n++ {
			row := HTMLChange{Kind: "changed"}
			if n < len(removed) {
				row.Before = removed[n]
			} else {
				row.Kind = "added"
			}
			if n < len(added) {
				row.After = added[n]
			} else {
				row.Kind = "removed"
			}
			rows = append(rows, row)
		}
		removed, added = nil, nil
	}
	for i, j := 0, 0; i < len(a) || j < len(b); {
		if i < len(a) && j < len(b) && a[i] == b[j] {
			flush()
			rows = append(rows, HTMLChange{Before: a[i], After: b[j], Kind: "same"})
			i++
			j++
		} else if i < len(a) && (j == len(b) || dp[(i+1)*cols+j] >= dp[i*cols+j+1]) {
			removed = append(removed, a[i])
			i++
		} else {
			added = append(added, b[j])
			j++
		}
	}
	flush()
	return rows, nil
}
