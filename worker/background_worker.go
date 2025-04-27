package worker

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/iabalyuk/mskpicnic/mosru"
	"github.com/iabalyuk/mskpicnic/storage"
	// "golang.org/x/time/rate" // Removed import
)

// const eventsCacheFile = "events_cache.json" // Removed hardcoded constant
const cacheMaxAge = 6 * time.Hour // Align with the refresh ticker

// BackgroundWorker represents an improved background worker that efficiently checks all gazebos
type BackgroundWorker struct {
	client       *mosru.Client
	storage      storage.StorageInterface
	stopCh       chan struct{}
	wg           sync.WaitGroup
	notifyCh     chan string
	isRunning    bool
	runningMutex sync.Mutex
	// limiter      *rate.Limiter // Removed limiter field
	events          []mosru.FullEventData // <-- Use FullEventData
	eventsMutex     sync.RWMutex
	checkDays       int    // Number of days ahead to check
	eventsCachePath string // Path for the events cache file
}

// NewBackgroundWorkerConfig represents the configuration for the background worker
type NewBackgroundWorkerConfig struct {
	Client          *mosru.Client
	Storage         storage.StorageInterface
	NotifyCh        chan string
	CheckDays       int    // Number of days ahead to check
	EventsCachePath string // Path for the events cache file
}

// NewBackgroundWorker creates a new background worker instance
func NewBackgroundWorker(config NewBackgroundWorkerConfig) *BackgroundWorker {
	// Limiter initialization removed

	// Use default checkDays if not provided or invalid
	checkDays := config.CheckDays
	if checkDays <= 0 {
		checkDays = 30 // Default to 30 days
		log.Printf("Invalid or zero CheckDays provided, defaulting to %d", checkDays)
	}

	return &BackgroundWorker{
		client:   config.Client,
		storage:  config.Storage,
		stopCh:   make(chan struct{}),
		notifyCh: config.NotifyCh,
		// limiter:   limiter, // Removed limiter assignment
		events:          nil, // Initialize as nil []FullEventData
		checkDays:       checkDays,
		eventsCachePath: config.EventsCachePath, // Assign cache path
	}
}

// saveEventsToCache saves the list of full event data to the cache file
func (w *BackgroundWorker) saveEventsToCache(events []mosru.FullEventData) error { // <-- Use FullEventData
	data, err := json.MarshalIndent(events, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal full events for cache: %w", err)
	}
	err = os.WriteFile(w.eventsCachePath, data, 0644) // Use configured path
	if err != nil {
		return fmt.Errorf("failed to write events cache file %s: %w", w.eventsCachePath, err)
	}
	log.Printf("Saved %d events to cache file %s", len(events), w.eventsCachePath)
	return nil
}

// loadEventsFromCache loads full event data from the cache file and checks its age
func (w *BackgroundWorker) loadEventsFromCache() ([]mosru.FullEventData, error) { // <-- Use FullEventData
	fileInfo, err := os.Stat(w.eventsCachePath) // Use configured path
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("cache file %s does not exist", w.eventsCachePath)
		}
		return nil, fmt.Errorf("failed to stat cache file %s: %w", w.eventsCachePath, err)
	}

	// Check cache file age
	if time.Since(fileInfo.ModTime()) > cacheMaxAge {
		return nil, fmt.Errorf("cache file %s is older than %v", w.eventsCachePath, cacheMaxAge)
	}

	data, err := os.ReadFile(w.eventsCachePath) // Use configured path
	if err != nil {
		return nil, fmt.Errorf("failed to read cache file %s: %w", w.eventsCachePath, err)
	}

	var events []mosru.FullEventData // <-- Use FullEventData
	err = json.Unmarshal(data, &events)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal full events from cache file %s: %w", w.eventsCachePath, err)
	}

	log.Printf("Loaded %d events from cache file %s (modified %v ago)", len(events), w.eventsCachePath, time.Since(fileInfo.ModTime()).Round(time.Second))
	return events, nil
}

// Start starts the background worker
func (w *BackgroundWorker) Start() {
	w.runningMutex.Lock()
	defer w.runningMutex.Unlock()

	if w.isRunning {
		return
	}

	w.isRunning = true
	w.wg.Add(1) // Add for the main run loop
	go w.run()
}

