package bot

import (
	"fmt"
	"html" // <-- Add html import
	"log"
	"sort"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/iabalyuk/mskpicnic/storage"
)

// showAvailableSlots shows available slots for the selected date and time range
// It now requires userID to access the correct state.
func (b *Bot) showAvailableSlots(userID, chatID int64, date string, startHour, endHour int) { // Added userID parameter
	log.Printf("[showAvailableSlots] UserID: %d, ChatID: %d, Date: %s, StartHour: %d, EndHour: %d", userID, chatID, date, startHour, endHour)
	// Get all slots from storage for the date
	allSlots := b.storage.GetSlots(date)
	log.Printf("[showAvailableSlots] Retrieved %d total slots from storage for date %s", len(allSlots), date)

	if len(allSlots) == 0 {
		// This case seems unlikely if the date button was shown, but log it.
		log.Printf("[showAvailableSlots] No slots found in storage for date %s. Sending message.", date)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("На %s нет доступных беседок (данные могли обновиться).", date))
		_, err := b.api.Send(msg)
		if err != nil {
			log.Printf("[showAvailableSlots] Error sending 'no slots' message: %v", err)
		}
		return
	}

	// Filter slots by the selected time range [startHour, endHour)
	var filteredSlots []storage.SlotInfo
	log.Printf("[showAvailableSlots] Filtering slots. Comparing slotHour >= %d && slotHour < %d", startHour, endHour) // Log the condition
	for _, slot := range allSlots {
		slotHour := slot.StartTime.Hour()
		// Log each comparison (conditionally)
		passesFilter := slotHour >= startHour && slotHour < endHour
		if b.client.Debug { // Use the client stored in the bot struct for the debug flag
			log.Printf("[showAvailableSlots]   - Slot: '%s', StartTime: %s, slotHour: %d. Passes filter: %t", slot.Name, slot.StartTime.Format(time.RFC3339), slotHour, passesFilter)
		}
		// Slot starts at or after startHour AND before endHour
		if passesFilter {
			filteredSlots = append(filteredSlots, slot)
		}
	}
	// Log removed from here as it's now inside the loop or covered by the next log

	if len(filteredSlots) == 0 {
		log.Printf("[showAvailableSlots] No slots found after filtering for range %d:00-%d:00. Sending message.", startHour, endHour)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("На %s в интервале %02d:00 - %02d:00 нет доступных беседок.", date, startHour, endHour))
		_, err := b.api.Send(msg)
		if err != nil {
			log.Printf("[showAvailableSlots] Error sending 'no filtered slots' message: %v", err)
		}
		return
	}
	log.Printf("[showAvailableSlots] Found %d slots after filtering.", len(filteredSlots))

	// --- Pagination Logic ---
	state, ok := b.userStates[userID] // Use the userID passed as parameter
	if !ok {
		log.Printf("[showAvailableSlots] Error: Could not find user state for userID %d", userID)
		// Send error message?
		msg := tgbotapi.NewMessage(chatID, "Произошла ошибка состояния. Попробуйте /start.")
		b.api.Send(msg)
		return
	}

	// Sort the filtered slots before storing them
	sort.Slice(filteredSlots, func(i, j int) bool {
		if !filteredSlots[i].StartTime.Equal(filteredSlots[j].StartTime) {
			return filteredSlots[i].StartTime.Before(filteredSlots[j].StartTime)
		}
		return filteredSlots[i].Name < filteredSlots[j].Name // Sort by name second
	})
	log.Printf("[showAvailableSlots] Sorted %d filtered slots.", len(filteredSlots))

	state.FilteredSlots = filteredSlots
	state.CurrentPage = 0 // Start at the first page

	// Get the message ID of the message that triggered this (the "Select start time" message)
	// This requires passing the original message ID through the callback process or storing it.
	// For now, let's assume we need to send a *new* message for the first page.
	// TODO: Refactor callback handling to pass/store original message ID for editing.

	// Send the first page as a new message
	b.sendPaginatedSlots(chatID, state)

	// --- Old message sending logic removed ---
	// The old code block was here and is now removed.
}

