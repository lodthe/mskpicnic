package bot

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// handleMessage handles incoming messages
func (b *Bot) handleMessage(message *tgbotapi.Message) {
	userID := message.From.ID
	chatID := message.Chat.ID

	// Initialize user state if not exists
	if _, ok := b.userStates[userID]; !ok {
		b.userStates[userID] = &UserState{
			Stage: StageIdle,
		}
	}

	// Handle commands
	if message.IsCommand() {
		switch message.Command() {
		case "start":
			b.handleStartCommand(message)
		case "help":
			b.handleHelpCommand(message)
		case "check":
			b.handleCheckCommand(message)
		case "cancel":
			b.handleCancelCommand(message)
		default:
			// Updated hint to suggest /check
			msg := tgbotapi.NewMessage(chatID, "Неизвестная команда. Используйте /check для проверки доступных беседок или /help для списка команд.")
			b.api.Send(msg)
		}
		return
	}

	// Handle user input based on the current stage
	state := b.userStates[userID]
	switch state.Stage {
	// Text input for date/time selection is disabled in favor of keyboards.
	case StageSelectingDate:
		msg := tgbotapi.NewMessage(chatID, "Пожалуйста, выберите дату с помощью клавиатуры выше.")
		b.api.Send(msg)
	case StageSelectingStartTime:
		msg := tgbotapi.NewMessage(chatID, "Пожалуйста, выберите НАЧАЛЬНОЕ время с помощью клавиатуры выше.")
		b.api.Send(msg)
	// case StageSelectingEndTime: // Removed stage
	// 	msg := tgbotapi.NewMessage(chatID, "Пожалуйста, выберите КОНЕЧНОЕ время с помощью клавиатуры выше.")
	// 	b.api.Send(msg)
	default:
		// User is not in any specific stage
		msg := tgbotapi.NewMessage(chatID, "Используйте /check для проверки доступных беседок.")
		b.api.Send(msg)
	}
}

