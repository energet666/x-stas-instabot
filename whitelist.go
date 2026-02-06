package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sync"
)

// WhitelistConfig represents the whitelist configuration
type WhitelistConfig struct {
	Users []int64 `json:"users"`
	mu    sync.RWMutex
}

// LoadWhitelist loads the whitelist configuration from whitelist.json
// If the file doesn't exist, it creates a new empty whitelist file
func LoadWhitelist() (*WhitelistConfig, error) {
	data, err := os.ReadFile("whitelist.json")
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, create a new empty whitelist
			config := &WhitelistConfig{Users: []int64{}}
			if saveErr := SaveWhitelist(config); saveErr != nil {
				return nil, fmt.Errorf("failed to create whitelist file: %w", saveErr)
			}
			return config, nil
		}
		return nil, err
	}

	var config WhitelistConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// IsUserWhitelisted checks if a user ID is in the whitelist
func IsUserWhitelisted(whitelist *WhitelistConfig, userID int64) bool {
	whitelist.mu.RLock()
	defer whitelist.mu.RUnlock()
	return slices.Contains(whitelist.Users, userID)
}

// IsUserWhitelisted checks if a user ID is in the whitelist (method for WhitelistConfig)
func (w *WhitelistConfig) IsUserWhitelisted(userID int64) bool {
	return IsUserWhitelisted(w, userID)
}

// SaveWhitelist saves the whitelist configuration to whitelist.json
// Note: This function doesn't lock because it's called by locked functions or during init.
// However, if called externally, it should be careful.
// To be safe, we might want to lock here too, but usually we prefer locking at the operation level.
// Let's assume SaveWhitelist is mostly internal. But AddUserToWhitelist calls it.
// If AddUserToWhitelist holds the lock, SaveWhitelist shouldn't lock again if it's not recursive.
// sync.RWMutex is NOT recursive.
// So we should NOT lock in SaveWhitelist if it's called from a locked context/method.
// Let's Change AddUserToWhitelist to handle the logic.
func SaveWhitelist(whitelist *WhitelistConfig) error {
	// We need to marshal the data. Accessing w.Users needs a Read Lock technically if we are outside,
	// but usage in AddUserToWhitelist (which holds a Write Lock) makes it safe to read/write.
	// We won't add locking here to avoid deadlocks when called from AddUserToWhitelist.
	data, err := json.MarshalIndent(whitelist, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("whitelist.json", data, 0644)
}

// AddUserToWhitelist adds a user ID to the whitelist
func AddUserToWhitelist(whitelist *WhitelistConfig, userID int64) error {
	whitelist.mu.Lock()
	defer whitelist.mu.Unlock()

	// Check if user is already in whitelist
	if slices.Contains(whitelist.Users, userID) {
		return nil // Already in whitelist
	}

	whitelist.Users = append(whitelist.Users, userID)
	return SaveWhitelist(whitelist)
}

// AddUserToWhitelist adds a user ID to the whitelist (method for WhitelistConfig)
func (w *WhitelistConfig) AddUserToWhitelist(userID int64) error {
	return AddUserToWhitelist(w, userID)
}
