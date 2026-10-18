package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/Fan-chou/magpie-native-routing/internal/config"
	"github.com/Fan-chou/magpie-native-routing/internal/routing"
	"github.com/Fan-chou/magpie-native-routing/internal/testutil"
)

func testModel(t *testing.T, snap routing.Snapshot, actions Actions) (*Model, *routing.Store) {
	t.Helper()
	store := routing.NewStore()
	settings := config.DefaultSettings()
	settings.Language = "en"
	settings.Theme = config.ThemeLight
	paths := config.Paths{
		MagpieDir: "/synthetic/magpie", RoutingDir: "/synthetic/magpie/routing",
		AppConfig: "/synthetic/app", AppCache: "/synthetic/cache",
	}
	model := NewModel(store, settings, paths, paths.MagpieDir, actions)
	model.SetSnapshot(snap)
	return model, store
}

func render(t *testing.T, model *Model, width, height int) *ui.Tester {
	t.Helper()
	tester := ui.NewTester(model.View, width, height)
	if got := tester.Image().Bounds().Dx(); got != width {
		t.Fatalf("rendered width = %d, want %d", got, width)
	}
	return tester
}

func completedSyntheticSnapshot(t *testing.T) routing.Snapshot {
	t.Helper()
	store := routing.NewStore()
	routes := readSyntheticRoutes(t)
	store.ReplaceHistory(routes)
	return store.Snapshot()
}

func readSyntheticRoutes(t *testing.T) []routing.Route {
	t.Helper()
	dayFile := t.TempDir()
	if err := writeTestFile(dayFile, "2026-10-18.jsonl", testutil.SyntheticRouteJSON+"\n"); err != nil {
		t.Fatal(err)
	}
	routes, err := routing.NewHistoryReader(dayFile).ReadDay("2026-10-18")
	if err != nil {
		t.Fatal(err)
	}
	return routes
}

// writeTestFile is kept here instead of exporting a test-only production
// helper that the UI package does not need.
func writeTestFile(dir, name, body string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600)
}

func TestLiveViewShowsObservedRequestAndProvenance(t *testing.T) {
	now := time.Date(2026, 10, 18, 2, 0, 0, 0, time.UTC)
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 44, Agent: "codex", Model: "openai/synthetic-live",
		Provider: "openai", Session: "session-live", Started: now,
		Millis: 1250, Phase: routing.PhaseUpstream, RouteID: 9001,
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	tester := render(t, model, 1240, 820)

	for _, want := range []string{
		"Codex", "openai/synthetic-live", "Live observation",
		"upstream", "Recent requests",
	} {
		if !tester.HasText(want) {
			t.Fatalf("live view lacks %q: %v", want, tester.Texts())
		}
	}
	if tester.HasText("Completed history") {
		t.Fatalf("live-only request was labeled as completed history: %v", tester.Texts())
	}
	if tester.HasText("Actual candidate order") {
		t.Fatalf("live-only request claimed an observed candidate order")
	}
}

func TestCompletedHistoryShowsOrderTriesAndProvenance(t *testing.T) {
	model, _ := testModel(t, completedSyntheticSnapshot(t), Actions{})
	tester := render(t, model, 1240, 900)

	for _, want := range []string{
		"Completed history",
		"Completed history only", "Synthetic Provider", "Backup Provider",
		"Attempt 1", "synthetic failure", "Attempt 2",
	} {
		if !tester.HasText(want) {
			t.Fatalf("history detail lacks %q: %v", want, tester.Texts())
		}
	}
}

func TestHistoryViewLabelsReplayAndHasControls(t *testing.T) {
	snapshot := completedSyntheticSnapshot(t)
	snapshot.Days = []routing.HistoryDay{{Day: "2026-10-18", Requests: 1}}
	model, _ := testModel(t, snapshot, Actions{OpenDay: func(string) {}})
	model.setTab(tabHistory)
	model.SetHistoryDay("2026-10-18")
	tester := render(t, model, 1240, 900)

	for _, want := range []string{
		"Replay", "Play", "Replay uses Route.Time", "Completed history",
		"Completed history only",
	} {
		if !tester.HasText(want) {
			t.Fatalf("history view lacks %q: %v", want, tester.Texts())
		}
	}
	if err := tester.Click("Play"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !tester.HasText("Pause") {
		t.Fatalf("play control did not enter the playing state: %v", tester.Texts())
	}
	tester.Advance(50 * time.Millisecond)
	tester.Frame()
	if !tester.HasText("Pause") {
		t.Fatalf("playback did not remain active before reaching the timeline end: %v", tester.Texts())
	}
	tester.Advance(200 * time.Millisecond)
	tester.Frame()
	if !tester.HasText("Play") {
		t.Fatalf("playback did not stop at the timeline end: %v", tester.Texts())
	}
}

func TestNoSyntheticRuntimeDataInEmptyModel(t *testing.T) {
	model, _ := testModel(t, routing.Snapshot{}, Actions{})
	tester := render(t, model, 1240, 820)
	if !tester.HasText("No requests") {
		t.Fatalf("empty view does not show its empty state: %v", tester.Texts())
	}
	for _, fake := range []string{"Codex", "Claude Code", "synthetic/alpha", "OpenAI"} {
		if tester.HasText(fake) {
			t.Fatalf("empty runtime view invented %q: %v", fake, tester.Texts())
		}
	}
}

func TestSettingsModalExposesGatewayAndReadOnlyData(t *testing.T) {
	model, _ := testModel(t, routing.Snapshot{}, Actions{})
	model.setSettingsOpen(true)
	tester := render(t, model, 1240, 900)
	for _, want := range []string{
		"Connection & data", "Gateway URL", "Magpie data directory",
		"Read-only HTTP requests only", "Save",
	} {
		if !tester.HasText(want) {
			t.Fatalf("settings modal lacks %q: %v", want, tester.Texts())
		}
	}
}

func TestProviderAccountMaskingDoesNotLeakRawAccount(t *testing.T) {
	route := routing.RouteView{
		Route: routing.Route{
			ID: 10, Time: time.Now(), Agent: "codex", Model: "synthetic/model",
			Done: true, Status: 200,
			Order: []routing.Weighed{{
				ID: "acct", Provider: "synthetic", Name: "Synthetic", Who: "user@example.test", Model: "model",
			}},
		},
		CandidateOrderKnown: true,
	}
	model, _ := testModel(t, routing.Snapshot{Routes: []routing.RouteView{route}}, Actions{})
	tester := render(t, model, 1240, 820)
	if !tester.HasText("user@example.test") {
		t.Fatalf("unmasked provider account is not shown from its real trace")
	}
	if err := tester.Click("Hide accounts"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if tester.HasText("user@example.test") {
		t.Fatalf("masked view leaked the provider account: %v", tester.Texts())
	}
}

func TestLiveAndHistoryMergeStaysLabeled(t *testing.T) {
	routes := readSyntheticRoutes(t)
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 1, Agent: "codex", RouteID: routes[0].ID, Started: routes[0].Time,
		Model: routes[0].Model, Provider: routes[0].Provider,
	}})
	store.MergeHistory(routes)
	model, _ := testModel(t, store.Snapshot(), Actions{})
	tester := render(t, model, 1240, 900)
	if !tester.HasText("Live and completed history") {
		t.Fatalf("merged route provenance is not explicit: %v", tester.Texts())
	}
}
