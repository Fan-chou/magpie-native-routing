// Package config discovers Magpie's read-only data and keeps this application's
// own settings separate from Magpie's files.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// AppName is the directory name used for this client's own settings.
const AppName = "magpie-native-routing"

// Paths is the result of looking for Magpie and this app's directories.
type Paths struct {
	MagpieDir  string
	RoutingDir string
	Providers  string
	Settings   string
	AppConfig  string
	AppCache   string
	Detection  string
}

// Discover finds Magpie's configuration directory and this app's own
// directories. explicitMagpie wins. Auto-detection considers a portable data
// folder beside Magpie.app, XDG_CONFIG_HOME, and ~/.config/magpie.
func Discover(explicitMagpie string) (Paths, error) {
	magpieDir, how, err := findMagpieDir(explicitMagpie)
	if err != nil {
		return Paths{}, err
	}
	appConfig, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, fmt.Errorf("find app config dir: %w", err)
	}
	appCache, err := os.UserCacheDir()
	if err != nil {
		return Paths{}, fmt.Errorf("find app cache dir: %w", err)
	}
	appConfig = filepath.Join(appConfig, AppName)
	appCache = filepath.Join(appCache, AppName)
	return Paths{
		MagpieDir:  magpieDir,
		RoutingDir: filepath.Join(magpieDir, "routing"),
		Providers:  filepath.Join(magpieDir, "providers.json"),
		Settings:   filepath.Join(magpieDir, "settings.json"),
		AppConfig:  appConfig,
		AppCache:   appCache,
		Detection:  how,
	}, nil
}

func findMagpieDir(explicit string) (string, string, error) {
	if strings.TrimSpace(explicit) != "" {
		p, err := normalizeDir(explicit)
		if err != nil {
			return "", "explicit", err
		}
		if err := requireReadableDir(p); err != nil {
			return "", "explicit", fmt.Errorf("Magpie data directory: %w", err)
		}
		return p, "explicit", nil
	}

	var candidates []struct{ dir, how string }
	if p := xdgMagpieDir(); p != "" {
		candidates = append(candidates, struct{ dir, how string }{p, "XDG_CONFIG_HOME"})
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, struct{ dir, how string }{filepath.Join(home, ".config", "magpie"), "home"})
	}
	for _, c := range portableMagpieDirs() {
		candidates = append(candidates, struct{ dir, how string }{c, "portable"})
	}
	for _, c := range candidates {
		if err := requireReadableDir(c.dir); err == nil {
			return c.dir, c.how, nil
		}
	}
	// Return a useful default even when Magpie has not created the folder yet.
	if len(candidates) > 0 {
		return candidates[0].dir, "default-missing", nil
	}
	return "", "missing", errors.New("cannot determine Magpie data directory")
}

func xdgMagpieDir() string {
	x := os.Getenv("XDG_CONFIG_HOME")
	if x == "" || !filepath.IsAbs(x) {
		return ""
	}
	return filepath.Join(x, "magpie")
}

// portableMagpieDirs mirrors Magpie's documented candidates conservatively:
// data beside the executable, and on macOS data beside Magpie.app. We never
// infer a portable directory merely because a generic 'data' folder exists.
func portableMagpieDirs() []string {
	var out []string
	add := func(root string) {
		if root == "" {
			return
		}
		p := filepath.Join(root, "data")
		if hasMagpieData(p) {
			out = append(out, p)
		}
	}
	if exe, err := os.Executable(); err == nil {
		add(filepath.Dir(exe))
	}
	if runtime.GOOS == "darwin" {
		app := "/Applications/Magpie.app"
		if _, err := os.Stat(app); err == nil {
			add(filepath.Dir(app))
		}
	}
	return out
}

func hasMagpieData(dir string) bool {
	for _, name := range []string{"providers.json", "settings.json", "logins.json"} {
		if fi, err := os.Stat(filepath.Join(dir, name)); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
}

func normalizeDir(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("empty path")
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~/"))
	} else if strings.HasPrefix(p, "~") {
		return "", errors.New("only ~ and ~/ paths are supported")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func requireReadableDir(p string) error {
	fi, err := os.Stat(p)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", p)
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	return f.Close()
}

// EnsureAppDirs creates only this app's own configuration/cache directories.
func EnsureAppDirs(p Paths) error {
	for _, d := range []string{p.AppConfig, p.AppCache} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}
