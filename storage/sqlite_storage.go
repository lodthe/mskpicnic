package storage

import (
	"database/sql" // Keep only one import
	"fmt"
	"log"
	"sort" // Added import
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteStorage represents a persistent storage using SQLite
type SQLiteStorage struct {
	db           *sql.DB
	memoryCache  *Storage // In-memory cache for faster access
	dbPath       string
	lastSaveTime time.Time
}

// NewSQLiteStorage creates a new SQLite storage instance
func NewSQLiteStorage(dbPath string) (*SQLiteStorage, error) {
	if dbPath == "" {
		dbPath = "mskpicnic.db" // Default database file
	}

	// Open SQLite database
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create tables if they don't exist
	if err := createTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	// Create in-memory cache
	memoryCache := New()

	// --- Add Schema Migration ---
	if err := migrateSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}
	// --- End Schema Migration ---

	// Create storage instance
	storage := &SQLiteStorage{
		db:           db,
		memoryCache:  memoryCache,
		dbPath:       dbPath,
		lastSaveTime: time.Time{},
	}

	// Load data from database
	if err := storage.loadFromDB(); err != nil {
		log.Printf("Warning: failed to load data from database: %v", err)
	}

	return storage, nil
}

// createTables creates the necessary tables in the database
func createTables(db *sql.DB) error {
	// Create slots table
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS slots (
			date TEXT NOT NULL,
			event_id INTEGER NOT NULL,
			event_name TEXT NOT NULL,
			agent_uid TEXT NOT NULL DEFAULT '', -- Added agent_uid column
			name TEXT NOT NULL,
			start_time TEXT NOT NULL,
			end_time TEXT,
			location TEXT,
			price REAL DEFAULT 0.0, -- Added price column
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			-- Ensure uniqueness for a specific slot at a specific time for an event
			PRIMARY KEY (date, event_id, start_time, name)
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create slots table: %w", err)
	}

	// Create metadata table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create metadata table: %w", err)
	}

	// Create indices for faster queries
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_slots_date_event ON slots(date, event_id)`)
	if err != nil {
		return fmt.Errorf("failed to create date_event index: %w", err)
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_slots_date ON slots(date)`)
	if err != nil {
		// Log warning but continue, primary index is more important
		log.Printf("Warning: failed to create date index: %v", err)
	}

	return nil
}