// handleCallbackQuery handles callback queries from inline keyboards
func (b *Bot) handleCallbackQuery(query *tgbotapi.CallbackQuery) {
	userID := query.From.ID
	chatID := query.Message.Chat.ID

	// Initialize user state if not exists
	if _, ok := b.userStates[userID]; !ok {
		b.userStates[userID] = &UserState{
			Stage: StageIdle,
		}
	}

	state := b.userStates[userID]

	// Parse callback data
	data := query.Data
	parts := strings.Split(data, ":")
	if len(parts) < 1 { // Need at least an action
		log.Printf("Invalid callback data format: %s", data)
		b.answerCallbackQuery(query.ID, "Ошибка данных")
		return
	}

	action := parts[0]
	value := ""
	if len(parts) > 1 {
		value = parts[1]
	}

	switch action {
	case "date":
		if value == "" {
			b.answerCallbackQuery(query.ID, "Ошибка данных")
			return
		} // Need a date value
		// User selected a date
		state.SelectedDate = value
		state.Stage = StageSelectingStartTime // Move to start time selection

		// Ask for start time
		msg := tgbotapi.NewMessage(chatID, "Выберите НАЧАЛО временного интервала:")
		msg.ReplyMarkup = b.getStartTimeKeyboard(value) // Will be moved later
		b.api.Send(msg)
		// Edit the original message to remove the date keyboard
		editMsg := tgbotapi.NewEditMessageText(chatID, query.Message.MessageID, fmt.Sprintf("Выбрана дата: %s", value))
		b.api.Send(editMsg)
		b.answerCallbackQuery(query.ID, "") // Answer callback

	case "startTime":
		if value == "" {
			b.answerCallbackQuery(query.ID, "Ошибка данных")
			return
		} // Need a time value
		// User selected a start time
		startHour, err := strconv.Atoi(value)
		if err != nil {
			log.Printf("Error converting startTime '%s' to int: %v", value, err)
			b.answerCallbackQuery(query.ID, "Ошибка выбора времени")
			return
		}
		state.SelectedStartTime = startHour
		state.Stage = StageIdle // Interaction complete after selecting start time

		// Calculate end time (start + 4 hours)
		endHour := startHour + 4
		// Clamp end time to a maximum (e.g., 21:00, as slots might not exist after that)
		// Mos.ru booking slots seem to end around 20:00 or 21:00 typically.
		// A 4-hour slot starting at 18:00 would end at 22:00, which is likely too late.
		// Let's cap the end hour at 21.
		if endHour > 21 {
			endHour = 21
			log.Printf("Clamping end hour to 21 for start hour %d", startHour)
		}
		// Also ensure endHour is strictly greater than startHour after clamping
		if endHour <= startHour {
			log.Printf("Calculated end hour %d is not after start hour %d, adjusting", endHour, startHour)
			endHour = startHour + 1 // Minimum 1 hour duration if clamping caused issues
		}

		// Edit the original message to show selected start time and calculated range
		editMsg := tgbotapi.NewEditMessageText(chatID, query.Message.MessageID,
			fmt.Sprintf("Выбрана дата: %s\nИнтервал: %02d:00 - %02d:00",
				state.SelectedDate, startHour, endHour))
		b.api.Send(editMsg)
		b.answerCallbackQuery(query.ID, "") // Answer callback

		// Show available slots for the calculated range (pass userID)
		b.showAvailableSlots(userID, chatID, state.SelectedDate, startHour, endHour) // Will be moved later

	// case "endTime": // Removed case
	// ... (entire case removed) ...

	case "action": // Handle generic actions like "none"
		if value == "none" {
			// User clicked a button indicating no options available
			b.answerCallbackQuery(query.ID, "Нет доступных опций") // Answer callback
			// Optionally edit the message
			editMsg := tgbotapi.NewEditMessageText(chatID, query.Message.MessageID, query.Message.Text+"\n(Нет доступных опций)")
			b.api.Send(editMsg)
			state.Stage = StageIdle // Reset stage
		}
	case "prevPage":
		if state.Stage != StageIdle || len(state.FilteredSlots) == 0 {
			b.answerCallbackQuery(query.ID, "Нет активного списка для навигации.")
			return
		}
		if state.CurrentPage > 0 {
			state.CurrentPage--
			b.updateSlotListPage(query.Message.Chat.ID, query.Message.MessageID, state) // Will be moved later
			b.answerCallbackQuery(query.ID, "")                                         // Answer callback
		} else {
			b.answerCallbackQuery(query.ID, "Вы уже на первой странице.")
		}
	case "nextPage":
		if state.Stage != StageIdle || len(state.FilteredSlots) == 0 {
			b.answerCallbackQuery(query.ID, "Нет активного списка для навигации.")
			return
		}
		// Calculate total pages later when needed
		itemsPerPage := 15 // Define items per page
		totalPages := (len(state.FilteredSlots) + itemsPerPage - 1) / itemsPerPage
		if state.CurrentPage < totalPages-1 {
			state.CurrentPage++
			b.updateSlotListPage(query.Message.Chat.ID, query.Message.MessageID, state) // Will be moved later
			b.answerCallbackQuery(query.ID, "")                                         // Answer callback
		} else {
			b.answerCallbackQuery(query.ID, "Вы уже на последней странице.")
		}
	case "back":
		switch value { // Value determines which menu to go back to
		case "date": // Go back to date selection from time selection
			state.Stage = StageSelectingDate
			state.SelectedDate = "" // Clear selected date
			// Edit the message to show the date keyboard again
			editMsg := tgbotapi.NewEditMessageText(chatID, query.Message.MessageID, "Выберите дату:")
			dateKeyboard := b.getDateKeyboard() // Get keyboard struct
			editMsg.ReplyMarkup = &dateKeyboard // Assign pointer
			b.api.Send(editMsg)
			b.answerCallbackQuery(query.ID, "") // Answer callback
		case "startTime": // Go back to time selection from slot list
			if state.SelectedDate == "" {
				// Should not happen if we are viewing slots, but handle defensively
				b.answerCallbackQuery(query.ID, "Ошибка: Дата не выбрана.")
				return
			}
			state.Stage = StageSelectingStartTime
			state.SelectedStartTime = 0 // Clear selected time
			state.FilteredSlots = nil   // Clear filtered slots
			state.CurrentPage = 0
			// Edit the message to show the start time keyboard again
			editMsg := tgbotapi.NewEditMessageText(chatID, query.Message.MessageID, "Выберите НАЧАЛО временного интервала:")
			startTimeKeyboard := b.getStartTimeKeyboard(state.SelectedDate) // Get keyboard struct
			editMsg.ReplyMarkup = &startTimeKeyboard                        // Assign pointer
			b.api.Send(editMsg)
			b.answerCallbackQuery(query.ID, "") // Answer callback
		default:
			log.Printf("Unhandled back value: %s", value)
			b.answerCallbackQuery(query.ID, "Неизвестное действие 'назад'")
		}
	case "cancel": // Handle cancel button from date keyboard
		state.Stage = StageIdle
		state.SelectedDate = ""
		state.SelectedStartTime = 0
		state.FilteredSlots = nil
		state.CurrentPage = 0
		// Edit the message to confirm cancellation and remove keyboard
		editMsg := tgbotapi.NewEditMessageText(chatID, query.Message.MessageID, "Выбор отменен.")
		// No ReplyMarkup needed to remove inline keyboard
		b.api.Send(editMsg)
		b.answerCallbackQuery(query.ID, "Отменено") // Answer callback

	default:
		log.Printf("Unhandled callback action: %s", action)
		b.answerCallbackQuery(query.ID, "") // Answer unknown actions
	}

}

