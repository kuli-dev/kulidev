// Package settings is the user's saved configuration (the Settings screen), kept in
// the user config directory with owner-only permissions because it can hold an API key.
package settings

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// LLM is the OpenAI-compatible model connection used by Ask and DocsMana.
type LLM struct {
	BaseURL         string `json:"baseURL"`
	Model           string `json:"model"`
	APIKey          string `json:"apiKey,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	MaxTokens       int    `json:"maxTokens,omitempty"`
}

// Settings is everything saved from the Settings screen.
type Settings struct {
	LLM LLM `json:"llm"`
}

// Load reads path; a missing file is empty settings.
func Load(path string) (Settings, error) {
	var s Settings
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

// Save writes path atomically, readable by the owner only.
func Save(path string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var efforts = map[string]bool{"": true, "none": true, "minimal": true, "low": true, "medium": true, "high": true}

// Validate checks a model connection before it is saved or tested.
func (l LLM) Validate() error {
	if l.BaseURL != "" {
		u, err := url.Parse(l.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("the base URL must start with http:// or https://, e.g. http://localhost:11434/v1")
		}
	}
	if (l.BaseURL == "") != (strings.TrimSpace(l.Model) == "") {
		return errors.New("set both the base URL and the model, or neither")
	}
	if !efforts[l.ReasoningEffort] {
		return errors.New("reasoning effort must be none, minimal, low, medium or high")
	}
	if l.MaxTokens < 0 || l.MaxTokens > 200000 {
		return errors.New("max tokens must be between 0 and 200000")
	}
	return nil
}
