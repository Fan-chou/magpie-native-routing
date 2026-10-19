package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ThemeMode is this app's appearance preference.
type ThemeMode string

const (
	ThemeSystem ThemeMode = "system"
	ThemeLight  ThemeMode = "light"
	ThemeDark   ThemeMode = "dark"
)

// AppSettings is stored only in this application's own config directory.
// APIKey is never logged. Callers should avoid persisting it unless the user
// explicitly chooses to.
type AppSettings struct {
	GatewayURL      string    `json:"gatewayURL"`
	MagpieDir       string    `json:"magpieDir,omitempty"`
	APIKey          string    `json:"apiKey,omitempty"`
	Theme           ThemeMode `json:"theme"`
	Language        string    `json:"language,omitempty"`
	PollIntervalMS  int       `json:"pollIntervalMS"`
	HistoryTailMS   int       `json:"historyTailMS"`
	ShowHistory     bool      `json:"showHistory"`
	HideAccounts    bool      `json:"hideAccounts,omitempty"`
	HistoryDayLimit int       `json:"historyDayLimit"`
	OnboardingDone  bool      `json:"onboardingDone,omitempty"`
}

// DefaultSettings returns conservative local defaults.
func DefaultSettings() AppSettings {
	return AppSettings{
		GatewayURL:      "http://127.0.0.1:3425",
		Theme:           ThemeSystem,
		Language:        "zh",
		PollIntervalMS:  1000,
		HistoryTailMS:   1500,
		ShowHistory:     true,
		HistoryDayLimit: 7,
	}
}

// Validate normalizes settings and rejects unsafe gateway schemes/URLs.
func (s *AppSettings) Validate() error {
	s.GatewayURL = strings.TrimRight(strings.TrimSpace(s.GatewayURL), "/")
	if s.GatewayURL == "" {
		s.GatewayURL = DefaultSettings().GatewayURL
	}
	u, err := url.Parse(s.GatewayURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("gateway URL must be an absolute http:// or https:// URL")
	}
	if u.User != nil {
		return errors.New("gateway URL must not contain credentials")
	}
	if strings.ContainsAny(u.Path, "\r\n") || strings.ContainsAny(s.APIKey, "\r\n") {
		return errors.New("gateway settings contain invalid characters")
	}
	if s.Theme != ThemeSystem && s.Theme != ThemeLight && s.Theme != ThemeDark {
		s.Theme = ThemeSystem
	}
	if s.PollIntervalMS < 250 {
		s.PollIntervalMS = 1000
	}
	if s.PollIntervalMS > 60_000 {
		s.PollIntervalMS = 60_000
	}
	if s.HistoryTailMS < 250 {
		s.HistoryTailMS = 1500
	}
	if s.HistoryTailMS > 60_000 {
		s.HistoryTailMS = 60_000
	}
	if s.HistoryDayLimit < 1 {
		s.HistoryDayLimit = 7
	}
	if s.HistoryDayLimit > 30 {
		s.HistoryDayLimit = 30
	}
	return nil
}

// SettingsStore reads and atomically writes this app's settings.
type SettingsStore struct {
	Path string
}

func NewSettingsStore(appConfigDir string) SettingsStore {
	return SettingsStore{Path: filepath.Join(appConfigDir, "settings.json")}
}

// Load returns defaults when no settings file exists. A malformed file is an
// error rather than being silently overwritten.
func (s SettingsStore) Load() (AppSettings, error) {
	cfg := DefaultSettings()
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return DefaultSettings(), fmt.Errorf("decode %s: %w", s.Path, err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Save writes settings atomically with owner-only permissions.
func (s SettingsStore) Save(cfg AppSettings) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := fmt.Sprintf("%s.tmp-%d", s.Path, time.Now().UnixNano())
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.Path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// PersistedSettings returns what a Save should write for in. Launch overrides
// (MAGPIE_API_KEY, -api-key, -gateway, -data-dir, -theme, -lang) apply to the
// running process only: when a field still equals its launch override, the
// value read from disk is kept, so an unrelated save never persists it. A
// field the user changed away from the override is written as edited.
// persisted is the settings as loaded from disk, launch the effective
// settings after overrides.
func PersistedSettings(in, persisted, launch AppSettings) AppSettings {
	out := in
	if in.GatewayURL == launch.GatewayURL {
		out.GatewayURL = persisted.GatewayURL
	}
	if in.MagpieDir == launch.MagpieDir {
		out.MagpieDir = persisted.MagpieDir
	}
	if in.APIKey == launch.APIKey {
		out.APIKey = persisted.APIKey
	}
	if in.Theme == launch.Theme {
		out.Theme = persisted.Theme
	}
	if in.Language == launch.Language {
		out.Language = persisted.Language
	}
	return out
}
