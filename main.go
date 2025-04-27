package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/iabalyuk/mskpicnic/bot"
	"github.com/iabalyuk/mskpicnic/mosru"
	"github.com/iabalyuk/mskpicnic/storage"
	"github.com/iabalyuk/mskpicnic/worker"
)

func main() {
	// Parse command-line flags
	debug := flag.Bool("debug", false, "Enable debug mode")
	token := flag.String("token", "", "Telegram bot token (or use TELEGRAM_BOT_TOKEN env var)")
	// dbPath := flag.String("db", "mskpicnic.db", "Path to SQLite database file") // Removed flag
	checkDays := flag.Int("checkDays", 30, "Number of days ahead to check for availability")
	flag.Parse()

	// --- Configuration from Environment Variables ---
	botToken := *token
	if botToken == "" {
		botToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	}
	if botToken == "" {
		log.Fatal("Telegram bot token is required. Provide it with -token flag or TELEGRAM_BOT_TOKEN environment variable")
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "data/mskpicnic.db" // Default path within a 'data' directory
		log.Printf("DB_PATH not set, using default: %s", dbPath)
	}

	eventsCachePath := os.Getenv("EVENTS_CACHE_PATH")
	if eventsCachePath == "" {
		eventsCachePath = "data/events_cache.json" // Default path within a 'data' directory
		log.Printf("EVENTS_CACHE_PATH not set, using default: %s", eventsCachePath)
	}
	// --- End Configuration ---

	// Initialize components
	client := mosru.NewClient(*debug)

	// Initialize SQLite storage
	store, err := storage.NewSQLiteStorage(dbPath) // Use configured dbPath
	if err != nil {
		log.Fatalf("Failed to initialize SQLite storage at %s: %v", dbPath, err)
	}
	defer store.Close()

	// Initialize and start the bot
	telegramBot, err := bot.New(botToken, client, store) // Use configured botToken
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	// Initialize and start the background worker
	bgWorker := worker.NewBackgroundWorker(worker.NewBackgroundWorkerConfig{
		Client:          client,
		Storage:         store,
		NotifyCh:        telegramBot.GetNotifyChannel(),
		CheckDays:       *checkDays,      // Pass the flag value
		EventsCachePath: eventsCachePath, // Pass configured cache path
	})

	// Start the background worker
	bgWorker.Start()
	log.Printf("Background worker started (DB: %s, Cache: %s)", dbPath, eventsCachePath)

	// Start the bot in a separate goroutine
	go func() {
		if err := telegramBot.Start(); err != nil {
			log.Fatalf("Failed to start bot: %v", err)
		}
	}()

	// Wait for termination signal
	sigCh := make(chan os.Signal, 1)
	// Listen for SIGINT, SIGTERM, and SIGSTOP
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGSTOP)

	// Wait for a signal
	receivedSignal := <-sigCh
	log.Printf("Received signal: %v", receivedSignal)

	// Handle the signal
	if receivedSignal == syscall.SIGSTOP {
		log.Println("Received SIGSTOP, forcing immediate exit.")
		os.Exit(1) // Exit immediately with a non-zero status
	} else {
		// For SIGINT or SIGTERM, perform graceful shutdown
		log.Println("Initiating graceful shutdown...")
		bgWorker.Stop()
		log.Println("Background worker stopped")
		log.Println("Bot stopped")
	}
}
