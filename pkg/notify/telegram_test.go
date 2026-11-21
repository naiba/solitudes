package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

// Telegram rejects a Markdown message with an unbalanced "_" or "*" with HTTP
// 400. Comment text is written by visitors, so such notifications were dropped,
// and silently: the response status was never looked at.
func TestTGNotifySendsVisitorTextAsPlainTextAndReportsRejection(t *testing.T) {
	var received TelegramMessage
	var rawRequest map[string]any
	rejectNext := false
	telegram := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&rawRequest); err != nil {
			t.Errorf("decode request: %v", err)
		}
		received.Text, _ = rawRequest["text"].(string)
		received.ChatID, _ = rawRequest["chat_id"].(string)
		if rejectNext {
			http.Error(writer, "Bad Request: can't parse entities", http.StatusBadRequest)
		}
	}))
	defer telegram.Close()
	previousEndpoint, previousSystem := telegramAPIBaseURL, solitudes.System
	t.Cleanup(func() { telegramAPIBaseURL, solitudes.System = previousEndpoint, previousSystem })
	telegramAPIBaseURL = telegram.URL
	config := &model.Config{}
	config.TGBotToken, config.TGChatID = "bot-token", "chat-1"
	solitudes.System = &solitudes.SysVariable{Config: config}

	comment := &model.Comment{Nickname: "visitor_name", Content: "snake_case and 2*3 are fine"}
	TGNotify(comment, &model.Article{Title: "Post"}, nil)

	if !strings.Contains(received.Text, "snake_case and 2*3 are fine") || received.ChatID != "chat-1" {
		t.Fatalf("notification text = %q, chat = %q", received.Text, received.ChatID)
	}
	if parseMode, present := rawRequest["parse_mode"]; present && parseMode != "" {
		t.Errorf("visitor text was sent with parse_mode %q; unbalanced markup makes Telegram reject it", parseMode)
	}

	rejectNext = true
	if err := sendTelegramMessage(TelegramMessage{ChatID: "chat-1", Text: "hello"}); err == nil {
		t.Error("a rejected message was reported as sent")
	}
}
