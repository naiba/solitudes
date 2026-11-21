package router

import (
	"strings"
	"testing"
)

func fencedBlock(language, body string) string {
	fence := strings.Repeat(string(rune(96)), 3)
	return fence + language + "\n" + body + "\n" + fence
}

// The site footer hands these blocks to Vditor's browser renderers, which turn
// their text back into DOM: links on diagram nodes, HTML in chart tooltips,
// error messages written with innerHTML. Escaping on the server does not reach
// that layer, so for an editor they are privileged content like raw HTML.
func TestEditorContentAllowed_BrowserRenderedBlocksArePrivileged(t *testing.T) {
	adminDiagram := fencedBlock("mermaid", "graph TD\nA-->B")
	for _, tc := range []struct {
		name, original, proposed string
		allowed                  bool
	}{
		{"new mermaid diagram", "", fencedBlock("mermaid", "graph TD\nA-->B\nclick A href \"javascript:alert(1)\""), false},
		{"new chart", "", fencedBlock("echarts", "{\"tooltip\":{\"formatter\":\"<img src=x onerror=alert(1)>\"}}"), false},
		{"new graphviz diagram", "", fencedBlock("graphviz", "digraph { a [URL=\"javascript:alert(1)\"] }"), false},
		{"new diagram with capitalised language", "", fencedBlock("Mermaid", "graph TD\nA-->B"), false},
		{"new math block", "", "$$\n\\unknown{<img src=x onerror=alert(1)>}\n$$", false},
		{"new math fence", "", fencedBlock("math", "x^2"), false},
		{"ordinary code block", "", fencedBlock("go", "func main() {}"), true},
		{"code block that only mentions a diagram language", "", fencedBlock("text", "mermaid echarts math"), true},
		{"admin diagram preserved", adminDiagram, "edited text\n\n" + adminDiagram, true},
		{"admin diagram changed", adminDiagram, fencedBlock("mermaid", "graph TD\nA-->C"), false},
		{"admin diagram removed", adminDiagram, "edited text", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := editorContentAllowed(tc.original, tc.proposed); got != tc.allowed {
				t.Errorf("editorContentAllowed = %t, want %t\nrendered: %s", got, tc.allowed, mdRender("editor-permissions", tc.proposed))
			}
		})
	}
}

// Only the host and path decide whether a link is a video. Matching the whole
// URL let a link carry a video site's name in its fragment or query and skip
// the /r/go interstitial every other external link goes through.
func TestIsVideoLinkLooksAtHostAndPathOnly(t *testing.T) {
	for link, want := range map[string]bool{
		"https://www.youtube.com/watch?v=abc":            true,
		"https://youtu.be/abc":                           true,
		"https://YOUTUBE.com/watch?v=abc":                true,
		"https://www.ted.com/talks/some_talk":            true,
		"https://www.facebook.com/someone/videos/123":    true,
		"https://www.facebook.com/someone/photos/123":    false,
		"https://www.ted.com/about":                      false,
		"https://evil.example/#youtube.com":              false,
		"https://evil.example/?next=youtube.com":         false,
		"https://evil.example/youtube.com":               false,
		"https://notyoutube.com/watch":                   false,
		"https://youtube.com.evil.example/watch":         false,
		"https://evil.example/x/facebook.com/a/videos/1": false,
	} {
		if got := isVideoLink(link); got != want {
			t.Errorf("isVideoLink(%q) = %t, want %t", link, got, want)
		}
	}
}