// handleStartCommand handles the /start command
func (b *Bot) handleStartCommand(message *tgbotapi.Message) {
	chatID := message.Chat.ID
	msg := tgbotapi.NewMessage(chatID, "Добро пожаловать в бот для проверки доступных беседок для шашлыков в Москве! "+
		"Используйте /check для проверки доступных беседок.")
	b.api.Send(msg)
}

// handleHelpCommand handles the /help command
func (b *Bot) handleHelpCommand(message *tgbotapi.Message) {
	chatID := message.Chat.ID
	msg := tgbotapi.NewMessage(chatID, "Доступные команды:\n"+
		"/check - Проверить доступные беседки\n"+
		"/cancel - Отменить текущую операцию\n"+
		"/help - Показать эту справку")
	b.api.Send(msg)
}

// handleCheckCommand handles the /check command
func (b *Bot) handleCheckCommand(message *tgbotapi.Message) {
	userID := message.From.ID
	chatID := message.Chat.ID

	// Update user state
	b.userStates[userID] = &UserState{
		Stage: StageSelectingDate,
	}

	// Show last update status
	lastUpdated := b.storage.GetLastUpdated()
	updateStatus := b.storage.GetUpdateStatus()

	var statusMsg string
	if lastUpdated.IsZero() {
		statusMsg = "Информация о беседках еще не загружена. "
	} else {
		timeAgo := time.Since(lastUpdated).Round(time.Minute)
		statusMsg = fmt.Sprintf("Последнее обновление: %s (%s назад). Статус: %s\n\n",
			lastUpdated.Format("15:04:05"), timeAgo, updateStatus)
	}

	// Show date selection keyboard
	msg := tgbotapi.NewMessage(chatID, statusMsg+"Выберите дату:")
	msg.ReplyMarkup = b.getDateKeyboard() // Will be moved later
	b.api.Send(msg)
}

// handleCancelCommand handles the /cancel command
func (b *Bot) handleCancelCommand(message *tgbotapi.Message) {
	userID := message.From.ID
	chatID := message.Chat.ID

	// Reset user state
	if _, ok := b.userStates[userID]; ok {
		b.userStates[userID].Stage = StageIdle
		b.userStates[userID].SelectedDate = ""
		b.userStates[userID].SelectedStartTime = 0
		// b.userStates[userID].SelectedEndTime = 0 // Field removed
	}

	msg := tgbotapi.NewMessage(chatID, "Операция отменена.")
	// Remove any existing keyboard
	msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
	b.api.Send(msg)

	// Also try to remove inline keyboard from previous message if applicable
	// This requires knowing the previous message ID, which we don't store here.
	// For simplicity, we'll just send the cancel message.
}
