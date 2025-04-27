package storage

import "time"

// StorageInterface defines the interface for storage implementations
type StorageInterface interface {
	// GetSlots returns all slots for a specific date
	GetSlots(date string) []SlotInfo

	// GetAllDates returns all dates that have slots
	GetAllDates() []string

	// GetSlotCountByDate returns a map of dates to the number of available slots
	GetSlotCountByDate() map[string]int

	// GetSlotCountByTime returns a map of hours to the number of available slots for a specific date
	GetSlotCountByTime(date string) map[int]int

	// UpdateSlots updates all slots for a specific event on a specific date
	// It should first remove existing slots for this date/event before adding the new ones.
	UpdateSlots(date string, eventID int, eventName string, slots []SlotInfo)

	// SetUpdateError sets the update status to an error message
	SetUpdateError(err string)

	// SetUpdateStatus sets the update status to a custom message
	SetUpdateStatus(status string)

	// GetLastUpdated returns the time of the last update
	GetLastUpdated() time.Time

	// GetUpdateStatus returns the status of the last update
	GetUpdateStatus() string

	// ClearOldDates removes dates that are in the past
	ClearOldDates()

	// ResetStorage clears all stored data
	ResetStorage()

	// GetDatesWithSlots returns a sorted list of dates that have at least one slot stored
	GetDatesWithSlots() ([]string, error)

	// RemoveSlotsForDateEvent removes all slots associated with a specific event on a specific date
	RemoveSlotsForDateEvent(date string, eventID int) error
}
