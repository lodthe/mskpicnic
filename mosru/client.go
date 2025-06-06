package mosru

import (
	"context"
	"encoding/json"
	"fmt"
	"io"  // <-- Add io
	"log" // <-- Add log
	"net/http"
	"time"

	"golang.org/x/time/rate" // Add rate limiter import
)

// Client represents a client for the mos.ru API
type Client struct {
	httpClient *http.Client
	limiter    *rate.Limiter // Add limiter field
	Debug      bool
}

// Poster represents poster information
type Poster struct {
	PicUID  string `json:"picUid"`
	Main    bool   `json:"main"`
	EventID int    `json:"eventId"` // Note: API shows eventId, Go convention is EventID
}

// Spot represents spot information within an event
type Spot struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
}

// FullEventData represents the complete structure for an event from the API
type FullEventData struct {
	ID                                   int            `json:"id"`
	Name                                 string         `json:"name"`
	NameVariant                          *string        `json:"nameVariant"` // Use pointer for nullable fields
	Description                          string         `json:"description"`
	FullDescription                      string         `json:"fullDescription"`
	EventKindID                          int            `json:"eventKindId"`
	EventKindName                        string         `json:"eventKindName"`
	EventKindShortName                   *string        `json:"eventKindShortName"`
	EventTypeKey                         string         `json:"eventTypeKey"`
	StartDate                            string         `json:"startDate"` // Keep as string YYYY-MM-DD
	StartDateTime                        *string        `json:"startDateTime"`
	EndDate                              string         `json:"endDate"` // Keep as string YYYY-MM-DD
	EndDateTime                          *string        `json:"endDateTime"`
	Pg                                   string         `json:"pg"`
	IsActive                             bool           `json:"isActive"`
	SpotID                               int            `json:"spotId"`
	FullPriceID                          int            `json:"fullPriceId"`
	SpotName                             string         `json:"spotName"`
	SpotAddress                          string         `json:"spotAddress"`
	MuseumID                             int            `json:"museumId"`
	MuseumName                           string         `json:"museumName"`
	AgentUID                             string         `json:"agent_uid"` // Corrected tag back to snake_case
	Longitude                            float64        `json:"longitude"` // Changed type from string to float64
	Latitude                             float64        `json:"latitude"`  // Changed type from string to float64
	PriceFrom                            float64        `json:"priceFrom"`
	PriceTo                              float64        `json:"priceTo"`
	InsideLocation                       *string        `json:"inside_location"` // API uses snake_case here
	NearestDate                          *string        `json:"nearestDate"`     // Keep as string ISO 8601 format
	Posters                              []Poster       `json:"posters"`         // Nested array
	Tags                                 []any          `json:"tags"`            // Use any for unknown/mixed tag structure
	OnlinePlatforms                      []any          `json:"onlinePlatforms"`
	SpecialEventType                     *string        `json:"specialEventType"`
	ProCultureID                         *string        `json:"proCultureId"`
	DistrChannelType                     *string        `json:"distrChannelType"`
	WithPromoAvailable                   bool           `json:"withPromoAvailable"`
	Anonymously                          bool           `json:"anonymously"`
	ShowDuration                         bool           `json:"showDuration"`
	MoskvenokAvailable                   bool           `json:"moskvenokAvailable"`
	IsLimitTicketsSale                   bool           `json:"isLimitTicketsSale"`
	MaxQuantityForSaleTickets            int            `json:"maxQuantityForSaleTickets"`
	EventClass                           *string        `json:"eventClass"`
	DocIdentifier                        bool           `json:"docIdentifier"`
	IsBiometricsPassAvailable            bool           `json:"isBiometricsPassAvailable"`
	AllowedEditPersonalDataMinutesOffset int            `json:"allowedEditPersonalDataMinutesOffset"`
	TicketLimitID                        *int           `json:"ticketLimitId"`
	IsAllowedEditPersonalDataBeforeStart bool           `json:"isAllowedEditPersonalDataBeforeStart"`
	Spots                                []Spot         `json:"spots"` // Nested array
	TicketLimitResponseDto               map[string]any `json:"ticketLimitResponseDto"`
}

// Event represents an event from the mos.ru API (Kept for potential backward compatibility or other uses, though not used by AllEvents anymore)
type Event struct {
	ID       int    `json:"id"`
	AgentUID string `json:"agent_uid"`
	Name     string `json:"name"`
}

// Slot represents a time slot (performance) for an event
type Slot struct {
	ID            int    `json:"id"` // Added performance ID
	Name          string `json:"name"`
	StartDatetime string `json:"start_datetime"`
	MaxVisitors   int    `json:"max_visitors"`
	TotalTickets  int    `json:"total_tickets"`
}

