package translator

import (
	"log"

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

func Init() {
	en := en.New()
	Trans = ut.New(en, en, zh.New())

	err := Trans.Import(ut.FormatJSON, "resource/translation")
	if err != nil {
		panic(err)
	}

	err = Trans.VerifyTranslations()
	if err != nil {
		panic(err)
	}
}
