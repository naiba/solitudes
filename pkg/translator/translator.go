package translator

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/go-playground/locales"
	"github.com/go-playground/locales/en"
	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
)

// Translator 翻译
type Translator struct {
	locales.Translator
	Trans ut.Translator
}

// T 普通翻译
func (t *Translator) T(key interface{}, params ...string) string {

	s, err := t.Trans.T(key, params...)
	if err != nil {
		log.Printf("issue translating key: '%v' error: '%s'", key, err)
	}
	return s
}

// Trans translations
var Trans *ut.UniversalTranslator

type transEntry struct {
	Locale string `json:"locale"`
	Key    string `json:"key"`
	Trans  string `json:"trans"`
}

func Init() {
	Reload("", "")
}

// Reload reloads all translations with overrides
func Reload(siteTheme, adminTheme string) {
	enLoc := en.New()
	Trans = ut.New(enLoc, enLoc, zh.New())

	// 用于存储合并后的翻译
	// map[locale]map[key]trans
	merged := make(map[string]map[string]string)
	localesList := []string{"en", "zh"}
	for _, l := range localesList {
		merged[l] = make(map[string]string)
	}

	// 辅助函数：从目录加载并合并
	loadFromDir := func(dir string) {
		if dir == "" {
			return
		}
		for _, l := range localesList {
			path := filepath.Join(dir, l+".json")
			if _, err := os.Stat(path); err != nil {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				log.Printf("failed to read translation file %s: %v", path, err)
				continue
			}
			var entries []transEntry
			if err := json.Unmarshal(data, &entries); err != nil {
				log.Printf("failed to unmarshal translation file %s: %v", path, err)
				continue
			}
			for _, e := range entries {
				loc := e.Locale
				if loc == "" {
					loc = l
				}
				if _, ok := merged[loc]; ok {
					merged[loc][e.Key] = e.Trans
				}
			}
		}
	}

	// 1. 加载基础翻译
	loadFromDir("resource/translation")

	// 2. 加载管理后台翻译
	if adminTheme == "" {
		adminTheme = "default"
	}
	loadFromDir(fmt.Sprintf("resource/themes/admin/%s/translations", adminTheme))

	// 3. 加载站点主题翻译
	loadFromDir(fmt.Sprintf("resource/themes/site/%s/translations", siteTheme))

	// 将合并后的翻译注入 UniversalTranslator
	for locale, keys := range merged {
		t, _ := Trans.GetTranslator(locale)
		for key, trans := range keys {
			// 对于包含 {0} 等占位符的翻译，universal-translator 需要特殊处理
			// 这里简单起见使用 Add，如果报错说明需要处理复数形式等，但 Solitudes 目前都是简单字符串
			if err := t.Add(key, trans, true); err != nil {
				// log.Printf("failed to add translation [%s] %s: %v", locale, key, err)
			}
		}
	}

	if err := Trans.VerifyTranslations(); err != nil {
		log.Printf("failed to verify translations: %v", err)
	}
}