// sendPaginatedSlots sends a new message with the current page of slots and pagination buttons
func (b *Bot) sendPaginatedSlots(chatID int64, state *UserState) {
	itemsPerPage := 15
	startIdx := state.CurrentPage * itemsPerPage
	endIdx := startIdx + itemsPerPage
	if endIdx > len(state.FilteredSlots) {
		endIdx = len(state.FilteredSlots)
	}

	if startIdx < 0 || startIdx >= len(state.FilteredSlots) {
		log.Printf("[sendPaginatedSlots] Invalid start index %d for %d slots", startIdx, len(state.FilteredSlots))
		msg := tgbotapi.NewMessage(chatID, "Ошибка отображения страницы.")
		b.api.Send(msg)
		return
	}

	pageSlots := state.FilteredSlots[startIdx:endIdx]

	var sb strings.Builder
	endHour := state.SelectedStartTime + 4
	if endHour > 21 {
		endHour = 21
	}
	if endHour <= state.SelectedStartTime {
		endHour = state.SelectedStartTime + 1
	}
	escapedDate := htmlEscape(state.SelectedDate)
	escapedRange := htmlEscape(fmt.Sprintf("%02d:00 - %02d:00", state.SelectedStartTime, endHour))
	totalPages := (len(state.FilteredSlots) + itemsPerPage - 1) / itemsPerPage
	// Use HTML bold tag for header
	sb.WriteString(fmt.Sprintf("<b>Доступные беседки на %s (%s) - Стр. %d/%d:</b>\n\n", escapedDate, escapedRange, state.CurrentPage+1, totalPages))

	for _, slot := range pageSlots {
		priceStr := ""
		if slot.Price > 0 {
			// Escape the price string for HTML
			priceStr = htmlEscape(fmt.Sprintf(" (%.0f ₽)", slot.Price))
		}
		// Escape other dynamic parts for HTML
		escapedSlotName := htmlEscape(slot.Name)
		escapedTime := htmlEscape(slot.StartTime.Format("15:04"))
		// Construct the details string without Markdown escaping
		slotDetails := fmt.Sprintf("🕒 %s - %s%s", escapedTime, escapedSlotName, priceStr)

		bookingLink := ""
		if slot.AgentUID != "" && slot.EventID > 0 { // Ensure EventID is valid
			// Escape the booking link URL just in case (though often not strictly needed for href)
			escapedBookingLink := html.EscapeString(fmt.Sprintf("https://tickets.mos.ru/widget/visit?eventId=%d&agentId=%s&date=%s", slot.EventID, slot.AgentUID, state.SelectedDate))
			bookingLink = escapedBookingLink
		}

		if bookingLink != "" {
			// Use HTML anchor tag
			sb.WriteString(fmt.Sprintf("<a href=\"%s\">%s</a>\n", bookingLink, slotDetails))
		} else {
			sb.WriteString(fmt.Sprintf("%s\n", slotDetails))
		}
	}

	// Send the new message with pagination keyboard
	msg := tgbotapi.NewMessage(chatID, sb.String())
	msg.ParseMode = tgbotapi.ModeHTML                // Use HTML parse mode
	msg.ReplyMarkup = b.getPaginationKeyboard(state) // Add pagination buttons

	_, err := b.api.Send(msg)
	if err != nil {
		log.Printf("[sendPaginatedSlots] Error sending message as HTML to chat %d: %v. Falling back to plain text.", chatID, err)
		msg.ParseMode = ""                               // Clear parse mode
		msg.ReplyMarkup = b.getPaginationKeyboard(state) // Keep buttons for fallback
		_, fallbackErr := b.api.Send(msg)
		if fallbackErr != nil {
			log.Printf("[sendPaginatedSlots] Error sending fallback plain text message to chat %d: %v", chatID, fallbackErr)
		} else {
			log.Printf("[sendPaginatedSlots] Successfully sent fallback plain text message to chat %d.", chatID)
		}
	} else {
		log.Printf("[sendPaginatedSlots] Successfully sent MarkdownV2 message for page %d to chat %d.", state.CurrentPage+1, chatID)
	}
}