// migrateSchema checks for and applies necessary schema changes
func migrateSchema(db *sql.DB) error {
	log.Println("Checking database schema...")

	// Check if 'agent_uid' column exists in 'slots' table
	rows, err := db.Query("PRAGMA table_info(slots)")
	if err != nil {
		return fmt.Errorf("failed to query table info for slots: %w", err)
	}
	// Removed defer rows.Close() here

	agentUIDColumnExists := false
	for rows.Next() {
		var cid int
		var name string
		var typeName string // Corrected variable name
		var notnull int
		var dfltValue sql.NullString // Use sql.NullString for potentially NULL default values
		var pk int
		if err := rows.Scan(&cid, &name, &typeName, &notnull, &dfltValue, &pk); err != nil { // Corrected variable name
			return fmt.Errorf("failed to scan table info row: %w", err)
		}
		if name == "agent_uid" {
			agentUIDColumnExists = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating table info rows: %w", err)
	}

	// Add 'agent_uid' column if it doesn't exist
	if !agentUIDColumnExists {
		log.Println("Schema migration: Adding 'agent_uid' column to 'slots' table...")
		_, err := db.Exec("ALTER TABLE slots ADD COLUMN agent_uid TEXT NOT NULL DEFAULT ''")
		if err != nil {
			return fmt.Errorf("failed to add agent_uid column: %w", err)
		}
		log.Println("Schema migration: 'agent_uid' column added successfully.")
	} else {
		log.Println("AgentUID column check complete.")
	}
	rows.Close() // Explicitly close rows after checking for agent_uid

	// Check if 'price' column exists
	priceColumnExists := false
	priceRows, err := db.Query("PRAGMA table_info(slots)") // Use a new variable name for rows
	if err != nil {
		return fmt.Errorf("failed to re-query table info for slots (price check): %w", err)
	}
	defer priceRows.Close() // Defer close for the price check query

	for priceRows.Next() { // Iterate using the new variable
		var cid int
		var name string
		var typeName string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := priceRows.Scan(&cid, &name, &typeName, &notnull, &dfltValue, &pk); err != nil { // Corrected variable to priceRows
			return fmt.Errorf("failed to scan table info row (price check): %w", err)
		}
		if name == "price" {
			priceColumnExists = true
			break
		}
	}
	if err := priceRows.Err(); err != nil { // Check error on the correct rows variable
		return fmt.Errorf("error iterating table info rows (price check): %w", err)
	}
	// priceRows is closed by defer

	// Add 'price' column if it doesn't exist
	if !priceColumnExists {
		log.Println("Schema migration: Adding 'price' column to 'slots' table...")
		_, err := db.Exec("ALTER TABLE slots ADD COLUMN price REAL DEFAULT 0.0")
		if err != nil {
			return fmt.Errorf("failed to add price column: %w", err)
		}
		log.Println("Schema migration: 'price' column added successfully.")
	} else {
		log.Println("Price column check complete.")
	}

	log.Println("Database schema check finished.")
	return nil
}

// Close closes the database connection
func (s *SQLiteStorage) Close() error {
	// Save any pending changes
	if err := s.saveToDB(); err != nil {
		log.Printf("Warning: failed to save data to database on close: %v", err)
	}

	return s.db.Close()
}

// GetSlots returns all slots for a specific date (from cache)
func (s *SQLiteStorage) GetSlots(date string) []SlotInfo {
	return s.memoryCache.GetSlots(date)
}

// GetAllDates returns all dates that have slots (from cache)
func (s *SQLiteStorage) GetAllDates() []string {
	return s.memoryCache.GetAllDates()
}

// GetSlotCountByDate returns a map of dates to the number of available slots (from cache)
func (s *SQLiteStorage) GetSlotCountByDate() map[string]int {
	return s.memoryCache.GetSlotCountByDate()
}

// GetSlotCountByTime returns a map of hours to the number of available slots for a specific date (from cache)
func (s *SQLiteStorage) GetSlotCountByTime(date string) map[int]int {
	return s.memoryCache.GetSlotCountByTime(date)
}

// UpdateSlots first removes existing slots for the given date/event,
// then adds the new slots, and triggers a save periodically.
func (s *SQLiteStorage) UpdateSlots(date string, eventID int, eventName string, slots []SlotInfo) {
	// 1. Remove existing slots for this specific date and event from DB and cache
	//    We ignore the error here as logging is done within the function.
	//    The primary goal is to clear the way for new data.
	_ = s.RemoveSlotsForDateEvent(date, eventID)

	// 2. Add new slots to the in-memory cache
	//    We need to ensure the EventID and EventName are set on the SlotInfo objects
	//    before adding them to the cache.
	s.memoryCache.mu.Lock()
	// Ensure the date entry exists
	if _, ok := s.memoryCache.slots[date]; !ok {
		s.memoryCache.slots[date] = []SlotInfo{}
	}
	// Add new slots, ensuring EventID and EventName are set
	addedCount := 0
	for _, slot := range slots {
		// Create a copy to avoid modifying the input slice directly if it's reused
		newSlot := slot
		newSlot.EventID = eventID
		newSlot.EventName = eventName
		s.memoryCache.slots[date] = append(s.memoryCache.slots[date], newSlot)
		addedCount++
	}
	if addedCount > 0 {
		log.Printf("UpdateSlots: Added %d new slots to cache for date %s, event %d", addedCount, date, eventID)
	}
	// Update metadata in cache
	s.memoryCache.lastUpdated = time.Now()
	s.memoryCache.updateStatus = "Success" // Assume success if we got this far
	s.memoryCache.mu.Unlock()

	// 3. Save to database periodically (e.g., every 10 seconds)
	if time.Since(s.lastSaveTime) > 10*time.Second { // <-- Reduced interval
		if err := s.saveToDB(); err != nil {
			log.Printf("Warning: failed to save data to database: %v", err)
			// Update status to reflect save error
			s.memoryCache.SetUpdateError(fmt.Sprintf("DB save failed: %v", err))
			// Don't update lastSaveTime if save failed
		} else {
			s.lastSaveTime = time.Now()
			// Ensure metadata reflects successful save
			s.saveMetadata("update_status", s.memoryCache.GetUpdateStatus()) // Save current status (likely "Success")
			s.saveMetadata("last_updated", s.memoryCache.GetLastUpdated().Format(time.RFC3339))
		}
	}
}

// SetUpdateError sets the update status to an error message
func (s *SQLiteStorage) SetUpdateError(err string) {
	s.memoryCache.SetUpdateError(err)
	s.saveMetadata("update_status", "Error: "+err)
}

// SetUpdateStatus sets the update status to a custom message
func (s *SQLiteStorage) SetUpdateStatus(status string) {
	s.memoryCache.SetUpdateStatus(status)
	s.saveMetadata("update_status", status)
}

// GetLastUpdated returns the time of the last update (from cache)
func (s *SQLiteStorage) GetLastUpdated() time.Time {
	return s.memoryCache.GetLastUpdated()
}

// GetUpdateStatus returns the status of the last update (from cache)
func (s *SQLiteStorage) GetUpdateStatus() string {
	return s.memoryCache.GetUpdateStatus()
}

// ClearOldDates removes dates that are in the past
func (s *SQLiteStorage) ClearOldDates() {
	// Get dates to clear
	today := time.Now().Format("2006-01-02")
	var datesToClear []string

	// Use DB query for reliability if possible
	dbDates, err := s.GetDatesWithSlots() // This function now logs internally
	if err != nil {
		log.Printf("Warning: Could not get dates from DB for ClearOldDates, using cache: %v", err)
		dbDates = s.memoryCache.GetAllDates() // Fallback to cache dates
	}

	for _, date := range dbDates {
		if date < today {
			datesToClear = append(datesToClear, date)
		}
	}

	// Clear from in-memory cache first
	s.memoryCache.ClearOldDates() // Let the cache handle its internal logic

	// Clear from database
	if len(datesToClear) > 0 {
		tx, err := s.db.Begin()
		if err != nil {
			log.Printf("Warning: failed to begin transaction for clearing old dates: %v", err)
			return
		}

		stmt, err := tx.Prepare("DELETE FROM slots WHERE date = ?")
		if err != nil {
			log.Printf("Warning: failed to prepare statement for clearing old dates: %v", err)
			tx.Rollback()
			return
		}
		defer stmt.Close()

		deletedCount := 0
		for _, date := range datesToClear {
			res, err := stmt.Exec(date)
			if err != nil {
				log.Printf("Warning: failed to delete slots for date %s: %v", date, err)
			} else {
				rowsAffected, _ := res.RowsAffected()
				deletedCount += int(rowsAffected)
			}
		}

		if err := tx.Commit(); err != nil {
			log.Printf("Warning: failed to commit transaction for clearing old dates: %v", err)
			tx.Rollback()
		} else {
			if deletedCount > 0 {
				log.Printf("Cleared %d old slots from DB for dates before %s", deletedCount, today)
			}
		}
	}
}

// ResetStorage clears all stored data
func (s *SQLiteStorage) ResetStorage() {
	// Reset in-memory cache
	s.memoryCache.ResetStorage()

	// Clear database tables
	_, err := s.db.Exec("DELETE FROM slots")
	if err != nil {
		log.Printf("Warning: failed to clear slots table: %v", err)
	}
	_, err = s.db.Exec("DELETE FROM metadata")
	if err != nil {
		log.Printf("Warning: failed to clear metadata table: %v", err)
	}

	s.saveMetadata("update_status", "Storage reset")
	log.Println("SQLite storage reset.")
}

// GetDatesWithSlots returns a sorted list of dates that have at least one slot stored
func (s *SQLiteStorage) GetDatesWithSlots() ([]string, error) {
	// Query distinct dates from the database for reliability
	log.Println("GetDatesWithSlots: Querying DB for distinct dates...") // Log DB query start
	rows, err := s.db.Query("SELECT DISTINCT date FROM slots ORDER BY date ASC")
	if err != nil {
		// Fallback to in-memory cache if DB query fails
		log.Printf("Warning: GetDatesWithSlots - failed to query distinct dates from DB: %v. Falling back to cache.", err)
		// Get dates from cache (already implemented in memoryCache)
		dates := s.memoryCache.GetAllDates() // This gets keys from the cache map
		log.Printf("GetDatesWithSlots: Falling back to cache, found %d dates.", len(dates))
		// Sort dates from cache
		sort.Strings(dates)
		return dates, nil // Return cache dates, but no error indication for simplicity
	}
	defer rows.Close()

	var dates []string
	for rows.Next() {
		var date string
		if err := rows.Scan(&date); err != nil {
			log.Printf("Warning: failed to scan date row: %v", err)
			continue // Skip problematic rows
		}
		dates = append(dates, date)
	}

	if err := rows.Err(); err != nil { // Check for errors during iteration
		log.Printf("Warning: GetDatesWithSlots - error iterating date rows: %v. Falling back to cache.", err)
		// Fallback to cache if iteration had errors
		cacheDates := s.memoryCache.GetAllDates()
		log.Printf("GetDatesWithSlots: Error during DB iteration, falling back to cache, found %d dates.", len(cacheDates))
		sort.Strings(cacheDates)
		return cacheDates, nil
	}

	// Cache synchronization logic removed as it could lead to premature cache clearing.
	// UpdateSlots and RemoveSlotsForDateEvent are responsible for keeping cache consistent.

	log.Printf("GetDatesWithSlots: Successfully queried DB, found %d distinct dates.", len(dates))
	return dates, nil // Return dates from DB
}

// saveToDB saves all in-memory data to the database
func (s *SQLiteStorage) saveToDB() error {
	log.Println("saveToDB: Starting periodic save...") // Log save start

	// Begin transaction
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Prepare statement for inserting or replacing slots based on PRIMARY KEY
	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO slots (date, event_id, event_name, agent_uid, name, start_time, end_time, location, price)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) -- Added agent_uid placeholder
	`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to prepare insert or replace statement: %w", err)
	}
	defer stmt.Close()

	// Insert all slots from cache
	s.memoryCache.mu.RLock() // Lock cache for reading
	insertedCount := 0
	for date, slots := range s.memoryCache.slots {
		for _, slot := range slots {
			startTime := slot.StartTime.Format(time.RFC3339)
			endTime := ""
			if !slot.EndTime.IsZero() {
				endTime = slot.EndTime.Format(time.RFC3339)
			}

			// Use 0.0 if price is not set or negative (though it shouldn't be negative)
			priceToSave := slot.Price
			if priceToSave < 0 {
				priceToSave = 0.0
			}
			_, err := stmt.Exec(date, slot.EventID, slot.EventName, slot.AgentUID, slot.Name, startTime, endTime, slot.Location, priceToSave) // Added agent_uid and price
			if err != nil {
				s.memoryCache.mu.RUnlock() // Unlock cache before returning
				tx.Rollback()
				return fmt.Errorf("failed to insert slot (date: %s, event: %d, name: %s): %w", date, slot.EventID, slot.Name, err)
			}
			insertedCount++
		}
	}
	s.memoryCache.mu.RUnlock() // Unlock cache after reading

	// Save metadata
	s.saveMetadataInTx(tx, "last_updated", s.memoryCache.lastUpdated.Format(time.RFC3339))
	s.saveMetadataInTx(tx, "update_status", s.memoryCache.updateStatus)

	// Commit transaction
	if err := tx.Commit(); err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("saveToDB: Successfully saved/replaced %d slots to DB.", insertedCount) // Log save end
	return nil
}

// loadFromDB loads all data from the database into memory
func (s *SQLiteStorage) loadFromDB() error {
	log.Println("Loading data from SQLite database...")
	// Load slots
	rows, err := s.db.Query(`
		SELECT date, event_id, event_name, agent_uid, name, start_time, end_time, location, price
		FROM slots
	`) // Added agent_uid and price columns
	if err != nil {
		return fmt.Errorf("failed to query slots: %w", err)
	}
	defer rows.Close()

	// Group slots by date
	slotsByDate := make(map[string][]SlotInfo)
	loadedCount := 0
	for rows.Next() {
		var slot SlotInfo
		var date, startTimeStr string
		// Handle potential NULL values for end_time and location
		var endTimeNullable, locationNullable sql.NullString

		if err := rows.Scan(
			&date, &slot.EventID, &slot.EventName, &slot.AgentUID, &slot.Name, // Added agent_uid scan target
			&startTimeStr, &endTimeNullable, &locationNullable, &slot.Price,
		); err != nil {
			log.Printf("Warning: failed to scan slot row: %v", err)
			continue // Skip problematic rows
		}

		slot.StartTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			log.Printf("Warning: failed to parse start time '%s' for event %d, slot '%s': %v", startTimeStr, slot.EventID, slot.Name, err)
			continue // Skip slots with invalid start times
		}

		if endTimeNullable.Valid && endTimeNullable.String != "" { // Add check for non-empty string
			slot.EndTime, err = time.Parse(time.RFC3339, endTimeNullable.String)
			if err != nil {
				log.Printf("Warning: failed to parse end time '%s' for event %d, slot '%s': %v", endTimeNullable.String, slot.EventID, slot.Name, err)
				// Keep EndTime as zero if parsing fails
				slot.EndTime = time.Time{} // Explicitly set to zero time on parse error
			}
		} else {
			// If end time is NULL or an empty string in DB, ensure EndTime is zero
			slot.EndTime = time.Time{}
		}

		if locationNullable.Valid {
			slot.Location = locationNullable.String
		}

		slotsByDate[date] = append(slotsByDate[date], slot)
		loadedCount++
	}
	if err := rows.Err(); err != nil {
		log.Printf("Warning: error iterating slot rows during load: %v", err)
	}

	// Update in-memory cache with loaded slots
	s.memoryCache.mu.Lock()
	s.memoryCache.slots = slotsByDate // Replace cache content
	s.memoryCache.mu.Unlock()

	// Load metadata
	lastUpdatedStr, err := s.getMetadata("last_updated")
	if err == nil && lastUpdatedStr != "" {
		lastUpdated, err := time.Parse(time.RFC3339, lastUpdatedStr)
		if err == nil {
			s.memoryCache.lastUpdated = lastUpdated
		} else {
			log.Printf("Warning: failed to parse last_updated metadata '%s': %v", lastUpdatedStr, err)
		}
	} else if err != nil {
		log.Printf("Warning: failed to load last_updated metadata: %v", err)
	}

	updateStatus, err := s.getMetadata("update_status")
	if err == nil && updateStatus != "" {
		s.memoryCache.updateStatus = updateStatus
	} else if err != nil {
		log.Printf("Warning: failed to load update_status metadata: %v", err)
		s.memoryCache.updateStatus = "Unknown (failed to load)"
	} else {
		s.memoryCache.updateStatus = "Not updated yet (no status in DB)"
	}

	log.Printf("Loaded %d slots from DB. Last updated: %s, Status: %s",
		loadedCount, s.memoryCache.lastUpdated.Format(time.RFC3339), s.memoryCache.updateStatus)

	return nil
}

// saveMetadata saves a metadata key-value pair to the database
func (s *SQLiteStorage) saveMetadata(key, value string) {
	_, err := s.db.Exec(
		"INSERT OR REPLACE INTO metadata (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)",
		key, value,
	)
	if err != nil {
		log.Printf("Warning: failed to save metadata %s: %v", key, err)
	}
}

// saveMetadataInTx saves a metadata key-value pair within an existing transaction
func (s *SQLiteStorage) saveMetadataInTx(tx *sql.Tx, key, value string) {
	_, err := tx.Exec(
		"INSERT OR REPLACE INTO metadata (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)",
		key, value,
	)
	if err != nil {
		log.Printf("Warning: failed to save metadata %s in transaction: %v", key, err)
	}
}

// getMetadata retrieves a metadata value by key
func (s *SQLiteStorage) getMetadata(key string) (string, error) {
	var value string
	err := s.db.QueryRow("SELECT value FROM metadata WHERE key = ?", key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil // Return empty string, not an error, if key not found
		}
		return "", fmt.Errorf("failed to get metadata %s: %w", key, err)
	}
	return value, nil
}

// SaveEventData saves event data to the database (Placeholder, might not be needed if caching events separately)
func (s *SQLiteStorage) SaveEventData(events []byte) error {
	log.Println("SaveEventData called (saving to metadata key 'events_data')")
	return s.saveEventDataInTx(nil, events)
}

// saveEventDataInTx saves event data within an existing transaction
func (s *SQLiteStorage) saveEventDataInTx(tx *sql.Tx, events []byte) error {
	var err error
	commitNeeded := false
	if tx == nil {
		tx, err = s.db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		commitNeeded = true
		// Use defer with explicit error check for rollback/commit
		defer func() {
			if p := recover(); p != nil {
				tx.Rollback()
				panic(p) // Re-panic after rollback
			} else if err != nil {
				log.Printf("Rolling back transaction due to error: %v", err)
				tx.Rollback()
			} else if commitNeeded {
				err = tx.Commit()
				if err != nil {
					log.Printf("Failed to commit transaction for saveEventDataInTx: %v", err)
				}
			}
		}()
	}

	_, err = tx.Exec(
		"INSERT OR REPLACE INTO metadata (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)",
		"events_data", string(events), // Store as string/text
	)
	if err != nil {
		// Error will be handled by defer
		return fmt.Errorf("failed to save events data: %w", err)
	}

	return err // Return nil if successful, or the error captured by defer
}

// GetEventData retrieves event data from the database
func (s *SQLiteStorage) GetEventData() ([]byte, error) {
	var value string
	err := s.db.QueryRow("SELECT value FROM metadata WHERE key = ?", "events_data").Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No data found is not an error in this context
		}
		return nil, fmt.Errorf("failed to get events data: %w", err)
	}
	return []byte(value), nil
}

// RemoveSlotsForDateEvent removes all slots associated with a specific event on a specific date
func (s *SQLiteStorage) RemoveSlotsForDateEvent(date string, eventID int) error {
	// Remove from database
	res, err := s.db.Exec("DELETE FROM slots WHERE date = ? AND event_id = ?", date, eventID)
	if err != nil {
		log.Printf("Warning: failed to delete slots from DB for date %s, event %d: %v", date, eventID, err)
		// Continue to try and remove from cache even if DB fails
	} else {
		rowsAffected, _ := res.RowsAffected()
		if rowsAffected > 0 {
			log.Printf("Deleted %d slots from DB for date %s, event %d", rowsAffected, date, eventID)
		}
	}

	// Remove from in-memory cache
	s.memoryCache.mu.Lock()
	defer s.memoryCache.mu.Unlock()

	if slots, ok := s.memoryCache.slots[date]; ok {
		var remainingSlots []SlotInfo
		removedCount := 0
		for _, slot := range slots {
			if slot.EventID != eventID {
				remainingSlots = append(remainingSlots, slot)
			} else {
				removedCount++
			}
		}
		if len(remainingSlots) == 0 {
			delete(s.memoryCache.slots, date) // Remove date entry if no slots left
			if removedCount > 0 {
				log.Printf("Removed date %s from cache after deleting last %d slots for event %d", date, removedCount, eventID)
			}
		} else if removedCount > 0 {
			s.memoryCache.slots[date] = remainingSlots
			log.Printf("Removed %d slots from cache for date %s, event %d", removedCount, date, eventID)
		}
	}

	// Note: We don't trigger a full saveToDB here.
	// Changes will persist after the next periodic save or on close.
	return err // Return the DB error, if any
}
