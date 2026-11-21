package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

// telegramAPIBaseURL is a variable so tests can point it at a local server.
var telegramAPIBaseURL = "https://api.telegram.org"

// A notification must not hold its goroutine forever when Telegram is
// unreachable; http.Post has no timeout at all.
var telegramHTTPClient = &http.Client{Timeout: 15 * time.Second}

// TelegramMessage Telegram消息结构
type TelegramMessage struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

// TGNotify TG推送
func TGNotify(comment *model.Comment, article *model.Article, err error) {
	// when err == nil skip admin
	if comment.PublicRole() == string(model.RoleAdmin) && err == nil {
		return
	}

	if solitudes.System.Config.TGBotToken == "" || solitudes.System.Config.TGChatID == "" {
		return
	}

	var errmsg string
	if err != nil {
		errmsg = `

### Email notify error

` + err.Error()
	}

	content := fmt.Sprintf(`### %s got a new comment

- Article: %s  
- Author: %s (%s)   
- Content: %s%s`,
		article.Title,
		article.Title,
		comment.Nickname,
		comment.Email,
		comment.Content,
		errmsg)

	// Sent as plain text on purpose. The nickname and the comment are written
	// by visitors: with parse_mode "Markdown" a single unbalanced "_" or "*"
	// makes Telegram answer 400 and the notification is lost.
	msg := TelegramMessage{
		ChatID: solitudes.System.Config.TGChatID,
		Text:   content,
	}

	if err := sendTelegramMessage(msg); err != nil {
		log.Printf("Telegram notification failed: %v", err)
	}
}

func sendTelegramMessage(msg TelegramMessage) error {
	url := fmt.Sprintf("%s/bot%s/sendMessage", telegramAPIBaseURL, solitudes.System.Config.TGBotToken)

	jsonData, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	resp, err := telegramHTTPClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		// The request URL contains the bot token; do not pass it on to the log.
		return fmt.Errorf("send message: request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		reason, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("send message: HTTP %d: %s", resp.StatusCode, reason)
	}
	return nil
}