// updateSlotListPage edits the message to show the current page of slots
func (b *Bot) updateSlotListPage(chatID int64, messageID int, state *UserState) {
	itemsPerPage := 15 // Must match the value used in callback handler
	startIdx := state.CurrentPage * itemsPerPage
	endIdx := startIdx + itemsPerPage
	if endIdx > len(state.FilteredSlots) {
		endIdx = len(state.FilteredSlots)
	}

	// Ensure indices are valid
	if startIdx < 0 || startIdx >= len(state.FilteredSlots) {
		log.Printf("[updateSlotListPage] Invalid start index %d for %d slots", startIdx, len(state.FilteredSlots))
		// Optionally send an error message back to user?
		return
	}

	pageSlots := state.FilteredSlots[startIdx:endIdx]

	// Build the message content for the current page
	var sb strings.Builder
	// Re-calculate endHour for header (or retrieve from state if stored)
	endHour := state.SelectedStartTime + 4
	if endHour > 21 {
		endHour = 21
	}
	if endHour <= state.SelectedStartTime {
		endHour = state.SelectedStartTime + 1
	}
	escapedDate := htmlEscape(state.SelectedDate)
	escapedRange := htmlEscape(fmt.Sprintf("%02d:00 - %02d:00", state.SelectedStartTime, endHour))
	totalPages := (len(state.FilteredSlots) + itemsPerPage - 1) / itemsPerPage
	// Escape page info for HTML
	pageInfo := htmlEscape(fmt.Sprintf("Стр. %d/%d", state.CurrentPage+1, totalPages))
	// Use HTML bold tag for header
	sb.WriteString(fmt.Sprintf("<b>Доступные беседки на %s (%s) - %s:</b>\n\n", escapedDate, escapedRange, pageInfo))

	for _, slot := range pageSlots {
		priceStr := ""
		if slot.Price > 0 {
			// Escape the price string for HTML
			priceStr = htmlEscape(fmt.Sprintf(" (%.0f ₽)", slot.Price))
		}
		// Escape other dynamic parts for HTML
		escapedTime := htmlEscape(slot.StartTime.Format("15:04"))
		escapedName := htmlEscape(slot.Name)
		// Construct the details string without Markdown escaping
		slotDetailsText := fmt.Sprintf("🕒 %s - %s%s", escapedTime, escapedName, priceStr)

		// Booking link URL
		bookingLink := ""
		if slot.AgentUID != "" && slot.EventID > 0 { // Add check for valid EventID
			// Escape the booking link URL just in case
			escapedBookingLink := html.EscapeString(fmt.Sprintf("https://tickets.mos.ru/widget/visit?eventId=%d&agentId=%s&date=%s", slot.EventID, slot.AgentUID, state.SelectedDate))
			bookingLink = escapedBookingLink
		} else {
			log.Printf("[updateSlotListPage] Warning: Invalid EventID (%d) or empty AgentUID for slot '%s', cannot generate link.", slot.EventID, slot.Name)
		}

		// Create HTML anchor tag or just text if no link
		if bookingLink != "" {
			sb.WriteString(fmt.Sprintf("<a href=\"%s\">%s</a>\n", bookingLink, slotDetailsText))
		} else {
			sb.WriteString(fmt.Sprintf("%s\n", slotDetailsText)) // Display text even without link
		}
	}

	// Add footer
	lastUpdated := b.storage.GetLastUpdated()
	if !lastUpdated.IsZero() {
		// Use HTML italic tag and escape the time
		footer := fmt.Sprintf("\n<i>Последнее обновление: %s</i>", htmlEscape(lastUpdated.Format("15:04:05")))
		sb.WriteString(footer)
	}

	// Create the edit message config
	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, sb.String())
	editMsg.ParseMode = tgbotapi.ModeHTML                // Use HTML parse mode
	editMsg.ReplyMarkup = b.getPaginationKeyboard(state) // Add pagination buttons

	// Send the edit request
	_, err := b.api.Send(editMsg)
	if err != nil {
		// Log the specific error from Telegram API
		log.Printf("[updateSlotListPage] Error editing message %d in chat %d using HTML: %v", messageID, chatID, err)
		// Consider falling back to sending a new message if editing fails
	} else {
		log.Printf("[updateSlotListPage] Successfully updated page %d for chat %d", state.CurrentPage+1, chatID)
	}
}

// escapeMarkdownV2 function removed
