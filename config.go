package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// config is the small bit of state we persist between runs. Fields are stored
// by name (not index) so reordering ResumeModes/sortModes never corrupts a
// saved preference.
type config struct {
	ResumeMode string `json:"resumeMode"`
	SortMode   string `json:"sortMode"`
}

// configPath returns ~/.config/ccsessions/config.json
func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ccsessions", "config.json"), nil
}

// loadConfig reads the persisted config. A missing/invalid file is not an
// error: it just means "use defaults".
func loadConfig() config {
	var c config
	path, err := configPath()
	if err != nil {
		return c
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(data, &c)
	return c
}

// saveConfig persists the whole config. Errors are ignored: failing to
// remember a preference should never break the UI.
func saveConfig(c config) {
	path, err := configPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// loadResumeModeIndex returns the saved resume mode's index, or 0 (normal).
func loadResumeModeIndex() int {
	name := loadConfig().ResumeMode
	for i, m := range ResumeModes {
		if m.Name == name {
			return i
		}
	}
	return 0
}

// saveResumeModeIndex persists the selected resume mode by name.
func saveResumeModeIndex(i int) {
	if i < 0 || i >= len(ResumeModes) {
		return
	}
	c := loadConfig()
	c.ResumeMode = ResumeModes[i].Name
	saveConfig(c)
}

// loadSortMode returns the saved sort mode's index, or 0 (recent).
func loadSortMode() int {
	name := loadConfig().SortMode
	for i, s := range sortModes {
		if s.Name == name {
			return i
		}
	}
	return 0
}

// saveSortMode persists the selected sort mode by name.
func saveSortMode(i int) {
	if i < 0 || i >= len(sortModes) {
		return
	}
	c := loadConfig()
	c.SortMode = sortModes[i].Name
	saveConfig(c)
}
