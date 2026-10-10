package router

import (
	"encoding/base64"
	"html"
	"html/template"
	"net/url"
	"regexp"
	"strings"
)

var profileURLPattern = regexp.MustCompile(`(?i)https?://[^\s<>"'，。；！？、（）【】「」]+`)

func externalLink(raw string) string {
	if raw == "" {
		return ""
	}
	return "/r/go?url=" + base64.URLEncoding.EncodeToString([]byte(raw))
}

// profileBio linkifies plain text, not Markdown or user HTML. Escape every
// segment before marking the generated markup safe; otherwise profile bios
// could inject HTML. Only HTTP(S) links with a host and no credentials qualify.
func profileBio(text string) template.HTML {
	var result strings.Builder
	cursor := 0
	for _, match := range profileURLPattern.FindAllStringIndex(text, -1) {
		start, end := match[0], match[1]
		candidate := strings.TrimRight(text[start:end], ".,;:!?")
		for _, pair := range [][2]string{{"(", ")"}, {"[", "]"}, {"{", "}"}} {
			for strings.HasSuffix(candidate, pair[1]) && strings.Count(candidate, pair[1]) > strings.Count(candidate, pair[0]) {
				candidate = strings.TrimSuffix(candidate, pair[1])
			}
		}
		candidate = strings.TrimRight(candidate, ".,;:!?")
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			continue
		}
		result.WriteString(html.EscapeString(text[cursor:start]))
		result.WriteString(`<a href="` + html.EscapeString(externalLink(candidate)) + `" target="_blank" rel="nofollow ugc noopener noreferrer">`)
		result.WriteString(html.EscapeString(candidate))
		result.WriteString(`</a>`)
		cursor = start + len(candidate)
	}
	result.WriteString(html.EscapeString(text[cursor:]))
	return template.HTML(result.String())
}
