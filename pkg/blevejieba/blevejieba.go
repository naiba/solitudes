package blevejieba

import (
	"errors"
	"fmt"
	"strings"

	"github.com/blevesearch/bleve/v2/analysis"
	"github.com/blevesearch/bleve/v2/registry"
	"github.com/liuzl/gocc"
	"github.com/yanyiwu/gojieba"
)

var goccI *gocc.OpenCC

func init() {
	var err error
	goccI, err = gocc.New("t2s")
	if err != nil {
		panic(err)
	}
	registry.RegisterAnalyzer("jieba", analyzerConstructor)
	registry.RegisterTokenizer("jieba", tokenizerConstructor)
	fmt.Println("[bleve-jieba] inited")
}

// JiebaTokenizer ..
type JiebaTokenizer struct {
	jieba        *gojieba.Jieba
	useHmm       bool
	tokenizeMode gojieba.TokenizeMode
}

func CleanText(text string) string {
	text = strings.ToLower(text)
	textNew, err := goccI.Convert(text)
	if err != nil {
		return text
	}
	return textNew
}

// Tokenize ..
func (s *JiebaTokenizer) Tokenize(sentence []byte) analysis.TokenStream {
	result := make(analysis.TokenStream, 0)
	words := s.jieba.Tokenize(string(sentence), s.tokenizeMode, s.useHmm)
	for pos, word := range words {
		token := analysis.Token{
			Start:    word.Start,
			End:      word.End,
			Position: pos + 1,
			Term:     []byte(CleanText(word.Str)),
			Type:     analysis.Ideographic,
		}
		result = append(result, &token)
	}
	return result
}

func tokenizerConstructor(config map[string]interface{}, cache *registry.Cache) (analysis.Tokenizer, error) {
	useHmm, ok := config["useHmm"].(bool)
	if !ok {
		return nil, errors.New("must specify useHmm")
	}
	tokenizeMode, ok := config["tokenizeMode"].(float64)
	if !ok {
		return nil, errors.New("must specify tokenizeMode")
	}
	tokenizer := &JiebaTokenizer{
		jieba:        gojieba.NewJieba(),
		useHmm:       useHmm,
		tokenizeMode: gojieba.TokenizeMode(tokenizeMode),
	}
	return tokenizer, nil
}

func analyzerConstructor(config map[string]interface{}, cache *registry.Cache) (analysis.Analyzer, error) {
	tokenizerName, ok := config["tokenizer"].(string)
	if !ok {
		return nil, errors.New("must specify tokenizer")
	}
	tokenizer, err := cache.TokenizerNamed(tokenizerName)
	if err != nil {
		return nil, err
	}
	jbtk, ok := tokenizer.(*JiebaTokenizer)
	if !ok {
		return nil, errors.New("tokenizer must be of type jieba")
	}
	alz := &JiebaAnalyzer{
		Tokenizer: jbtk,
	}
	return alz, nil
}

// JiebaAnalyzer from analysis.DefaultAnalyzer
type JiebaAnalyzer struct {
	CharFilters  []analysis.CharFilter
	Tokenizer    *JiebaTokenizer
	TokenFilters []analysis.TokenFilter
}

func (a *JiebaAnalyzer) Analyze(input []byte) analysis.TokenStream {
	if a.CharFilters != nil {
		for _, cf := range a.CharFilters {
			input = cf.Filter(input)
		}
	}
	tokens := a.Tokenizer.Tokenize(input)
	if a.TokenFilters != nil {
		for _, tf := range a.TokenFilters {
			tokens = tf.Filter(tokens)
		}
	}
	return tokens
}

func (a *JiebaAnalyzer) Free() {
	if a.Tokenizer != nil {
		a.Tokenizer.jieba.Free()
	} else {
		panic("JiebaAnalyzer.Tokenizer is nil, this should not happen")
	}
}
