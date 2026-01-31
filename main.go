package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"io"

	"github.com/joho/godotenv"
	tele "gopkg.in/telebot.v3"
)

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, reading from environment")
	}

	token := os.Getenv("TELEGRAM_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_TOKEN environment variable is required")
	}

	adminID := os.Getenv("ADMIN_ID")

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

	b.Handle("/start", func(c tele.Context) error {
		return c.Send("Привет! Отправь мне ссылку на Instagram пост, Reels или Carousel, и я скачаю контент для тебя.")
	})

	b.Handle("/log", func(c tele.Context) error {
		if adminID == "" || fmt.Sprintf("%d", c.Sender().ID) != adminID {
			return nil // Silence for non-admins
		}

		// Read last lines of the log file
		data, err := os.ReadFile("app.log")
		if err != nil {
			return c.Send("❌ Ошибка чтения логов.")
		}

		lines := strings.Split(string(data), "\n")
		start := len(lines) - 21
		if start < 0 {
			start = 0
		}

		lastLogs := strings.Join(lines[start:], "\n")
		if len(lastLogs) > 4000 {
			lastLogs = lastLogs[len(lastLogs)-4000:]
		}

		return c.Send(fmt.Sprintf("📋 Последние логи:\n```\n%s\n```", lastLogs), &tele.SendOptions{ParseMode: tele.ModeMarkdown})
	})

	b.Handle(tele.OnText, func(c tele.Context) error {
		text := c.Text()
		user := c.Sender()

		// Simple validation for Instagram URL
		if !strings.Contains(text, "instagram.com/") {
			return c.Send("⚠️ Пожалуйста, отправь корректную ссылку на Instagram.")
		}

		log.Printf("[REQ] User: %s (@%s) | URL: %s", user.FirstName, user.Username, text)

		statusMsg, _ := b.Send(c.Chat(), "⏳ Начинаю скачивание... Это может занять до минуты.")

		// Start download process
		result, err := DownloadContent(text, cookieFile)
		if err != nil {
			log.Printf("[ERR] Download failed for %s: %v", text, err)
			b.Edit(statusMsg, fmt.Sprintf("❌ Ошибка при скачивании:\n`%v`", err), &tele.SendOptions{ParseMode: tele.ModeMarkdown})
			return nil
		}
		defer result.Cleanup()

		log.Printf("[LOG] Downloaded %d files for %s", len(result.Files), text)

		hasVideos := false
		for _, f := range result.Files {
			if strings.HasSuffix(strings.ToLower(f), ".mp4") {
				hasVideos = true
				break
			}
		}

		if hasVideos {
			b.Edit(statusMsg, "🛠 Оптимизирую видео для Telegram...")
		} else {
			b.Edit(statusMsg, fmt.Sprintf("✅ Скачано файлов: %d. Начинаю отправку...", len(result.Files)))
		}

		// Send files back
		for i, filePath := range result.Files {
			log.Printf("[PROC] Processing file %d/%d: %s", i+1, len(result.Files), filepath.Base(filePath))

			var err error
			var finalPath = filePath

			if strings.HasSuffix(strings.ToLower(filePath), ".mp4") {
				b.Edit(statusMsg, fmt.Sprintf("🛠 Оптимизирую видео %d из %d...", i+1, len(result.Files)))
				log.Printf("[LOG] Optimizing video for compatibility: %s", filepath.Base(filePath))
				optimizedPath, optErr := OptimizeVideo(filePath)
				if optErr != nil {
					log.Printf("[WRN] Optimization failed: %v. Sending original.", optErr)
					c.Send(fmt.Sprintf("⚠️ Ошибка при оптимизации видео: %v. Отправляю оригинал...", optErr))
				} else {
					finalPath = optimizedPath
				}

				v := &tele.Video{
					File:      tele.FromDisk(finalPath),
					Streaming: true,
				}

				if meta, err := GetVideoMetadata(finalPath); err == nil {
					v.Width = meta.Width
					v.Height = meta.Height
					v.Duration = meta.Duration
				} else {
					log.Printf("[WRN] Could not get metadata for %s: %v", finalPath, err)
				}

				b.Edit(statusMsg, fmt.Sprintf("📤 Отправляю файл %d из %d...", i+1, len(result.Files)))
				log.Printf("[SEND] Sending video: %s", filepath.Base(finalPath))
				err = c.Send(v)
			} else {
				b.Edit(statusMsg, fmt.Sprintf("📤 Отправляю файл %d из %d...", i+1, len(result.Files)))
				p := &tele.Photo{File: tele.FromDisk(filePath)}
				log.Printf("[SEND] Sending photo: %s", filepath.Base(filePath))
				err = c.Send(p)
			}

			if err != nil {
				log.Printf("[ERR] Failed to send file %s: %v", filePath, err)
				c.Send(fmt.Sprintf("⚠️ Не удалось отправить файл %s: %v", filepath.Base(filePath), err))
			}
		}

		log.Printf("[DONE] Finished processing request from @%s", user.Username)
		b.Delete(statusMsg)
		return nil
	})

	log.Printf("Bot started: @%s", b.Me.Username)
	b.Start()
}
