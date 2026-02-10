package handlers

import (
	"fmt"
	"os"
	"strings"

	tele "gopkg.in/telebot.v3"
)

// HandleLog handles the /log command (admin only)
func HandleLog(config *HandlerConfig) func(tele.Context) error {
	return func(c tele.Context) error {
		if config.AdminID == "" || fmt.Sprintf("%d", c.Sender().ID) != config.AdminID {
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

		safeLogs := strings.ReplaceAll(lastLogs, "`", "'")
		return c.Send(fmt.Sprintf("📋 Последние логи:\n```\n%s\n```", safeLogs), &tele.SendOptions{ParseMode: tele.ModeMarkdown})
	}
}
