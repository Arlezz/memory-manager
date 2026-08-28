// Package config reads memory-manager's own settings.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Arlezz/memory-manager/internal/claudedir"
)

// Config is the user's memory-manager settings.
type Config struct {
	// PersonalRepo is the git URL of the private personal memory repository.
	// Empty means the personal layer is disabled and only project memory syncs.
	PersonalRepo string `json:"personal_repo"`
	// PersonalBranch defaults to the remote's default branch when empty.
	PersonalBranch string `json:"personal_branch,omitempty"`
}

// File returns the path of the config file.
func File() (string, error) {
	root, err := claudedir.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "memory-manager", "config.json"), nil
}

// PersonalClonePath returns the local clone of the personal memory repository.
func PersonalClonePath() (string, error) {
	root, err := claudedir.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "memory-manager", "personal"), nil
}

// ErrNotConfigured reports a missing config file.
var ErrNotConfigured = errors.New("memory-manager is not configured")

// Load reads the config. A missing file yields ErrNotConfigured wrapped with
// the expected path, so the message tells the user what to create.
func Load() (Config, error) {
	var c Config
	p, err := File()
	if err != nil {
		return c, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return c, fmt.Errorf("%w: create %s", ErrNotConfigured, p)
		}
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("config %s is not valid JSON: %w", p, err)
	}
	c.PersonalRepo = strings.TrimSpace(c.PersonalRepo)
	return c, nil
}

// Save writes the config, creating its directory.
func Save(c Config) error {
	p, err := File()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o600)
}
