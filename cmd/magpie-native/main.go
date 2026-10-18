// Command magpie-native is an independent, read-only native routing client
// for a running Magpie Desktop Gateway.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/egoist/mygo"
	mygui "github.com/egoist/mygo/ui"

	"github.com/Fan-chou/magpie-native-routing/internal/config"
	"github.com/Fan-chou/magpie-native-routing/internal/magpie"
	"github.com/Fan-chou/magpie-native-routing/internal/routing"
	"github.com/Fan-chou/magpie-native-routing/internal/ui"
)

const (
	appTitle        = "Magpie Native Routing"
	defaultWindowW  = 1240
	defaultWindowH  = 820
	minimumWindowW  = 880
	minimumWindowH  = 620
	diagnosticLimit = 500
)

type options struct {
	gateway  string
	dataDir  string
	apiKey   string
	theme    string
	lang     string
	diagnose bool
	jsonOut  bool
}

type application struct {
	settings config.AppSettings
	paths    config.Paths
	loc      ui.Localization

	store      *routing.Store
	client     *magpie.Client
	supervisor *routing.Supervisor
	watcher    *routing.HistoryWatcher
	model      *ui.Model
	win        *mygo.Window

	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	stopped bool

	historyReader *routing.HistoryReader
	configReader  routing.ConfigReader
}

func main() {
	opts := parseFlags()
	cfg, paths, err := loadConfiguration(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, sanitize(err.Error()))
		os.Exit(1)
	}
	if opts.diagnose {
		os.Exit(runDiagnostics(cfg, paths, opts))
	}
	if err := runGUI(cfg, paths, opts); err != nil {
		fmt.Fprintln(os.Stderr, sanitize(err.Error()))
		os.Exit(1)
	}
}

func parseFlags() options {
	var o options
	flag.StringVar(&o.gateway, "gateway", "", "Magpie Gateway base URL (default: settings or http://127.0.0.1:3425)")
	flag.StringVar(&o.dataDir, "data-dir", "", "Magpie data directory (default: auto-detect)")
	flag.StringVar(&o.apiKey, "api-key", "", "Gateway API key (prefer the settings dialog or MAGPIE_API_KEY)")
	flag.StringVar(&o.theme, "theme", "", "appearance: system, light or dark")
	flag.StringVar(&o.lang, "lang", "", "interface language: zh or en")
	flag.BoolVar(&o.diagnose, "diagnose", false, "print a read-only connection and history report, then exit")
	flag.BoolVar(&o.jsonOut, "json", false, "with -diagnose, print JSON")
	flag.Parse()
	return o
}

func loadConfiguration(opts options) (config.AppSettings, config.Paths, error) {
	// Locate the app's own settings directory before reading Magpie's data.
	// Discover returns the app paths even when Magpie has not created its
	// folder yet; loading settings can then override the auto-detected one.
	base, err := config.Discover("")
	if err != nil {
		return config.AppSettings{}, config.Paths{}, err
	}
	if err := config.EnsureAppDirs(base); err != nil {
		return config.AppSettings{}, config.Paths{}, err
	}
	settingsStore := config.NewSettingsStore(base.AppConfig)
	cfg, err := settingsStore.Load()
	if err != nil {
		return config.AppSettings{}, config.Paths{}, err
	}
	if v := strings.TrimSpace(opts.gateway); v != "" {
		cfg.GatewayURL = v
	}
	if v := strings.TrimSpace(opts.dataDir); v != "" {
		cfg.MagpieDir = v
	}
	if v := strings.TrimSpace(os.Getenv("MAGPIE_API_KEY")); v != "" {
		cfg.APIKey = v
	}
	if v := strings.TrimSpace(opts.apiKey); v != "" {
		cfg.APIKey = v
	}
	if v := strings.TrimSpace(opts.theme); v != "" {
		cfg.Theme = config.ThemeMode(strings.ToLower(v))
	}
	if v := strings.TrimSpace(opts.lang); v != "" {
		cfg.Language = strings.ToLower(v)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, base, err
	}
	if strings.TrimSpace(os.Getenv("MAGPIE_API_KEY")) != "" {
		// An environment-injected key is applied for this process only. Do
		// not persist it just because the user launched the app.
		cfg.APIKey = strings.TrimSpace(os.Getenv("MAGPIE_API_KEY"))
	}
	if strings.TrimSpace(opts.apiKey) != "" {
		cfg.APIKey = strings.TrimSpace(opts.apiKey)
	}
	paths, err := config.Discover(cfg.MagpieDir)
	if err != nil {
		return cfg, base, err
	}
	return cfg, paths, nil
}

