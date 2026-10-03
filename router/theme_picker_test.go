package router

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/naiba/solitudes/internal/theme"
)

type pickerTranslator struct{}

func (pickerTranslator) T(key string) string { return key }

func TestThemePickerUsesIdenticalMetadataAndNativeSelectionForBothKinds(t *testing.T) {
	body, err := os.ReadFile("../resource/themes/admin/default/templates/theme_picker.html")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("picker").Parse(string(body))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"site", "admin"} {
		var output bytes.Buffer
		data := map[string]interface{}{"kind": kind, "title": "Themes", "selected": "second", "tr": pickerTranslator{}, "themes": []theme.ThemeMeta{
			{ID: "first", Name: "First", Version: "1.0", Author: "Author", Description: "Description", Link: "https://example.com"},
			{ID: "second", Name: "<script>unsafe</script>", Version: "2.0", Author: "Other", Description: "Second description", Link: "javascript:alert(1)"},
		}}
		if err := tmpl.ExecuteTemplate(&output, "admin/theme_picker", data); err != nil {
			t.Fatal(err)
		}
		html := output.String()
		for _, required := range []string{`role="radiogroup"`, `name="` + kind + `_theme"`, `value="second"`, ` checked`, `/admin/theme/preview/` + kind + `/first?v=1.0`, `/admin/theme/preview/` + kind + `/second?v=2.0`, `Second description`, `theme-preview-fallback`} {
			if !strings.Contains(html, required) {
				t.Fatalf("%s picker missing %q", kind, required)
			}
		}
		if strings.Count(html, `type="radio"`) != 2 || strings.Count(html, ` checked`) != 1 {
			t.Fatal("wrong radio group", html)
		}
		if strings.Contains(html, "<script>") || strings.Contains(html, `href="javascript:`) || strings.Contains(html, "placehold.co") {
			t.Fatal("unsafe metadata or external fallback", html)
		}
	}
}
