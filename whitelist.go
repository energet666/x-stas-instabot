package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
)

// WhitelistConfig represents the whitelist configuration
type WhitelistConfig struct {
	Users []int64 `json:"users"`
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
	return slices.Contains(whitelist.Users, userID)
}

// SaveWhitelist saves the whitelist configuration to whitelist.json
func SaveWhitelist(whitelist *WhitelistConfig) error {
	data, err := json.MarshalIndent(whitelist, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("whitelist.json", data, 0644)
}

// AddUserToWhitelist adds a user ID to the whitelist
func AddUserToWhitelist(whitelist *WhitelistConfig, userID int64) error {
	// Check if user is already in whitelist
	if slices.Contains(whitelist.Users, userID) {
		return nil // Already in whitelist
	}

	whitelist.Users = append(whitelist.Users, userID)
	return SaveWhitelist(whitelist)
}