func runGUI(cfg config.AppSettings, paths config.Paths, opts options) error {
	client, err := magpie.NewClient(magpie.Options{
		BaseURL: cfg.GatewayURL,
		APIKey:  cfg.APIKey,
		Timeout: 8 * time.Second,
	})
	if err != nil {
		return err
	}
	app := &application{
		settings: cfg, paths: paths, loc: ui.NewLocalization(cfg.Language),
		store:         routing.NewStore(),
		client:        client,
		historyReader: routing.NewHistoryReader(paths.RoutingDir),
		configReader:  routing.NewConfigReader(paths.Providers),
	}
	app.model = ui.NewModel(app.store, cfg, paths, paths.MagpieDir, ui.Actions{
		Refresh:        app.refresh,
		OpenDay:        app.openDay,
		RefreshHistory: app.refreshHistory,
		ReloadDay: func(day string) {
			_ = day
			app.refreshHistory()
		},
		SaveSettings:  app.saveSettings,
		Restart:       app.restartWorkers,
		ChooseDataDir: app.chooseDataDir,
		ApplyTheme: func(mode config.ThemeMode) {
			app.model.SetSettings(app.modelSettingsWithTheme(mode))
		},
	})

	app.store.OnChange(func() {
		app.publishSnapshot()
	})
	app.startWorkers(context.Background())
	app.publishSnapshot()

	mygo.App.WhenReady(func() {
		app.win = mygo.NewWindow(mygo.WindowOptions{
			Title:                appTitle,
			Width:                defaultWindowW,
			Height:               defaultWindowH,
			MinWidth:             minimumWindowW,
			MinHeight:            minimumWindowH,
			TitleBarStyle:        mygo.TitleBarHiddenInset,
			TrafficLightPosition: &mygo.Point{X: 14, Y: 12},
			StateKey:             "magpie-native-routing",
			BackgroundColor:      "#f2f2f5",
			Content:              mygui.View(app.model.View),
		})
		app.publishSnapshot()
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		app.shutdown()
	})
	mygo.App.OnWindowAllClosed(func() {
		mygo.App.Quit()
	})

	sig := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sig:
			mygo.App.Quit()
		case <-done:
		}
	}()
	err = mygo.App.Run()
	app.shutdown()
	close(done)
	signal.Stop(sig)
	return err
}

func (a *application) modelSettingsWithTheme(mode config.ThemeMode) config.AppSettings {
	cfg := a.model.Settings()
	cfg.Theme = mode
	return cfg
}

func (a *application) startWorkers(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		cancel()
		return
	}
	a.ctx, a.cancel = ctx, cancel
	client := a.client
	historyReader := a.historyReader
	configReader := a.configReader
	settings := a.settings
	a.mu.Unlock()

	supervisor := routing.NewSupervisor(client, a.store,
		time.Duration(settings.PollIntervalMS)*time.Millisecond,
		time.Now().Local().Format("2006-01-02"), settings.HistoryDayLimit)
	supervisor.ConfigureProviders(&configReader)

	watcher := routing.NewHistoryWatcher(historyReader, a.store,
		settings.HistoryDayLimit,
		time.Duration(settings.HistoryTailMS)*time.Millisecond)

	a.mu.Lock()
	a.supervisor, a.watcher = supervisor, watcher
	a.mu.Unlock()

	// The watcher owns completed history; the supervisor owns network
	// polling. Both publish through the same Route ID keyed store.
	supervisor.Start(ctx)
	watcher.Start(ctx)
}

func (a *application) refresh() {
	a.mu.Lock()
	supervisor := a.supervisor
	a.mu.Unlock()
	if supervisor != nil {
		supervisor.Refresh()
	}
}

func (a *application) refreshHistory() {
	a.mu.Lock()
	watcher := a.watcher
	a.mu.Unlock()
	if watcher != nil {
		watcher.Refresh()
	}
}

func (a *application) openDay(day string) {
	a.model.SetHistoryDay(day)
	a.mu.Lock()
	watcher := a.watcher
	a.mu.Unlock()
	if watcher != nil {
		watcher.SetSelectedDay(day)
	}
}

// publishSnapshot copies the store on the worker's goroutine and delivers the
// immutable copy to the UI thread. It intentionally does not read the store
// from a draw callback, and it coalesces naturally because every callback
// posts one Update.
func (a *application) publishSnapshot() {
	snapshot := a.store.Snapshot()
	a.mu.Lock()
	win := a.win
	a.mu.Unlock()
	if win == nil {
		a.model.SetSnapshot(snapshot)
		return
	}
	if win.IsDestroyed() {
		return
	}
	win.Update(func() {
		a.model.SetSnapshot(snapshot)
	})
}

func (a *application) chooseDataDir() {
	dirs, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      a.win,
		Title:       a.loc.T("settings.dataDir"),
		ButtonLabel: a.loc.T("app.save"),
		Directory:   true,
	})
	if err != nil || len(dirs) == 0 {
		return
	}
	a.model.SetDraftDataDir(dirs[0])
}

