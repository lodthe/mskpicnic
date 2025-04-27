package storage

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

// Storage represents a thread-safe in-memory storage for available slots
// It implements the StorageInterface
type Storage struct {
	mu           sync.RWMutex
	slots        map[string][]SlotInfo // map[date][]SlotInfo
	lastUpdated  time.Time             // Last time the data was updated
	updateStatus string                // Status of the last update (success, error, etc.)
}

// SlotInfo represents information about an available slot
type SlotInfo struct {
	EventID   int       // ID of the event (e.g., park)
	EventName string    // Name of the event (e.g., park name)
	AgentUID  string    // Added AgentUID for linking
	Name      string    // Name of the specific picnic area/gazebo
	StartTime time.Time // Start time of the slot
	EndTime   time.Time // End time of the slot (if available)
	Location  string    // Location information (often part of Name)
	Price     float64   // Added price field
}

// New creates a new storage instance
func New() *Storage {
	return &Storage{
		slots:        make(map[string][]SlotInfo),
		lastUpdated:  time.Time{}, // Zero time
		updateStatus: "Not updated yet",
	}
}

// GetSlots returns all slots for a specific date
func (s *Storage) GetSlots(date string) []SlotInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy of the slice to prevent concurrent modification
	slots := make([]SlotInfo, len(s.slots[date]))
	copy(slots, s.slots[date])
	return slots
}

// GetAllDates returns all dates that have slots
func (s *Storage) GetAllDates() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dates := make([]string, 0, len(s.slots))
	for date := range s.slots {
		dates = append(dates, date)
	}
	return dates
}

// GetSlotCountByDate returns a map of dates to the number of available slots
func (s *Storage) GetSlotCountByDate() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]int)
	for date, slots := range s.slots {
		result[date] = len(slots)
	}
	return result
}

// GetSlotCountByTime returns a map of hours to the number of available slots for a specific date
func (s *Storage) GetSlotCountByTime(date string) map[int]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[int]int)
	for _, slot := range s.slots[date] {
		hour := slot.StartTime.Hour()
		result[hour]++
	}
	return result
}

// UpdateSlots updates all slots for a specific date
func (s *Storage) UpdateSlots(date string, slots []SlotInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.slots[date] = slots
	s.lastUpdated = time.Now()
	s.updateStatus = "Success"
}

// SetUpdateError sets the update status to an error message
func (s *Storage) SetUpdateError(err string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.updateStatus = "Error: " + err
}

// SetUpdateStatus sets the update status to a custom message
func (s *Storage) SetUpdateStatus(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.updateStatus = status
}

// GetLastUpdated returns the time of the last update
func (s *Storage) GetLastUpdated() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.lastUpdated
}

// GetUpdateStatus returns the status of the last update
func (s *Storage) GetUpdateStatus() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.updateStatus
}

// ClearOldDates removes dates that are in the past
func (s *Storage) ClearOldDates() {
	s.mu.Lock()
	defer s.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	for date := range s.slots {
		if date < today {
			delete(s.slots, date)
		}
	}
}

// ParseSlotInfo parses a slot string from the mos.ru API into a SlotInfo struct
func ParseSlotInfo(slotStr string) (SlotInfo, error) {
	// Example format: "Беседка №1: 2025-05-01T12:00:00"
	parts := make([]string, 0)

	// Try to split by ": " first
	if idx := indexOf(slotStr, ": "); idx != -1 {
		parts = append(parts, slotStr[:idx], slotStr[idx+2:])
	} else if idx := indexOf(slotStr, " - "); idx != -1 {
		// Try to split by " - " if ": " is not found
		parts = append(parts, slotStr[:idx], slotStr[idx+3:])
	} else {
		// Look for a date pattern in the string
		datePattern := regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
		match := datePattern.FindString(slotStr)

		if match != "" {
			// If we found a date, split the string at that point
			idx := strings.Index(slotStr, match)
			if idx > 0 {
				// Use everything before the date as the name
				parts = append(parts, strings.TrimSpace(slotStr[:idx]), match)
			} else {
				// If the date is at the beginning, use a default name
				parts = append(parts, "Беседка", match)
			}
		} else {
			// If we couldn't find a date, use the whole string as the name
			parts = append(parts, slotStr, "")
		}
	}

	// Ensure we have two parts
	if len(parts) < 2 {
		parts = append(parts, "")
	}

	name := strings.TrimSpace(parts[0])
	startTimeStr := strings.TrimSpace(parts[1])

	// For test slots, ensure they have a proper name
	if strings.Contains(name, "Тестовая") {
		name = strings.TrimSpace(name)
	}

	// Parse the start time
	var startTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse("2006-01-02T15:04:05", startTimeStr)
		if err != nil {
			// Try alternative formats
			formats := []string{
				"2006-01-02 15:04:05",
				"2006-01-02 15:04",
				"15:04:05",
				"15:04",
			}

			for _, format := range formats {
				startTime, err = time.Parse(format, startTimeStr)
				if err == nil {
					break
				}
			}

			if err != nil {
				// If we still can't parse the time, use current date with 12:00 time
				startTime = time.Now().Truncate(24 * time.Hour).Add(12 * time.Hour)
			}
		}
	} else {
		// If no time string, use current date with 12:00 time
		startTime = time.Now().Truncate(24 * time.Hour).Add(12 * time.Hour)
	}

	return SlotInfo{
		Name:      name,
		StartTime: startTime,
		// EndTime is not provided in the API response
		// Location is not provided in the API response
	}, nil
}

// Helper function to find the index of a substring
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// ResetStorage clears all stored data
func (s *Storage) ResetStorage() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.slots = make(map[string][]SlotInfo)
	s.lastUpdated = time.Time{}
	s.updateStatus = "Storage reset"
}
