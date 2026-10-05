package content

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/88250/lute"
)

func TestRenderedDiffAlignsBlocksWithoutLosingContent(t *testing.T) {
	for _, tc := range []struct{ before, after, kinds string }{
		{"", "", ""},
		{"<p>A</p>", "<p>A</p>", "same"},
		{"<p>A</p>", "<p>B</p>", "changed"},
		{"", "<p>A</p>", "added"},
		{"<p>A</p>", "", "removed"},
		{"<p>A</p><p>B</p>", "<p>X</p><p>A</p><p>B</p><p>Y</p>", "added,same,same,added"},
		{"<p>A</p><p>B</p><p>C</p>", "<p>A</p><p>C</p>", "same,removed,same"},
	} {
		rows, err := CompareHTML(tc.before, tc.after)
		if err != nil {
			t.Fatal(err)
		}
		var kinds []string
		var old, new strings.Builder
		for _, row := range rows {
			kinds = append(kinds, row.Kind)
			old.WriteString(row.Before)
			new.WriteString(row.After)
		}
		if strings.Join(kinds, ",") != tc.kinds || old.String() != tc.before || new.String() != tc.after {
			t.Fatalf("incorrect alignment: %+v", rows)
		}
	}
}

func TestRenderedDiffComparesHTMLNotMarkdown(t *testing.T) {
	engine := lute.New()
	rows, err := CompareHTML(engine.MarkdownStr("old", "**Bold**"), engine.MarkdownStr("new", "__Bold__"))
	if err != nil || len(rows) != 1 || rows[0].Kind != "same" || !strings.Contains(rows[0].After, "<strong>Bold</strong>") {
		t.Fatalf("Markdown-only difference was highlighted: %+v %v", rows, err)
	}
	markup := "<h2>Heading</h2><ul><li>Item</li></ul><table><tbody><tr><td>Cell</td></tr></tbody></table><pre><code>a &lt; b</code></pre>"
	rows, err = CompareHTML("", markup)
	if err != nil || len(rows) != 4 {
		t.Fatalf("structured blocks lost: %+v %v", rows, err)
	}
}

func TestRenderedDiffIsInert(t *testing.T) {
	unsafe := `<script>alert('secret-script')</script><style>body{display:none}</style><p id="header" onclick="alert(1)">Visible <a href="javascript:alert(1)">link</a><img src="/logo.png" onerror="alert(1)"></p><iframe src="https://example.test"></iframe><form action="/logout"><input autofocus name="x"></form><svg onload="alert(1)"></svg>`
	rows, err := CompareHTML(unsafe, unsafe)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		for _, danger := range []string{"<script", "<style", "<iframe", "<form", "<input", "<svg", "onclick", "onerror", "onload", "javascript:", `id="header"`, "secret-script"} {
			if strings.Contains(row.Before, danger) || strings.Contains(row.After, danger) {
				t.Fatalf("unsafe comparison: %s", danger)
			}
		}
	}
}

func TestRenderedDiffBoundsWork(t *testing.T) {
	for _, input := range []string{strings.Repeat("x", MaxDiffBytes+1), strings.Repeat("<p>x</p>", maxDiffBlocks+1)} {
		for _, pair := range [][2]string{{input, ""}, {"", input}} {
			if _, err := CompareHTML(pair[0], pair[1]); !errors.Is(err, ErrDiffTooLarge) {
				t.Fatalf("unbounded input accepted: %v", err)
			}
		}
	}
}

func BenchmarkRenderedDiff(b *testing.B) {
	var before, after strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&before, "<p>Paragraph %d before</p>", i)
		fmt.Fprintf(&after, "<p>Paragraph %d after</p>", i)
	}
	a, z := before.String(), after.String()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := CompareHTML(a, z); err != nil {
			b.Fatal(err)
		}
	}
}