// Stop stops the background worker
func (w *BackgroundWorker) Stop() {
	w.runningMutex.Lock()
	defer w.runningMutex.Unlock()

	if !w.isRunning {
		return
	}

	log.Println("Stopping background worker...")
	close(w.stopCh)
	// w.wg.Wait() // Consider re-enabling if graceful shutdown of checks is needed
	w.isRunning = false
	log.Println("Background worker stop signal sent.")
}

// run is the main worker loop
func (w *BackgroundWorker) run() {
	defer w.wg.Done()
	log.Println("Background worker run loop started.")

	// Initial fetch of all events
	if err := w.fetchAllEvents(); err != nil {
		log.Printf("Error fetching events on start: %v", err)
		w.storage.SetUpdateError(fmt.Sprintf("Failed to fetch events: %v", err))
	}

	// Start checking availability for all dates
	w.startAvailabilityChecking() // This starts its own goroutine

	// Refresh events list every 6 hours
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Println("Periodic event refresh triggered.")
			// Refresh the events list periodically
			if err := w.fetchAllEvents(); err != nil {
				log.Printf("Error refreshing events: %v", err)
			}
		case <-w.stopCh:
			log.Println("Background worker run loop stopping.")
			return // Exit the loop
		}
	}
}

// fetchAllEvents fetches all events, trying the cache first
func (w *BackgroundWorker) fetchAllEvents() error {
	log.Println("Attempting to load events from cache...")
	cachedEvents, err := w.loadEventsFromCache()
	if err == nil {
		// Cache hit and valid
		w.eventsMutex.Lock()
		w.events = cachedEvents // Assign FullEventData slice
		w.eventsMutex.Unlock()
		log.Printf("Using %d events from cache.", len(cachedEvents))
		return nil
	}

	// Cache miss or error, log the reason and fetch from API
	log.Printf("Cache load failed (%v), fetching from API...", err)

	allEvents, err := w.client.AllEvents() // Fetches []FullEventData
	if err != nil {
		return fmt.Errorf("failed to fetch events from API: %w", err)
	}

	// Filter events
	var picnicEvents []mosru.FullEventData // <-- Use FullEventData
	for _, event := range allEvents {
		if containsPicnicKeyword(event.Name) {
			picnicEvents = append(picnicEvents, event)
			// Optional: Keep the detailed log if needed
			// log.Printf("Found picnic event: %d %s %s", event.ID, event.AgentUID, event.Name)
		}
	}
	log.Printf("Fetched %d total events, filtered down to %d picnic events.", len(allEvents), len(picnicEvents))

	// Update the in-memory list
	w.eventsMutex.Lock()
	w.events = picnicEvents // Assign FullEventData slice
	w.eventsMutex.Unlock()

	// Save the *filtered* list to cache
	if err := w.saveEventsToCache(picnicEvents); err != nil {
		log.Printf("Warning: Failed to save events to cache: %v", err)
		// Continue execution even if caching fails
	}

	return nil
}

// containsPicnicKeyword checks if the event name contains picnic-related keywords
func containsPicnicKeyword(name string) bool {
	// Added "беседк" (partial match for беседка/беседки) and "мангал"
	keywords := []string{"беседк", "пикник", "шашлык", "барбекю", "мангал"}
	nameLower := strings.ToLower(name)

	for _, keyword := range keywords {
		if strings.Contains(nameLower, keyword) {
			return true
		}
	}

	return false
}

