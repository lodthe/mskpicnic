package mosru

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileManager handles saving and loading events from a file
type FileManager struct {
	filePath    string
	mutex       sync.RWMutex
	lastUpdated time.Time
}

// NewFileManager creates a new file manager
func NewFileManager(filePath string) *FileManager {
	if filePath == "" {
		filePath = "events.json"
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(filePath)
	if dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("Warning: failed to create directory %s: %v", dir, err)
		}
	}

	return &FileManager{
		filePath:    filePath,
		lastUpdated: time.Time{},
	}
}

// SaveEvents saves events to a file
func (fm *FileManager) SaveEvents(events []Event) error {
	fm.mutex.Lock()
	defer fm.mutex.Unlock()

	// Marshal events to JSON
	data, err := json.MarshalIndent(events, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal events: %w", err)
	}

	// Write to file
	if err := ioutil.WriteFile(fm.filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write events to file: %w", err)
	}

	fm.lastUpdated = time.Now()
	log.Printf("Saved %d events to file %s", len(events), fm.filePath)
	return nil
}

// LoadEvents loads events from a file
func (fm *FileManager) LoadEvents() ([]Event, error) {
	fm.mutex.RLock()
	defer fm.mutex.RUnlock()

	// Check if file exists
	if _, err := os.Stat(fm.filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("events file does not exist: %w", err)
	}

	// Read file
	data, err := ioutil.ReadFile(fm.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read events file: %w", err)
	}

	// Unmarshal events
	var events []Event
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, fmt.Errorf("failed to unmarshal events: %w", err)
	}

	log.Printf("Loaded %d events from file %s", len(events), fm.filePath)
	return events, nil
}

// GetLastUpdated returns the time when the events were last updated
func (fm *FileManager) GetLastUpdated() time.Time {
	fm.mutex.RLock()
	defer fm.mutex.RUnlock()
	return fm.lastUpdated
}

// ShouldUpdate returns true if the events should be updated
func (fm *FileManager) ShouldUpdate(interval time.Duration) bool {
	fm.mutex.RLock()
	defer fm.mutex.RUnlock()

	// If never updated or interval has passed since last update
	return fm.lastUpdated.IsZero() || time.Since(fm.lastUpdated) > interval
}
