package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsRoundTripAndValidation(t *testing.T) {
	dir := t.TempDir()
	store := NewSettingsStore(dir)
	cfg := DefaultSettings()
	cfg.GatewayURL = "http://127.0.0.1:9999/"
	cfg.APIKey = "test-key"
	cfg.Theme = ThemeDark
	cfg.PollIntervalMS = 50
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.GatewayURL != "http://127.0.0.1:9999" || got.APIKey != "test-key" || got.Theme != ThemeDark || got.PollIntervalMS != 1000 {
		t.Fatalf("settings = %+v", got)
	}
	fi, err := os.Stat(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", fi.Mode().Perm())
	}
}

func TestSettingsRejectUnsafeGateway(t *testing.T) {
	for _, u := range []string{"ftp://x", "http://user:pass@host", "host:3425"} {
		cfg := DefaultSettings()
		cfg.GatewayURL = u
		if err := cfg.Validate(); err == nil {
			t.Errorf("%q accepted", u)
		}
	}
}

func TestDiscoverExplicit(t *testing.T) {
	dir := t.TempDir()
	p, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.MagpieDir != dir || p.Detection != "explicit" || filepath.Base(p.RoutingDir) != "routing" {
		t.Fatalf("paths = %+v", p)
	}
}
