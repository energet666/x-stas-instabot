package handlers

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tele "gopkg.in/telebot.v3"
)

// HandleText handles text messages (Instagram URLs)
func HandleText(config *HandlerConfig) func(tele.Context) error {
	return func(c tele.Context) error {
		text := c.Text()
		user := c.Sender()

		// Check whitelist
		if !config.Whitelist.IsUserWhitelisted(user.ID) {
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
		if config.AdminID != "" {
			adminIDInt, err := strconv.ParseInt(config.AdminID, 10, 64)
			if err == nil {
				adminChat, err := config.Bot.ChatByID(adminIDInt)
				if err == nil {
					notificationMsg := fmt.Sprintf("🔔 *Новая ссылка от пользователя*\n\n"+
						"👤 Имя: %s\n"+
						"🆔 ID: %d\n"+
						"📝 Username: @%s\n"+
						"🔗 Ссылка: %s",
						escapeMarkdown(user.FirstName), user.ID, escapeMarkdown(user.Username), escapeMarkdown(text))
					config.Bot.Send(adminChat, notificationMsg, &tele.SendOptions{ParseMode: tele.ModeMarkdown})
				}
			}
		}

		statusMsg, err := config.Bot.Send(c.Chat(), "⏳ Начинаю скачивание... Это может занять до минуты.")
		if err != nil {
			log.Printf("[ERR] Failed to send status message: %v", err)
		}

		// Helper to safely edit status message
		safeEdit := func(text string, opts ...interface{}) {
			if statusMsg != nil {
				_, err := config.Bot.Edit(statusMsg, text, opts...)
				if err != nil {
					log.Printf("[WRN] Failed to edit status message: %v", err)
				}
			}
		}

		// Limit concurrent downloads
		select {
		case config.Semaphore <- struct{}{}:
			// Slot available, proceed immediately
		default:
			// Slots full, wait in queue
			safeEdit("⏳ Сервер загружен. Вы в очереди...")
			config.Semaphore <- struct{}{}
		}
		defer func() { <-config.Semaphore }()

		// Start download process
		result, err := config.DownloadContent(text, config.CookieFile)
		if err != nil {
			log.Printf("[ERR] Download failed for %s: %v", text, err)
			cleanErr := strings.ReplaceAll(err.Error(), "`", "'")
			safeEdit(fmt.Sprintf("❌ Ошибка при скачивании:\n`%v`", cleanErr), &tele.SendOptions{ParseMode: tele.ModeMarkdown})
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
			safeEdit("🛠 Оптимизирую видео для Telegram...")
		} else {
			safeEdit(fmt.Sprintf("✅ Скачано файлов: %d. Начинаю отправку...", len(result.Files)))
		}

		// Send files back
		finalPaths := make([]string, len(result.Files))
		for i, filePath := range result.Files {
			log.Printf("[PROC] Processing file %d/%d: %s", i+1, len(result.Files), filepath.Base(filePath))

			var err error
			var finalPath = filePath

			if strings.HasSuffix(strings.ToLower(filePath), ".mp4") {
				safeEdit(fmt.Sprintf("🛠 Оптимизирую видео %d из %d...", i+1, len(result.Files)))
				log.Printf("[LOG] Optimizing video for compatibility: %s", filepath.Base(filePath))
				optimizedPath, optErr := config.OptimizeVideo(filePath, func() {
					safeEdit(fmt.Sprintf("📦 Видео %d из %d превышает 50 МБ — перекодирую с уменьшенным битрейтом...", i+1, len(result.Files)))
				})
				if optErr != nil {
					log.Printf("[WRN] Optimization failed: %v. Sending original.", optErr)
					c.Send(fmt.Sprintf("⚠️ Ошибка при оптимизации видео: %v. Отправляю оригинал...", optErr))
				} else {
					finalPath = optimizedPath
				}

				v := &tele.Video{
					File:      tele.FromDisk(finalPath),
					FileName:  filepath.Base(filePath),
					Streaming: true,
				}

				if meta, err := config.GetVideoMetadata(finalPath); err == nil {
					v.Width = meta.Width
					v.Height = meta.Height
					v.Duration = meta.Duration
				} else {
					log.Printf("[WRN] Could not get metadata for %s: %v", finalPath, err)
				}

				safeEdit(fmt.Sprintf("📤 Отправляю файл %d из %d...", i+1, len(result.Files)))
				log.Printf("[SEND] Sending video: %s", filepath.Base(finalPath))
				err = c.Send(v)
			} else {
				safeEdit(fmt.Sprintf("📤 Отправляю файл %d из %d...", i+1, len(result.Files)))
				p := &tele.Photo{File: tele.FromDisk(filePath)}
				log.Printf("[SEND] Sending photo: %s", filepath.Base(filePath))
				err = c.Send(p)
			}

			finalPaths[i] = finalPath

			if err != nil {
				log.Printf("[ERR] Failed to send file %s: %v", filePath, err)
				c.Send(fmt.Sprintf("⚠️ Не удалось отправить файл %s: %v", filepath.Base(filePath), err))
			}
		}

		// Move files to permanent storage if enabled and user is admin
		if config.PermanentStoragePath != "" && config.AdminID != "" {
			adminIDInt, err := strconv.ParseInt(config.AdminID, 10, 64)
			if err == nil && user.ID == adminIDInt {
				if err := os.MkdirAll(config.PermanentStoragePath, 0755); err != nil {
					log.Printf("[ERR] Failed to create permanent storage dir: %v", err)
				} else {
					log.Printf("[LOG] Moving files to permanent storage: %s", config.PermanentStoragePath)
					for i, path := range finalPaths {
						// Use original filename but optimized content
						originalName := filepath.Base(result.Files[i])
						destPath := filepath.Join(config.PermanentStoragePath, originalName)

						if err := MoveFile(path, destPath); err != nil {
							log.Printf("[ERR] Failed to move file %s to %s: %v", path, destPath, err)
						} else {
							log.Printf("[LOG] Moved %s to permanent storage", originalName)
						}
					}
				}
			}
		}

		log.Printf("[DONE] Finished processing request from @%s", user.Username)
		if statusMsg != nil {
			config.Bot.Delete(statusMsg)
		}
		return nil
	}
}