// startAvailabilityChecking starts checking availability for all dates
func (w *BackgroundWorker) startAvailabilityChecking() {
	log.Println("Starting availability checking loop...")

	// Dates will be generated inside the loop now
	// dates := w.generateDates(w.checkDays)

	// Start a goroutine to continuously check availability for all dates
	w.wg.Add(1) // Add for the availability checking goroutine
	go func() {
		defer w.wg.Done()
		log.Println("Availability checking goroutine started.")

		for {
			select {
			case <-w.stopCh:
				log.Println("Availability checking loop stopping.")
				return
			default:
				// Regenerate dates based on the current time for each cycle
				dates := w.generateDates(w.checkDays)
				if len(dates) == 0 {
					log.Println("Warning: generateDates returned empty list, skipping check cycle.")
				} else {
					// Check availability for the newly generated dates
					w.checkAvailabilityForDates(dates)
				}

				// Sleep for a short time before the next cycle
				// Use a select with timeout to allow faster shutdown
				select {
				case <-time.After(1 * time.Minute):
					// Continue loop
				case <-w.stopCh:
					log.Println("Availability checking loop stopping during sleep.")
					return
				}
			}
		}
	}()
}

// generateDates generates a list of dates in YYYY-MM-DD format for the next n days
func (w *BackgroundWorker) generateDates(days int) []string { // Make it a method of BackgroundWorker
	var dates []string
	now := time.Now()

	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, i)
		dates = append(dates, date.Format("2006-01-02"))
	}

	return dates
}

