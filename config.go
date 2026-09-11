package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// rangeConfig is the on-disk shape of the user's port range overrides.
// "generic" overrides the fallback range used for unknown services.
type rangeConfig struct {
	Ranges map[string][2]int `json:"ranges"`
}

func configPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "dibs", "config.json"), nil
}

// loadRangeOverrides reads ~/.config/dibs/config.json, if present. A
// missing or unreadable file just means no overrides — not an error.
func loadRangeOverrides() map[string][2]int {
	cp, err := configPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(cp)
	if err != nil {
		return nil
	}
	var cfg rangeConfig
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	return cfg.Ranges
}