func (a *application) saveSettings(cfg config.AppSettings) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	a.mu.Lock()
	old := a.settings
	oldPaths := a.paths
	a.mu.Unlock()
	var newPaths config.Paths
	if cfg.MagpieDir != old.MagpieDir {
		paths, err := config.Discover(cfg.MagpieDir)
		if err != nil {
			return err
		}
		newPaths = paths
	}
	if err := config.NewSettingsStore(oldPaths.AppConfig).Save(cfg); err != nil {
		return err
	}
	a.mu.Lock()
	a.settings = cfg
	a.loc = ui.NewLocalization(cfg.Language)
	a.mu.Unlock()
	a.model.SetSettings(cfg)
	// Connection changes take effect immediately. A data directory change
	// requires re-reading; restart only the worker context, not the UI.
	if old.GatewayURL != cfg.GatewayURL || old.APIKey != cfg.APIKey ||
		old.PollIntervalMS != cfg.PollIntervalMS || old.HistoryTailMS != cfg.HistoryTailMS ||
		old.HistoryDayLimit != cfg.HistoryDayLimit {
		if client, err := magpie.NewClient(magpie.Options{
			BaseURL: cfg.GatewayURL, APIKey: cfg.APIKey, Timeout: 8 * time.Second,
		}); err == nil {
			a.mu.Lock()
			a.client = client
			a.mu.Unlock()
			a.restartWorkers()
		}
	}
	if old.MagpieDir != cfg.MagpieDir {
		a.mu.Lock()
		a.paths = newPaths
		a.historyReader = routing.NewHistoryReader(newPaths.RoutingDir)
		a.configReader = routing.NewConfigReader(newPaths.Providers)
		a.mu.Unlock()
		a.model.SetPaths(newPaths, newPaths.MagpieDir)
		a.restartWorkers()
	}
	return nil
}

func (a *application) restartWorkers() {
	a.mu.Lock()
	oldCancel := a.cancel
	a.cancel = nil
	supervisor, watcher := a.supervisor, a.watcher
	a.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
	}
	if watcher != nil {
		watcher.Stop()
	}
	if supervisor != nil {
		supervisor.Stop()
	}
	a.startWorkers(context.Background())
}

func (a *application) shutdown() {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return
	}
	a.stopped = true
	cancel := a.cancel
	watcher, supervisor := a.watcher, a.supervisor
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if watcher != nil {
		watcher.Stop()
	}
	if supervisor != nil {
		supervisor.Stop()
	}
}

func runDiagnostics(cfg config.AppSettings, paths config.Paths, opts options) int {
	report := map[string]any{
		"readOnly":   true,
		"magpieDir":  paths.MagpieDir,
		"routingDir": paths.RoutingDir,
		"appConfig":  paths.AppConfig,
		"gatewayURL": cfg.GatewayURL,
		"detection":  paths.Detection,
		"hasAPIKey":  cfg.APIKey != "",
		"checkedAt":  time.Now().UTC().Format(time.RFC3339),
	}
	client, err := magpie.NewClient(magpie.Options{
		BaseURL: cfg.GatewayURL, APIKey: cfg.APIKey, Timeout: 5 * time.Second,
	})
	if err != nil {
		report["gatewayError"] = sanitize(err.Error())
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		concurrency, err := client.Concurrency(ctx)
		if err != nil {
			report["gatewayError"] = sanitize(err.Error())
		} else {
			report["gatewayOnline"] = true
			report["activeRequests"] = len(concurrency.Active)
			report["waiting"] = concurrency.Waiting
			report["requests"] = concurrency.Requests
		}
	}
	reader := routing.NewHistoryReader(paths.RoutingDir)
	days, err := reader.Days()
	if err != nil {
		report["historyError"] = sanitize(err.Error())
	} else {
		report["historyDays"] = len(days)
		if len(days) > 0 {
			report["latestDay"] = days[0].Day
			if rs, err := reader.ReadDay(days[0].Day); err == nil {
				report["latestDayRoutes"] = len(rs)
			} else {
				report["historyError"] = sanitize(err.Error())
			}
		}
	}
	if _, err := os.Stat(paths.Providers); err != nil {
		report["providersError"] = sanitize(err.Error())
	} else {
		report["providersReadable"] = true
	}
	if opts.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		fmt.Fprintf(os.Stdout, "Magpie Native Routing diagnostics (read-only)\n")
		for _, key := range []string{"gatewayURL", "gatewayOnline", "activeRequests", "waiting", "requests",
			"gatewayError", "magpieDir", "routingDir", "historyDays", "latestDay", "latestDayRoutes",
			"historyError", "providersReadable", "providersError", "detection", "hasAPIKey"} {
			if v, ok := report[key]; ok {
				fmt.Fprintf(os.Stdout, "%-18s %v\n", key, v)
			}
		}
	}
	if _, ok := report["gatewayError"]; ok {
		return 1
	}
	return 0
}

func sanitize(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	if len(s) > diagnosticLimit {
		s = s[:diagnosticLimit]
	}
	return s
}
