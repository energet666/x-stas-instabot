package handlers

import (
	"fmt"

	tele "gopkg.in/telebot.v3"
)

// HandleStart handles the /start command
func HandleStart(config *HandlerConfig) func(tele.Context) error {
	return func(c tele.Context) error {
		if !config.Whitelist.IsUserWhitelisted(c.Sender().ID) {
			btn := &tele.ReplyMarkup{
				InlineKeyboard: [][]tele.InlineButton{
					{{Text: "🔐 Запросить доступ", Data: fmt.Sprintf("request_access:%d", c.Sender().ID)}},
				},
			}
			return c.Send("⛔ У вас нет доступа к этому боту.", btn)
		}
		return c.Send("Привет! Отправь мне ссылку на Instagram пост, Reels или Carousel, и я скачаю контент для тебя.")
	}
}
