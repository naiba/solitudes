package model

import "testing"

func TestCommentCountsTowardArticle(t *testing.T) {
	replyID := "00000000-0000-0000-0000-000000000001"
	tests := []struct {
		name    string
		comment Comment
		want    bool
	}{
		{name: "visible root", comment: Comment{}, want: true},
		{name: "spam root", comment: Comment{IsSpam: true}, want: false},
		{name: "visible reply", comment: Comment{ReplyTo: &replyID}, want: false},
		{name: "spam reply", comment: Comment{ReplyTo: &replyID, IsSpam: true}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.comment.CountsTowardArticle(); got != tt.want {
				t.Fatalf("CountsTowardArticle() = %v, want %v", got, tt.want)
			}
		})
	}
}
