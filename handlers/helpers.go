package handlers

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// escapeMarkdown escapes characters for Telegram Markdown (legacy)
func escapeMarkdown(text string) string {
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"`", "\\`",
	)
	return replacer.Replace(text)
}

// Cleanup removes the temporary directory and its contents
func (r *DownloadResult) Cleanup() {
	if r.Dir != "" {
		os.RemoveAll(r.Dir)
	}
}

// MoveFile moves a file from source to destination, handling cross-device links
func MoveFile(sourcePath, destPath string) error {
	// Try atomic rename first
	err := os.Rename(sourcePath, destPath)
	if err == nil {
		return nil
	}

	// If rename failed, try copy and delete
	// We proceed with current error usually being cross-device link, but valid for permission issues potentially addressed by copy if different users etc (unlikely here but still).

	inputFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("couldn't open source file: %w", err)
	}
	defer inputFile.Close()

	outputFile, err := os.Create(destPath)
	if err != nil {
		inputFile.Close()
		return fmt.Errorf("couldn't create dest file: %w", err)
	}
	defer outputFile.Close()

	if _, err = io.Copy(outputFile, inputFile); err != nil {
		outputFile.Close()  // Close before removing partial file
		os.Remove(destPath) // Clean up target on failure
		return fmt.Errorf("writing to output file failed: %w", err)
	}

	// Close files explicitly to ensure flush before removing source
	inputFile.Close()
	if err := outputFile.Close(); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("closing output file failed: %w", err)
	}

	// The copy was successful, so now delete the original file
	if err := os.Remove(sourcePath); err != nil {
		return fmt.Errorf("failed removing original file: %w", err)
	}

	return nil
}
