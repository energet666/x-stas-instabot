# AGENTS.md — Руководство для AI-агентов

Этот файл содержит важный контекст для AI-агентов (Copilot, Antigravity, Claude и др.), работающих с репозиторием **x-stas-instabot**.

---

## Обзор проекта

Telegram-бот на Go для скачивания Reels и фото из Instagram.

| Компонент | Роль |
|---|---|
| `gallery-dl` | Скачивание контента по URL |
| `ffmpeg` / `ffprobe` | Перекодирование видео, получение метаданных |
| `gopkg.in/telebot.v3` | Telegram Bot API |
| `whitelist.json` | Персистентный список допущенных пользователей |

---

## Структура репозитория

```
.
├── main.go             # Точка входа: инициализация бота, конфиг, регистрация хэндлеров
├── downloader.go       # DownloadContent, GetVideoMetadata, OptimizeVideo, getVideoDuration
├── whitelist.go        # LoadWhitelist, AddUserToWhitelist, IsUserWhitelisted
├── whitelist.json      # Персистентный список ID пользователей
├── cookies.txt         # Netscape-куки для gallery-dl (не коммитить!)
├── app.log             # Лог-файл (не коммитить!)
├── downloads/          # Временные директории скачанных файлов (очищаются автоматически)
└── handlers/
    ├── types.go        # Все типы и интерфейсы: HandlerConfig, DownloadResult, VideoMetadata, func-типы
    ├── text.go         # HandleText — основной поток: скачать → оптимизировать → отправить → сохранить
    ├── callback.go     # HandleCallback — approve/deny доступа
    ├── start.go        # HandleStart — приветственное сообщение
    ├── log.go          # HandleLog — отправка app.log администратору
    └── helpers.go      # escapeMarkdown, MoveFile, DownloadResult.Cleanup
```

---

## Ключевые инварианты

### 1. Интерфейс HandlerConfig

`HandlerConfig` в `handlers/types.go` — единственное место для зависимостей хэндлеров.  
Все бизнес-функции (скачивание, оптимизация, метаданные) передаются **как функции-поля**, а не вызываются напрямую. Это позволяет легко тестировать хэндлеры.

```go
type HandlerConfig struct {
    Whitelist            WhitelistChecker
    AdminID              string
    CookieFile           string
    Bot                  *tele.Bot
    DownloadContent      DownloadContentFunc        // func(url, cookieFile) (*DownloadResult, error)
    OptimizeVideo        OptimizeVideoFunc          // func(inputPath string, onReencode func()) (string, error)
    GetVideoMetadata     GetVideoMetadataFunc       // func(filePath) (*VideoMetadata, error)
    PermanentStoragePath string
    Semaphore            chan struct{}
}
```

> **Важно:** при изменении сигнатуры любого `*Func`-типа нужно обновить:
> 1. `handlers/types.go` — само определение типа
> 2. `downloader.go` — реализацию функции
> 3. `handlers/text.go` (или другой хэндлер) — место вызова
> 4. `main.go` — присвоение в `handlerConfig`

### 2. Лимит Telegram — 50 МБ

`OptimizeVideo` работает в **два прохода**:
1. **Первый проход** — качество-ориентированное кодирование (`-crf 28`).
2. **Проверка размера** — если файл ≤ 50 МБ, возвращаем сразу.
3. **Второй проход** — если файл > 50 МБ, вычисляется целевой видеобитрейт:
   ```
   videoBitrate = (50MB × 8бит × 0.98) / duration_sec − 128_000
   ```
   Минимальный пол — 100 кбит/с. Константы `telegramMaxBytes` и `audioReserveBitrate` объявлены в `downloader.go`.

Параметр `onReencode func()` вызывается **перед** стартом второго прохода — используется для уведомления пользователя в Telegram.

### 3. Постоянное хранилище

- Активируется только если заданы `PERMANENT_STORAGE_PATH` и `ADMIN_ID` **и** запрос пришёл от администратора.
- В хранилище перемещается **оптимизированный** файл (`finalPath`), но под **оригинальным** именем (без суффикса `.optimized.mp4`).
- Используется `MoveFile` из `handlers/helpers.go` — она корректно обрабатывает перемещение между разными файловыми системами (copy+delete вместо rename).

### 4. Семафор и очередь

`Semaphore` — небуферизованный (или ограниченный) канал, настраивается через `CONCURRENT_LIMIT`.  
В `HandleText` сначала делается неблокирующая попытка (`select { case ... default }`), и только при занятости слотов пользователю сообщается, что он в очереди.

### 5. Временные файлы

Каждый запрос создаёт `downloads/instabot-*` директорию через `os.MkdirTemp`.  
`result.Cleanup()` (= `os.RemoveAll`) вызывается через `defer` — директория удаляется **после** отправки файлов и перемещения в хранилище.

---

## Переменные окружения

| Переменная | Обязательна | Описание |
|---|---|---|
| `TELEGRAM_TOKEN` | ✅ | Токен от @BotFather |
| `ADMIN_ID` | ✅ (рекомендуется) | Telegram ID администратора |
| `COOKIES_FILE` | ❌ | Путь к файлу куки для gallery-dl |
| `PERMANENT_STORAGE_PATH` | ❌ | Директория для постоянного хранения файлов (только для admin) |
| `CONCURRENT_LIMIT` | ❌ | Макс. параллельных загрузок (по умолчанию: 1) |

---

## Логирование

Все логи дублируются в `stdout` и `app.log`.  
Используемые префиксы:

| Префикс | Значение |
|---|---|
| `[REQ]` | Входящий запрос от пользователя |
| `[LOG]` | Информационное событие |
| `[PROC]` | Обработка файла |
| `[SEND]` | Отправка файла в Telegram |
| `[WRN]` | Некритическое предупреждение |
| `[ERR]` | Ошибка |
| `[DONE]` | Завершение обработки запроса |

---

## Типичные задачи для агентов

### Добавить новый источник (не Instagram)
1. Расширить валидацию URL в `handlers/text.go` (сейчас: `strings.Contains(text, "instagram.com/")`).
2. Убедиться, что `gallery-dl` поддерживает нужный сайт.

### Изменить параметры ffmpeg
- Параметры первого прохода — в `OptimizeVideo` (`downloader.go`), команда `ffmpeg` с `-crf 28`.
- Параметры второго прохода (bitrate-cap) — там же, ниже по коду.

### Добавить новую команду бота
1. Создать файл `handlers/mycommand.go` по образцу `handlers/log.go`.
2. Зарегистрировать хэндлер в `main.go`: `b.Handle("/mycommand", handlers.HandleMyCommand(handlerConfig))`.

### Изменить лимит размера файла
Изменить константу `telegramMaxBytes` в `downloader.go`. Лимит Telegram для ботов — **50 МБ** (2025).

---

## Чего не делать

- **Не коммитить** `cookies.txt`, `app.log`, `.env`, `whitelist.json` с реальными ID.
- **Не вызывать** функции `DownloadContent` / `OptimizeVideo` / `GetVideoMetadata` напрямую внутри `handlers/` — только через `config.*`.
- **Не удалять** `result.Cleanup()` из `defer` — иначе временные файлы будут накапливаться на диске.
- **Не менять** сигнатуры `*Func`-типов без обновления всех трёх точек использования (types → impl → caller).
