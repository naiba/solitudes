package router

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Validate both directions: references must exist, and definitions must have a
// consumer. Dynamic keys are enumerated explicitly, never guessed by prefix.
func TestTranslationCatalogContracts(t *testing.T) {
	t.Run("backend", func(t *testing.T) {
		used := map[string]bool{"access_editors": true, "access_private": true}
		for _, root := range []string{"../router", "../internal", "../pkg", "../cmd"} {
			err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
				if err != nil {
					return err
				}
				ast.Inspect(file, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok || len(call.Args) == 0 {
						return true
					}
					selector, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || selector.Sel.Name != "T" {
						return true
					}
					if literal, ok := call.Args[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
						key, err := strconv.Unquote(literal.Value)
						if err != nil {
							t.Fatal(err)
						}
						used[key] = true
					}
					return true
				})
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		checkTranslationCatalog(t, "../resource/translation", used)
	})

	pattern := regexp.MustCompile(`\.T\s+"([^"]+)"`)
	for _, kind := range []string{"site", "admin"} {
		roots, err := filepath.Glob(filepath.Join("..", "resource", "themes", kind, "*", "metadata.json"))
		if err != nil || len(roots) == 0 {
			t.Fatalf("list %s themes: %v", kind, err)
		}
		for _, metadata := range roots {
			root := filepath.Dir(metadata)
			t.Run(kind+"/"+filepath.Base(root), func(t *testing.T) {
				used := map[string]bool{}
				err := filepath.WalkDir(filepath.Join(root, "templates"), func(path string, entry fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if entry.IsDir() || !strings.HasSuffix(path, ".html") {
						return nil
					}
					body, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					for _, match := range pattern.FindAllSubmatch(body, -1) {
						used[string(match[1])] = true
					}
					if strings.Contains(string(body), ".Tr.T .Action") {
						for _, key := range auditActionNames {
							used[key] = true
						}
					}
					if strings.Contains(string(body), `printf "visibility_%s"`) {
						for _, level := range []string{"public", "members", "editors", "private"} {
							used["visibility_"+level] = true
						}
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				checkTranslationCatalog(t, filepath.Join(root, "translations"), used)
			})
		}
	}
}

func checkTranslationCatalog(t *testing.T, root string, used map[string]bool) {
	t.Helper()
	for _, locale := range []string{"en", "zh"} {
		body, err := os.ReadFile(filepath.Join(root, locale+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var entries []struct{ Locale, Key, Trans string }
		if err := json.Unmarshal(body, &entries); err != nil {
			t.Fatal(err)
		}
		defined := map[string]bool{}
		for _, entry := range entries {
			if defined[entry.Key] {
				t.Errorf("%s: duplicate key %q", locale, entry.Key)
			}
			defined[entry.Key] = true
			if entry.Locale != locale || strings.TrimSpace(entry.Trans) == "" {
				t.Errorf("%s: invalid locale or empty translation for %q", locale, entry.Key)
			}
			if !used[entry.Key] {
				t.Errorf("%s: unused translation %q", locale, entry.Key)
			}
		}
		for key := range used {
			if !defined[key] {
				t.Errorf("%s: missing translation %q", locale, key)
			}
		}
	}
}
