package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/energet666/x-stas-instabot/handlers"
)

// DownloadResult holds information about downloaded files
type DownloadResult = handlers.DownloadResult

// VideoMetadata holds basic video information
type VideoMetadata = handlers.VideoMetadata

// DownloadContent uses gallery-dl to download content from the given URL
func DownloadContent(url string, cookieFile string) (*DownloadResult, error) {
	// Create a local downloads directory
	baseDir := "downloads"
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create downloads dir: %w", err)
	}

	// Create a unique temporary directory inside 'downloads'
	tempDir, err := os.MkdirTemp(baseDir, "instabot-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Execute gallery-dl
	// -d: specify destination directory
	// --no-mtime: don't set file modification time to server's time
	args := []string{"-d", tempDir, "--no-mtime"}
	if cookieFile != "" {
		args = append(args, "--cookies", cookieFile)
	}
	args = append(args, url)

	cmd := exec.Command("gallery-dl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		os.RemoveAll(tempDir) // Clean up on failure
		return nil, fmt.Errorf("gallery-dl failed: %v, output: %s", err, string(output))
	}

	// Find all files in the temp directory (recursive)
	var files []string
	err = filepath.Walk(tempDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			// Skip metadata files if any (gallery-dl sometimes creates .json files)
			if !strings.HasSuffix(info.Name(), ".json") {
				files = append(files, path)
			}
		}
		return nil
	})

	if err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to walk temp dir: %w", err)
	}

	if len(files) == 0 {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("no files downloaded")
	}

	return &DownloadResult{
		Files: files,
		Dir:   tempDir,
	}, nil
}

// GetVideoMetadata uses ffprobe to get video width, height, and duration
func GetVideoMetadata(filePath string) (*VideoMetadata, error) {
	// ffprobe command to get width, height and duration
	cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height:format=duration",
		"-of", "csv=s=x:p=0", filePath)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %v", err)
	}

	// Output format might be multiple lines:
	// 720x1280
	// 15.5
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("unexpected ffprobe output format: %s", string(output))
	}

	var w, h int
	_, err = fmt.Sscanf(lines[0], "%dx%d", &w, &h)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dimensions: %v", err)
	}

	var d float64
	_, err = fmt.Sscanf(lines[1], "%f", &d)
	if err != nil {
		log.Printf("[WRN] Failed to parse duration from %s: %v", lines[1], err)
	}

	return &VideoMetadata{
		Width:    w,
		Height:   h,
		Duration: int(d),
	}, nil
}

// telegramMaxBytes is the Telegram bot file size limit (50 MB)
const telegramMaxBytes = 50 * 1024 * 1024

// audioReserveBitrate is the bitrate (bits/s) reserved for the AAC audio track
const audioReserveBitrate = 128_000

// getVideoDuration uses ffprobe to return video duration in seconds
func getVideoDuration(filePath string) (float64, error) {
	cmd := exec.Command("ffprobe", "-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe duration failed: %v", err)
	}
	val, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0, fmt.Errorf("parse duration: %v", err)
	}
	return val, nil
}

// OptimizeVideo for Telegram (H.264, AAC, FastStart).
// If the output exceeds Telegram's 50 MB limit, it re-encodes with a
// calculated target bitrate to guarantee the file fits within the limit.
// onReencode is called (if non-nil) right before the second pass begins.
func OptimizeVideo(inputPath string, onReencode func()) (string, error) {
	outputPath := inputPath + ".optimized.mp4"

	// First pass: quality-based encoding
	cmd := exec.Command("ffmpeg", "-i", inputPath,
		"-c:v", "libx264", "-preset", "superfast", "-crf", "28",
		"-profile:v", "main", "-level", "3.0", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart",
		"-y", outputPath)

	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("ffmpeg optimization failed: %v, output: %s", err, string(output))
	}

	// Check resulting file size
	info, err := os.Stat(outputPath)
	if err != nil {
		return "", fmt.Errorf("stat optimized file: %v", err)
	}

	if info.Size() <= telegramMaxBytes {
		// File is within the limit — we're done
		return outputPath, nil
	}

	log.Printf("[WRN] Optimized video is %.2f MB, exceeds 50 MB limit. Re-encoding with bitrate cap...",
		float64(info.Size())/(1024*1024))

	// Get duration to calculate the required bitrate
	duration, err := getVideoDuration(outputPath)
	if err != nil || duration <= 0 {
		// Fallback: try to get duration from original file
		duration, err = getVideoDuration(inputPath)
		if err != nil || duration <= 0 {
			return "", fmt.Errorf("cannot determine video duration for bitrate calculation: %v", err)
		}
	}

	// targetTotalBits = 50 MB * 8 bits/byte
	// videoBitrate = (targetTotalBits / duration) - audioReserveBitrate
	// Apply a 2% safety margin to reliably stay under the limit
	const safetyFactor = 0.98
	targetTotalBits := float64(telegramMaxBytes) * 8
	videoBitrateBps := int((targetTotalBits/duration)*safetyFactor) - audioReserveBitrate
	if videoBitrateBps < 100_000 {
		videoBitrateBps = 100_000 // floor: 100 kbps to keep watchable quality
	}
	videoBitrateStr := fmt.Sprintf("%dk", videoBitrateBps/1000)

	log.Printf("[LOG] Re-encoding at video bitrate: %s (duration: %.1fs)", videoBitrateStr, duration)

	// Notify caller that a second pass is starting
	if onReencode != nil {
		onReencode()
	}

	// Remove the oversized first-pass output
	os.Remove(outputPath)

	// Second pass: bitrate-capped encoding
	cmd = exec.Command("ffmpeg", "-i", inputPath,
		"-c:v", "libx264", "-preset", "superfast",
		"-b:v", videoBitrateStr, "-maxrate", videoBitrateStr,
		"-bufsize", fmt.Sprintf("%dk", videoBitrateBps/500), // 2× bitrate buffer
		"-profile:v", "main", "-level", "3.0", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart",
		"-y", outputPath)

	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("ffmpeg bitrate-cap re-encode failed: %v, output: %s", err, string(output))
	}

	// Final size check for logging
	if fi, err := os.Stat(outputPath); err == nil {
		log.Printf("[LOG] Re-encoded video size: %.2f MB", float64(fi.Size())/(1024*1024))
	}

	return outputPath, nil
}
