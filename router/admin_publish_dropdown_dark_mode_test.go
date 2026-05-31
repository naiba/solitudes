package router

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultAdminPublishDropdownDarkModeUtilities(t *testing.T) {
	publishTemplate := readDefaultAdminThemeFile(t, "templates", "publish.html")
	for _, expected := range []string{
		`id="tagsDropdown"`,
		`id="booksDropdown"`,
		"bg-white",
		"hover:bg-gray-100",
		"text-gray-500",
	} {
		if !strings.Contains(publishTemplate, expected) {
			t.Fatalf("publish template missing %q", expected)
		}
	}

	adminStyles := readDefaultAdminThemeFile(t, "static", "solitudes.css")
	darkModeStart := strings.Index(adminStyles, "@media (prefers-color-scheme: dark) {")
	if darkModeStart == -1 {
		t.Fatal("default admin stylesheet missing dark-mode override block")
	}

	comment := "Publish-page async dropdowns need explicit colors because their items are injected with utility classes."
	commentIndex := strings.Index(adminStyles, comment)
	if commentIndex == -1 {
		t.Fatalf("default admin stylesheet missing dropdown contrast comment %q", comment)
	}
	if commentIndex < darkModeStart {
		t.Fatalf("dropdown contrast rules must live inside the dark-mode override block")
	}

	for _, expected := range []string{
		"#tagsDropdown,\n  #booksDropdown {",
		"background-color: var(--bg-primary) !important;",
		"color: var(--text-primary) !important;",
		"border-color: var(--border-color) !important;",
		"box-shadow: var(--shadow-lg) !important;",
		"#tagsDropdown > div,\n  #booksDropdown > div {",
		"#tagsDropdown .text-gray-500,\n  #booksDropdown .text-gray-500 {",
		"color: var(--text-tertiary) !important;",
		"#tagsDropdown .hover\\:bg-gray-100:hover,\n  #booksDropdown .hover\\:bg-gray-100:hover {",
		"background-color: var(--bg-tertiary) !important;",
	} {
		if !strings.Contains(adminStyles, expected) {
			t.Fatalf("default admin stylesheet missing dropdown dark-mode rule %q", expected)
		}
	}
}

func readDefaultAdminThemeFile(t *testing.T, parts ...string) string {
	t.Helper()

	pathParts := append([]string{"..", "resource", "themes", "admin", "default"}, parts...)
	content, err := os.ReadFile(filepath.Join(pathParts...))
	if err != nil {
		t.Fatalf("read default admin theme file %v: %v", parts, err)
	}
	return string(content)
}
