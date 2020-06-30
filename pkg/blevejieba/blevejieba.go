package blevejieba

import (
	"errors"
	"fmt"

	"github.com/blevesearch/bleve/analysis"
	"github.com/blevesearch/bleve/registry"
	"github.com/yanyiwu/gojieba"
)

func init() {
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

// Tokenize ..
func (s *JiebaTokenizer) Tokenize(sentence []byte) analysis.TokenStream {
	result := make(analysis.TokenStream, 0)
	words := s.jieba.Tokenize(string(sentence), s.tokenizeMode, s.useHmm)
	for pos, word := range words {
		token := analysis.Token{
			Start:    word.Start,
			End:      word.End,
			Position: pos + 1,
			Term:     []byte(word.Str),
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

// JiebaAnalyzer ..
type JiebaAnalyzer struct{}

func analyzerConstructor(config map[string]interface{}, cache *registry.Cache) (*analysis.Analyzer, error) {
	tokenizerName, ok := config["tokenizer"].(string)
	if !ok {
		return nil, errors.New("must specify tokenizer")
	}
	tokenizer, err := cache.TokenizerNamed(tokenizerName)
	if err != nil {
		return nil, err
	}
	alz := &analysis.Analyzer{
		Tokenizer: tokenizer,
	}
	return alz, nil
}
