package bot

import "github.com/iabalyuk/mskpicnic/storage"

// UserState represents the state of a user's interaction with the bot
type UserState struct {
	Stage             Stage
	SelectedDate      string
	SelectedStartTime int // Store hour as int
	// Pagination state
	CurrentPage   int                // Current page number (0-based)
	FilteredSlots []storage.SlotInfo // Full list of slots for the current query
}

// Stage represents the stage of the user's interaction with the bot
type Stage int

const (
	StageIdle Stage = iota
	StageSelectingDate
	StageSelectingStartTime
	// StageSelectingEndTime is removed
)
