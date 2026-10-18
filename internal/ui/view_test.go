package ui

import (
	"image"
	"math"
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
	if !tester.FramePending() {
		t.Fatal("an in-flight request did not request the next animation frame")
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

func TestStaticStageCanvasReusedAcrossFlightFrames(t *testing.T) {
	now := time.Now()
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 9, Agent: "codex", Provider: "qoder", RouteID: 1703,
		Started: now, Millis: 600,
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	tester := render(t, model, 1240, 820)
	model.mu.RLock()
	canvas := model.topology.stageCanvas
	key := model.topology.stageCanvasKey
	model.mu.RUnlock()
	if canvas == nil {
		t.Fatal("stage canvas was not created")
	}
	tester.Advance(16 * time.Millisecond)
	tester.Frame()
	model.mu.RLock()
	sameCanvas := model.topology.stageCanvas == canvas
	sameKey := model.topology.stageCanvasKey == key
	model.mu.RUnlock()
	if !sameCanvas || !sameKey {
		t.Fatal("static stage canvas was redrawn during a bird-only frame")
	}
}

func TestStageCanvasResizesWithWindowAndDPI(t *testing.T) {
	now := time.Now()
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 19, Agent: "codex", Provider: "qoder", RouteID: 1710,
		Started: now, Millis: 600,
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	tester := render(t, model, 1000, 760)
	model.mu.RLock()
	before := model.topology.stageCanvas
	beforeW, beforeH := model.topology.stageCanvasW, model.topology.stageCanvasH
	model.mu.RUnlock()
	if before == nil || beforeW <= 0 || beforeH != 258 {
		t.Fatalf("initial canvas = %v (%dx%d), want the stage card raster", before != nil, beforeW, beforeH)
	}

	tester.SetSize(1240, 900)
	tester.Frame()
	model.mu.RLock()
	afterResize := model.topology.stageCanvas
	resizedW, resizedH := model.topology.stageCanvasW, model.topology.stageCanvasH
	model.mu.RUnlock()
	if afterResize == before {
		t.Fatal("window resize reused a canvas with the old raster size")
	}
	if resizedW <= beforeW || resizedH != beforeH {
		t.Fatalf("resized canvas = %dx%d, want wider than %dx%d", resizedW, resizedH, beforeW, beforeH)
	}
	if gotW, gotH := afterResize.Size(); gotW != resizedW || gotH != resizedH {
		t.Fatalf("actual canvas bitmap = %dx%d, tracked %dx%d", gotW, gotH, resizedW, resizedH)
	}

	tester.SetScale(2)
	tester.Frame()
	model.mu.RLock()
	afterScale := model.topology.stageCanvas
	scaledW, scaledH := model.topology.stageCanvasW, model.topology.stageCanvasH
	model.mu.RUnlock()
	if afterScale == afterResize {
		t.Fatal("DPI change reused a canvas rendered for the old scale")
	}
	if scaledW != resizedW*2 || scaledH != resizedH*2 {
		t.Fatalf("High-DPI canvas = %dx%d, want %dx%d", scaledW, scaledH, resizedW*2, resizedH*2)
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
	pal := colors(nil, config.ThemeDark)
	render := func(phase float32) *image.RGBA {
		return ui.NewTester(func(c *ui.Context) {
			ui.Box(c).Fill().Background(ui.RGB(255, 255, 255)).Draw(func(p *ui.Painter, r ui.Rect) {
				wing, skew := wingFrame(phase)
				farWing, farSkew := wingFrame(phase - 0.03/0.42)
				p.Transform(ui.Translation(r.X+r.W/2, r.Y+r.H/2), 0, 0, func() {
					p.Transform(ui.Scaling(5, 5), 0, 0, func() {
						drawMagpieShape(p, ui.RGB(20, 20, 20), ui.RGB(255, 255, 255),
							pal.BirdWing, wing, skew, farWing, farSkew, 1)
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

func TestStageCanvasMatchesHighDPIRaster(t *testing.T) {
	now := time.Now()
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 12, Agent: "codex", Provider: "qoder", RouteID: 1704,
		Started: now, Millis: 600,
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	tester := ui.NewTester(model.View, 1240, 820)
	tester.SetScale(2)
	tester.SetScale(1)
	model.mu.RLock()
	w, h := model.topology.stageCanvasW, model.topology.stageCanvasH
	model.mu.RUnlock()
	if w <= 0 || h <= 0 {
		t.Fatalf("stage canvas has no DIP size: %dx%d", w, h)
	}
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
	windows := buildReplayFlightWindows(timeline, routes)
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
	model.replay.windows = buildReplayFlightWindows(model.replay.timeline, routes)
	model.replay.windowEnds = replayWindowEnds(model.replay.windows)
	model.replay.position = 500 * time.Millisecond
	model.replay.playing = false
	model.mu.Unlock()
	tester := render(t, model, 1240, 900)
	tester.Frame()
	if tester.FramePending() {
		t.Fatal("a paused history replay requested animation frames")
	}
}

func TestReplaySeekIntoReturnRebuildsInboundFlight(t *testing.T) {
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
	routes := store.Snapshot().Routes
	attempts, total := traceFlightAttempts(routes[0])
	phases := buildFlightPhases(attempts, total, true)
	inbound := phases[len(phases)-1]
	if inbound.kind != flightInbound {
		t.Fatalf("last phase = %+v, want inbound", inbound)
	}
	position := time.Duration(route.Millis)*time.Millisecond + 200*time.Millisecond
	if position <= total || position >= flightPhaseEnd(phases) {
		t.Fatalf("seek position %v is not inside return leg %v..%v", position, total, flightPhaseEnd(phases))
	}
	model.seekReplay(position.Seconds())
	tester.Frame()
	model.mu.RLock()
	flights := append([]flightTrack(nil), model.topology.flights...)
	model.mu.RUnlock()
	if len(flights) != 1 {
		t.Fatalf("direct seek reconstructed %d flights, want 1", len(flights))
	}
	track := flights[0]
	moment := flightMomentAt(track, start.Add(position))
	if track.routeID != route.ID || !track.done || moment.phase.kind != flightInbound {
		t.Fatalf("direct seek flight = %+v, moment = %+v; want inbound for route %d", track, moment, route.ID)
	}
	stage, active := model.replayTopologyState()
	if !active || len(stage.routes) != 1 || stage.routes[0].ID != route.ID {
		t.Fatalf("seek stage = %+v, active=%v; want route %d", stage.routes, active, route.ID)
	}
	if !tester.HasText("Claude Code") || tester.FramePending() {
		t.Fatalf("paused seek rendered an unaddressable or animating stage: texts=%v pending=%v", tester.Texts(), tester.FramePending())
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
	agents := model.stageAgents(model.snapshot(), routing.RouteView{}, false, nil, replay, replayActive)
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
	canvasH := model.topology.stageCanvasH
	model.mu.RUnlock()
	if canvasH < int(height) {
		t.Fatalf("stage canvas height = %d, want at least %.0f for all concurrent nodes", canvasH, height)
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
		if indexProviderNode(rows, flight.provider) < 0 {
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
	tester.Advance(4500 * time.Millisecond)
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