// --- Structs for Tariff API ---

// TariffResponse represents the top-level structure of the tariff API response
type TariffResponse struct {
	Data TariffData `json:"data"`
}

// TariffData contains the main tariff details
type TariffData struct {
	ID            int            `json:"id"`
	Name          string         `json:"name"`
	TicketTariffs []TicketTariff `json:"ticket_tariffs"`
	// Add other fields from TariffData if needed later
}

// TicketTariff contains details about a specific ticket price within a tariff
type TicketTariff struct {
	ID               int     `json:"id"`
	VisitorCatName   string  `json:"visitor_cat_name"`
	TicketTypeName   string  `json:"ticket_type_name"`
	Price            float64 `json:"price"`
	VisibleForWidget bool    `json:"visible_for_widget"`
	// Add other fields from TicketTariff if needed later
}

// --- End Tariff Structs ---

// AvailabilityItem represents availability information for a specific date and time
type AvailabilityItem struct {
	FreeSeatsNumber int `json:"free_seats_number"`
}

// NewClient creates a new mos.ru API client
func NewClient(debug bool) *Client {
	// Initialize the rate limiter (e.g., 5 requests/sec, burst 5)
	limiter := rate.NewLimiter(20, 5)
	log.Printf("Initialized mosru.Client rate limiter: %v requests/sec, burst %d", limiter.Limit(), limiter.Burst())

	return &Client{
		httpClient: &http.Client{
			Timeout: 5 * time.Minute, // Increased timeout
			Transport: &http.Transport{
				MaxIdleConns:          10,
				MaxIdleConnsPerHost:   5,
				IdleConnTimeout:       60 * time.Second,
				ResponseHeaderTimeout: 5 * time.Minute, // Match overall timeout
			},
		},
		limiter: limiter, // Assign the limiter
		Debug:   debug,
	}
}

// doRequest handles the common logic for making HTTP requests to the mos.ru API
func (c *Client) doRequest(ctx context.Context, method, url string, target interface{}) error {
	// Wait for rate limiter before creating the request
	if err := c.limiter.Wait(ctx); err != nil {
		// Check if the error is due to context cancellation
		if ctx.Err() != nil {
			return fmt.Errorf("rate limiter context error (%s %s): %w", method, url, ctx.Err())
		}
		// Log other limiter errors but potentially allow proceeding if minor?
		// For now, return the error.
		log.Printf("Rate limiter error for %s %s: %v", method, url, err)
		return fmt.Errorf("rate limiter error (%s %s): %w", method, url, err)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add common headers
	req.Header.Add("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.114 Safari/537.36")
	req.Header.Add("Accept", "application/json, text/plain, */*")
	req.Header.Add("Accept-Language", "en-US,en;q=0.9,ru;q=0.8")
	req.Header.Add("Connection", "keep-alive")

	if c.Debug {
		fmt.Printf("Making request: %s %s\n", method, url)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Check for context deadline exceeded
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("request context error (%s %s): %w", method, url, ctxErr)
		}
		return fmt.Errorf("failed to execute request (%s %s): %w", method, url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// TODO: Consider reading the body here for better error messages
		return fmt.Errorf("unexpected status code %d for %s %s", resp.StatusCode, method, url)
	}

	// Decode if a target is provided
	if target != nil {
		if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
			// TODO: Consider reading the body as string for logging before decode attempt
			return fmt.Errorf("failed to decode response for %s %s: %w", method, url, err)
		}
	}

	return nil
}

