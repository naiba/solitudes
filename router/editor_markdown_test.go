package router

import (
	"strings"
	"testing"
)

func TestEditorContentAllowed(t *testing.T) {
	const adminScript = "<script>console.log('admin')</script>"
	for _, tc := range []struct {
		name, original, proposed string
		allowed                  bool
	}{
		{"normal Markdown", "hello", "# Hello\n[docs](https://example.org)\n![image](/photo.jpg)", true},
		{"JavaScript example is inert", "", "```javascript\nalert('example')\n```", true},
		{"new script", "", adminScript, false},
		{"new inline event", "", `<img src=x onerror=alert(1)>`, false},
		{"new iframe", "", `<iframe srcdoc="<script>alert(1)</script>"></iframe>`, false},
		{"new SVG", "", `<svg onload=alert(1)></svg>`, false},
		{"new HTML comment", "", "<!-- hidden -->", false},
		{"script inside code fence stays inert", "", "```html\n" + adminScript + "\n```", true},
		{"new javascript link", "", "[click](javascript:alert%281%29)", false},
		{"encoded javascript link", "", "[click](jav&#x61;script:alert%281%29)", false},
		{"new data image", "", "![x](data:image/svg+xml;base64,PHN2Zz4=)", false},
		{"reference javascript link", "", "[click][x]\n\n[x]: javascript:alert%281%29", false},
		{"admin script preserved", "before\n" + adminScript, "edited text\n" + adminScript, true},
		{"admin script changed", "before\n" + adminScript, "edited text\n<script>console.log('editor')</script>", false},
		{"inline admin script body changed", "before <script>alert(1)</script> after", "before <script>alert(2)</script> after", false},
		{"inline admin script body preserved", "before <script>alert(1)</script> after", "revised <script>alert(1)</script> after", true},
		{"mixed-case module script changed", "before <ScRiPt type=module>window.x=1</ScRiPt> after", "before <ScRiPt type=module>window.x=2</ScRiPt> after", false},
		{"inline admin style body changed", "before <style>body{color:red}</style> after", "before <style>body{color:blue}</style> after", false},
		{"inline template body changed", "before <template><b>safe</b></template> after", "before <template><b>unsafe</b></template> after", false},
		{"admin script removed", "before\n" + adminScript, "edited text", false},
		{"admin script duplicated", adminScript, adminScript + "\n" + adminScript, false},
		{"existing unsafe link preserved", "[go](javascript:alert%281%29)", "more\n[go](javascript:alert%281%29)", true},
		{"existing unsafe link changed", "[go](javascript:alert%281%29)", "[go](javascript:alert%282%29)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := editorContentAllowed(tc.original, tc.proposed); got != tc.allowed {
				t.Errorf("editorContentAllowed = %t, want %t", got, tc.allowed)
			}
		})
	}
}

func TestMarkdownImageAltEscaped(t *testing.T) {
	html := mdRender("image-alt-test", `![" onerror="alert(1)](photo.png)`)
	if strings.Contains(html, `onerror="alert(1)"`) || !strings.Contains(html, "&quot;") {
		t.Fatalf("unsafe image alt attribute: %s", html)
	}
}