// checkAvailabilityForDates checks availability for all dates
func (w *BackgroundWorker) checkAvailabilityForDates(dates []string) {
	// Get the current list of events
	w.eventsMutex.RLock()
	currentEvents := make([]mosru.FullEventData, len(w.events)) // Create a copy
	copy(currentEvents, w.events)
	w.eventsMutex.RUnlock()

	if len(currentEvents) == 0 {
		log.Println("No events loaded to check availability for.")
		w.storage.SetUpdateStatus("No events loaded")
		return
	}

	log.Printf("Checking availability for %d events across %d dates (next %d days)", len(currentEvents), len(dates), w.checkDays)

	// Context for rate limiting removed (client handles it)
	// ctx := context.Background()

	// Create a wait group to wait for all goroutines to finish
	var checkWg sync.WaitGroup // Use a local WaitGroup for this check cycle

	// Create a channel to collect results (now includes full event info and price)
	resultCh := make(chan struct {
		date  string
		event mosru.FullEventData
		slots []storage.SlotInfo // SlotInfo now includes Price
	}, len(dates)*len(currentEvents))

	// Start a goroutine for each date and event combination
	for _, date := range dates {
		for _, event := range currentEvents { // Use the copied slice
			checkWg.Add(1)
			go func(date string, event mosru.FullEventData) { // <-- Use FullEventData
				defer checkWg.Done()

				// Check stop channel before making network calls
				select {
				case <-w.stopCh:
					return // Stop early if requested
				default:
					// Continue
				}

				if w.client.Debug {
					log.Printf("[Trace %s/%d] Checking availability...", date, event.ID)
				}
				// Check if this event is available on this date
				available, err := w.client.IsAvailable(date, event.ID)
				if err != nil {
					// Log actual errors regardless of debug mode
					log.Printf("Error checking availability for event %d (%s) on %s: %v", event.ID, event.Name, date, err)
					return // Don't try to remove slots if availability check failed
				}
				if w.client.Debug {
					log.Printf("[Trace %s/%d] IsAvailable result: %t", date, event.ID, available)
				}

				if !available {
					if w.client.Debug {
						log.Printf("[Trace %s/%d] Not available. Removing existing slots from storage.", date, event.ID)
					}
					// Event is not available on this date, remove any existing slots from storage
					err := w.storage.RemoveSlotsForDateEvent(date, event.ID)
					if err != nil {
						// Log actual errors regardless of debug mode
						log.Printf("Error removing slots for unavailable event %d (%s) on %s: %v", event.ID, event.Name, date, err)
					}
					return // Don't proceed further for this event/date
				}

				// Event IS available, proceed to list slots
				if w.client.Debug {
					log.Printf("[Trace %s/%d] Available. Proceeding to list slots.", date, event.ID)
				}

				// Check stop channel again before next network call
				select {
				case <-w.stopCh:
					return // Stop early if requested
				default:
					// Continue
				}

				// Get slots for this event on this date
				if w.client.Debug {
					log.Printf("[Trace %s/%d] Calling ListSlots (AgentUID: '%s')...", date, event.ID, event.AgentUID)
				}
				apiSlots, err := w.client.ListSlots(date, event.ID, event.AgentUID)
				if err != nil {
					// Log actual errors regardless of debug mode
					log.Printf("Error listing slots for event %d (%s) on %s: %v", event.ID, event.Name, date, err)
					// Don't remove slots here, as IsAvailable was true. Maybe a temporary API error.
					return
				}
				if w.client.Debug {
					log.Printf("[Trace %s/%d] ListSlots returned %d raw slots.", date, event.ID, len(apiSlots))
				}

				// Convert and filter API slots to SlotInfo objects
				var processedSlotInfos []storage.SlotInfo
				for i, slot := range apiSlots { // Use index i for logging
					if w.client.Debug {
						// Include Slot ID in trace log
						log.Printf("[Trace %s/%d] Processing raw slot %d: ID=%d, Name='%s', Start='%s', Max=%d, Total=%d", date, event.ID, i, slot.ID, slot.Name, slot.StartDatetime, slot.MaxVisitors, slot.TotalTickets)
					}
					// Skip slots that are full
					if slot.MaxVisitors <= slot.TotalTickets {
						if w.client.Debug {
							log.Printf("[Trace %s/%d]   -> Skipping slot %d: Full (%d/%d)", date, event.ID, i, slot.TotalTickets, slot.MaxVisitors)
						}
						continue
					}

					// Skip slots before 10:00 AM
					timePart := ""
					if len(slot.StartDatetime) > len(date+"T") {
						timePart = slot.StartDatetime[len(date+"T"):]
					}
					if timePart < "10:00:00" { // Use string comparison, should be fine for HH:MM:SS
						if w.client.Debug {
							log.Printf("[Trace %s/%d]   -> Skipping slot %d: Too early (%s)", date, event.ID, i, timePart)
						}
						continue
					}
					// Parse slot string
					// Note: ParseSlotInfo doesn't know EventID/Name, they are added in UpdateSlots
					slotInfo, err := storage.ParseSlotInfo(fmt.Sprintf("%s: %s", slot.Name, slot.StartDatetime))
					if err != nil {
						// Log actual errors regardless of debug mode
						log.Printf("Error parsing slot info string '%s' for event %d on %s: %v", fmt.Sprintf("%s: %s", slot.Name, slot.StartDatetime), event.ID, date, err)
						continue
					}

					// --- Fetch Tariff Info ---
					var price float64 = 0.0 // Default price if tariff fetch fails or no price found
					// Wait for rate limiter removed (client handles it)

					// Directly call GetTariff, client handles limiting
					tariffData, tariffErr := w.client.GetTariff(slot.ID, event.AgentUID) // Use slot.ID (performance ID)
					if tariffErr != nil {
						// Error is already logged within GetTariff, just proceed with default price
						log.Printf("[Trace %s/%d] Non-fatal error fetching tariff for slot %d (ID: %d): %v. Using price 0.0", date, event.ID, i, slot.ID, tariffErr)
					} else if tariffData != nil && len(tariffData.TicketTariffs) > 0 {
						// Find the most relevant price (e.g., first visible one)
						foundPrice := false
						for _, tt := range tariffData.TicketTariffs {
							if tt.VisibleForWidget { // Prioritize widget-visible tariffs
								price = tt.Price
								foundPrice = true
								if w.client.Debug {
									log.Printf("[Trace %s/%d]   -> Found price %.2f from tariff '%s' (Visitor: %s)", date, event.ID, price, tariffData.Name, tt.VisitorCatName)
								}
								break
							}
						}
						// Fallback if no widget-visible price found
						if !foundPrice && len(tariffData.TicketTariffs) > 0 {
							price = tariffData.TicketTariffs[0].Price // Use the first one as fallback
							if w.client.Debug {
								log.Printf("[Trace %s/%d]   -> No widget-visible price found, using fallback price %.2f from tariff '%s'", date, event.ID, price, tariffData.Name)
							}
						}
					} else if w.client.Debug {
						log.Printf("[Trace %s/%d]   -> No tariff data or empty ticket tariffs received for slot %d (ID: %d). Using price 0.0", date, event.ID, i, slot.ID)
					}
					// --- End Fetch Tariff Info ---

					slotInfo.Price = price             // Set the fetched or default price
					slotInfo.AgentUID = event.AgentUID // Set the AgentUID from the parent event
					if w.client.Debug {                // Make this log conditional
						log.Printf("[Trace %s/%d]   -> Adding processed slot: %+v", date, event.ID, slotInfo) // Log includes price and AgentUID now
					}
					processedSlotInfos = append(processedSlotInfos, slotInfo)
				}
				if w.client.Debug { // Make this log conditional
					log.Printf("[Trace %s/%d] Finished processing slots. Found %d valid slots with prices.", date, event.ID, len(processedSlotInfos))
				}

				// Send results (including the full event info and the processed slots with prices) to the channel.
				// Send even if processedSlotInfos is empty, so UpdateSlots can clear old entries.
				resultCh <- struct {
					date  string
					event mosru.FullEventData // <-- Use FullEventData
					slots []storage.SlotInfo
				}{date, event, processedSlotInfos}

			}(date, event)
		}
	}

	// Start a goroutine to close the result channel when all check goroutines are done
	go func() {
		checkWg.Wait()
		close(resultCh)
		log.Println("Finished checking all event/date combinations for this cycle.")
	}()

	// Process results as they come in
	processedCount := 0
	for result := range resultCh {
		// Update storage: This call will remove old slots for the date/event
		// and insert the new ones (or none if result.slots is empty).
		if w.client.Debug {
			log.Printf("[Result Processing] Updating storage for Date: %s, EventID: %d, EventName: '%s' with %d slots.", result.date, result.event.ID, result.event.Name, len(result.slots))
		}
		// TODO: Add error handling for UpdateSlots?
		w.storage.UpdateSlots(result.date, result.event.ID, result.event.Name, result.slots)
		processedCount++

		// --- NOTIFICATION DISABLED ---
		// // Send notification ONLY if new slots were actually found and we have a channel
		// if len(result.slots) > 0 && w.notifyCh != nil {
		// 	// Generate event URL using the correct format
		// 	eventURL := fmt.Sprintf("https://bilet.mos.ru/event/%d/", result.event.ID)
		// 	notification := fmt.Sprintf("Found %d slots for %s (%s): %s", len(result.slots), result.date, result.event.Name, eventURL)
		// 	select {
		// 	case w.notifyCh <- notification:
		// 		// Notification sent
		// 	default:
		// 		// Channel is full or closed, just log
		// 		log.Println("Notify channel full/closed, logging instead:", notification)
		// 	}
		// }
		// --- END NOTIFICATION DISABLED ---
	}
	// No need for the extra loop checking processedEvents anymore,
	// as UpdateSlots handles removal implicitly when called with an empty slice.

	// Update storage with success status
	statusMsg := fmt.Sprintf("Success (processed %d event/date results for next %d days)", processedCount, w.checkDays)
	w.storage.SetUpdateStatus(statusMsg)
	log.Printf("Finished processing availability results: %s", statusMsg)
}

// ForceCheck forces an immediate check for all dates
func (w *BackgroundWorker) ForceCheck() {
	w.runningMutex.Lock()
	defer w.runningMutex.Unlock()

	if !w.isRunning {
		log.Println("ForceCheck called, but worker is not running.")
		return
	}

	// Fetch events if we don't have any (or if cache is stale, fetchAllEvents handles this)
	w.eventsMutex.RLock()
	eventCount := len(w.events)
	w.eventsMutex.RUnlock()
	if eventCount == 0 {
		log.Println("ForceCheck: No events loaded, fetching...")
		if err := w.fetchAllEvents(); err != nil {
			log.Printf("ForceCheck: Error fetching events: %v", err)
			return
		}
		w.eventsMutex.RLock()
		eventCount = len(w.events)
		w.eventsMutex.RUnlock()
		if eventCount == 0 {
			log.Println("ForceCheck: Still no events after fetching, cannot check.")
			return
		}
	}

	// Generate dates for the configured number of days
	dates := w.generateDates(w.checkDays)

	// Start a goroutine to check availability for all dates
	log.Println("ForceCheck: Triggering background availability check...")
	go w.checkAvailabilityForDates(dates)
}
