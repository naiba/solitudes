package router

import (
	"strings"
	"testing"
)

func TestValidTagParam(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		want bool
	}{
		{name: "regular", tag: "Go", want: true},
		{name: "unicode", tag: "编程", want: true},
		{name: "empty", tag: "", want: false},
		{name: "control character", tag: "bad\ntag", want: false},
		{name: "maximum length", tag: strings.Repeat("界", maxTagLength), want: true},
		{name: "too long", tag: strings.Repeat("界", maxTagLength+1), want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validTagParam(test.tag); got != test.want {
				t.Fatalf("validTagParam(%q) = %t, want %t", test.tag, got, test.want)
			}
		})
	}
}
