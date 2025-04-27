package bot

import (
	"fmt" // <-- Import html package
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/iabalyuk/mskpicnic/mosru"
	"github.com/iabalyuk/mskpicnic/storage"
)

// Bot represents a Telegram bot for checking picnic areas
type Bot struct {
	api        *tgbotapi.BotAPI
	client     *mosru.Client
	storage    storage.StorageInterface
	notifyCh   chan string
	userStates map[int64]*UserState
}

// New creates a new bot instance
func New(token string, client *mosru.Client, storage storage.StorageInterface) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	return &Bot{
		api:        api,
		client:     client,
		storage:    storage,
		notifyCh:   make(chan string, 100), // Notifications are disabled in worker, but channel remains
		userStates: make(map[int64]*UserState),
	}, nil
}

// Start starts the bot
func (b *Bot) Start() error {
	log.Printf("Bot started: @%s", b.api.Self.UserName)

	// Start notification handler (even if disabled, keep it running)
	go b.handleNotifications()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil {
			b.handleMessage(update.Message)
		} else if update.CallbackQuery != nil {
			b.handleCallbackQuery(update.CallbackQuery)
		}
	}

	return nil
}

// GetNotifyChannel returns the notification channel
func (b *Bot) GetNotifyChannel() chan string {
	return b.notifyCh
}

// handleNotifications handles notifications from the worker (currently disabled)
func (b *Bot) handleNotifications() {
	for notification := range b.notifyCh {
		// Notifications are disabled in the worker, but we keep the loop
		// in case they are re-enabled later.
		log.Printf("Received notification (sending disabled): %s", notification)
		// // Send notification to all users who have interacted with the bot
		// for userID := range b.userStates {
		// 	msg := tgbotapi.NewMessage(userID, notification)
		// 	_, err := b.api.Send(msg)
		// 	if err != nil {
		// 		log.Printf("Error sending notification to user %d: %v", userID, err)
		// 	}
		// }
	}
}
