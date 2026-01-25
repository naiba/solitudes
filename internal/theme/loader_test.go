package theme

import (
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
