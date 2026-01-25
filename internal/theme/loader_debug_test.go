package theme

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

func TestLoadThemesDebug(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "resource", "themes")
	
	themes, err := LoadThemes(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	
	// 打印详细调试信息
	fmt.Println("\n=== Theme Loading Debug Info ===")
	
	fmt.Printf("\nSite Themes (%d):\n", len(themes.Site))
	for i, t := range themes.Site {
		data, _ := json.MarshalIndent(t, "  ", "  ")
		fmt.Printf("  [%d] %s\n", i, data)
	}
	
	fmt.Printf("\nAdmin Themes (%d):\n", len(themes.Admin))
	for i, t := range themes.Admin {
		data, _ := json.MarshalIndent(t, "  ", "  ")
		fmt.Printf("  [%d] %s\n", i, data)
	}
	
	// 验证 admin 主题数量
	if len(themes.Admin) == 0 {
		t.Error("❌ No admin themes loaded!")
	} else {
		fmt.Printf("\n✅ SUCCESS: %d admin theme(s) loaded\n", len(themes.Admin))
	}
}
