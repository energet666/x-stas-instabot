package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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

// OptimizeVideo for Telegram (H.264, AAC, FastStart)
func OptimizeVideo(inputPath string) (string, error) {
	outputPath := inputPath + ".optimized.mp4"

	cmd := exec.Command("ffmpeg", "-i", inputPath,
		"-c:v", "libx264", "-preset", "superfast", "-crf", "28",
		"-profile:v", "main", "-level", "3.0", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-movflags", "+faststart",
		"-y", outputPath)

	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("ffmpeg optimization failed: %v, output: %s", err, string(output))
	}

	return outputPath, nil
}
