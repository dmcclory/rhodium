package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	Repos []string `json:"repos"`
}

func loadConfig() (*Config, error) {
	path := os.Getenv("RHODIUM_CONFIG")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".config", "rhodium", "config.json")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no config at %s — create one with {\"repos\": [\"owner/name\"]}", path)
		}
		return nil, err
	}

	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(c.Repos) == 0 {
		return nil, fmt.Errorf("config at %s has no repos", path)
	}
	return &c, nil
}
