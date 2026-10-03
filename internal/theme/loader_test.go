package theme

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestBundledThemeResourceLayoutIsIdenticalForBothKinds(t *testing.T) {
	root := filepath.Join("..", "..", "resource", "themes")
	themes, err := LoadThemes(root)
	if err != nil {
		t.Fatal(err)
	}
	for kind, list := range map[string][]ThemeMeta{"site": themes.Site, "admin": themes.Admin} {
		for _, meta := range list {
			t.Run(kind+"/"+meta.ID, func(t *testing.T) {
				if meta.Name == "" || meta.Author == "" || meta.Version == "" || meta.Description == "" || meta.Link == "" {
					t.Fatal("incomplete metadata", meta)
				}
				for _, directory := range []string{"templates", "static", "translations"} {
					info, err := os.Stat(filepath.Join(root, kind, meta.ID, directory))
					if err != nil || !info.IsDir() {
						t.Fatalf("missing theme directory %s: %v", directory, err)
					}
				}
				for _, lang := range []string{"en", "zh"} {
					if _, err := os.Stat(filepath.Join(root, kind, meta.ID, "translations", lang+".json")); err != nil {
						t.Fatal(err)
					}
				}
				file, err := os.Open(filepath.Join(root, kind, meta.ID, "screenshot.png"))
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				config, err := png.DecodeConfig(file)
				if err != nil || config.Width < 1 || config.Height < 1 {
					t.Fatal("invalid screenshot", err)
				}
			})
		}
	}
}

func TestLoadThemes(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "resource", "themes")
	themes, err := LoadThemes(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(themes.Site) == 0 {
		t.Fatal("site themes empty")
	}
	foundDefault := false
	for _, entry := range themes.Admin {
		foundDefault = foundDefault || entry.ID == "default"
	}
	if !foundDefault {
		t.Fatal("bundled default administration theme is missing")
	}
}

func TestLoadThemesDiscoversMultipleAdminThemes(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"default", "custom-admin"} {
		path := filepath.Join(root, "admin", name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "metadata.json"), []byte(`{"id":"`+name+`","name":"`+name+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	themes, err := LoadThemes(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(themes.Admin) != 2 {
		t.Fatalf("custom administration theme not discovered: %+v", themes.Admin)
	}
}

func TestLoadThemesRejectsPathLikeMetadataID(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "site", "safe")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "metadata.json"), []byte(`{"id":"../../data"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadThemes(root); err == nil {
		t.Fatal("path-like theme ID was accepted")
	}
}
