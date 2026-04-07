package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PersistentConfig holds the core connection settings saved to ~/.codient/config.json.
type PersistentConfig struct {
	BaseURL       string `json:"base_url,omitempty"`
	APIKey        string `json:"api_key,omitempty"`
	Model         string `json:"model,omitempty"`
	SearchBaseURL string `json:"search_url,omitempty"`
}

func stateDir() (string, error) {
	if d := strings.TrimSpace(os.Getenv("CODIENT_STATE_DIR")); d != "" {
		return filepath.Abs(d)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codient"), nil
}

func configFilePath() (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// LoadPersistentConfig reads ~/.codient/config.json.
// Returns a zero-value struct (not an error) if the file does not exist.
func LoadPersistentConfig() (*PersistentConfig, error) {
	path, err := configFilePath()
	if err != nil {
		return &PersistentConfig{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &PersistentConfig{}, nil
		}
		return nil, err
	}
	var pc PersistentConfig
	if err := json.Unmarshal(data, &pc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &pc, nil
}

// SavePersistentConfig writes the config atomically to ~/.codient/config.json.
func SavePersistentConfig(pc *PersistentConfig) error {
	dir, err := stateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config state dir: %w", err)
	}
	path, err := configFilePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(pc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
