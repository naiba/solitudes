package theme

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadThemes(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "resource", "themes")
	themes, err := LoadThemes(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(themes.Site) == 0 {
		t.Fatal("site themes empty")
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
