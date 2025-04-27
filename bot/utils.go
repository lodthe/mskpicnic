package bot

import (
	"html" // <-- Add html import
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// answerCallbackQuery sends an answer to a callback query.
func (b *Bot) answerCallbackQuery(queryID string, text string) {
	callback := tgbotapi.NewCallback(queryID, text)
	if _, err := b.api.Request(callback); err != nil {
		log.Printf("Error answering callback query %s: %v", queryID, err)
	}
}

// htmlEscape escapes a string for safe use in Telegram HTML messages.
func htmlEscape(s string) string {
	return html.EscapeString(s)
}
