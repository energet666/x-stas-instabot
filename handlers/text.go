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
						user.FirstName, user.ID, user.Username, text)
					config.Bot.Send(adminChat, notificationMsg, &tele.SendOptions{ParseMode: tele.ModeMarkdown})
				}
			}
		}

		statusMsg, _ := config.Bot.Send(c.Chat(), "⏳ Начинаю скачивание... Это может занять до минуты.")

		// Start download process
		result, err := config.DownloadContent(text, config.CookieFile)
		if err != nil {
			log.Printf("[ERR] Download failed for %s: %v", text, err)
			config.Bot.Edit(statusMsg, fmt.Sprintf("❌ Ошибка при скачивании:\n`%v`", err), &tele.SendOptions{ParseMode: tele.ModeMarkdown})
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
			config.Bot.Edit(statusMsg, "🛠 Оптимизирую видео для Telegram...")
		} else {
			config.Bot.Edit(statusMsg, fmt.Sprintf("✅ Скачано файлов: %d. Начинаю отправку...", len(result.Files)))
		}

		// Send files back
		finalPaths := make([]string, len(result.Files))
		for i, filePath := range result.Files {
			log.Printf("[PROC] Processing file %d/%d: %s", i+1, len(result.Files), filepath.Base(filePath))

			var err error
			var finalPath = filePath

			if strings.HasSuffix(strings.ToLower(filePath), ".mp4") {
				config.Bot.Edit(statusMsg, fmt.Sprintf("🛠 Оптимизирую видео %d из %d...", i+1, len(result.Files)))
				log.Printf("[LOG] Optimizing video for compatibility: %s", filepath.Base(filePath))
				optimizedPath, optErr := config.OptimizeVideo(filePath)
				if optErr != nil {
					log.Printf("[WRN] Optimization failed: %v. Sending original.", optErr)
					c.Send(fmt.Sprintf("⚠️ Ошибка при оптимизации видео: %v. Отправляю оригинал...", optErr))
				} else {
					finalPath = optimizedPath
				}

				v := &tele.Video{
					File:      tele.FromDisk(finalPath),
					FileName:  filepath.Base(finalPath),
					Streaming: true,
				}

				if meta, err := config.GetVideoMetadata(finalPath); err == nil {
					v.Width = meta.Width
					v.Height = meta.Height
					v.Duration = meta.Duration
				} else {
					log.Printf("[WRN] Could not get metadata for %s: %v", finalPath, err)
				}

				config.Bot.Edit(statusMsg, fmt.Sprintf("📤 Отправляю файл %d из %d...", i+1, len(result.Files)))
				log.Printf("[SEND] Sending video: %s", filepath.Base(finalPath))
				err = c.Send(v)
			} else {
				config.Bot.Edit(statusMsg, fmt.Sprintf("📤 Отправляю файл %d из %d...", i+1, len(result.Files)))
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
		config.Bot.Delete(statusMsg)
		return nil
	}
}
