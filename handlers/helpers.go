package handlers

import (
	"os"
)

// Cleanup removes the temporary directory and its contents
func (r *DownloadResult) Cleanup() {
	if r.Dir != "" {
		os.RemoveAll(r.Dir)
	}
}
