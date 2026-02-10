package handlers

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	tele "gopkg.in/telebot.v3"
)

// HandleCallback handles callback queries
func HandleCallback(config *HandlerConfig) func(tele.Context) error {
	return func(c tele.Context) error {
		callback := c.Callback()
		if callback == nil {
			return nil
		}

		data := callback.Data

		// Handle user's request access button
		if strings.HasPrefix(data, "request_access:") {
			return handleRequestAccess(c, config)
		}

		// Handle admin's approve button
		if strings.HasPrefix(data, "approve_access:") {
			return handleApproveAccess(c, config)
		}

		// Handle admin's deny button
		if strings.HasPrefix(data, "deny_access:") {
			return handleDenyAccess(c, config)
		}

		return c.Respond()
	}
}

// handleRequestAccess handles the request access callback
func handleRequestAccess(c tele.Context, config *HandlerConfig) error {
	data := c.Callback().Data
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
	if config.AdminID == "" {
		return c.Respond(&tele.CallbackResponse{Text: "❌ Админ не настроен. Свяжитесь с владельцем бота."})
	}

	adminIDInt, err := strconv.ParseInt(config.AdminID, 10, 64)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "❌ Ошибка конфигурации админа"})
	}

	// Send request to admin
	adminChat, err := config.Bot.ChatByID(adminIDInt)
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
		escapeMarkdown(c.Sender().FirstName), userID, escapeMarkdown(c.Sender().Username))

	if _, err := config.Bot.Send(adminChat, requestMsg, &tele.SendOptions{ParseMode: tele.ModeMarkdown, ReplyMarkup: adminBtn}); err != nil {
		log.Printf("[ERR] Failed to send access request to admin: %v", err)
		return c.Respond(&tele.CallbackResponse{Text: "❌ Ошибка отправки запроса"})
	}

	log.Printf("[REQ] Access request from user %d (%s @%s)", userID, c.Sender().FirstName, c.Sender().Username)
	return c.Respond(&tele.CallbackResponse{Text: "✅ Запрос отправлен администратору"})
}

// handleApproveAccess handles the approve access callback
func handleApproveAccess(c tele.Context, config *HandlerConfig) error {
	// Verify admin
	if config.AdminID == "" || fmt.Sprintf("%d", c.Sender().ID) != config.AdminID {
		return c.Respond(&tele.CallbackResponse{Text: "❌ Только админ может одобрять запросы"})
	}

	data := c.Callback().Data
	parts := strings.Split(data, ":")
	if len(parts) != 2 {
		return c.Respond(&tele.CallbackResponse{Text: "❌ Неверный формат запроса"})
	}

	userID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "❌ Неверный ID пользователя"})
	}

	// Add user to whitelist
	if err := config.Whitelist.AddUserToWhitelist(userID); err != nil {
		log.Printf("[ERR] Failed to add user %d to whitelist: %v", userID, err)
		return c.Respond(&tele.CallbackResponse{Text: "❌ Ошибка добавления в белый список"})
	}

	log.Printf("[LOG] User %d added to whitelist by admin", userID)

	// Notify user
	userChat, err := config.Bot.ChatByID(userID)
	if err == nil {
		config.Bot.Send(userChat, "✅ Ваш запрос на доступ одобрен! Теперь вы можете использовать бота.")
	}

	// Update admin's message
	if err := c.Edit("✅ Пользователь добавлен в белый список"); err != nil {
		log.Printf("[ERR] Failed to edit admin message: %v", err)
	}

	return c.Respond(&tele.CallbackResponse{Text: "✅ Пользователь добавлен"})
}

// handleDenyAccess handles the deny access callback
func handleDenyAccess(c tele.Context, config *HandlerConfig) error {
	// Verify admin
	if config.AdminID == "" || fmt.Sprintf("%d", c.Sender().ID) != config.AdminID {
		return c.Respond(&tele.CallbackResponse{Text: "❌ Только админ может отклонять запросы"})
	}

	data := c.Callback().Data
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
	userChat, err := config.Bot.ChatByID(userID)
	if err == nil {
		config.Bot.Send(userChat, "❌ Ваш запрос на доступ отклонен.")
	}

	// Update admin's message
	if err := c.Edit("❌ Запрос отклонен"); err != nil {
		log.Printf("[ERR] Failed to edit admin message: %v", err)
	}

	return c.Respond(&tele.CallbackResponse{Text: "❌ Запрос отклонен"})
}
