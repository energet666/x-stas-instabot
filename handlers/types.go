package handlers

import (
	tele "gopkg.in/telebot.v3"
)

// DownloadContentFunc is a function type for downloading content
type DownloadContentFunc func(url string, cookieFile string) (*DownloadResult, error)

// OptimizeVideoFunc is a function type for optimizing videos.
// onReencode is called (if non-nil) right before a second re-encode pass
// is started to satisfy Telegram's 50 MB file size limit.
type OptimizeVideoFunc func(inputPath string, onReencode func()) (string, error)

// GetVideoMetadataFunc is a function type for getting video metadata
type GetVideoMetadataFunc func(filePath string) (*VideoMetadata, error)

// WhitelistChecker is an interface for checking whitelist
type WhitelistChecker interface {
	IsUserWhitelisted(userID int64) bool
	AddUserToWhitelist(userID int64) error
}

// HandlerConfig contains configuration needed by handlers
type HandlerConfig struct {
	Whitelist            WhitelistChecker
	AdminID              string
	CookieFile           string
	Bot                  *tele.Bot
	DownloadContent      DownloadContentFunc
	OptimizeVideo        OptimizeVideoFunc
	GetVideoMetadata     GetVideoMetadataFunc
	PermanentStoragePath string
	Semaphore            chan struct{}
}

// DownloadResult holds information about downloaded files
type DownloadResult struct {
	Files []string
	Dir   string
}

// VideoMetadata holds basic video information
type VideoMetadata struct {
	Width    int
	Height   int
	Duration int
}
