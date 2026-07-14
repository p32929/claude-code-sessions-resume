package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// config is the small bit of state we persist between runs.
type config struct {
	ResumeMode string `json:"resumeMode"` // stored by name for stability across reorders
}

// configPath returns ~/.config/ccsessions/config.json
func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ccsessions", "config.json"), nil
}

// loadResumeModeIndex reads the saved mode and returns its index into
// ResumeModes. Missing/invalid config falls back to 0 (normal).
func loadResumeModeIndex() int {
	path, err := configPath()
	if err != nil {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var c config
	if json.Unmarshal(data, &c) != nil {
		return 0
	}
	for i, m := range ResumeModes {
		if m.Name == c.ResumeMode {
			return i
		}
	}
	return 0
}

// saveResumeModeIndex persists the selected mode by name. Errors are ignored:
// failing to remember a preference should never break the UI.
func saveResumeModeIndex(i int) {
	if i < 0 || i >= len(ResumeModes) {
		return
	}
	path, err := configPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(config{ResumeMode: ResumeModes[i].Name}, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}
