package router

import "testing"

func TestNormalizeCommentStatus(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: commentStatusAll},
		{input: commentStatusAll, want: commentStatusAll},
		{input: commentStatusVisible, want: commentStatusVisible},
		{input: commentStatusSpam, want: commentStatusSpam},
		{input: "unknown", want: commentStatusAll},
	}

	for _, tt := range tests {
		if got := normalizeCommentStatus(tt.input); got != tt.want {
			t.Errorf("normalizeCommentStatus(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
