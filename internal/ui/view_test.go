package ui

import (
	"bytes"
	"encoding/base64"
	"image"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"weak"

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
		"Codex", "openai/synthetic-live", "Live-only observations omit",
		"Recent requests",
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

func TestThemeUsesMagpieDesktopPalette(t *testing.T) {
	dark := colors(nil, config.ThemeDark)
	if dark.Canvas != ui.Hex("#1a1a1e") || dark.Panel != ui.Hex("#232327") ||
		dark.Accent != ui.Hex("#a5b4fc") || dark.Line != ui.Hex("#333339") {
		t.Fatalf("dark palette drifted from app.css: %+v", dark)
	}
	light := colors(nil, config.ThemeLight)
	if light.Canvas != ui.Hex("#f2f2f5") || light.Panel != ui.Hex("#ffffff") ||
		light.Accent != ui.Hex("#4f46e5") || light.Line != ui.Hex("#e3e3e8") {
		t.Fatalf("light palette drifted from app.css: %+v", light)
	}
}

func TestAgentHueMatchesOriginalRoutingColors(t *testing.T) {
	cases := map[string]ui.Color{
		"codex":    ui.Hex("#6366f1"),
		"claude":   ui.Hex("#d97757"),
		"gemini":   ui.Hex("#0ea5e9"),
		"copilot":  ui.Hex("#a855f7"),
		"opencode": ui.Hex("#eab308"),
	}
	for id, want := range cases {
		if got := agentHue(id); got != want {
			t.Errorf("agentHue(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestFlightTracksOnlyRealLiveRoutesAndKeepsStableIdentity(t *testing.T) {
	now := time.Date(2026, 10, 18, 2, 0, 0, 0, time.UTC)
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{
		{ID: 1, Agent: "codex", Provider: "p1", RouteID: 501, Started: now, Millis: 2000},
		{ID: 2, Agent: "claude", Provider: "p2", RouteID: 502, Started: now, Millis: 2500},
		{ID: 3, Agent: "opencode", Provider: "p3", RouteID: 503, Started: now, Millis: 1500},
	})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	model.syncFlightTracks(store.Snapshot(), store.Snapshot().Routes, now)
	model.mu.RLock()
	got := append([]flightTrack(nil), model.topology.flights...)
	model.mu.RUnlock()
	if len(got) != 3 {
		t.Fatalf("flight tracks = %d, want 3 real live routes", len(got))
	}
	firstSeen := got[0].firstSeen
	model.syncFlightTracks(store.Snapshot(), store.Snapshot().Routes, now.Add(200*time.Millisecond))
	model.mu.RLock()
	if model.topology.flights[0].firstSeen != firstSeen {
		t.Fatal("a repeated concurrency poll restarted an existing flight")
	}
	model.mu.RUnlock()
}

func TestCompletedRouteDoesNotAnimateAsLive(t *testing.T) {
	model, _ := testModel(t, completedSyntheticSnapshot(t), Actions{})
	model.syncFlightTracks(model.snapshot(), model.snapshot().Routes, time.Now())
	model.mu.RLock()
	defer model.mu.RUnlock()
	if len(model.topology.flights) != 0 {
		t.Fatalf("completed-only history animated as live: %+v", model.topology.flights)
	}
}

func TestIdleViewDoesNotRequestContinuousFrames(t *testing.T) {
	model, _ := testModel(t, routing.Snapshot{
		GatewayOnline: true, LastPoll: time.Now(),
	}, Actions{})
	tester := render(t, model, 1240, 820)
	tester.Frame()
	if tester.FramePending() {
		t.Fatal("idle Routing page requested another frame without an in-flight request")
	}
}

func TestRequestLayoutFollowsUpstreamBreakpoints(t *testing.T) {
	narrow := requestSectionLayout(600)
	if narrow.twoColumn || narrow.metaUnder {
		t.Fatalf("600px layout = %+v, want one column without meta-under", narrow)
	}
	wide := requestSectionLayout(1240)
	if !wide.twoColumn || !wide.metaUnder {
		t.Fatalf("1240px layout = %+v, want accounts beside a meta-under list", wide)
	}
	if math.Abs(float64(wide.accountWidth-336)) > 0.1 {
		t.Fatalf("account column = %.1f, want 28%% of the page", wide.accountWidth)
	}
}

func TestAccountTallyUsesCurrentPlanAndAllowance(t *testing.T) {
	start := time.Date(2026, 10, 19, 12, 10, 0, 0, time.UTC)
	routes := []routing.RouteView{
		{Route: routing.Route{
			ID: 1, Time: start, Provider: "qoder",
			Order: []routing.Weighed{{
				ID: "qoder", Provider: "qoder", Name: "Qoder", Who: "first@example.test",
				Kind: "account", Plan: "Pro", Model: "old-model", Known: true, Used: 20,
			}},
		}},
		{Route: routing.Route{
			ID: 2, Time: start.Add(time.Minute), Provider: "qoder",
			Order: []routing.Weighed{{
				ID: "qoder", Provider: "qoder", Name: "Qoder", Who: "second@example.test",
				Kind: "account", Plan: "Teams", Model: "new-model", Known: true, Used: 29,
			}},
		}},
	}
	rows := accountTally(routes)
	if len(rows) != 1 {
		t.Fatalf("account tally = %+v, want one", rows)
	}
	row := rows[0]
	if row.Who != "second@example.test" || row.Plan != "Teams" || row.Model != "new-model" {
		t.Fatalf("latest identity = %+v, want the newest seat", row)
	}
	if !row.Known || math.Abs(row.Used-29) > 0.001 || !row.ObservedAt.Equal(start.Add(time.Minute)) {
		t.Fatalf("latest allowance = %+v, want 29%% at %v", row, start.Add(time.Minute))
	}
}

func TestLiveFlightRequestsAnimationFrames(t *testing.T) {
	now := time.Now()
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 7, Agent: "codex", Provider: "qoder", RouteID: 1701,
		Started: now, Millis: 600,
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	tester := render(t, model, 1240, 820)
	tester.Frame()
	before := tester.Image()
	tester.Advance(routingAnimationInterval)
	if bytes.Equal(before.Pix, tester.Image().Pix) {
		t.Fatal("the in-flight bird did not move on its retained-tree repaint")
	}
}

func TestPausedReplayWithRealTryDoesNotRequestAnimationFrames(t *testing.T) {
	start := time.Date(2026, 10, 19, 5, 0, 0, 0, time.UTC)
	route := routing.Route{
		ID: 94, Time: start, Agent: "codex", Model: "qoder/model",
		Provider: "qoder", Done: true, Status: 200, Millis: 2000,
		HistoryDay: start.Format("2006-01-02"),
		Tries: []routing.Try{{
			ID: "qoder", Model: "qoder/model", Start: start,
			Millis: 2000, Status: 200, Done: true,
		}},
	}
	store := routing.NewStore()
	store.ReplaceHistory([]routing.Route{route})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	model.setTab(tabHistory)
	model.SetHistoryDay(start.Format("2006-01-02"))
	tester := render(t, model, 1240, 900)
	tester.Frame()
	if err := tester.Click("Play"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if err := tester.Click("Pause"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	model.mu.RLock()
	playing := model.replay.playing
	flightCount := len(model.topology.flights)
	model.mu.RUnlock()
	if playing {
		t.Fatal("Pause left the replay clock running")
	}
	if flightCount == 0 {
		t.Fatal("test did not construct a real replay flight")
	}
	tester.Frame()
	if tester.FramePending() {
		t.Fatal("paused replay with a real Try kept requesting animation frames")
	}
}

func TestReducedMotionStopsFlightFrames(t *testing.T) {
	now := time.Now()
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 8, Agent: "codex", Provider: "qoder", RouteID: 1702,
		Started: now, Millis: 600,
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	tester := render(t, model, 1240, 820)
	tester.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tester.Frame()
	tester.Frame()
	if tester.FramePending() {
		t.Fatal("ReduceMotion left the flight frame loop running")
	}
}

func TestFlightFramesReuseStageGeometry(t *testing.T) {
	now := time.Now()
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 9, Agent: "codex", Provider: "qoder", RouteID: 1703,
		Started: now, Millis: 600,
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	tester := render(t, model, 1240, 820)
	model.mu.RLock()
	geometry := model.topology.flightGeometry
	model.mu.RUnlock()
	if geometry == nil {
		t.Fatal("flight geometry was not created")
	}
	tester.Advance(16 * time.Millisecond)
	tester.Frame()
	model.mu.RLock()
	sameGeometry := model.topology.flightGeometry == geometry
	model.mu.RUnlock()
	if !sameGeometry {
		t.Fatal("a bird-only frame rebuilt the stage geometry")
	}
}

func TestFlightFramesReuseStaticStageContent(t *testing.T) {
	now := time.Now()
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 20, Agent: "codex", Provider: "qoder", RouteID: 1711,
		Started: now, Millis: 600,
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	tester := render(t, model, 1240, 820)
	tester.Frame()
	for i := 0; i < 8; i++ {
		tester.Advance(routingAnimationInterval)
	}
	model.mu.RLock()
	geometry := model.topology.stageGeometry
	model.mu.RUnlock()
	if geometry == nil {
		t.Fatal("bird-only frames discarded the stage geometry")
	}
}

func TestFlightPacingUsesTravelRateOnlyWhileTraveling(t *testing.T) {
	now := time.Now()
	phase := func(kind flightPhaseKind) flightTrack {
		return flightTrack{
			inReplay: true, replayPlay: true, firstSeen: now,
			phases: []flightPhase{{kind: kind, start: 0, end: time.Second}},
		}
	}
	for _, kind := range []flightPhaseKind{flightOutbound, flightRetryOut, flightRetryBack, flightInbound} {
		if !flightTraveling(phase(kind), now.Add(100*time.Millisecond)) {
			t.Errorf("phase %d did not use the travel cadence", kind)
		}
	}
	// The carrier's 450ms exit still moves; the long wait after it is still.
	if !flightTraveling(phase(flightHold), now.Add(100*time.Millisecond)) {
		t.Error("the carrier's exit at an account did not use the travel cadence")
	}
	if flightTraveling(phase(flightHold), now.Add(900*time.Millisecond)) {
		t.Error("a provider hold used the travel cadence")
	}
}

func TestProviderHoldIsNotATravelLeg(t *testing.T) {
	now := time.Now()
	// A live request past its outbound leg is waiting at its account. The
	// packet is held still there; only its wire's flow and its pulse keep
	// the stage drawing, not a flight along a wire.
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 41, Agent: "codex", Provider: "qoder", RouteID: 1741,
		Started: now.Add(-5 * time.Second), Millis: 5000,
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	tester := render(t, model, 1240, 820)
	tester.Frame()
	model.mu.RLock()
	flights := append([]flightTrack(nil), model.topology.flights...)
	model.mu.RUnlock()
	if len(flights) != 1 {
		t.Fatalf("flight tracks = %d, want 1", len(flights))
	}
	// A bird first seen mid-request still flies its whole outbound leg
	// first; once it has landed the packet waits at the account.
	held := flights[0].firstSeen.Add(liveOutboundDuration + time.Second)
	moment := flightMomentAt(flights[0], held)
	if !moment.hasPhase || moment.phase.kind != flightHold || flightTraveling(flights[0], held) {
		t.Fatalf("provider hold moment = %+v, want a static hold", moment)
	}
}

func TestReplayPlaybackRebuildsViewBelowAnimationRate(t *testing.T) {
	start := time.Date(2026, 10, 19, 5, 0, 0, 0, time.UTC)
	route := routing.Route{
		ID: 93, Time: start, Agent: "codex", Model: "qoder/model",
		Provider: "qoder", Done: true, Status: 200, Millis: 2000,
		HistoryDay: start.Format("2006-01-02"),
		Tries: []routing.Try{{
			ID: "qoder", Model: "qoder/model", Start: start,
			Millis: 2000, Status: 200, Done: true,
		}},
	}
	store := routing.NewStore()
	store.ReplaceHistory([]routing.Route{route})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	model.setTab(tabHistory)
	model.SetHistoryDay(start.Format("2006-01-02"))
	builds := 0
	tester := ui.NewTester(func(c *ui.Context) {
		builds++
		model.View(c)
	}, 1240, 900)
	if err := tester.Click("Play"); err != nil {
		t.Fatal(err)
	}
	builds = 0
	for i := 0; i < 8; i++ {
		tester.Advance(routingAnimationInterval)
	}
	if builds > 1 {
		t.Fatalf("replay rebuilt the view %d times in 128ms at 60 Hz; want paint-only flight frames", builds)
	}
}

func TestStageGeometryTracksWindowAndDPI(t *testing.T) {
	now := time.Now()
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 19, Agent: "codex", Provider: "qoder", RouteID: 1710,
		Started: now, Millis: 600,
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	tester := render(t, model, 1000, 760)
	model.mu.RLock()
	before := model.topology.flightGeometry
	model.mu.RUnlock()
	if before == nil {
		t.Fatal("initial flight geometry was not created")
	}

	tester.SetSize(1240, 900)
	tester.Frame()
	model.mu.RLock()
	afterResize := model.topology.flightGeometry
	model.mu.RUnlock()
	if afterResize == before {
		t.Fatal("window resize reused geometry from the old layout")
	}

	tester.SetScale(2)
	tester.Frame()
	model.mu.RLock()
	afterScale := model.topology.flightGeometry
	model.mu.RUnlock()
	if afterScale != afterResize {
		t.Fatal("DPI change rebuilt geometry that did not move in DIPs")
	}
}

func TestFlightPathSamplesByArcLength(t *testing.T) {
	path := newFlightPath([]flightPoint{
		{0, 0}, {100, 0}, {100, 10},
	})
	if got := path.length(); got != 110 {
		t.Fatalf("path length = %v, want 110", got)
	}
	x, y, angle, ok := path.at(100)
	if !ok || x != 100 || y != 0 {
		t.Fatalf("halfway point = (%v,%v,%v), want (100,0)", x, y, angle)
	}
	x, y, angle, ok = path.at(105)
	if !ok || x != 100 || y != 5 || angle < 89 || angle > 91 {
		t.Fatalf("bend point = (%v,%v,%v), want (100,5,90deg)", x, y, angle)
	}
}

func TestPathAimClipsBankAndFlipsLeft(t *testing.T) {
	right := newFlightPath([]flightPoint{{0, 0}, {100, 0}})
	if aim, flip := pathAim(right, 50); aim != 0 || flip != 1 {
		t.Fatalf("rightward aim = (%v,%v), want (0,1)", aim, flip)
	}
	left := right.reversed()
	if aim, flip := pathAim(left, 50); aim != 0 || flip != -1 {
		t.Fatalf("leftward aim = (%v,%v), want (0,-1)", aim, flip)
	}
	steep := newFlightPath([]flightPoint{{0, 0}, {10, 100}})
	if aim, _ := pathAim(steep, 50); aim != 24 {
		t.Fatalf("steep aim = %v, want the upstream 24 degree bank cap", aim)
	}
}

func TestWingFlapMatchesUpstreamKeyframes(t *testing.T) {
	cases := []struct {
		phase       float32
		scale, skew float32
	}{
		{0, 1, 0},
		{0.55, -0.62, -8},
		{1, 1, 0},
	}
	for _, c := range cases {
		scale, skew := wingFrame(c.phase)
		if math.Abs(float64(scale-c.scale)) > 0.02 || math.Abs(float64(skew-c.skew)) > 0.1 {
			t.Errorf("wingFrame(%v) = (%v,%v), want (%v,%v)", c.phase, scale, skew, c.scale, c.skew)
		}
	}
}

func TestFlightViaJoinsWiresWithoutHubCenterDetour(t *testing.T) {
	agent := nodeBox{x: 10, y: 80, w: 124, h: 82}
	hub := nodeBox{x: 190, y: 70, w: 124, h: 104}
	provider := nodeBox{x: 370, y: 100, w: 260, h: 44}
	path := stageOutboundPath([]nodeBox{agent}, 0, hub, &provider, false)
	if len(path.pts) < 4 {
		t.Fatalf("outbound path has only %d points", len(path.pts))
	}
	ax, ay := agent.rightCenter()
	px, py := provider.leftCenter()
	first, last := path.pts[0], path.pts[len(path.pts)-1]
	if math.Abs(float64(first.x-ax)) > 0.01 || math.Abs(float64(first.y-ay)) > 0.01 {
		t.Fatalf("path starts at (%v,%v), want agent anchor (%v,%v)", first.x, first.y, ax, ay)
	}
	if math.Abs(float64(last.x-px)) > 0.01 || math.Abs(float64(last.y-py)) > 0.01 {
		t.Fatalf("path ends at (%v,%v), want provider anchor (%v,%v)", last.x, last.y, px, py)
	}
	hcx, hcy := hub.x+hub.w/2, hub.y+hub.h/2
	for _, pt := range path.pts {
		if math.Hypot(float64(pt.x-hcx), float64(pt.y-hcy)) < 0.5 {
			t.Fatal("outbound bird passed through the hub center instead of the via arc")
		}
	}
	// A return is the exact reverse geometry, so it cannot jump lanes.
	back := path.reversed()
	if math.Abs(float64(back.length()-path.length())) > 0.01 {
		t.Fatalf("reverse path length = %v, want %v", back.length(), path.length())
	}
	lastBack := back.pts[len(back.pts)-1]
	if math.Abs(float64(lastBack.x-ax)) > 0.01 || math.Abs(float64(lastBack.y-ay)) > 0.01 {
		t.Fatalf("reverse path ends at (%v,%v), want agent anchor (%v,%v)", lastBack.x, lastBack.y, ax, ay)
	}
}

func TestNarrowProviderLaneKeepsHubBottomAsItsStart(t *testing.T) {
	hub := nodeBox{x: 260, y: 40, w: 180, h: 104}
	agents := []nodeBox{{x: 20, y: 40, w: 210, h: 82}}
	provider := nodeBox{x: 22, y: 220, w: 418, h: 44}
	path := hubToNarrowProviderPath(hub, agents, &provider)
	if len(path.pts) == 0 {
		t.Fatal("narrow provider path is empty")
	}
	wantX, wantY := hub.x+hub.w/2, hub.y+hub.h
	if dx, dy := path.pts[0].x-wantX, path.pts[0].y-wantY; math.Hypot(float64(dx), float64(dy)) > 0.01 {
		t.Fatalf("narrow path starts at (%.1f,%.1f), want hub bottom (%.1f,%.1f)", path.pts[0].x, path.pts[0].y, wantX, wantY)
	}
	// The lane must descend below the lowest agent before it bends left.
	lowest := agents[0].y + agents[0].h
	seenBelowAgents := false
	for _, pt := range path.pts {
		if pt.y >= lowest-0.01 {
			seenBelowAgents = true
			break
		}
	}
	if !seenBelowAgents {
		t.Fatal("narrow path never reaches the lane below all agents")
	}
}

func TestSideBySideNarrowStageUsesHorizontalAgentWire(t *testing.T) {
	// At 560px the agents and hub are side by side. Upstream's
	// `h.l > s.r` test therefore chooses the horizontal cubic, not a
	// bottom-to-top line merely because the page is in its narrow layout.
	agent := nodeBox{x: 2, y: 40, w: 210, h: 82}
	hub := nodeBox{x: 240, y: 40, w: 210, h: 104}
	wire := stageAgentHubPath(agent, hub)
	wantX, wantY := agent.rightCenter()
	if math.Hypot(float64(wire.pts[0].x-wantX), float64(wire.pts[0].y-wantY)) > 0.01 {
		t.Fatalf("wire starts at %+v, want agent right center", wire.pts[0])
	}
	end := wire.pts[len(wire.pts)-1]
	wantX, wantY = hub.leftCenter()
	if math.Hypot(float64(end.x-wantX), float64(end.y-wantY)) > 0.01 {
		t.Fatalf("wire ends at %+v, want hub left center", end)
	}
	if math.Abs(float64(end.x-wire.pts[0].x)) < 10 || math.Abs(float64(end.y-wire.pts[0].y)) > 20 {
		t.Fatalf("side-by-side wire is not the shallow horizontal cubic: %+v -> %+v", wire.pts[0], end)
	}
}

func TestNarrowOutboundUsesAllAgentsForTheLane(t *testing.T) {
	narrow := flightStage{
		narrow: true,
		agents: []nodeBox{
			{x: 20, y: 40, w: 210, h: 82},
			{x: 20, y: 130, w: 210, h: 82},
		},
		hub:      nodeBox{x: 260, y: 40, w: 210, h: 104},
		rows:     []nodeBox{{x: 22, y: 250, w: 448, h: 44}},
		accounts: []providerNode{{Provider: "qoder", Model: "m"}},
	}
	geometry := newFlightGeometry(narrow)
	first := geometry.outboundPath(0, 0)
	shared := geometry.hubProvider[0]
	if len(first.pts) < 3 || len(shared.pts) < 3 {
		t.Fatalf("narrow paths are too short: flight=%d shared=%d", len(first.pts), len(shared.pts))
	}
	// The agent wire itself is selected from the real node rectangles. The
	// outbound flight must begin at exactly that wire's first sample, rather
	// than a separately recomputed anchor.
	wantStart := geometry.agentHub[0].pts[0]
	if math.Abs(float64(first.pts[0].x-wantStart.x)) > 0.01 || math.Abs(float64(first.pts[0].y-wantStart.y)) > 0.01 {
		t.Fatalf("flight starts at %+v, want agent wire start %+v", first.pts[0], wantStart)
	}
	wire := geometry.agentHub[0]
	wireEnd := wire.pts[len(wire.pts)-1]
	wantEndX, wantEndY := narrow.hub.leftCenter()
	if math.Abs(float64(wireEnd.x-wantEndX)) > 0.01 ||
		math.Abs(float64(wireEnd.y-wantEndY)) > 0.01 {
		t.Fatalf("agent wire ends at %+v, want hub left center", wireEnd)
	}
	sharedStart := shared.pts[0]
	if math.Abs(float64(sharedStart.x-(narrow.hub.x+narrow.hub.w/2))) > 0.01 ||
		math.Abs(float64(sharedStart.y-(narrow.hub.y+narrow.hub.h))) > 0.01 {
		t.Fatalf("shared narrow wire starts at %+v, want hub bottom", sharedStart)
	}
	// The lane must pass below the lowest agent, not just the selected one.
	lowest := narrow.agents[1].y + narrow.agents[1].h
	reachesLowest := false
	for _, pt := range shared.pts {
		if pt.y >= lowest-0.01 {
			reachesLowest = true
			break
		}
	}
	if !reachesLowest {
		t.Fatalf("lane built from a single agent stopped above the lowest agent: %.1f", lowest)
	}

	// The provider-lane segment is shared geometry, not merely a matching
	// start point. Every outbound flight must contain the full static wire
	// after its via, otherwise an agent with a different vertical position
	// can visibly leave the lane mid-flight.
	containsPath := func(path, want flightPath) bool {
		if len(want.pts) == 0 || len(path.pts) < len(want.pts) {
			return false
		}
		for start := 0; start+len(want.pts) <= len(path.pts); start++ {
			ok := true
			for i, pt := range want.pts {
				if got := path.pts[start+i]; math.Hypot(float64(got.x-pt.x), float64(got.y-pt.y)) > 0.02 {
					ok = false
					break
				}
			}
			if ok {
				return true
			}
		}
		return false
	}
	for agentIndex := range narrow.agents {
		if got := geometry.outboundPath(agentIndex, 0); !containsPath(got, shared) {
			t.Fatalf("agent %d outbound path does not contain the shared provider lane", agentIndex)
		}
	}
}

func TestFlightTipTangentMatchesUpstreamSampling(t *testing.T) {
	// tip(wire, true) walks back from the path's actual end. A vertical
	// descent must therefore arrive pointing down, not right as the old
	// start-tangent call did in via().
	descending := newFlightPath([]flightPoint{{x: 100, y: 10}, {x: 100, y: 30}, {x: 100, y: 50}, {x: 100, y: 70}})
	tx, ty := flightTipTangent(descending, true, false)
	if math.Abs(float64(tx)) > 0.01 || math.Abs(float64(ty-1)) > 0.01 {
		t.Fatalf("end tangent = (%.3f,%.3f), want (0,1)", tx, ty)
	}
	// The sample point is two DIPs from the endpoint, not simply the last
	// segment, so a short final turn cannot skew the direction.
	turning := newFlightPath([]flightPoint{{x: 100, y: 10}, {x: 100, y: 40}, {x: 100, y: 70}, {x: 130, y: 70}, {x: 160, y: 70}})
	tx, ty = flightTipTangent(turning, true, false)
	if math.Abs(float64(tx-1)) > 0.01 || math.Abs(float64(ty)) > 0.01 {
		t.Fatalf("turning end tangent = (%.3f,%.3f), want (1,0)", tx, ty)
	}
	startTX, startTY := flightTipTangent(turning, false, false)
	if math.Abs(float64(startTX)) > 0.01 || math.Abs(float64(startTY-1)) > 0.01 {
		t.Fatalf("start tangent = (%.3f,%.3f), want (0,1)", startTX, startTY)
	}
	backTX, backTY := flightTipTangent(turning, true, true)
	if math.Abs(float64(backTX+1)) > 0.01 || math.Abs(float64(backTY)) > 0.01 {
		t.Fatalf("reversed end tangent = (%.3f,%.3f), want (-1,0)", backTX, backTY)
	}
}

func TestHeldPacketPulseMatchesRtheldEndpoints(t *testing.T) {
	pulseAt := func(now time.Time) float32 {
		phase := float32(now.UnixMilli()%1200) / 1200
		return 1 - 0.55*cosineEase(phase*2)
	}
	base := time.UnixMilli(1_200_000)
	if got := pulseAt(base); math.Abs(float64(got-1)) > 0.001 {
		t.Fatalf("rtheld at 0%% = %v, want 1", got)
	}
	if got := pulseAt(base.Add(600 * time.Millisecond)); math.Abs(float64(got-0.45)) > 0.001 {
		t.Fatalf("rtheld at 50%% = %v, want .45", got)
	}
	if got := pulseAt(base.Add(1200 * time.Millisecond)); math.Abs(float64(got-1)) > 0.001 {
		t.Fatalf("rtheld at 100%% = %v, want 1", got)
	}
}

func TestCubicBezierEaseMatchesUpstreamFlapCurve(t *testing.T) {
	// rtflap's cubic-bezier(.45, 0, .3, 1), read from CSS: the curve
	// rises monotonically and reaches y=1 at x=1.
	last := float32(0)
	for i := 0; i <= 100; i++ {
		v := cubicBezierEase(0.45, 0, 0.3, 1, float32(i)/100)
		if v < last-0.001 {
			t.Fatalf("rtflap ease went backwards at %d%%: %v < %v", i, v, last)
		}
		last = v
	}
	if got := cubicBezierEase(0.45, 0, 0.3, 1, 0); got != 0 {
		t.Fatalf("ease(0) = %v", got)
	}
	if got := cubicBezierEase(0.45, 0, 0.3, 1, 1); got != 1 {
		t.Fatalf("ease(1) = %v", got)
	}
}

func TestFlightPhasesKeepReturnAfterArrival(t *testing.T) {
	start := time.Date(2026, 10, 19, 1, 0, 0, 0, time.UTC)
	rv := routing.RouteView{Route: routing.Route{
		Time: start, Millis: 3823,
		Tries: []routing.Try{{
			ID: "qoder", Model: "dfmodel", Start: start.Add(203 * time.Millisecond),
			Millis: 3620, Status: 200, Done: true,
		}},
	}}
	attempts, total := traceFlightAttempts(rv)
	phases := buildFlightPhases(attempts, total, true)
	if len(phases) != 3 {
		t.Fatalf("phases = %+v, want outbound, hold, inbound", phases)
	}
	if phases[0].kind != flightOutbound || phases[0].end != 1400*time.Millisecond {
		t.Fatalf("outbound = %+v, want upstream's 1.4s leg", phases[0])
	}
	if phases[1].kind != flightHold || phases[1].end < 3823*time.Millisecond {
		t.Fatalf("hold = %+v, want it to reach the recorded try end", phases[1])
	}
	if phases[2].kind != flightInbound || phases[2].end <= total {
		t.Fatalf("inbound = %+v, want a return leg after the recorded completion", phases[2])
	}
}

func TestNativeMagpiePortraitIsNotBlank(t *testing.T) {
	pal := colors(nil, config.ThemeDark)
	tester := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Background(ui.RGB(255, 255, 255)).Draw(func(p *ui.Painter, r ui.Rect) {
			drawStillMagpie(p, r.X+r.W/2, r.Y+r.H/2, 2.2, pal)
		})
	}, 160, 120)
	img := tester.Image()
	nonWhite, dark := 0, 0
	for y := 20; y < 100; y++ {
		for x := 30; x < 130; x++ {
			i := img.PixOffset(x, y)
			r, g, b := img.Pix[i], img.Pix[i+1], img.Pix[i+2]
			if r < 245 || g < 245 || b < 245 {
				nonWhite++
			}
			if r < 100 && g < 100 && b < 100 {
				dark++
			}
		}
	}
	if nonWhite < 180 || dark < 20 {
		t.Fatalf("magpie portrait has %d non-white and %d dark pixels; the FLIER layers did not draw", nonWhite, dark)
	}
}

func TestNativeMagpieFlapChangesWingSilhouette(t *testing.T) {
	render := func(phase float32) *image.RGBA {
		return ui.NewTester(func(c *ui.Context) {
			ui.Box(c).Fill().Background(ui.RGB(255, 255, 255)).Draw(func(p *ui.Painter, r ui.Rect) {
				wing, skew := wingFrame(phase)
				farWing, farSkew := wingFrame(phase - 0.03/0.42)
				p.Transform(ui.Translation(r.X+r.W/2, r.Y+r.H/2), 0, 0, func() {
					p.Transform(ui.Scaling(5, 5), 0, 0, func() {
						drawMagpieShape(p, ui.RGB(20, 20, 20), ui.RGB(255, 255, 255),
							wing, skew, farWing, farSkew, 1)
					})
				})
			})
		}, 360, 240).Image()
	}
	difference := 0
	a, b := render(0), render(0.25)
	for i := 0; i < len(a.Pix) && i < len(b.Pix); i += 4 {
		if a.Pix[i] != b.Pix[i] {
			difference++
		}
	}
	if difference < 120 {
		t.Fatalf("wing keyframes differ by only %d pixels", difference)
	}
}

func TestFlightDashPathKeepsDashCycle(t *testing.T) {
	path := newFlightPath([]flightPoint{{x: 0, y: 10}, {x: 96, y: 10}})
	for _, offset := range []float32{0, 1, 3, 5, 7.9} {
		var flow ui.Path
		for d := -offset; d < path.length(); d += 8 {
			appendFlightArc(&flow, path, max(d, 0), min(d+3, path.length()))
		}
		got, ok := flightDashPath(path, offset, 3, 8)
		if !ok {
			t.Fatalf("offset %.1f produced no dashes", offset)
		}
		if flow.Bounds() != got.Bounds() {
			t.Fatalf("offset %.1f bounds = %v, new = %v", offset, flow.Bounds(), got.Bounds())
		}
	}
}

func TestStageRendersWithoutRasterGhostingAtHighDPI(t *testing.T) {
	now := time.Now()
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 12, Agent: "codex", Provider: "qoder", RouteID: 1704,
		Started: now, Millis: 600,
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	tester := ui.NewTester(model.View, 1240, 820)
	tester.SetScale(2)
	tester.Frame()
	img := tester.Image()
	if img.Bounds().Dx() != 1240*2 || img.Bounds().Dy() != 820*2 {
		t.Fatalf("high-DPI frame = %v, want %dx%d", img.Bounds(), 1240*2, 820*2)
	}
	assertImageHasContrast(t, img, 0, 0, img.Bounds().Dx(), img.Bounds().Dy(), config.ThemeLight)
}

func TestStaticStageBitmapUsesDevicePixels(t *testing.T) {
	now := time.Now()
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 12, Agent: "codex", Provider: "qoder", RouteID: 1712,
		Started: now, Millis: 600,
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	tester := render(t, model, 1000, 760)
	tester.SetScale(2)
	tester.Frame()
	model.mu.RLock()
	bitmap := model.topology.staticStage
	model.mu.RUnlock()
	if bitmap == nil {
		t.Fatal("static stage bitmap was not created")
	}
	w, h := bitmap.Size()
	if w%2 != 0 || h%2 != 0 {
		t.Fatalf("static stage bitmap = %dx%d, want device-pixel dimensions at 2x", w, h)
	}
	if w < 900 {
		t.Fatalf("static stage bitmap = %dx%d, want device-resolution pixels", w, h)
	}
	if !tester.HasText("Codex") {
		t.Fatalf("an accessible Codex element disappeared: %v", tester.Texts())
	}
}

func TestFlightFramesDoNotInvalidateStaticStageBitmap(t *testing.T) {
	now := time.Now()
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 13, Agent: "codex", Provider: "qoder", RouteID: 1713,
		Started: now, Millis: 600,
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	tester := render(t, model, 1240, 820)
	tester.Frame()
	model.mu.RLock()
	before := model.topology.staticStage
	beforeKey := model.topology.staticStageKey
	model.mu.RUnlock()
	if before == nil {
		t.Fatal("static stage bitmap was not created")
	}
	for i := 0; i < 6; i++ {
		tester.Advance(routingAnimationInterval)
	}
	model.mu.RLock()
	after := model.topology.staticStage
	afterKey := model.topology.staticStageKey
	model.mu.RUnlock()
	if after != before || afterKey != beforeKey {
		t.Fatal("a bird-only frame rebuilt the cached static stage bitmap")
	}
}

func TestFlightFramesDoNotLayOutStaticStageText(t *testing.T) {
	now := time.Now()
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 21, Agent: "codex", Provider: "qoder", RouteID: 1721,
		Started: now, Millis: 600,
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	tester := render(t, model, 1240, 820)
	tester.Frame()
	// A flight frame is a retained-tree repaint. It may draw the live bird,
	// but must sample the cached static stage instead of laying out its
	// labels and account text again.
	model.mu.RLock()
	key := model.topology.staticStageKey
	model.mu.RUnlock()
	for i := 0; i < 10; i++ {
		tester.Advance(routingAnimationInterval)
	}
	model.mu.RLock()
	after := model.topology.staticStageKey
	model.mu.RUnlock()
	if after != key {
		t.Fatal("flight frames invalidated the static stage bitmap")
	}
}

func TestStaticStageReleasesReplacedBitmaps(t *testing.T) {
	now := time.Now()
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 31, Agent: "codex", Provider: "qoder", RouteID: 1731,
		Started: now, Millis: 600,
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	tester := render(t, model, 1240, 820)
	tester.Frame()
	model.mu.RLock()
	first := model.topology.staticStage
	model.mu.RUnlock()
	if first == nil {
		t.Fatal("static stage bitmap was not created")
	}
	refs := []weak.Pointer[ui.Bitmap]{weak.Make(first)}

	// Exercise the same cache invalidation points as a long-running
	// desktop session: theme, window size, and display scale.
	cfg := model.Settings()
	for i := 0; i < 8; i++ {
		cfg.Theme = config.ThemeDark
		if i%2 == 1 {
			cfg.Theme = config.ThemeLight
		}
		model.SetSettings(cfg)
		tester.SetSize(1000+i*20, 720+i*10)
		tester.SetScale(1 + float32(i%2))
		tester.Frame()
		model.mu.RLock()
		bitmap := model.topology.staticStage
		model.mu.RUnlock()
		if bitmap == nil {
			t.Fatalf("static stage bitmap disappeared after refresh %d", i)
		}
		refs = append(refs, weak.Make(bitmap))
	}

	// The model retains only the newest bitmap. The older ones must not be
	// kept alive by a hidden cache or by the renderer's last frame.
	runtime.GC()
	runtime.GC()
	model.mu.RLock()
	current := model.topology.staticStage
	model.mu.RUnlock()
	for i, ref := range refs[:len(refs)-1] {
		if ref.Value() != nil {
			t.Fatalf("old static stage bitmap %d survived cache replacement", i)
		}
	}
	if refs[len(refs)-1].Value() == nil {
		t.Fatal("the current static stage bitmap was collected")
	}
	runtime.KeepAlive(current)
}

func TestFlightGeometryCachesOutboundAndInboundPaths(t *testing.T) {
	stage := flightStage{
		agents: []nodeBox{{x: 10, y: 20, w: 124, h: 78}, {x: 10, y: 110, w: 124, h: 78}},
		hub:    nodeBox{x: 200, y: 80, w: 124, h: 98},
		accounts: []providerNode{
			{Provider: "one"}, {Provider: "two"},
		},
		rows: []nodeBox{{x: 390, y: 30, w: 220, h: 44}, {x: 390, y: 90, w: 220, h: 44}},
	}
	first := newFlightGeometry(stage)
	pathA := first.outboundPath(0, 0)
	pathB := first.outboundPath(0, 0)
	if pathA.length() <= 0 || pathA.length() != pathB.length() {
		t.Fatalf("outbound cache returned inconsistent paths: %v vs %v", pathA.length(), pathB.length())
	}
	if &pathA.pts[0] != &pathB.pts[0] {
		t.Fatal("outbound path was rebuilt within the same layout")
	}
	inboundA := first.inboundPath(0, 0)
	inboundB := first.inboundPath(0, 0)
	if &inboundA.pts[0] != &inboundB.pts[0] {
		t.Fatal("inbound path was rebuilt within the same layout")
	}

	moved := stage
	moved.agents = append([]nodeBox(nil), stage.agents...)
	moved.agents[0].y += 10
	if newFlightGeometry(moved).key == first.key {
		t.Fatal("node movement did not invalidate the geometry key")
	}
}

func TestStageLayersShareOnePresentationPerFrame(t *testing.T) {
	now := time.Now()
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 31, Agent: "codex", Provider: "qoder", RouteID: 1731,
		Started: now, Millis: 600,
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	tester := ui.NewTester(func(c *ui.Context) {
		model.View(c)
	}, 1240, 820)
	tester.Frame()
	model.mu.RLock()
	first := model.topology.flightGeometry
	model.mu.RUnlock()
	if first == nil {
		t.Fatal("the shared stage presentation did not create geometry")
	}
	tester.Advance(routingAnimationInterval)
	model.mu.RLock()
	second := model.topology.flightGeometry
	model.mu.RUnlock()
	if second != first {
		t.Fatal("a retained animation frame rebuilt the shared stage geometry")
	}

	stage := model.routingStagePresentationAt(store.Snapshot(), store.Snapshot().Routes,
		routing.RouteView{}, false, tester.Now())
	if stage == nil || len(stage.agents) != 1 || len(stage.rows) != 1 ||
		len(stage.flights) != 1 || stage.stageIdentity == "" {
		t.Fatalf("stage presentation = %+v, want one shared agent/row/flight", stage)
	}
}

func TestFilteredRoutesAreReusedUntilSnapshotOrFilterChanges(t *testing.T) {
	routes := readSyntheticRoutes(t)
	store := routing.NewStore()
	store.ReplaceHistory(routes)
	model, _ := testModel(t, store.Snapshot(), Actions{})
	first := model.viewRoutes(model.snapshot(), false)
	second := model.viewRoutes(model.snapshot(), false)
	if len(first) == 0 || &first[0] != &second[0] {
		t.Fatal("an unchanged snapshot/filter rebuilt the filtered route slice")
	}
	model.mu.Lock()
	model.query = "no route matches this"
	model.mu.Unlock()
	filtered := model.viewRoutes(model.snapshot(), false)
	if len(filtered) != 0 {
		t.Fatalf("filter change reused %d stale routes", len(filtered))
	}
}

func TestActiveReplayWindowsIncludesReturnOnlyInterval(t *testing.T) {
	windows := []replayFlightWindow{
		{routeID: 1, start: 0, end: 1000 * time.Millisecond},
		{routeID: 2, start: 500 * time.Millisecond, end: 5000 * time.Millisecond},
		{routeID: 3, start: 2000 * time.Millisecond, end: 3000 * time.Millisecond},
	}
	ends := replayWindowEnds(windows)
	active := activeReplayWindows(windows, ends, 3500*time.Millisecond)
	if len(active) != 1 || active[0].routeID != 2 {
		t.Fatalf("windows at 3.5s = %+v, want only route 2", active)
	}
	active = activeReplayWindows(windows, ends, 1500*time.Millisecond)
	if len(active) != 1 || active[0].routeID != 2 {
		t.Fatalf("windows at 1.5s = %+v, want route 2", active)
	}
	active = activeReplayWindows(windows, ends, 2500*time.Millisecond)
	if len(active) != 2 || active[0].routeID != 2 || active[1].routeID != 3 {
		t.Fatalf("windows at 2.5s = %+v, want routes 2 and 3", active)
	}
	if active := activeReplayWindows(windows, ends, 6000*time.Millisecond); len(active) != 0 {
		t.Fatalf("windows after the timeline = %+v, want none", active)
	}
}

func TestReplayWithoutTriesDoesNotSynthesizeFlight(t *testing.T) {
	start := time.Date(2026, 10, 19, 4, 0, 0, 0, time.UTC)
	routes := []routing.RouteView{{Route: routing.Route{
		ID: 91, Time: start, Agent: "codex", Model: "qoder/model",
		Provider: "qoder", Done: true, Status: 200, Millis: 2000,
		HistoryDay: "2026-10-19",
	}}}
	timeline := routing.BuildDayReplay(routes, "2026-10-19")
	windows := buildReplayFlightWindows(timeline, routes, 1)
	if len(windows) != 0 {
		t.Fatalf("a trace without tries created flight windows: %+v", windows)
	}
}

func TestTraceFlightAttemptsFollowRecordedOrder(t *testing.T) {
	start := time.Date(2026, 10, 19, 1, 0, 0, 0, time.UTC)
	rv := routing.RouteView{Route: routing.Route{
		Time: start, Millis: 2000,
		Tries: []routing.Try{
			{Model: "first/model", Start: start.Add(100 * time.Millisecond), Millis: 400, Status: 429, Fail: "rate"},
			{Model: "second/model", Start: start.Add(700 * time.Millisecond), Millis: 900, Status: 200, Done: true},
		},
	}}
	attempts, total := traceFlightAttempts(rv)
	if len(attempts) != 2 || total != 2*time.Second {
		t.Fatalf("attempts = %+v, total = %v", attempts, total)
	}
	if attempts[0].provider != "first" || !attempts[0].failed {
		t.Fatalf("first attempt lost failure/provider: %+v", attempts[0])
	}
	if attempts[1].provider != "second" || attempts[1].failed {
		t.Fatalf("second attempt lost success/provider: %+v", attempts[1])
	}
	if attempts[0].end != 500*time.Millisecond {
		t.Fatalf("first try end = %v, want its recorded start + 400ms", attempts[0].end)
	}
	phases := buildFlightPhases(attempts, total, true)
	var sawRetryBack, sawRetryOut, sawInbound bool
	for _, p := range phases {
		switch p.kind {
		case flightRetryBack:
			sawRetryBack = p.failed && p.provider == "first"
		case flightRetryOut:
			sawRetryOut = p.provider == "second"
		case flightInbound:
			sawInbound = true
		}
	}
	if !sawRetryBack || !sawRetryOut || !sawInbound {
		t.Fatalf("phases do not encode failure -> next -> answer: %+v", phases)
	}
}

func TestReplayFlightPausesWithReplayClock(t *testing.T) {
	start := time.Now().Add(-time.Minute)
	store := routing.NewStore()
	store.ReplaceHistory([]routing.Route{{
		ID: 77, Time: start, Agent: "codex", Model: "qoder/model",
		Provider: "qoder", Done: true, Status: 200, Millis: 2000,
		HistoryDay: start.Format("2006-01-02"),
		Order: []routing.Weighed{{
			ID: "qoder", Provider: "qoder", Name: "Qoder", Model: "qoder/model",
		}},
		Tries: []routing.Try{{
			ID: "qoder", Model: "qoder/model", Start: start,
			Millis: 2000, Status: 200, Done: true,
		}},
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	model.setTab(tabHistory)
	model.SetHistoryDay(start.Format("2006-01-02"))
	model.mu.Lock()
	model.replay.loaded = true
	model.replay.builtKey = "test"
	model.replay.timeline = routing.BuildDayReplay(store.Snapshot().Routes, model.historyDay)
	routes := store.Snapshot().Routes
	model.replay.windows = buildReplayFlightWindows(model.replay.timeline, routes, 1)
	model.replay.windowEnds = replayWindowEnds(model.replay.windows)
	model.replay.position = 500 * time.Millisecond
	model.replay.playing = false
	model.mu.Unlock()
	tester := render(t, model, 1240, 900)
	tester.Frame()
	model.mu.RLock()
	flights := len(model.topology.flights)
	model.mu.RUnlock()
	if flights == 0 {
		t.Fatal("paused replay fixture did not produce a real flight to test")
	}
	if tester.FramePending() {
		t.Fatal("a paused history replay requested animation frames")
	}
}

func TestReplayPauseStopsFramesWithLivePhaseFeedback(t *testing.T) {
	start := time.Now().Add(-time.Minute)
	store := routing.NewStore()
	store.ReplaceHistory([]routing.Route{{
		ID: 78, Time: start, Agent: "codex", Model: "qoder/model",
		Provider: "qoder", Done: true, Status: 200, Millis: 2000,
		Order: []routing.Weighed{{
			ID: "qoder", Provider: "qoder", Name: "Qoder", Model: "qoder/model",
		}},
		Tries: []routing.Try{{
			ID: "qoder", Model: "qoder/model", Start: start,
			Millis: 2000, Status: 200, Done: true,
		}},
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	model.setTab(tabHistory)
	model.SetHistoryDay(start.Format("2006-01-02"))
	tester := render(t, model, 1240, 900)
	model.mu.Lock()
	model.replay.position = 500 * time.Millisecond
	model.replay.playing = false
	for i := range model.topology.flights {
		model.topology.flights[i].phaseAt = time.Now()
		model.topology.flights[i].phaseKind = flightHold
	}
	model.mu.Unlock()
	tester.Frame()
	if tester.FramePending() {
		t.Fatal("paused replay kept scheduling a feedback repaint")
	}
}

func TestReplaySeekAfterARequestEndedFliesNoBird(t *testing.T) {
	start := time.Date(2026, 10, 19, 3, 0, 0, 0, time.UTC)
	route := routing.Route{
		ID: 88, Time: start, Agent: "claude", Model: "qoder/model",
		Provider: "qoder", Done: true, Status: 200, Millis: 2000,
		HistoryDay: start.Format("2006-01-02"),
		Tries: []routing.Try{{
			ID: "qoder", Model: "qoder/model", Start: start,
			Millis: 2000, Status: 200, Done: true,
		}},
	}
	store := routing.NewStore()
	store.ReplaceHistory([]routing.Route{route})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	model.setTab(tabHistory)
	model.SetHistoryDay(start.Format("2006-01-02"))
	tester := render(t, model, 1240, 900)
	// client.js's seek(): the flights end; a request already over at the
	// new position is done and does not fly, even if its answer's
	// presentation flight home would still have been in the air.
	model.seekReplay((2*time.Second + 200*time.Millisecond).Seconds())
	tester.Frame()
	model.mu.RLock()
	flights := append([]flightTrack(nil), model.topology.flights...)
	model.mu.RUnlock()
	if len(flights) != 0 {
		t.Fatalf("seek past a finished request flew %d birds, want none", len(flights))
	}
	if tester.FramePending() {
		t.Fatal("paused seek requested animation frames")
	}
}

func TestProviderNodeResolvesSameProviderByModelAccount(t *testing.T) {
	rows := []providerNode{
		{Provider: "openai", Name: "OpenAI", Model: "gpt-6", AccountID: "key-a", Who: "alice@example"},
		{Provider: "openai", Name: "OpenAI", Model: "gpt-5.6", AccountID: "key-b", Who: "bob@example"},
	}
	cases := []struct {
		target providerTarget
		want   int
	}{
		{providerTarget{provider: "openai", model: "gpt-6", accountID: "key-a"}, 0},
		{providerTarget{provider: "openai", model: "gpt-5.6", accountID: "key-b"}, 1},
		{providerTarget{provider: "openai", model: "gpt-6", accountID: "key-b"}, 1},
		{providerTarget{provider: "openai", model: "gpt-6"}, 0},
		{providerTarget{provider: "openai", model: "gpt-5.6"}, 1},
		{providerTarget{provider: "openai"}, -1},
	}
	for _, c := range cases {
		if got := indexProviderNode(rows, c.target); got != c.want {
			t.Errorf("indexProviderNode(%+v) = %d, want %d", c.target, got, c.want)
		}
	}
}

func TestProviderNodeRejectsAmbiguousSameProviderModel(t *testing.T) {
	rows := []providerNode{
		{Provider: "openai", Model: "gpt-6", AccountID: "key-a"},
		{Provider: "openai", Model: "gpt-6", AccountID: "key-b"},
	}
	if got := indexProviderNode(rows, providerTarget{provider: "openai", model: "gpt-6"}); got != -1 {
		t.Fatalf("ambiguous model resolved to row %d; want no endpoint", got)
	}
	if got := indexProviderNode(rows, providerTarget{provider: "openai", model: "gpt-6", accountID: "key-b"}); got != 1 {
		t.Fatalf("exact account resolved to row %d, want row 1", got)
	}
}

func TestProviderNodeRejectsKnownAccountMismatch(t *testing.T) {
	rows := []providerNode{
		{Provider: "openai", Model: "gpt-6", AccountID: "key-a"},
		{Provider: "openai", Model: "gpt-5.6", AccountID: "key-c"},
	}
	if got := indexProviderNode(rows, providerTarget{
		provider: "openai", model: "gpt-6", accountID: "key-b",
	}); got != -1 {
		t.Fatalf("known account mismatch resolved to row %d; want no endpoint", got)
	}
}

func TestProviderNodeMatchesPrefixedAndShortModelWithSameProvider(t *testing.T) {
	rows := []providerNode{
		{Provider: "openai", Model: "gpt-6", AccountID: "key-a"},
	}
	cases := []providerTarget{
		{provider: "openai", model: "openai/gpt-6"},
		{provider: "openai", model: "gpt-6"},
	}
	for _, target := range cases {
		if got := indexProviderNode(rows, target); got != 0 {
			t.Errorf("indexProviderNode(%+v) = %d, want row 0", target, got)
		}
	}
	prefixedRow := []providerNode{
		{Provider: "openai", Model: "openai/gpt-6", AccountID: "key-a"},
	}
	if got := indexProviderNode(prefixedRow, providerTarget{provider: "openai", model: "gpt-6"}); got != 0 {
		t.Fatalf("short model did not match prefixed row: %d", got)
	}
}

func TestModelMatchesRejectsDifferentProviderPrefixes(t *testing.T) {
	if modelMatches("openai/gpt-6", "other/gpt-6", "") {
		t.Fatal("modelMatches accepted different explicit provider prefixes")
	}
	if !modelMatches("openai/gpt-6", "gpt-6", "openai") {
		t.Fatal("modelMatches rejected a short name with the matching provider")
	}
	if modelMatches("openai/gpt-6", "gpt-6", "other") {
		t.Fatal("modelMatches accepted a short name with a conflicting provider")
	}
}

func TestTraceAttemptWithoutAccountMatchesShortModelByName(t *testing.T) {
	start := time.Date(2026, 10, 19, 4, 30, 0, 0, time.UTC)
	rv := routing.RouteView{Route: routing.Route{
		Time: start, Millis: 900,
		Tries: []routing.Try{{
			Model: "openai/gpt-6", Start: start,
			Millis: 900, Status: 200, Done: true,
		}},
	}}
	attempts, _ := traceFlightAttempts(rv)
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(attempts))
	}
	rows := []providerNode{
		{Provider: "openai", Model: "gpt-6"},
	}
	if got := indexProviderNode(rows, targetForPhase(flightTrack{}, flightPhase{
		provider: attempts[0].provider, model: attempts[0].model,
	})); got != 0 {
		t.Fatalf("short-name row resolved to %d, want row 0", got)
	}
}

func TestTraceAttemptTargetKeepsProviderModelAndAccount(t *testing.T) {
	start := time.Date(2026, 10, 19, 3, 30, 0, 0, time.UTC)
	rv := routing.RouteView{Route: routing.Route{
		Time: start, Millis: 1800,
		Order: []routing.Weighed{
			{ID: "key-a", Provider: "openai", Who: "alice@example", Model: "gpt-6"},
			{ID: "key-b", Provider: "openai", Who: "bob@example", Model: "gpt-5.6"},
		},
		Tries: []routing.Try{
			{ID: "key-a", Model: "gpt-6", Start: start, Millis: 400, Status: 429, Fail: "rate"},
			{ID: "key-b", Model: "gpt-5.6", Start: start.Add(700 * time.Millisecond), Millis: 900, Status: 200, Done: true},
		},
	}}
	attempts, _ := traceFlightAttempts(rv)
	if len(attempts) != 2 {
		t.Fatalf("attempts = %d, want 2", len(attempts))
	}
	want := []providerTarget{
		{provider: "openai", model: "gpt-6", accountID: "key-a", who: "alice@example"},
		{provider: "openai", model: "gpt-5.6", accountID: "key-b", who: "bob@example"},
	}
	for i, target := range want {
		got := providerTarget{
			provider: attempts[i].provider, model: attempts[i].model,
			accountID: attempts[i].accountID, who: attempts[i].who,
		}
		if got != target {
			t.Errorf("attempt %d target = %+v, want %+v", i, got, target)
		}
	}
	rows := []providerNode{
		{Provider: "openai", Model: "gpt-6", AccountID: "key-a", Who: "alice@example"},
		{Provider: "openai", Model: "gpt-5.6", AccountID: "key-b", Who: "bob@example"},
	}
	phases := buildFlightPhases(attempts, 1800*time.Millisecond, true)
	var retryOut, inbound int
	for _, phase := range phases {
		switch phase.kind {
		case flightRetryOut:
			retryOut = indexProviderNode(rows, targetForPhase(flightTrack{}, phase))
		case flightInbound:
			inbound = indexProviderNode(rows, targetForPhase(flightTrack{}, phase))
		}
	}
	if retryOut != 1 || inbound != 1 {
		t.Fatalf("retry landed on row %d and return on row %d, want both row 1", retryOut, inbound)
	}
}

func TestTraceAttemptTargetPrefersAccountIDOverModelConflict(t *testing.T) {
	start := time.Date(2026, 10, 19, 3, 40, 0, 0, time.UTC)
	rv := routing.RouteView{Route: routing.Route{
		Time: start, Millis: 900,
		Order: []routing.Weighed{
			{ID: "key-a", Provider: "openai", Who: "alice@example", Model: "gpt-6"},
			{ID: "key-b", Provider: "openai", Who: "bob@example", Model: "gpt-5.6"},
		},
		Tries: []routing.Try{{
			ID: "key-a", Model: "gpt-5.6", Start: start,
			Millis: 900, Status: 200, Done: true,
		}},
	}}
	got := traceAttemptTarget(rv, rv.Tries[0])
	want := providerTarget{
		provider: "openai", model: "gpt-5.6",
		accountID: "key-a", who: "alice@example",
	}
	if got != want {
		t.Fatalf("conflicting try target = %+v, want account-first %+v", got, want)
	}
	rows := []providerNode{
		{Provider: "openai", Model: "gpt-6", AccountID: "key-a", Who: "alice@example"},
		{Provider: "openai", Model: "gpt-5.6", AccountID: "key-b", Who: "bob@example"},
	}
	if row := indexProviderNode(rows, got); row != 0 {
		t.Fatalf("account-first target resolved to row %d, want row 0", row)
	}
}

func TestTraceAttemptUnknownAccountDoesNotFallBackByModel(t *testing.T) {
	start := time.Date(2026, 10, 19, 3, 50, 0, 0, time.UTC)
	rv := routing.RouteView{Route: routing.Route{
		Time: start, Millis: 900,
		Order: []routing.Weighed{
			{ID: "key-a", Provider: "openai", Model: "gpt-6"},
			{ID: "key-b", Provider: "openai", Model: "gpt-6"},
		},
		Tries: []routing.Try{{
			ID: "key-c", Model: "gpt-6", Start: start,
			Millis: 900, Status: 200, Done: true,
		}},
	}}
	got := traceAttemptTarget(rv, rv.Tries[0])
	if got.accountID != "key-c" {
		t.Fatalf("unknown try account = %q, want key-c", got.accountID)
	}
	rows := []providerNode{
		{Provider: "openai", Model: "gpt-6", AccountID: "key-a"},
		{Provider: "openai", Model: "gpt-6", AccountID: "key-b"},
	}
	if row := indexProviderNode(rows, got); row != -1 {
		t.Fatalf("unknown try account fell back to model row %d", row)
	}
}

func TestTraceAttemptWithoutIDDoesNotBorrowOrderAccount(t *testing.T) {
	start := time.Date(2026, 10, 19, 4, 10, 0, 0, time.UTC)
	rv := routing.RouteView{Route: routing.Route{
		Time: start, Millis: 900,
		Order: []routing.Weighed{
			{ID: "key-a", Provider: "openai", Model: "gpt-6"},
		},
		Tries: []routing.Try{{
			Model: "openai/gpt-6", Start: start,
			Millis: 900, Status: 200, Done: true,
		}},
	}}
	got := traceAttemptTarget(rv, rv.Tries[0])
	if got.accountID != "" {
		t.Fatalf("try without an id inherited account %q", got.accountID)
	}
	if got.provider != "openai" || got.model != "openai/gpt-6" {
		t.Fatalf("try without id lost provider/model: %+v", got)
	}
	rows := []providerNode{
		{Provider: "openai", Model: "gpt-6", AccountID: "key-a"},
		{Provider: "openai", Model: "gpt-6", AccountID: "key-b"},
	}
	if row := indexProviderNode(rows, got); row != -1 {
		t.Fatalf("account-less try chose one of two accounts: row %d", row)
	}
}

func TestTraceAttemptUsesModelWithinExactAccountOnly(t *testing.T) {
	start := time.Date(2026, 10, 19, 4, 20, 0, 0, time.UTC)
	rv := routing.RouteView{Route: routing.Route{
		Time: start, Millis: 900,
		Order: []routing.Weighed{
			{ID: "key-a", Provider: "openai", Model: "gpt-6"},
			{ID: "key-a", Provider: "openai", Model: "gpt-5.6"},
		},
		Tries: []routing.Try{{
			ID: "key-a", Model: "gpt-5.6", Start: start,
			Millis: 900, Status: 200, Done: true,
		}},
	}}
	got := traceAttemptTarget(rv, rv.Tries[0])
	if got.accountID != "key-a" || got.model != "gpt-5.6" {
		t.Fatalf("model did not disambiguate within exact account: %+v", got)
	}
}

func TestReplayCacheKeyIncludesTryDetails(t *testing.T) {
	start := time.Date(2026, 10, 19, 5, 0, 0, 0, time.UTC)
	route := routing.Route{
		ID: 901, Time: start, Agent: "codex", Session: "s", Model: "openai/gpt-6",
		Provider: "openai", Done: true, Status: 200, Millis: 3000,
		HistoryDay: "2026-10-19",
		Tries: []routing.Try{{
			ID: "key-a", Model: "gpt-6", Start: start, Millis: 3000, Status: 200, Done: true,
		}},
	}
	before := routing.Snapshot{Routes: []routing.RouteView{{Route: route, Completeness: routing.Completeness{History: true}}}}
	firstKey, _ := replaySource(before, "2026-10-19", routing.RouteFilter{})

	changed := route
	changed.Tries = append([]routing.Try(nil), route.Tries...)
	changed.Tries[0].Start = start.Add(700 * time.Millisecond)
	changed.Tries[0].Millis = 2200
	after := routing.Snapshot{Routes: []routing.RouteView{{Route: changed, Completeness: routing.Completeness{History: true}}}}
	secondKey, _ := replaySource(after, "2026-10-19", routing.RouteFilter{})
	if firstKey == secondKey {
		t.Fatal("replay cache key ignored a Try start/duration update")
	}

	changed.Tries[0].Model = "gpt-5.6"
	changed.Tries[0].ID = "key-b"
	thirdKey, _ := replaySource(after, "2026-10-19", routing.RouteFilter{})
	if secondKey == thirdKey {
		t.Fatal("replay cache key ignored a Try target update")
	}

	changed.Tries[0].Done = false
	changed.Tries[0].Status = 0
	fourthKey, _ := replaySource(after, "2026-10-19", routing.RouteFilter{})
	if thirdKey == fourthKey {
		t.Fatal("replay cache key ignored a Try completion update")
	}
}

func TestReplaySyncFiltersDailyHistoryOnlyWhenSourceChanges(t *testing.T) {
	start := time.Date(2026, 10, 19, 5, 0, 0, 0, time.UTC)
	store := routing.NewStore()
	store.ReplaceHistory([]routing.Route{{
		ID: 902, Time: start, Agent: "codex", Model: "openai/gpt-6",
		Provider: "openai", Done: true, Status: 200, Millis: 1000,
		HistoryDay: start.Format("2006-01-02"),
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	model.setTab(tabHistory)
	model.SetHistoryDay(start.Format("2006-01-02"))
	model.syncReplay(store.Snapshot(), start)
	key := model.replay.builtKey
	if key == "" {
		t.Fatal("replay source was not built")
	}
	// Same committed snapshot and filters: the source walk must be skipped
	// even when a replay frame is emitted several times a second.
	model.syncReplay(store.Snapshot(), start.Add(5*time.Second))
	if model.replay.builtKey != key {
		t.Fatalf("replay key changed without source data: %q -> %q", key, model.replay.builtKey)
	}
	model.setStatus("failed", 3)
	model.syncReplay(store.Snapshot(), start.Add(6*time.Second))
	if model.replay.builtKey == key {
		t.Fatal("filter change did not invalidate the replay source")
	}
}

func TestFinalRouteFailureIgnoresRecoveredAttempt(t *testing.T) {
	start := time.Date(2026, 10, 19, 5, 30, 0, 0, time.UTC)
	recovered := routing.RouteView{Route: routing.Route{
		ID: 910, Time: start, Agent: "codex", Done: true, Status: 200, Millis: 1200,
		Tries: []routing.Try{
			{ID: "a", Model: "vendor/a", Start: start, Millis: 400, Status: 429, Fail: "rate"},
			{ID: "b", Model: "vendor/b", Start: start.Add(500 * time.Millisecond), Millis: 700, Status: 200, Done: true},
		},
	}}
	if got := finalRouteFailure(recovered); got != "" {
		t.Fatalf("recovered route reported final failure %q", got)
	}
	failed := recovered
	failed.Tries = append([]routing.Try(nil), recovered.Tries...)
	failed.Tries[len(failed.Tries)-1].Status = 500
	failed.Tries[len(failed.Tries)-1].Fail = "upstream"
	if got := finalRouteFailure(failed); got != "upstream" {
		t.Fatalf("final failure = %q, want upstream", got)
	}
}

func TestRoutingStatsCountRequestsNotTries(t *testing.T) {
	start := time.Date(2026, 10, 19, 5, 40, 0, 0, time.UTC)
	routes := []routing.RouteView{
		{Route: routing.Route{ID: 1, Time: start, Done: true, Status: 200, Tries: []routing.Try{
			{ID: "a", Start: start, Millis: 100, Status: 429, Fail: "rate", Rest: &routing.Rest{Why: "rate"}},
			{ID: "b", Start: start.Add(200 * time.Millisecond), Millis: 300, Status: 200, Done: true},
		}}},
		{Route: routing.Route{ID: 2, Time: start.Add(time.Second), Done: true, Status: 500, Tries: []routing.Try{
			{ID: "a", Start: start.Add(time.Second), Millis: 100, Status: 500, Fail: "upstream", Done: true},
		}}},
	}
	requests, rerouted, failed := routingStats(routes)
	if requests != 2 || rerouted != 1 || failed != 1 {
		t.Fatalf("routingStats = (%d,%d,%d), want (2,1,1)", requests, rerouted, failed)
	}
}

func TestRoutingModeEmptyMeansSmartOnlyWithObservedEvidence(t *testing.T) {
	cfg := routing.ConfigSnapshot{Providers: []routing.ProviderInfo{{
		ID: "openai", Name: "OpenAI", Routing: "",
	}}}
	withOrder := routing.RouteView{Route: routing.Route{
		Time: time.Now(), Model: "openai/gpt-6",
		Order: []routing.Weighed{{ID: "key-a", Provider: "openai", Model: "gpt-6"}},
	}}
	if got := stageRoutingMode(withOrder, true, cfg); got != "Smart" {
		t.Fatalf("empty order routing = %q, want Smart", got)
	}
	observedWithoutConfig := withOrder
	if got := stageRoutingMode(observedWithoutConfig, true, routing.ConfigSnapshot{}); got != "Smart" {
		t.Fatalf("observed order with no config = %q, want Smart", got)
	}
	configOnly := routing.RouteView{Route: routing.Route{
		Time: time.Now(), Model: "openai/gpt-6", Provider: "openai",
	}}
	if got := stageRoutingMode(configOnly, true, cfg); got != "Smart" {
		t.Fatalf("empty provider routing = %q, want Smart", got)
	}
	unknown := routing.RouteView{Route: routing.Route{Time: time.Now(), Model: "other/model"}}
	if got := stageRoutingMode(unknown, true, cfg); got != "routing ?" {
		t.Fatalf("unobserved mode = %q, want an honest unknown", got)
	}
	if got := stageRoutingMode(withOrder, false, cfg); got != "" {
		t.Fatalf("no selected route mode = %q, want empty", got)
	}
}

func TestRoutingModeNamesMatchUpstreamVocabulary(t *testing.T) {
	cases := map[string]string{
		"":       "Smart",
		"order":  "In order",
		"rotate": "In turn",
		"usage":  "Least used",
		"pace":   "Weekly pace",
		"manual": "Manual",
		"weight": "By weight",
	}
	for id, want := range cases {
		rv := routing.RouteView{Route: routing.Route{
			Group: &routing.GroupRef{ID: "g", Routing: id},
		}}
		if got := routeModeLabel(rv, true, "", true); got != want {
			t.Errorf("mode %q = %q, want %q", id, got, want)
		}
	}
}

func TestAccountStatusUsesUpstreamFailureAndRestWords(t *testing.T) {
	now := time.Date(2026, 10, 19, 6, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		row  providerNode
		want string
	}{
		{
			name: "went on with status and mapped reason",
			row: providerNode{
				WentOn: true, Status: "HTTP 500", Note: "rate",
			},
			want: "HTTP 500 · rate limited · went on to the next",
		},
		{
			name: "rest with reason and countdown",
			row: providerNode{
				Rest: true, RestWhy: "rate",
				RestUntil: now.Add(90 * time.Second),
			},
			want: "rate limited · back in 2m",
		},
		{
			name: "rest without separator when reason absent",
			row: providerNode{
				Rest: true, RestUntil: now.Add(30 * time.Second),
			},
			want: "back in 30s",
		},
		{
			name: "final failure keeps literal unknown reason",
			row: providerNode{
				Failed: true, Status: "HTTP 418", Note: "vendor-specific reason",
			},
			want: "HTTP 418 · vendor-specific reason",
		},
	}
	for _, c := range cases {
		if got := accountStatusText(c.row, now); got != c.want {
			t.Errorf("%s = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestRestingRowExpiresWhenClockPassesUntil(t *testing.T) {
	now := time.Now()
	row := providerNode{Rest: true, RestUntil: now.Add(time.Second)}
	if !accountResting(row, now) {
		t.Fatal("future rest was not treated as resting")
	}
	if accountResting(row, now.Add(2*time.Second)) {
		t.Fatal("expired rest stayed active")
	}
	if !accountResting(providerNode{Rest: true}, now) {
		t.Fatal("a rest without an end should remain resting")
	}
}

func TestProviderRowsUseReadOnlyConfigIdentity(t *testing.T) {
	rv := routing.RouteView{Route: routing.Route{
		ID: 920, Time: time.Now(), Agent: "codex", Model: "deepseek/deepseek-flash",
		Done: true, Status: 200,
		Order: []routing.Weighed{{
			ID: "key-a", Provider: "deepseek", Model: "deepseek-flash",
		}},
		Tries: []routing.Try{{
			ID: "key-a", Model: "deepseek/deepseek-flash", Status: 200, Done: true,
		}},
	}}
	cfg := routing.ConfigSnapshot{Providers: []routing.ProviderInfo{{
		ID: "deepseek", Name: "DeepSeek", Icon: "deepseek-color", Preset: "deepseek",
	}}}
	rows := providerRowsWithConfig(rv, false, cfg)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one", rows)
	}
	if rows[0].Name != "DeepSeek" || rows[0].Icon != "deepseek-color" {
		t.Fatalf("config identity = name %q icon %q", rows[0].Name, rows[0].Icon)
	}
}

func TestRoutingIconCacheIncludesSVGAndPNG(t *testing.T) {
	loadRoutingIcons()
	for _, key := range []string{"codex-color", "openai", "magpie", "crush"} {
		icon, ok := routingIconFor(key)
		if !ok {
			t.Fatalf("embedded icon %q was not parsed", key)
		}
		if icon.svg == nil && icon.bitmap == nil {
			t.Fatalf("icon %q has neither SVG nor bitmap", key)
		}
	}
	crush, ok := routingIconFor("crush")
	if !ok || crush.bitmap == nil {
		t.Fatal("upstream PNG icon Crush was not decoded")
	}
}

func TestAgentIconKeysUseRealUpstreamAssets(t *testing.T) {
	cases := map[string]string{
		"codex":    "codex-color",
		"claude":   "claudecode-color",
		"opencode": "opencode",
		"cursor":   "cursor",
	}
	for id, want := range cases {
		got := agentIcon(id)
		if got != want {
			t.Errorf("agentIcon(%q) = %q, want %q", id, got, want)
		}
		if _, ok := routingIconFor(got); !ok {
			t.Errorf("agent icon asset %q is missing", got)
		}
	}
}

func TestNarrowLayoutAndRequestSectionDoNotClip(t *testing.T) {
	model, _ := testModel(t, completedSyntheticSnapshot(t), Actions{})
	tester := render(t, model, 600, 900)
	tester.Frame()
	if !tester.HasText("Recent requests") {
		t.Fatalf("narrow request section disappeared: %v", tester.Texts())
	}
	if r, ok := tester.Find("Recent requests"); !ok || r.X < 0 || r.X+r.W > 600 {
		t.Fatalf("request heading outside narrow window: %+v present=%v", r, ok)
	}
	for _, label := range []string{"Live", "Today", "All", "Active", "Completed", "Failed"} {
		if r, ok := tester.Find(label); ok && (r.X < 0 || r.X+r.W > 600) {
			t.Fatalf("%s control clipped by narrow window: %+v", label, r)
		}
	}
}

func TestReplaySeekIntoARequestFliesItAgainFromTheAgent(t *testing.T) {
	start := time.Date(2026, 10, 19, 3, 10, 0, 0, time.UTC)
	route := routing.Route{
		ID: 89, Time: start, Agent: "codex", Model: "qoder/model",
		Provider: "qoder", Done: true, Status: 200, Millis: 2000,
		HistoryDay: start.Format("2006-01-02"),
		Tries: []routing.Try{{
			ID: "qoder", Model: "qoder/model", Start: start,
			Millis: 2000, Status: 200, Done: true,
		}},
	}
	store := routing.NewStore()
	store.ReplaceHistory([]routing.Route{route})
	s := store.Snapshot()
	model, _ := testModel(t, s, Actions{})
	model.setTab(tabHistory)
	model.SetHistoryDay(start.Format("2006-01-02"))
	tester := render(t, model, 1240, 900)
	// One second into a two-second request: as client.js's seek() plays
	// it, the bird leaves the agent again, then flies the rest of the
	// story (here, the answer that came a second later).
	model.seekReplay(1)
	tester.Frame()
	now := tester.Now()
	model.syncFlightTracks(model.snapshot(), model.snapshot().Routes, now)
	geometry, stage := stageFor(t, model, model.snapshot(), now)
	f := trackFor(t, model, route.ID)
	assertLeavesAgent(t, geometry, stage, f, now)
	if last := f.phases[len(f.phases)-1]; last.kind != flightInbound {
		t.Fatalf("restarted flight legs end with %+v, want the answer flying home", last)
	}
	if tester.FramePending() {
		t.Fatal("paused seek requested animation frames")
	}
}

func TestStageGeometryUsesUpstreamColumnsAndNarrowLane(t *testing.T) {
	agents := []agentNode{{ID: "codex"}, {ID: "claude"}}
	rows := []providerNode{{Provider: "one", Model: "m"}, {Provider: "two", Model: "m"}}
	stage := layoutRoutingStage(ui.Rect{W: 900, H: 180}, agents, rows)
	if stage.narrow {
		t.Fatal("900px stage chose the narrow layout")
	}
	if stage.agents[0].w != routingStageAgentWidth || stage.hub.w != routingStageHubWidth {
		t.Fatalf("wide columns = %.0f / %.0f, want %.0f / %.0f", stage.agents[0].w, stage.hub.w, routingStageAgentWidth, routingStageHubWidth)
	}
	if stage.rows[0].x-stage.hub.x-stage.hub.w != routingStageColumnGap {
		t.Fatalf("hub/provider gap = %.0f, want %.0f", stage.rows[0].x-stage.hub.x-stage.hub.w, routingStageColumnGap)
	}
	narrow := layoutRoutingStage(ui.Rect{W: 540, H: 260}, agents, rows)
	if !narrow.narrow {
		t.Fatal("540px stage did not choose the narrow layout")
	}
	if narrow.rows[0].y <= narrow.hub.y+narrow.hub.h {
		t.Fatal("narrow providers did not move below the top row")
	}
	if narrow.rows[0].x-narrow.agents[0].x < 12 {
		t.Fatal("narrow provider lane lost its left inset")
	}
}

func TestRoutingStageKeepsAllConcurrentNodes(t *testing.T) {
	now := time.Now()
	store := routing.NewStore()
	active := make([]routing.LiveRequest, 0, 8)
	for i := 0; i < 8; i++ {
		active = append(active, routing.LiveRequest{
			ID: int64(100 + i), Agent: "agent-" + itoa(i),
			Provider: "provider-" + itoa(i), Model: "provider-" + itoa(i) + "/model",
			RouteID: int64(200 + i), Started: now, Millis: 600,
		})
	}
	store.ApplyActive(active)
	model, _ := testModel(t, store.Snapshot(), Actions{})
	replay, replayActive := model.replayTopologyState()
	agents := model.stageAgents(model.snapshot(), routing.RouteView{}, false, nil, replay, replayActive, now)
	rows := model.stageAccountRows(routing.RouteView{}, false, model.snapshot().Live, nil, replayActive)
	if len(agents) != len(active) {
		t.Fatalf("stage agents = %d, want all %d concurrent agents", len(agents), len(active))
	}
	if len(rows) != len(active) {
		t.Fatalf("stage rows = %d, want all %d concurrent providers", len(rows), len(active))
	}
	height := model.routingStageHeight(model.snapshot(), nil, routing.RouteView{}, false, now)
	if height <= routingStageMinHeight {
		t.Fatalf("stage height = %v, want growth for concurrent nodes", height)
	}
	tester := render(t, model, 1240, 1200)
	model.mu.RLock()
	geometry := model.topology.flightGeometry
	model.mu.RUnlock()
	if geometry == nil || len(geometry.agents) != len(agents) || len(geometry.rows) != len(rows) {
		t.Fatalf("flight geometry = %+v, want all %d agents and %d rows", geometry, len(agents), len(rows))
	}

	model.syncFlightTracks(model.snapshot(), model.snapshot().Routes, now)
	model.mu.RLock()
	flights := append([]flightTrack(nil), model.topology.flights...)
	model.mu.RUnlock()
	if len(flights) != len(active) {
		t.Fatalf("flight tracks = %d, want %d", len(flights), len(active))
	}
	stage := layoutRoutingStage(ui.Rect{W: 900, H: height}, agents, rows)
	for _, flight := range flights {
		if indexAgentNode(agents, flight.agent) < 0 {
			t.Errorf("active flight agent %q has no node", flight.agent)
		}
		if indexProviderNode(rows, targetForPhase(flight, flightPhase{})) < 0 {
			t.Errorf("active flight provider %q has no node", flight.provider)
		}
	}
	if len(stage.agents) != len(agents) || len(stage.rows) != len(rows) {
		t.Fatalf("stage layout dropped nodes: %d agents, %d rows", len(stage.agents), len(stage.rows))
	}
	_ = tester
}

func TestCompletedHistoryShowsOrderTriesAndProvenance(t *testing.T) {
	model, _ := testModel(t, completedSyntheticSnapshot(t), Actions{})
	tester := render(t, model, 1240, 900)

	for _, want := range []string{
		"Completed history",
		"Completed history only", "Synthetic Provider", "Backup",
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
	// Presentation time runs until the final return leg lands; recorded
	// request duration itself remains 150ms.
	tester.Advance(5000 * time.Millisecond)
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
	if !tester.HasText("Accounts hidden") {
		t.Fatalf("masked button did not switch to upstream's Accounts hidden label: %v", tester.Texts())
	}
}

func TestAccountMaskUsesShapePreservingStandIns(t *testing.T) {
	raw := "chou@example.com"
	masked := maskWho(raw)
	if masked == raw || strings.Contains(masked, "chou") {
		t.Fatalf("maskWho leaked the account: %q", masked)
	}
	if len([]rune(masked)) != len([]rune(raw)) || strings.Count(masked, "@") != 1 || strings.Count(masked, ".") != 1 {
		t.Fatalf("maskWho did not preserve the address shape: %q", masked)
	}
	if again := maskWho(raw); again != masked {
		t.Fatalf("maskWho is not deterministic: %q then %q", masked, again)
	}
}

func TestAccountMaskMatchesMagpieUTF16StandIns(t *testing.T) {
	cases := map[string]string{
		"chou@example.com":  "dGluckBzc3VsaG9kLm5paA==",
		"张三@例子.中国":          "aGNAbGwuYWg=",
		"😀user@example.com": "dGltdHNtQGR1bXRpcm8udW9y",
		"a😀@b.com":          "cm5zQHMudGx1",
	}
	for in, want := range cases {
		got := base64.StdEncoding.EncodeToString([]byte(maskWho(in)))
		if got != want {
			t.Errorf("maskWho(%q) = %q (%s), want Magpie's %s", in, maskWho(in), got, want)
		}
	}
}

func TestAccountMaskTransitionMatchesRTPII(t *testing.T) {
	model, _ := testModel(t, routing.Snapshot{}, Actions{})
	model.setAccountMask(true)
	if blur, opacity := model.accountMaskStyle(time.Now()); blur != 2.2 || opacity != 0.5 {
		t.Fatalf("restored mask style = %.2f/%.2f, want static 2.2/0.5", blur, opacity)
	}
	start := time.Now()
	model.mu.Lock()
	model.maskRevealAt = start
	model.mu.Unlock()
	if blur, opacity := model.accountMaskStyle(start); blur != 0 || opacity != 1 {
		t.Fatalf("rtpii at 0ms = %.2f/%.2f, want 0/1", blur, opacity)
	}
	mid := start.Add(175 * time.Millisecond)
	blur, opacity := model.accountMaskStyle(mid)
	if blur <= 0 || blur >= 2.2 || opacity >= 1 || opacity <= 0.5 {
		t.Fatalf("rtpii at 175ms = %.2f/%.2f, want an in-between state", blur, opacity)
	}
	if blur, opacity := model.accountMaskStyle(start.Add(350 * time.Millisecond)); blur != 2.2 || opacity != 0.5 {
		t.Fatalf("rtpii final = %.2f/%.2f, want 2.2/0.5", blur, opacity)
	}
	if model.accountMaskAnimating(start.Add(350 * time.Millisecond)) {
		t.Fatal("mask transition still requested frames after 350ms")
	}
	model.toggleAccountMask()
	if model.accountsMasked() {
		t.Fatal("mask toggle did not turn masking off")
	}
	if blur, opacity := model.accountMaskStyle(start.Add(175 * time.Millisecond)); blur != 0 || opacity != 1 {
		t.Fatalf("unmask style = %.2f/%.2f, want immediate 0/1", blur, opacity)
	}
}

func TestAccountMaskToggleRequestsOnlyTransitionFrames(t *testing.T) {
	route := routing.RouteView{
		Route: routing.Route{
			ID: 44, Time: time.Now().Add(-time.Minute), Agent: "codex",
			Model: "synthetic/model", Done: true, Status: 200,
			Order: []routing.Weighed{{
				ID: "acct", Provider: "synthetic", Name: "Synthetic",
				Who: "private@example.test", Model: "model",
			}},
		},
		CandidateOrderKnown: true,
	}
	model, _ := testModel(t, routing.Snapshot{Routes: []routing.RouteView{route}}, Actions{})
	tester := render(t, model, 1240, 820)
	tester.Frame()
	if tester.FramePending() {
		t.Fatal("idle route requested a frame before masking")
	}
	if err := tester.Click("Hide accounts"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	model.mu.RLock()
	revealing := model.accountMaskAnimating(tester.Now())
	model.mu.RUnlock()
	if !revealing && tester.Now().Sub(model.maskRevealAt) < 350*time.Millisecond {
		// The test clock may already be past the transition; at minimum the
		// click must have scheduled its 350ms animation window.
		t.Fatal("mask toggle did not start the rtpii transition")
	}
	tester.Advance(400 * time.Millisecond)
	tester.Frame()
	if tester.FramePending() {
		t.Fatal("masked page kept requesting frames after rtpii")
	}
}

// Masked labels used to be retained Elements with Blur(). MyGo paints an
// Element.Blur through a fresh offscreen layer every retained-tree repaint,
// so a single request flight rebuilt several blur textures at 60 FPS. Keep
// checking that the mask reaches those labels while the stage stays an
// ordinary cached Painter layer.
func TestAccountMaskPaintsStandInsWithoutPerFrameBlurLayers(t *testing.T) {
	route := routing.RouteView{
		Route: routing.Route{
			ID: 45, Time: time.Now().Add(-time.Minute), Agent: "codex",
			Model: "synthetic/model", Done: true, Status: 200,
			Order: []routing.Weighed{{
				ID: "acct", Provider: "synthetic", Name: "Synthetic",
				Who: "private@example.test", Model: "model",
			}},
			Tries: []routing.Try{{
				ID: "acct", Model: "synthetic/model", Done: true, Status: 200,
			}},
		},
		CandidateOrderKnown: true,
	}
	model, _ := testModel(t, routing.Snapshot{Routes: []routing.RouteView{route}}, Actions{})
	model.setAccountMask(true)
	tester := render(t, model, 1240, 820)
	tester.Frame()
	if tester.FramePending() {
		t.Fatal("stable masked stage kept asking for frames")
	}
	if tester.HasText("private@example.test") {
		t.Fatalf("masked labels leaked the account: %v", tester.Texts())
	}
	if got := model.routeAccessibilityLabel(route); strings.Contains(got, "private") {
		t.Fatalf("masked accessibility label leaked the account: %q", got)
	}
}

func TestAccountMaskCoversRequestDestinationAndAccessibility(t *testing.T) {
	route := routing.RouteView{
		Route: routing.Route{
			ID: 12, Time: time.Now(), Agent: "codex", Model: "synthetic/model",
			Done: true, Status: 200,
			Order: []routing.Weighed{{
				ID: "acct", Provider: "synthetic", Name: "Synthetic",
				Who: "private@example.test", Model: "model",
			}},
			Tries: []routing.Try{{
				ID: "acct", Model: "synthetic/model", Done: true, Status: 200,
			}},
		},
		CandidateOrderKnown: true,
	}
	model, _ := testModel(t, routing.Snapshot{Routes: []routing.RouteView{route}}, Actions{})
	model.setAccountMask(true)
	tester := render(t, model, 1240, 820)
	if tester.HasText("private@example.test") {
		t.Fatalf("masked request list leaked the account: %v", tester.Texts())
	}
	if got := model.routeAccessibilityLabel(route); strings.Contains(got, "private") {
		t.Fatalf("masked accessibility label leaked the account: %q", got)
	}
}

func TestAccountMaskPersistsInAppSettings(t *testing.T) {
	route := routing.RouteView{
		Route: routing.Route{
			ID: 13, Time: time.Now(), Agent: "codex", Model: "synthetic/model",
			Done: true, Status: 200,
			Order: []routing.Weighed{{
				ID: "acct", Provider: "synthetic", Name: "Synthetic",
				Who: "private@example.test", Model: "model",
			}},
		},
		CandidateOrderKnown: true,
	}
	var saved config.AppSettings
	var model *Model
	model, _ = testModel(t, routing.Snapshot{Routes: []routing.RouteView{route}}, Actions{
		SaveSettings: func(cfg config.AppSettings) error {
			saved = cfg
			model.SetSettings(cfg)
			return nil
		},
	})
	tester := render(t, model, 1240, 820)
	if err := tester.Click("Hide accounts"); err != nil {
		t.Fatal(err)
	}
	if !saved.HideAccounts || !model.accountsMasked() {
		t.Fatalf("mask preference was not persisted: saved=%t model=%t", saved.HideAccounts, model.accountsMasked())
	}
	reloaded := NewModel(model.store, saved, model.paths, model.actualMagpieDir, Actions{})
	if !reloaded.accountsMasked() {
		t.Fatal("mask preference was not restored from settings")
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

func TestProviderRowsFoldTryIntoExactOrderSeat(t *testing.T) {
	start := time.Date(2026, 10, 19, 7, 0, 0, 0, time.UTC)
	rv := routing.RouteView{Route: routing.Route{
		ID: 1201, Time: start, Agent: "codex", Model: "openai/gpt-6",
		Done: true, Status: 200, Order: []routing.Weighed{
			{ID: "key-a", Provider: "openai", Name: "OpenAI", Who: "alice", Model: "gpt-6"},
			{ID: "key-b", Provider: "openai", Name: "OpenAI", Who: "bob", Model: "gpt-5.6"},
		},
		Tries: []routing.Try{{
			ID: "key-b", Model: "openai/gpt-5.6", Start: start,
			Millis: 200, Status: 200, Done: true,
		}},
	}}
	rows := providerRows(rv, false)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want exactly two order seats", rows)
	}
	if rows[1].AccountID != "key-b" || rows[1].Model != "gpt-5.6" {
		t.Fatalf("try was not folded into its exact seat: %+v", rows[1])
	}
	if !rows[1].Answered {
		t.Fatal("the exact seat to which the try went is not marked answered")
	}
}

func TestProviderRowsKeepUnmatchedAccountAsExplicitTry(t *testing.T) {
	start := time.Date(2026, 10, 19, 7, 10, 0, 0, time.UTC)
	rv := routing.RouteView{Route: routing.Route{
		ID: 1202, Time: start, Agent: "codex", Model: "openai/gpt-6",
		Done: true, Status: 200, Order: []routing.Weighed{{
			ID: "key-a", Provider: "openai", Name: "OpenAI", Model: "gpt-6",
		}},
		Tries: []routing.Try{{
			ID: "key-z", Model: "openai/gpt-6", Start: start,
			Millis: 200, Status: 200, Done: true,
		}},
	}}
	rows := providerRows(rv, false)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want the order row plus the unmatched observed try", rows)
	}
	if rows[1].AccountID != "key-z" {
		t.Fatalf("unmatched try account = %q, want key-z", rows[1].AccountID)
	}
}

func TestMagpieFlipPlacesBeakOnFlightSide(t *testing.T) {
	pal := colors(nil, config.ThemeDark)
	renderBird := func(flip float32) *image.RGBA {
		return ui.NewTester(func(c *ui.Context) {
			ui.Box(c).Fill().Background(ui.RGB(255, 255, 255)).Draw(func(p *ui.Painter, r ui.Rect) {
				drawMagpie(p, magpiePose{
					x: r.X + r.W/2, y: r.Y + r.H/2, scale: 2, flip: flip,
					body: pal.BirdBody, belly: pal.BirdBelly, far: pal.BirdWing,
					appear: 1,
				}, ui.Hex("#6366f1"), magpieCarryNone, packetOutcomeUnknown, true)
			})
		}, 180, 140).Image()
	}
	right, left := renderBird(1), renderBird(-1)
	silhouette := func(img *image.RGBA) (float64, int) {
		var sum float64
		n := 0
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				i := img.PixOffset(x, y)
				if img.Pix[i] < 220 || img.Pix[i+1] < 220 || img.Pix[i+2] < 220 {
					sum += float64(x)
					n++
				}
			}
		}
		if n == 0 {
			return 0, 0
		}
		return sum / float64(n), n
	}
	rightBird, rn := silhouette(right)
	leftBird, ln := silhouette(left)
	if rn == 0 || ln == 0 {
		t.Fatalf("bird pixels missing: right=%d left=%d", rn, ln)
	}
	if !(leftBird > rightBird+4) {
		t.Fatalf("flipped bird centroid = %.1f, right-facing = %.1f; bird did not face the flight direction", leftBird, rightBird)
	}
}

func TestRestingCountdownRequestsOnlyIdleRepaint(t *testing.T) {
	now := time.Now()
	rows := []providerNode{{Rest: true, RestUntil: now.Add(90 * time.Second)}}
	if !restingCountdownVisible(rows, now) {
		t.Fatal("active countdown did not request a timed repaint")
	}
	if restingCountdownVisible(rows, now.Add(2*time.Minute)) {
		t.Fatal("expired countdown kept requesting frames")
	}
	if restingCountdownVisible([]providerNode{{Rest: true}}, now) {
		t.Fatal("a rest with no known end is static and needs no countdown frame")
	}
}

func TestStageDoesNotAppendSelectedConcurrencyAliasAsAccount(t *testing.T) {
	now := time.Now()
	route := routing.RouteView{Route: routing.Route{
		ID: 1300, Time: now, Agent: "codex", Model: "group/auto-model",
		Provider: "qoder",
		Order: []routing.Weighed{{
			ID: "qoder", Provider: "qoder", Name: "Qoder", Model: "dfmodel", Who: "alice",
		}},
		Tries: []routing.Try{{
			ID: "qoder", Model: "qoder/dfmodel", Start: now, Done: false,
		}},
	}}
	route.Active = true
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 9, RouteID: route.ID, Agent: "codex", Provider: "qoder",
		Model: "group/auto-model", Started: now, Phase: routing.PhaseUpstream,
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	snap := model.snapshot()
	snap.Routes = []routing.RouteView{route}
	rows := model.stageAccountRows(route, true, snap.Live, nil, false)
	if len(rows) != 1 {
		t.Fatalf("selected live stage rows = %+v, want only the trace account", rows)
	}
	if rows[0].Model != "dfmodel" || rows[0].AccountID != "qoder" {
		t.Fatalf("stage row = %+v, want the real qoder/dfmodel seat", rows[0])
	}
}

func TestOutcomeUsesOrderWhoInsteadOfRepeatingModel(t *testing.T) {
	start := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	rv := routing.RouteView{Route: routing.Route{
		ID: 1401, Time: start, Agent: "codex", Model: "group/auto-deepseek-flash",
		Provider: "qoder", Done: true, Status: 200,
		Order: []routing.Weighed{
			{ID: "qoder", Provider: "qoder", Name: "Qoder", Who: "user@example.test", Model: "dfmodel", Kind: "account"},
			{ID: "deepseek", Provider: "deepseek", Name: "DeepSeek", Model: "deepseek-flash", Kind: "provider"},
		},
		Tries: []routing.Try{{
			ID: "qoder", Model: "dfmodel", Served: "dfmodel", Start: start,
			Millis: 1200, Status: 200, Done: true,
		}},
	}}
	said, how, _ := routeOutcome(rv)
	if how != "ok" || said != "user@example.test · dfmodel" {
		t.Fatalf("outcome = %q/%q, want the real account and served model once", said, how)
	}
	if strings.Contains(said, "qoder") || said == "dfmodel · dfmodel" {
		t.Fatalf("outcome repeated internal or duplicate identity: %q", said)
	}
}

func TestAccountTallyUsesOrderSeatNotUnmatchedTry(t *testing.T) {
	start := time.Date(2026, 10, 19, 12, 10, 0, 0, time.UTC)
	rv := routing.RouteView{Route: routing.Route{
		ID: 1402, Time: start, Agent: "codex", Model: "group/auto-deepseek-flash",
		Provider: "qoder", Done: true, Status: 200,
		Order: []routing.Weighed{{
			ID: "qoder", Provider: "qoder", Name: "Qoder", Who: "user@example.test",
			Model: "dfmodel", Kind: "account",
		}},
		Tries: []routing.Try{{
			ID: "qoder", Model: "dfmodel", Start: start,
			Millis: 1200, Status: 200, Done: true,
		}},
	}}
	rows := accountTally([]routing.RouteView{rv})
	if len(rows) != 1 {
		t.Fatalf("account tally = %+v, want exactly one recorded seat", rows)
	}
	if rows[0].Who != "user@example.test" || rows[0].Model != "dfmodel" || rows[0].OK != 1 {
		t.Fatalf("account tally identity = %+v", rows[0])
	}
}

func TestLiveRoutesAreLimitedToOriginalSixtyWindow(t *testing.T) {
	routes := make([]routing.RouteView, 0, liveRouteLimit+5)
	base := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	for i := 0; i < liveRouteLimit+5; i++ {
		routes = append(routes, routing.RouteView{Route: routing.Route{
			ID: int64(i + 1), Time: base.Add(time.Duration(i) * time.Second),
			Agent: "codex", Model: "m", Done: true, Status: 200,
			HistoryDay: "2026-10-19",
		}, Completeness: routing.Completeness{History: true}})
	}
	model, _ := testModel(t, routing.Snapshot{Routes: routes}, Actions{})
	got := model.visibleRoutes(model.snapshot(), false)
	if len(got) != liveRouteLimit {
		t.Fatalf("live routes = %d, want the original 60-row map window", len(got))
	}
	model.setTab(tabHistory)
	model.SetHistoryDay("2026-10-19")
	day := model.visibleRoutes(model.snapshot(), false)
	if len(day) != len(routes) {
		t.Fatalf("history day routes = %d, want all %d", len(day), len(routes))
	}
}

func TestToolbarTabsSwitchRealViews(t *testing.T) {
	model, _ := testModel(t, routing.Snapshot{
		GatewayOnline: true, LastPoll: time.Now(),
	}, Actions{})
	tester := render(t, model, 1240, 820)
	tester.SetTitleBar(ui.TitleBar{Height: 50, Left: 82})
	tester.Frame()
	if err := tester.Click("History"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if got := model.tabKind(); got != tabHistory {
		t.Fatalf("toolbar History selected tab %v", got)
	}
	if err := tester.Click("Groups"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if got := model.tabKind(); got != tabGroups {
		t.Fatalf("toolbar Groups selected tab %v", got)
	}
	if err := tester.Click("Live"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if got := model.tabKind(); got != tabLive {
		t.Fatalf("toolbar Live selected tab %v", got)
	}
}

func TestNamedAndPrintfLocalizationPlaceholders(t *testing.T) {
	en := NewLocalization("en")
	if got := en.F("routing.listScope", 60); got != "the last 60 the gateway keeps" {
		t.Fatalf("named placeholder = %q", got)
	}
	if got := en.F("routing.accountTally.tried", 7); got != "tried 7" {
		t.Fatalf("printf placeholder = %q", got)
	}
}

func TestPlayRouteSeeksWithinAlreadyShownDay(t *testing.T) {
	day := "2026-10-18"
	base := time.Date(2026, 10, 18, 10, 0, 0, 0, time.Local)
	store := routing.NewStore()
	store.ReplaceHistory([]routing.Route{
		{ID: 1, Time: base, Agent: "codex", Model: "m", Done: true, Millis: 1000, HistoryDay: day,
			Tries: []routing.Try{{Model: "p/m", Done: true, Millis: 1000, Start: base}}},
		{ID: 2, Time: base.Add(30 * time.Second), Agent: "codex", Model: "m", Done: true, Millis: 1000, HistoryDay: day,
			Tries: []routing.Try{{Model: "p/m", Done: true, Millis: 1000, Start: base.Add(30 * time.Second)}}},
	})
	snap := store.Snapshot()
	model, _ := testModel(t, snap, Actions{})
	model.setTab(tabHistory)
	model.SetHistoryDay(day)
	now := time.Now()
	model.syncReplay(snap, now)
	model.PlayRoute(day, 2)
	model.syncReplay(snap, now)
	if got := model.replay.position; got != 30*time.Second {
		t.Fatalf("position = %v, want 30s", got)
	}
	if model.replay.pendingID != 0 || model.replay.playing {
		t.Fatalf("pendingID = %d playing = %t, want consumed and paused", model.replay.pendingID, model.replay.playing)
	}
}

func TestStaticStageBitmapKeyCoversDrawnInputs(t *testing.T) {
	stage := &routingStagePresentation{stageIdentity: "id"}
	rows := []providerNode{{Provider: "p"}, {Provider: "q"}}
	now := time.Now()
	key := func(loc Localization, online bool, url, mode string, lit map[int]bool) string {
		return staticStageBitmapKey(stage, flightStage{}, palette{}, rows, now, 0, 1, loc, online, url, mode, lit, nil, 900, 400)
	}
	en, zh := NewLocalization("en"), NewLocalization("zh")
	base := key(en, true, "http://a", "smart", map[int]bool{0: true})
	if base != key(en, true, "http://a", "smart", map[int]bool{0: true}) {
		t.Fatal("key changed with identical inputs")
	}
	for name, other := range map[string]string{
		"language": key(zh, true, "http://a", "smart", map[int]bool{0: true}),
		"online":   key(en, false, "http://a", "smart", map[int]bool{0: true}),
		"url":      key(en, true, "http://b", "smart", map[int]bool{0: true}),
		"mode":     key(en, true, "http://a", "order", map[int]bool{0: true}),
		"litRows":  key(en, true, "http://a", "smart", map[int]bool{1: true}),
	} {
		if other == base {
			t.Errorf("key ignores %s", name)
		}
	}
	// A resize must rebuild the bitmap even when the hub keeps its size.
	wide := staticStageBitmapKey(stage, flightStage{}, palette{}, rows, now, 0, 1, en, true, "http://a", "smart", nil, nil, 1000, 400)
	narrower := staticStageBitmapKey(stage, flightStage{}, palette{}, rows, now, 0, 1, en, true, "http://a", "smart", nil, nil, 800, 400)
	taller := staticStageBitmapKey(stage, flightStage{}, palette{}, rows, now, 0, 1, en, true, "http://a", "smart", nil, nil, 1000, 500)
	if wide == narrower || wide == taller {
		t.Error("key ignores stage size")
	}
}
