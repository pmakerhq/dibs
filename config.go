package main

import (
	"encoding/json"
	"fmt"
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

// loadRangeOverrides reads ~/.config/dibs/config.json, if present. A missing
// file yields no overrides and no error; a malformed one yields an error that
// callers on the allocation path deliberately ignore (falling back to the
// generic range) and that `dibs doctor` surfaces.
func loadRangeOverrides() (map[string][2]int, error) {
	cp, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(cp)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", cp, err)
	}
	var cfg rangeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", cp, err)
	}
	return cfg.Ranges, nil
}
