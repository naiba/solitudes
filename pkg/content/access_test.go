package content

import (
	"strings"
	"testing"

	"github.com/88250/lute"
)

func TestAccessAST(t *testing.T) {
	source := "# Public\n\n````access:members\n## Member heading\n\n**member-secret**\n\n```access:editors\neditor-secret\n```\n\n| A | B |\n| - | - |\n| 1 | 2 |\n````\n\nAfter\n"
	for _, tc := range []struct {
		name           string
		allowed        func(string) bool
		member, editor bool
	}{
		{"guest", nil, false, false},
		{"member", func(s string) bool { return s == "members" }, true, false},
		{"editor", func(string) bool { return true }, true, true},
		{"outer denied", func(s string) bool { return s == "editors" }, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Filter(source, tc.allowed, func(s string) string { return "> Restricted " + s })
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(got, "member-secret") != tc.member || strings.Contains(got, "editor-secret") != tc.editor {
				t.Fatalf("bad redaction: %s", got)
			}
			if !strings.Contains(got, "Public") || !strings.Contains(got, "After") {
				t.Fatal(got)
			}
			if tc.member && !strings.Contains(lute.New().MarkdownStr("", got), "<table>") {
				t.Fatal("nested Markdown table not rendered", got)
			}
		})
	}
}

func TestAccessDoesNotChangeOrdinaryMarkdown(t *testing.T) {
	for _, markdown := range []string{
		"# Heading\n\n> quote\n\n- one\n- two\n\n**bold** _em_ [link](https://example.com)",
		"```go\nfmt.Println(\"access:members\")\n```", "`access:private`", ":::access members\nordinary text\n:::",
		"````markdown\n```access:members\nexample, not restricted\n```\n````",
		"    ```access:members\n    indented example\n    ```", "<div>access:members</div>",
	} {
		got, err := Filter(markdown, nil, nil)
		if err != nil || got != markdown {
			t.Fatalf("ordinary Markdown changed: %q => %q (%v)", markdown, got, err)
		}
	}
}

func TestAccessMalformedFailsClosed(t *testing.T) {
	for _, markdown := range []string{"```access:members\nsecret", "```access:unknown\nsecret\n```", "````access:members\n```access:typo\nsecret\n```\n````"} {
		if _, err := Filter(markdown, nil, nil); err == nil {
			t.Fatalf("accepted invalid block: %s", markdown)
		}
		if got := Public(markdown); got != "" {
			t.Fatalf("malformed content leaked: %s", got)
		}
	}
}

func TestAccessInsideContainersAndReferences(t *testing.T) {
	for _, markdown := range []string{
		"> ```access:members\n> secret\n> ```\n\nOutside",
		"- Item\n\n  ```access:private\n  secret\n  ```\n\nOutside",
		"[outside][key]\n\n```access:members\n[key]: https://secret.example\n```",
	} {
		got := Public(markdown)
		if strings.Contains(got, "secret") {
			t.Fatal("container/reference leak", got)
		}
	}
}
