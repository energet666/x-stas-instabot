package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"io"

	"github.com/joho/godotenv"
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

	b.Handle("/start", func(c tele.Context) error {
		if !IsUserWhitelisted(whitelist, c.Sender().ID) {
			btn := &tele.ReplyMarkup{
				InlineKeyboard: [][]tele.InlineButton{
					{{Text: "🔐 Запросить доступ", Data: fmt.Sprintf("request_access:%d", c.Sender().ID)}},
				},
			}
			return c.Send("⛔ У вас нет доступа к этому боту.", btn)
		}
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
		start := max(len(lines)-21, 0)

		lastLogs := strings.Join(lines[start:], "\n")
		if len(lastLogs) > 4000 {
			lastLogs = lastLogs[len(lastLogs)-4000:]
		}

		return c.Send(fmt.Sprintf("📋 Последние логи:\n```\n%s\n```", lastLogs), &tele.SendOptions{ParseMode: tele.ModeMarkdown})
	})

	b.Handle(tele.OnText, func(c tele.Context) error {
		text := c.Text()
		user := c.Sender()

		// Check whitelist
		if !IsUserWhitelisted(whitelist, user.ID) {
			btn := &tele.ReplyMarkup{
				InlineKeyboard: [][]tele.InlineButton{
					{{Text: "🔐 Запросить доступ", Data: fmt.Sprintf("request_access:%d", user.ID)}},
				},
			}
			return c.Send("⛔ У вас нет доступа к этому боту.", btn)
		}

		// Simple validation for Instagram URL
		if !strings.Contains(text, "instagram.com/") {
			return c.Send("⚠️ Пожалуйста, отправь корректную ссылку на Instagram.")
		}

		log.Printf("[REQ] User: %s (@%s) | URL: %s", user.FirstName, user.Username, text)

		// Send notification to admin
		if adminID != "" {
			adminIDInt, err := strconv.ParseInt(adminID, 10, 64)
			if err == nil {
				adminChat, err := b.ChatByID(adminIDInt)
				if err == nil {
					notificationMsg := fmt.Sprintf("🔔 *Новая ссылка от пользователя*\n\n"+
						"👤 Имя: %s\n"+
						"🆔 ID: %d\n"+
						"📝 Username: @%s\n"+
						"🔗 Ссылка: %s",
						user.FirstName, user.ID, user.Username, text)
					b.Send(adminChat, notificationMsg, &tele.SendOptions{ParseMode: tele.ModeMarkdown})
				}
			}
		}

		statusMsg, _ := b.Send(c.Chat(), "⏳ Начинаю скачивание... Это может занять до минуты.")

		// Start download process
		result, err := DownloadContent(text, cookieFile)
		if err != nil {
			log.Printf("[ERR] Download failed for %s: %v", text, err)
			b.Edit(statusMsg, fmt.Sprintf("❌ Ошибка при скачивании:\n`%v`", err), &tele.SendOptions{ParseMode: tele.ModeMarkdown})
			return nil
		}
		defer result.Cleanup()

		log.Printf("[LOG] Downloaded %d files for %s:", len(result.Files), text)
		for _, f := range result.Files {
			if info, err := os.Stat(f); err == nil {
				sizeMB := float64(info.Size()) / (1024 * 1024)
				log.Printf("  - %s (%.2f MB)", filepath.Base(f), sizeMB)
			}
		}

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

	// Handle callback for "Запросить доступ" button
	b.Handle(tele.OnCallback, func(c tele.Context) error {
		callback := c.Callback()
		if callback == nil {
			return nil
		}

		data := callback.Data

		// Handle user's request access button
		if strings.HasPrefix(data, "request_access:") {
			parts := strings.Split(data, ":")
			if len(parts) != 2 {
				return c.Respond(&tele.CallbackResponse{Text: "❌ Неверный формат запроса"})
			}

			userID, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return c.Respond(&tele.CallbackResponse{Text: "❌ Неверный ID пользователя"})
			}

			// Only allow the user who clicked the button to request access
			if c.Sender().ID != userID {
				return c.Respond(&tele.CallbackResponse{Text: "❌ Вы можете запросить доступ только для себя"})
			}

			// Check if admin ID is configured
			if adminID == "" {
				return c.Respond(&tele.CallbackResponse{Text: "❌ Админ не настроен. Свяжитесь с владельцем бота."})
			}

			adminIDInt, err := strconv.ParseInt(adminID, 10, 64)
			if err != nil {
				return c.Respond(&tele.CallbackResponse{Text: "❌ Ошибка конфигурации админа"})
			}

			// Send request to admin
			adminChat, err := b.ChatByID(adminIDInt)
			if err != nil {
				log.Printf("[ERR] Failed to get admin chat: %v", err)
				return c.Respond(&tele.CallbackResponse{Text: "❌ Ошибка отправки запроса"})
			}

			// Create inline keyboard for admin
			adminBtn := &tele.ReplyMarkup{
				InlineKeyboard: [][]tele.InlineButton{
					{
						{Text: "✅ Да", Data: fmt.Sprintf("approve_access:%d", userID)},
						{Text: "❌ Нет", Data: fmt.Sprintf("deny_access:%d", userID)},
					},
				},
			}

			requestMsg := fmt.Sprintf("🔔 *Запрос на доступ*\n\n"+
				"👤 Имя: %s\n"+
				"🆔 ID: %d\n"+
				"📝 Username: @%s\n\n"+
				"Хочет получить доступ к боту.",
				c.Sender().FirstName, userID, c.Sender().Username)

			if _, err := b.Send(adminChat, requestMsg, &tele.SendOptions{ParseMode: tele.ModeMarkdown, ReplyMarkup: adminBtn}); err != nil {
				log.Printf("[ERR] Failed to send access request to admin: %v", err)
				return c.Respond(&tele.CallbackResponse{Text: "❌ Ошибка отправки запроса"})
			}

			log.Printf("[REQ] Access request from user %d (%s @%s)", userID, c.Sender().FirstName, c.Sender().Username)
			return c.Respond(&tele.CallbackResponse{Text: "✅ Запрос отправлен администратору"})
		}

		// Handle admin's approve button
		if strings.HasPrefix(data, "approve_access:") {
			// Verify admin
			if adminID == "" || fmt.Sprintf("%d", c.Sender().ID) != adminID {
				return c.Respond(&tele.CallbackResponse{Text: "❌ Только админ может одобрять запросы"})
			}

			parts := strings.Split(data, ":")
			if len(parts) != 2 {
				return c.Respond(&tele.CallbackResponse{Text: "❌ Неверный формат запроса"})
			}

			userID, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return c.Respond(&tele.CallbackResponse{Text: "❌ Неверный ID пользователя"})
			}

			// Add user to whitelist
			if err := AddUserToWhitelist(whitelist, userID); err != nil {
				log.Printf("[ERR] Failed to add user %d to whitelist: %v", userID, err)
				return c.Respond(&tele.CallbackResponse{Text: "❌ Ошибка добавления в белый список"})
			}

			log.Printf("[LOG] User %d added to whitelist by admin", userID)

			// Notify user
			userChat, err := b.ChatByID(userID)
			if err == nil {
				b.Send(userChat, "✅ Ваш запрос на доступ одобрен! Теперь вы можете использовать бота.")
			}

			// Update admin's message
			if err := c.Edit("✅ Пользователь добавлен в белый список"); err != nil {
				log.Printf("[ERR] Failed to edit admin message: %v", err)
			}

			return c.Respond(&tele.CallbackResponse{Text: "✅ Пользователь добавлен"})
		}

		// Handle admin's deny button
		if strings.HasPrefix(data, "deny_access:") {
			// Verify admin
			if adminID == "" || fmt.Sprintf("%d", c.Sender().ID) != adminID {
				return c.Respond(&tele.CallbackResponse{Text: "❌ Только админ может отклонять запросы"})
			}

			parts := strings.Split(data, ":")
			if len(parts) != 2 {
				return c.Respond(&tele.CallbackResponse{Text: "❌ Неверный формат запроса"})
			}

			userID, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return c.Respond(&tele.CallbackResponse{Text: "❌ Неверный ID пользователя"})
			}

			log.Printf("[LOG] Access request from user %d denied by admin", userID)

			// Notify user
			userChat, err := b.ChatByID(userID)
			if err == nil {
				b.Send(userChat, "❌ Ваш запрос на доступ отклонен.")
			}

			// Update admin's message
			if err := c.Edit("❌ Запрос отклонен"); err != nil {
				log.Printf("[ERR] Failed to edit admin message: %v", err)
			}

			return c.Respond(&tele.CallbackResponse{Text: "❌ Запрос отклонен"})
		}

		return c.Respond()
	})

	log.Printf("Bot started: @%s", b.Me.Username)
	b.Start()
}
