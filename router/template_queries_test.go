package router

import "testing"

func TestRandomIndices(t *testing.T) {
	for _, tc := range [][2]int{{0, 2}, {1, 2}, {10, 2}, {10, 10}, {10, 0}} {
		for attempt := 0; attempt < 20; attempt++ {
			indices, err := randomIndices(tc[0], tc[1])
			if err != nil || len(indices) != min(tc[0], tc[1]) {
				t.Fatalf("sample %v: %v %v", tc, indices, err)
			}
			seen := map[int]bool{}
			for _, i := range indices {
				if i < 0 || i >= tc[0] || seen[i] {
					t.Fatal("invalid or duplicate index", indices)
				}
				seen[i] = true
			}
		}
	}
	for _, tc := range [][2]int{{-1, 1}, {1, -1}, {10001, 1}, {2, 10001}} {
		if _, err := randomIndices(tc[0], tc[1]); err == nil {
			t.Fatal("unbounded sampling accepted")
		}
	}
}

func TestTemplateDict(t *testing.T) {
	if _, err := templateDict("odd"); err == nil {
		t.Fatal("odd arguments accepted")
	}
	if _, err := templateDict(1, "value"); err == nil {
		t.Fatal("non-string key accepted")
	}
	got, err := templateDict("value", 42)
	if err != nil || got["value"] != 42 {
		t.Fatal(got, err)
	}
}

func TestEditorCannotHideScriptsInRestrictedBlocks(t *testing.T) {
	for _, raw := range []string{
		"```access:members\n<script>alert(1)</script>\n```",
		"````access:private\n```access:editors\n[x](javascript:alert%281%29)\n```\n````",
		"```access:editors\n<img src=x onerror=alert(1)>\n```",
	} {
		if editorContentAllowed("", raw) {
			t.Fatal("editor bypassed script restriction", raw)
		}
	}
	if !editorContentAllowed("", "```access:members\n## Heading\n\n**Ordinary Markdown**\n```") {
		t.Fatal("ordinary restricted Markdown denied")
	}
}
