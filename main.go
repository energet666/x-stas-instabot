package main

import (
	"log"
	"net/http"
	"os"
	"slices"
	"strconv"
	"time"

	"io"

	"github.com/joho/godotenv"
	"github.com/x-stas/instabot/handlers"
	tele "gopkg.in/telebot.v3"
)

func main() {
	// This is a test comment for the new branch
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, reading from environment")
	}

	token := os.Getenv("TELEGRAM_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_TOKEN environment variable is required")
	}

	adminID := os.Getenv("ADMIN_ID")

	// Load whitelist configuration
	whitelist, err := LoadWhitelist()
	if err != nil {
		log.Fatalf("Failed to load whitelist: %v", err)
	}
	log.Printf("Whitelist loaded with %d users", len(whitelist.Users))

	// Automatically add admin to whitelist
	if adminID != "" {
		adminIDInt, err := strconv.ParseInt(adminID, 10, 64)
		if err != nil {
			log.Printf("[WARN] Invalid ADMIN_ID format: %v", err)
		} else {
			// Check if admin is already in whitelist
			if !slices.Contains(whitelist.Users, adminIDInt) {
				if err := AddUserToWhitelist(whitelist, adminIDInt); err != nil {
					log.Printf("[WARN] Failed to add admin to whitelist: %v", err)
				} else {
					log.Printf("[LOG] Admin (ID: %d) automatically added to whitelist", adminIDInt)
				}
			} else {
				log.Printf("[LOG] Admin (ID: %d) already in whitelist", adminIDInt)
			}
		}
	}

	// Setup logging to file and stdout
	logFile, err := os.OpenFile("app.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Failed to open log file: %v", err)
	} else {
		multi := io.MultiWriter(os.Stdout, logFile)
		log.SetOutput(multi)
	}

	cookieFile := os.Getenv("COOKIES_FILE")

	pref := tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
		Client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}

	b, err := tele.NewBot(pref)
	if err != nil {
		log.Fatal(err)
	}

	// Create handler config
	handlerConfig := &handlers.HandlerConfig{
		Whitelist:        whitelist,
		AdminID:          adminID,
		CookieFile:       cookieFile,
		Bot:              b,
		DownloadContent:  DownloadContent,
		OptimizeVideo:    OptimizeVideo,
		GetVideoMetadata: GetVideoMetadata,
	}

	// Register handlers
	b.Handle("/start", handlers.HandleStart(handlerConfig))
	b.Handle("/log", handlers.HandleLog(handlerConfig))
	b.Handle(tele.OnText, handlers.HandleText(handlerConfig))
	b.Handle(tele.OnCallback, handlers.HandleCallback(handlerConfig))

	log.Printf("Bot started: @%s", b.Me.Username)
	b.Start()
}
