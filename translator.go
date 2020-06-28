package solitudes

import (
	"log"

	"github.com/go-playground/locales"
	"github.com/go-playground/locales/currency"
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

// C cardinal
func (t *Translator) C(key interface{}, num float64, digits uint64, param string) string {

	s, err := t.Trans.C(key, num, digits, param)
	if err != nil {
		log.Printf("issue translating cardinal key: '%v' error: '%s'", key, err)
	}

	return s
}

// O ordinal
func (t *Translator) O(key interface{}, num float64, digits uint64, param string) string {

	s, err := t.Trans.C(key, num, digits, param)
	if err != nil {
		log.Printf("issue translating ordinal key: '%v' error: '%s'", key, err)
	}

	return s
}

// R range
func (t *Translator) R(key interface{}, num1 float64, digits1 uint64, num2 float64, digits2 uint64, param1, param2 string) string {

	s, err := t.Trans.R(key, num1, digits1, num2, digits2, param1, param2)
	if err != nil {
		log.Printf("issue translating range key: '%v' error: '%s'", key, err)
	}

	return s
}

// Currency 货币
func (t *Translator) Currency() currency.Type {
	switch t.Locale() {
	case "en":
		return currency.USD
	default:
		return currency.CNY
	}
}

// Trans translations
var Trans *ut.UniversalTranslator

func init() {
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