// AllEvents fetches the full data for all events from the mos.ru API
func (c *Client) AllEvents() ([]FullEventData, error) {
	if c.Debug {
		fmt.Println("Fetching all events (full data)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	url := "https://tickets.mos.ru/widget/api/widget/getevents"
	var result struct {
		Data []FullEventData `json:"data"`
	}

	err := c.doRequest(ctx, "GET", url, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to get all events: %w", err)
	}

	return result.Data, nil
}

// IsAvailable checks if there are free seats for a specific date and event
func (c *Client) IsAvailable(date string, eventID int) (bool, error) {
	if c.Debug {
		fmt.Printf("Checking availability %s %d\n", date, eventID)
	}

	// Parse the date and add one day to get the to date
	// This assumes the API requires a range [date_from, date_to)
	parsedDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		return false, fmt.Errorf("failed to parse date '%s': %w", date, err)
	}
	toDate := parsedDate.AddDate(0, 0, 1).Format("2006-01-02")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second) // Slightly increased timeout
	defer cancel()

	url := fmt.Sprintf("https://tickets.mos.ru/widget/api/widget/performance_free_seats?date_from=%s&date_to=%s&event_id=%d", date, toDate, eventID)

	var result map[string][]AvailabilityItem
	// We need to handle the response manually here to potentially log the body on error/unexpected structure
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false, fmt.Errorf("IsAvailable: failed to create request: %w", err)
	}
	req.Header.Add("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.114 Safari/537.36")
	req.Header.Add("Accept", "application/json, text/plain, */*")
	req.Header.Add("Accept-Language", "en-US,en;q=0.9,ru;q=0.8")
	req.Header.Add("Connection", "keep-alive")

	if c.Debug {
		fmt.Printf("Making request: GET %s\n", url)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, fmt.Errorf("IsAvailable context error (GET %s): %w", url, ctxErr)
		}
		return false, fmt.Errorf("IsAvailable failed request (GET %s): %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Try reading body for error info
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("IsAvailable unexpected status code %d for GET %s. Body: %s", resp.StatusCode, url, string(bodyBytes))
		return false, fmt.Errorf("IsAvailable unexpected status code %d for GET %s", resp.StatusCode, url)
	}

	// Read the body first
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("IsAvailable failed to read response body for GET %s: %w", url, err)
	}
	// Now decode
	// The variable 'result' was already declared on line 204
	if err := json.Unmarshal(bodyBytes, &result); err != nil { // Use the result declared on line 204
		log.Printf("IsAvailable failed to decode response for GET %s. Body: %s. Error: %v", url, string(bodyBytes), err)
		return false, fmt.Errorf("IsAvailable failed to decode response for GET %s: %w", url, err)
	}

	// Use the decoded result
	items, ok := result[date]
	if !ok {
		if c.Debug {
			// Log the actual structure if the key is missing
			log.Printf("IsAvailable: Date key '%s' not found in response for event %d. Response keys: %v", date, eventID, getMapKeys(result))
		}
		return false, nil // No items for the date means no availability
	}

	if len(items) == 0 {
		if c.Debug {
			log.Printf("IsAvailable: Items list is empty for date %s, event %d.", date, eventID)
		}
		return false, nil // Empty list also means no availability
	}

	for _, item := range items {
		if item.FreeSeatsNumber > 0 {
			if c.Debug {
				fmt.Printf("Availability found for date %s, event %d: %d free seats\n", date, eventID, item.FreeSeatsNumber)
			}
			return true, nil
		}
	}

	if c.Debug {
		fmt.Printf("No free seats found in availability items for date %s, event %d\n", date, eventID)
	}
	return false, nil
}

// ListSlots fetches detailed information about available slots
func (c *Client) ListSlots(date string, eventID int, agentUID string) ([]Slot, error) {
	if c.Debug {
		fmt.Printf("Listing slots %s %d %s\n", date, eventID, agentUID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second) // Slightly increased timeout
	defer cancel()

	url := fmt.Sprintf("https://tickets.mos.ru/widget/api/widget/events/getperformances?event_id=%d&agent_uid=%s&date=%s", eventID, agentUID, date)

	var result struct {
		Data []Slot `json:"data"`
	}
	err := c.doRequest(ctx, "GET", url, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to list slots: %w", err)
	}

	return result.Data, nil
}

// GetTariff fetches tariff information for a specific performance
func (c *Client) GetTariff(performanceID int, agentUID string) (*TariffData, error) {
	if c.Debug {
		fmt.Printf("Fetching tariff for performance %d (agent %s)\n", performanceID, agentUID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second) // Short timeout for tariff
	defer cancel()

	url := fmt.Sprintf("https://tickets.mos.ru/widget/api/widget/performance/gettariffs?performance_id=%d&agent_uid=%s", performanceID, agentUID)

	var result TariffResponse
	err := c.doRequest(ctx, "GET", url, &result) // Use the existing helper
	if err != nil {
		// Don't treat not found (e.g., 404) as a fatal error for tariffs, just return nil data
		// TODO: Improve doRequest to return status code or specific error types
		// For now, log the error and return nil data, allowing slot processing to continue
		log.Printf("Warning: Failed to get tariff for performance %d: %v. Proceeding without price.", performanceID, err)
		return nil, nil // Return nil data, nil error to indicate non-fatal issue
	}

	// Basic validation
	if len(result.Data.TicketTariffs) == 0 {
		log.Printf("Warning: No ticket tariffs found in response for performance %d.", performanceID)
		// Return the data structure even if tariffs are empty, maybe other fields are useful
	}

	return &result.Data, nil
}

// Helper function to get keys from the availability map for logging
func getMapKeys(m map[string][]AvailabilityItem) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// Note: The unused Event struct (lines 84-88) could be removed if not used elsewhere.
// Keeping it for now unless confirmed otherwise.
