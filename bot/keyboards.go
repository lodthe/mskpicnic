package bot

import (
	"fmt"
	"log"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// getDateKeyboard returns an inline keyboard with date options based on available slots in storage
func (b *Bot) getDateKeyboard() tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	// Get dates that actually have slots from storage
	availableDates, err := b.storage.GetDatesWithSlots()
	if err != nil {
		log.Printf("Error getting dates with slots: %v", err)
		button := tgbotapi.NewInlineKeyboardButtonData("Ошибка загрузки дат", "action:none")
		rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
		return tgbotapi.NewInlineKeyboardMarkup(rows...)
	}

	if len(availableDates) == 0 {
		button := tgbotapi.NewInlineKeyboardButtonData("Нет доступных дат", "action:none")
		rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
		return tgbotapi.NewInlineKeyboardMarkup(rows...)
	}

	// Get slot counts for display (optional, but nice)
	slotCounts := b.storage.GetSlotCountByDate()

	// Get today's date (YYYY-MM-DD format) for comparison
	today := time.Now().Format("2006-01-02")

	// Create buttons only for dates with slots that are today or in the future
	buttonsAdded := 0
	for _, dateStr := range availableDates {
		// Skip dates that are in the past
		if dateStr < today {
			continue
		}

		// Attempt to parse the date for display formatting
		displayDate := dateStr // Default display
		parsedDate, pErr := time.Parse("2006-01-02", dateStr)
		if pErr == nil {
			displayDate = parsedDate.Format("02.01 Mon") // Format as DD.MM Weekday
		} else {
			// Log error but still try to show the button with the raw date string
			log.Printf("Warning: Could not parse date '%s' for display: %v", dateStr, pErr)
		}

		// Add slot count if available
		count := slotCounts[dateStr]
		if count > 0 {
			displayDate = fmt.Sprintf("%s (%d)", displayDate, count)
		}

		button := tgbotapi.NewInlineKeyboardButtonData(displayDate, "date:"+dateStr)

		// Create a new row for every 2 dates for better layout
		if len(rows) == 0 || len(rows[len(rows)-1]) == 2 {
			rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
		} else {
			rows[len(rows)-1] = append(rows[len(rows)-1], button)
		}
		buttonsAdded++
	}

	// Handle case where all available dates were in the past
	if buttonsAdded == 0 && len(availableDates) > 0 {
		button := tgbotapi.NewInlineKeyboardButtonData("Нет доступных дат (будущих)", "action:none")
		rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
	}

	// Add a "Back" button (or Cancel in this top-level menu)
	backButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "cancel")
	rows = append(rows, []tgbotapi.InlineKeyboardButton{backButton})

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// getStartTimeKeyboard returns an inline keyboard with start time options for a specific date
func (b *Bot) getStartTimeKeyboard(date string) tgbotapi.InlineKeyboardMarkup {
	// Get slot counts by time for the selected date
	slotCountsByTime := b.storage.GetSlotCountByTime(date)

	var rows [][]tgbotapi.InlineKeyboardButton
	var row []tgbotapi.InlineKeyboardButton
	buttonAdded := false

	// Show available start hours (10:00 to 19:00, as end time must be later)
	for hour := 10; hour <= 19; hour++ {
		count := slotCountsByTime[hour]
		if count > 0 {
			timeStr := strconv.Itoa(hour) // Use hour as value
			displayTime := fmt.Sprintf("%02d:00 (%d)", hour, count)
			button := tgbotapi.NewInlineKeyboardButtonData(displayTime, "startTime:"+timeStr)
			buttonAdded = true

			row = append(row, button)

			// Create a new row every 2 buttons
			if len(row) == 2 {
				rows = append(rows, row)
				row = []tgbotapi.InlineKeyboardButton{}
			}
		}
	}

	// Add any remaining buttons
	if len(row) > 0 {
		rows = append(rows, row)
	}

	// If no times have slots, add a message button
	if !buttonAdded {
		button := tgbotapi.NewInlineKeyboardButtonData("Нет доступных интервалов", "action:none")
		rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
	}

	// Add a "Back" button to return to date selection
	backButton := tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад (к датам)", "back:date")
	rows = append(rows, []tgbotapi.InlineKeyboardButton{backButton})

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// getPaginationKeyboard creates the Prev/Next/Back inline keyboard
func (b *Bot) getPaginationKeyboard(state *UserState) *tgbotapi.InlineKeyboardMarkup {
	itemsPerPage := 15
	totalPages := (len(state.FilteredSlots) + itemsPerPage - 1) / itemsPerPage
	currentPage := state.CurrentPage // 0-based

	var row []tgbotapi.InlineKeyboardButton

	// Previous button
	prevText := "⬅️ Пред."
	if currentPage == 0 {
		prevText = "➖" // Indicate disabled
	}
	prevCallback := "prevPage"
	prevButton := tgbotapi.NewInlineKeyboardButtonData(prevText, prevCallback)
	if currentPage == 0 {
		actionNone := "action:none"           // Create string variable
		prevButton.CallbackData = &actionNone // Assign pointer
	}
	row = append(row, prevButton)

	// Page indicator button (optional, non-clickable)
	pageIndicatorCallback := "action:none"
	pageButton := tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d/%d", currentPage+1, totalPages), pageIndicatorCallback)
	// No need to change callback data for page indicator, it's already action:none
	row = append(row, pageButton)

	// Next button
	nextText := "След. ➡️"
	if currentPage >= totalPages-1 {
		nextText = "➖" // Indicate disabled
	}
	nextCallback := "nextPage"
	nextButton := tgbotapi.NewInlineKeyboardButtonData(nextText, nextCallback)
	if currentPage >= totalPages-1 {
		actionNone := "action:none"           // Create string variable
		nextButton.CallbackData = &actionNone // Assign pointer
	}
	row = append(row, nextButton)

	// Add a "Back" button to return to start time selection
	backButton := tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад (к времени)", "back:startTime")
	backRow := []tgbotapi.InlineKeyboardButton{backButton}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(row, backRow) // Add backRow
	return &keyboard
}
