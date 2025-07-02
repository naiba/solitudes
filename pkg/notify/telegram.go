package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

// TelegramMessage Telegram消息结构
type TelegramMessage struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode"`
}

// TGNotify TG推送
func TGNotify(comment *model.Comment, article *model.Article, err error) {
	// when err == nil skip admin
	if comment.IsAdmin && err == nil {
		return
	}

	// 检查配置
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

	msg := TelegramMessage{
		ChatID:    solitudes.System.Config.TGChatID,
		Text:      content,
		ParseMode: "Markdown",
	}

	sendTelegramMessage(msg)
}

func sendTelegramMessage(msg TelegramMessage) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", solitudes.System.Config.TGBotToken)

	jsonData, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
