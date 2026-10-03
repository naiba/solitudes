// Package content implements Markdown extensions independently of themes.
package content

import (
	"errors"
	"strings"

	"github.com/88250/lute"
	"github.com/88250/lute/ast"
	"github.com/88250/lute/parse"
	"github.com/88250/lute/render"
)

var ErrAccessBlock = errors.New("invalid access block: use a closed access:members, access:editors or access:private fence (maximum nesting 16)")

// Filter removes inaccessible subtrees before any Markdown reaches a template,
// excerpt generator or indexer. Nil allowed means the anonymous/public view.
// Fenced blocks isolate reference definitions as well as ordinary block syntax.
// On malformed input callers must discard the entire result (fail closed).
func Filter(markdown string, allowed func(string) bool, notice func(string) string) (string, error) {
	return filter(markdown, allowed, notice, 0)
}

func filter(markdown string, allowed func(string) bool, notice func(string) string, depth int) (string, error) {
	if depth > 16 {
		return "", ErrAccessBlock
	}
	if !strings.Contains(markdown, "access:") {
		return markdown, nil
	}
	engine := lute.New()
	engine.SetHeadingID(true)
	engine.SetSub(true)
	engine.SetSup(true)
	// Parse's inline phase synthesizes missing closing fences. Inspect the
	// native block AST first, while that distinction is still available.
	blocksOnly := parse.Block("", []byte(markdown), engine.ParseOptions)
	invalid := false
	ast.Walk(blocksOnly.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if entering && n.Type == ast.NodeCodeBlock && strings.HasPrefix(string(n.CodeBlockInfo), "access:") && len(n.CodeBlockCloseFence) == 0 {
			invalid = true
		}
		return ast.WalkContinue
	})
	if invalid {
		return "", ErrAccessBlock
	}
	tree := parse.Parse("", []byte(markdown), engine.ParseOptions)
	var blocks []*ast.Node
	ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if entering && n.Type == ast.NodeCodeBlock && n.IsFencedCodeBlock && strings.HasPrefix(string(n.CodeBlockInfo), "access:") {
			blocks = append(blocks, n)
			return ast.WalkSkipChildren
		}
		return ast.WalkContinue
	})
	if len(blocks) == 0 {
		return markdown, nil // ordinary Markdown stays byte-for-byte unchanged
	}
	for _, block := range blocks {
		level := strings.TrimPrefix(string(block.CodeBlockInfo), "access:")
		if (level != "members" && level != "editors" && level != "private") || len(block.CodeBlockCloseFence) == 0 {
			return "", ErrAccessBlock
		}
		code := block.ChildByType(ast.NodeCodeBlockCode)
		if code == nil {
			return "", ErrAccessBlock
		}
		// Validate nested blocks even when the outer block is denied.
		inner, err := filter(string(code.Tokens), allowed, notice, depth+1)
		if err != nil {
			return "", err
		}
		if allowed == nil || !allowed(level) {
			inner = ""
			if notice != nil {
				inner = notice(level)
			}
		}
		replacement := parse.Parse("", []byte(inner), engine.ParseOptions)
		for child := replacement.Root.FirstChild; child != nil; {
			next := child.Next
			block.InsertBefore(child)
			child = next
		}
		block.Unlink()
	}
	return string(render.NewFormatRenderer(tree, engine.RenderOptions, engine.ParseOptions).Render()), nil
}

func Validate(markdown string) error {
	_, err := Filter(markdown, nil, nil)
	return err
}

func Public(markdown string) string {
	result, err := Filter(markdown, nil, nil)
	if err != nil {
		return ""
	}
	return result
}
