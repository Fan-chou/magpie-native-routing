package ui

import (
	"math"
	"testing"
	"time"

	"github.com/Fan-chou/magpie-native-routing/internal/routing"
	"github.com/egoist/mygo/ui"
)

// stageFor lays out the stage the view would show for a snapshot at now and
// returns its flight geometry, as the live layer resolves it.
func stageFor(t *testing.T, model *Model, s routing.Snapshot, now time.Time) (*flightGeometry, *routingStagePresentation) {
	t.Helper()
	routes := model.visibleRoutes(s, false)
	sel, ok := model.selectedRoute(routes)
	stage := model.routingStagePresentationAt(s, routes, sel, ok, now)
	const width = 1000
	height := model.routingStageHeightForPresentation(width, stage)
	return model.routingStageGeometryKeyed(ui.Rect{W: width, H: height}, stage.agents, stage.rows, stage.stageIdentity), stage
}

func trackFor(t *testing.T, model *Model, id int64) flightTrack {
	t.Helper()
	model.mu.RLock()
	defer model.mu.RUnlock()
	for _, f := range model.topology.flights {
		if f.routeID == id {
			return f
		}
	}
	t.Fatalf("no flight track for route %d", id)
	return flightTrack{}
}

// assertLeavesAgent checks the user's rule: a bird appears at the agent's
// end of its wire, never part way along it.
func assertLeavesAgent(t *testing.T, geometry *flightGeometry, stage *routingStagePresentation, f flightTrack, at time.Time) {
	t.Helper()
	moment := flightMomentAt(f, at)
	if !moment.hasPhase || moment.phase.kind != flightOutbound || moment.local > 20*time.Millisecond {
		t.Fatalf("first moment = %+v, want the outbound leg just begun", moment)
	}
	ai := indexAgentNode(stage.agents, f.agent)
	x, y, ok := flightPacketPoint(geometry, ai, f, at)
	if !ok {
		t.Fatal("first frame draws no packet")
	}
	sx, sy, _, _ := geometry.agentHub[ai].at(0)
	if d := math.Hypot(float64(x-sx), float64(y-sy)); d > 4 {
		t.Fatalf("bird first drawn %.1fpx from the agent end of its wire", d)
	}
}

func TestLiveBirdLeavesAgentWhenFirstSeenLate(t *testing.T) {
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.Local)
	store := routing.NewStore()
	// The Gateway is polled once a second: this request is first seen
	// 700ms after it started, half way through upstream's 1.4s leg.
	store.ApplyActive([]routing.LiveRequest{{
		ID: 1, Agent: "codex", Provider: "qoder", Model: "qoder/m", RouteID: 501,
		Started: now.Add(-700 * time.Millisecond), Millis: 700,
	}})
	s := store.Snapshot()
	model, _ := testModel(t, s, Actions{})
	model.syncFlightTracks(s, s.Routes, now)
	geometry, stage := stageFor(t, model, s, now)
	assertLeavesAgent(t, geometry, stage, trackFor(t, model, 501), now)
}

func TestLiveBirdLeavesAgentWhenAppOpensOnOldRequest(t *testing.T) {
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.Local)
	start := now.Add(-30 * time.Second)
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 1, Agent: "codex", Provider: "beta", Model: "beta/m", Session: "s1",
		RouteID: 502, Started: start, Millis: 30000,
	}})
	store.ApplySessionRoute(routing.SessionRouteLike{
		ID: 502, Time: start, Asked: "group/x", Model: "beta/m",
		Tries: []routing.TryLike{
			{Model: "alpha/m", Done: true, Status: 500, Fail: "server", Millis: 4000},
			{Model: "beta/m"},
		},
	})
	s := store.Snapshot()
	model, _ := testModel(t, s, Actions{})
	model.syncFlightTracks(s, s.Routes, now)
	geometry, stage := stageFor(t, model, s, now)
	f := trackFor(t, model, 502)
	assertLeavesAgent(t, geometry, stage, f, now)
	// Like resume() → play(), it then flies the recorded story in order:
	// to the failed account, back to magpie, on to the one answering now.
	var kinds []flightPhaseKind
	for _, ph := range f.phases {
		kinds = append(kinds, ph.kind)
	}
	want := []flightPhaseKind{flightOutbound, flightHold, flightRetryBack, flightRetryOut, flightHold}
	if len(kinds) != len(want) {
		t.Fatalf("legs = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("legs = %v, want %v", kinds, want)
		}
	}
	assertContinuous(t, geometry, stage, func(time.Time) flightTrack { return f }, now, 6*time.Second)
}

// assertContinuous samples the packet every frame and fails on a jump no
// leg could make in one frame.
func assertContinuous(t *testing.T, geometry *flightGeometry, stage *routingStagePresentation, track func(time.Time) flightTrack, from time.Time, span time.Duration) {
	t.Helper()
	const frame = 16 * time.Millisecond
	var px, py float32
	have := false
	for at := from; at.Before(from.Add(span)); at = at.Add(frame) {
		f := track(at)
		x, y, ok := flightPacketPoint(geometry, indexAgentNode(stage.agents, f.agent), f, at)
		if !ok {
			have = false
			continue
		}
		if have {
			if d := math.Hypot(float64(x-px), float64(y-py)); d > 30 {
				t.Fatalf("packet jumped %.1fpx at +%v", d, at.Sub(from))
			}
		}
		px, py, have = x, y, true
	}
}

func TestLiveBirdWaitsAtMagpieThenFliesOnWhenTriesArrive(t *testing.T) {
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.Local)
	store := routing.NewStore()
	live := routing.LiveRequest{
		ID: 1, Agent: "codex", Session: "s1", RouteID: 503, Started: now, Millis: 10,
	}
	store.ApplyActive([]routing.LiveRequest{live})
	s := store.Snapshot()
	model, _ := testModel(t, s, Actions{})
	model.syncFlightTracks(s, s.Routes, now)
	before := trackFor(t, model, 503)
	if len(before.phases) != 1 || !before.phases[0].toHub || before.phases[0].end != liveToHubDuration {
		t.Fatalf("legs before routing picked = %+v, want one 620ms leg to magpie", before.phases)
	}

	// Two seconds later the Gateway reports which account was picked.
	arrive := now.Add(2 * time.Second)
	store.ApplySessionRoute(routing.SessionRouteLike{
		ID: 503, Time: now, Asked: "qoder/m", Model: "qoder/m",
		Tries: []routing.TryLike{{Model: "qoder/m"}},
	})
	s2 := store.Snapshot()
	model.SetSnapshot(s2)
	model.syncFlightTracks(s2, s2.Routes, arrive)
	after := trackFor(t, model, 503)
	geometry, stage := stageFor(t, model, s2, arrive)
	if m := flightMomentAt(after, arrive.Add(time.Millisecond)); m.phase.kind != flightHubOut {
		t.Fatalf("moment once picked = %+v, want the hub-out leg", m)
	}
	assertContinuous(t, geometry, stage, func(at time.Time) flightTrack {
		if at.Before(arrive) {
			return before
		}
		return after
	}, now, 5*time.Second)
}

func TestLiveBirdFinishesItsLegBeforeFlyingHome(t *testing.T) {
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.Local)
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 1, Agent: "codex", Provider: "qoder", Model: "qoder/m", Session: "s1",
		RouteID: 504, Started: now, Millis: 10,
	}})
	store.ApplySessionRoute(routing.SessionRouteLike{
		ID: 504, Time: now, Asked: "qoder/m", Model: "qoder/m",
		Tries: []routing.TryLike{{Model: "qoder/m"}},
	})
	s := store.Snapshot()
	model, _ := testModel(t, s, Actions{})
	model.syncFlightTracks(s, s.Routes, now)
	before := trackFor(t, model, 504)
	geometry, stage := stageFor(t, model, s, now)

	// The request is over 500ms in, while its bird is still outbound.
	gone := now.Add(500 * time.Millisecond)
	store.ApplySessionRoute(routing.SessionRouteLike{
		ID: 504, Time: now, Asked: "qoder/m", Model: "qoder/m", Done: true, Status: 200,
		Tries: []routing.TryLike{{Model: "qoder/m", Done: true, Status: 200, Millis: 400}},
	})
	store.ApplyActive(nil)
	s2 := store.Snapshot()
	model.SetSnapshot(s2)
	model.syncFlightTracks(s2, s2.Routes, gone)
	after := trackFor(t, model, 504)
	if m := flightMomentAt(after, gone); m.phase.kind != flightOutbound {
		t.Fatalf("moment when the request ended = %+v, want the outbound leg to go on", m)
	}
	if m := flightMomentAt(after, now.Add(liveOutboundDuration+100*time.Millisecond)); m.phase.kind != flightInbound {
		t.Fatalf("moment after landing = %+v, want the answer flying home", m)
	}
	assertContinuous(t, geometry, stage, func(at time.Time) flightTrack {
		if at.Before(gone) {
			return before
		}
		return after
	}, now, 4*time.Second)
}

func replayModel(t *testing.T, speed float64) (*Model, routing.Snapshot, time.Time) {
	t.Helper()
	day := "2026-10-18"
	base := time.Date(2026, 10, 18, 10, 0, 0, 0, time.Local)
	route := func(id int64, at time.Time) routing.Route {
		return routing.Route{
			ID: id, Time: at, Agent: "codex", Model: "qoder/m", Provider: "qoder",
			Done: true, Status: 200, Millis: 1000, HistoryDay: day,
			Tries: []routing.Try{{Model: "qoder/m", Done: true, Status: 200, Millis: 1000, Start: at}},
		}
	}
	store := routing.NewStore()
	store.ReplaceHistory([]routing.Route{route(1, base), route(2, base.Add(30*time.Second))})
	s := store.Snapshot()
	model, _ := testModel(t, s, Actions{})
	model.setTab(tabHistory)
	model.SetHistoryDay(day)
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.Local)
	model.mu.Lock()
	model.replay.speed = speed
	model.mu.Unlock()
	model.syncReplay(s, now)
	return model, s, now
}

func TestReplayBirdLeavesAgentBetweenRebuilds(t *testing.T) {
	for _, speed := range []float64{1, 8} {
		model, s, now := replayModel(t, speed)
		// Playing, half a second of wall time before route 2 starts.
		model.mu.Lock()
		model.replay.position = 30*time.Second - time.Duration(0.5*speed*float64(time.Second))
		model.replay.playing = true
		model.replay.lastTick = now
		model.mu.Unlock()
		model.syncFlightTracks(s, s.Routes, now)
		wait, ok := model.replayNextFlightIn(now)
		if !ok || wait > time.Second {
			t.Fatalf("speed %g: next rebuild in %v (%t), want before the route starts", speed, wait, ok)
		}
		at := now.Add(wait + time.Millisecond)
		model.syncFlightTracks(s, s.Routes, at)
		geometry, stage := stageFor(t, model, s, at)
		assertLeavesAgent(t, geometry, stage, trackFor(t, model, 2), at)
	}
}

func TestReplayBirdKeepsFlyingAtSpeed(t *testing.T) {
	model, s, now := replayModel(t, 8)
	// One wall second into route 2 at ×8: its 1.4s outbound leg is still
	// in the air even though eight replay seconds have passed.
	model.mu.Lock()
	model.replay.position = 30*time.Second + 8*time.Second
	model.replay.playing = true
	model.replay.lastTick = now
	model.mu.Unlock()
	model.syncFlightTracks(s, s.Routes, now)
	m := flightMomentAt(trackFor(t, model, 2), now)
	if !m.hasPhase || m.phase.kind != flightOutbound {
		t.Fatalf("moment one wall second in at ×8 = %+v, want the outbound leg", m)
	}
}

func TestReplayPauseAtSpeedDoesNotMoveTheBird(t *testing.T) {
	model, s, now := replayModel(t, 8)
	model.mu.Lock()
	model.replay.position = 30*time.Second + 4*time.Second
	model.replay.playing = true
	model.replay.lastTick = now
	model.mu.Unlock()
	model.syncFlightTracks(s, s.Routes, now)
	geometry, stage := stageFor(t, model, s, now)
	f := trackFor(t, model, 2)
	x0, y0, ok0 := flightPacketPoint(geometry, indexAgentNode(stage.agents, f.agent), f, now)
	model.toggleReplay(now)
	model.syncFlightTracks(s, s.Routes, now)
	f = trackFor(t, model, 2)
	x1, y1, ok1 := flightPacketPoint(geometry, indexAgentNode(stage.agents, f.agent), f, now.Add(time.Second))
	if !ok0 || !ok1 || x0 != x1 || y0 != y1 {
		t.Fatalf("pausing at ×8 moved the bird from (%.1f,%.1f) to (%.1f,%.1f)", x0, y0, x1, y1)
	}
}

func TestStageWiresFollowUpstreamLightRow(t *testing.T) {
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.Local)
	agents := []agentNode{{ID: "codex"}}
	rows := []providerNode{
		{Provider: "alpha", Model: "alpha/m", AccountID: "a"},
		{Provider: "beta", Model: "beta/m", AccountID: "b"},
	}
	at := func(kind flightPhaseKind, row int, toHub bool, local, span time.Duration) flightTrack {
		ph := flightPhase{kind: kind, start: 0, end: span, toHub: toHub}
		if !toHub {
			ph.provider, ph.model, ph.accountID = rows[row].Provider, rows[row].Model, rows[row].AccountID
		}
		return flightTrack{agent: "codex", firstSeen: now.Add(-local), phases: []flightPhase{ph}}
	}
	answered := routing.RouteView{Route: routing.Route{
		ID: 9, Agent: "codex", Done: true,
		Order: []routing.Weighed{{ID: "a", Provider: "alpha", Model: "alpha/m"}},
		Tries: []routing.Try{{ID: "a", Model: "alpha/m", Done: true, Status: 200}},
	}}
	cases := []struct {
		name            string
		flights         []flightTrack
		sel             *routing.RouteView
		agentLive       bool
		rowLive, rowLit []int
	}{
		{"to magpie", []flightTrack{at(flightOutbound, 0, true, 100*time.Millisecond, time.Second)}, nil, true, nil, nil},
		{"to an account", []flightTrack{at(flightOutbound, 0, false, 100*time.Millisecond, time.Second)}, nil, true, []int{0}, nil},
		{"waiting at an account", []flightTrack{at(flightHold, 1, false, time.Second, openPhaseEnd)}, nil, true, []int{1}, nil},
		{"on the way back to magpie", []flightTrack{at(flightRetryBack, 0, false, 100*time.Millisecond, time.Second)}, nil, true, []int{0}, nil},
		{"back at magpie", []flightTrack{at(flightRetryBack, 0, false, 2*time.Second, time.Second)}, nil, true, nil, nil},
		{"home", []flightTrack{at(flightInbound, 0, false, 1100*time.Millisecond, time.Second)}, nil, false, nil, nil},
		{"answered, idle", nil, &answered, false, nil, []int{0}},
		{"answered, busy again", []flightTrack{at(flightHold, 0, false, time.Second, openPhaseEnd)}, &answered, true, []int{0}, nil},
	}
	for _, c := range cases {
		stage := &routingStagePresentation{agents: agents, rows: rows, flights: c.flights}
		if c.sel != nil {
			stage.selection, stage.hasSelection = *c.sel, true
		}
		w := stageWireStates(agents, rows, stage, now)
		if _, live := w.agentLive[0]; live != c.agentLive {
			t.Errorf("%s: agent wire live = %t, want %t", c.name, live, c.agentLive)
		}
		for i := range rows {
			_, live := w.rowLive[i]
			_, lit := w.rowLit[i]
			if live != contains(c.rowLive, i) || lit != contains(c.rowLit, i) {
				t.Errorf("%s: row %d live=%t lit=%t, want live=%t lit=%t", c.name, i, live, lit, contains(c.rowLive, i), contains(c.rowLit, i))
			}
		}
	}
}

func contains(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestHeldRequestKeepsWiresFlowing(t *testing.T) {
	now := time.Now()
	store := routing.NewStore()
	store.ApplyActive([]routing.LiveRequest{{
		ID: 1, Agent: "codex", Provider: "qoder", Model: "qoder/m", RouteID: 505,
		Started: now, Millis: 10,
	}})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	tester := render(t, model, 1240, 820)
	tester.Frame()
	// Well past the outbound leg and the carrier's exit: only the held
	// packet is left, but its wires flow and it pulses, as upstream's
	// rtflow and rtheld keep doing while the account answers.
	tester.Advance(liveOutboundDuration + 2*time.Second)
	f := trackFor(t, model, 505)
	if m := flightMomentAt(f, tester.Now()); m.phase.kind != flightHold {
		t.Fatalf("moment = %+v, want the packet held at its account", m)
	}
	if !tester.FramePending() {
		t.Fatal("a request held at its account stopped its wires flowing")
	}

	// With nothing in flight the stage asks for no frames.
	store.ApplyActive(nil)
	model.SetSnapshot(store.Snapshot())
	tester.Frame()
	tester.Advance(10 * time.Second)
	if tester.FramePending() {
		t.Fatal("an idle stage kept requesting frames")
	}
}

func TestRestingAndUnlistedWiresAreInTheStageIdentity(t *testing.T) {
	agents := []agentNode{{ID: "codex"}}
	base := []providerNode{{Provider: "alpha", Model: "alpha/m"}}
	key := stageIdentityKey(agents, base, 0, 0)
	for name, row := range map[string]providerNode{
		"resting":  {Provider: "alpha", Model: "alpha/m", Rest: true},
		"unlisted": {Provider: "alpha", Model: "alpha/m", Unlisted: true},
	} {
		if stageIdentityKey(agents, []providerNode{row}, 0, 0) == key {
			t.Errorf("a %s account's dashed wire would not redraw the stage", name)
		}
	}
}

func TestFlowingDashesMoveForwardWithSquareEnds(t *testing.T) {
	// A straight wire from x=0 to x=100, read left to right as the request
	// goes. rtflow moves stroke-dashoffset from 0 to -8: forward.
	path := newFlightPath([]flightPoint{{x: 0, y: 10}, {x: 100, y: 10}})
	starts := func(now time.Time) []float32 {
		shift := flowDashShift(now)
		ribbons, ok := flightDashRibbons(path, shift-flowDashPeriod, 3, flowDashPeriod, 1.5)
		if !ok {
			t.Fatal("no dashes")
		}
		b := ribbons.Bounds()
		if b.H < 1.49 || b.H > 1.51 {
			t.Fatalf("dash band is %.2f tall, want the 1.5 stroke", b.H)
		}
		var out []float32
		for d := shift - 8; d < 100; d += 8 {
			if d >= 0 && d+3 <= 100 {
				out = append(out, d)
			}
		}
		return out
	}
	t0 := time.UnixMilli(7_000_000) // a whole number of 700ms cycles
	a, b := starts(t0), starts(t0.Add(100*time.Millisecond))
	if len(a) == 0 || len(b) == 0 || b[0] <= a[0] {
		t.Fatalf("dash starts %v then %v: want them moving forward", a, b)
	}
	// One full dash is exactly 3 long: SVG's butt ends, not round caps.
	one, _ := flightDashRibbons(path, 10, 3, 1000, 1.5)
	if w := one.Bounds().W; w < 2.99 || w > 3.01 {
		t.Fatalf("one dash is %.2f long, want 3", w)
	}
}

func TestBusyAgentWireIsInTheStaticStageKey(t *testing.T) {
	stage := &routingStagePresentation{agents: []agentNode{{ID: "codex"}}}
	rows := []providerNode{{Provider: "alpha"}}
	now := time.Now()
	loc := NewLocalization("en")
	idle := staticStageBitmapKey(stage, flightStage{}, palette{}, rows, now, 0, 1, loc, true, "u", "m", nil, nil, 900, 400)
	busy := staticStageBitmapKey(stage, flightStage{}, palette{}, rows, now, 0, 1, loc, true, "u", "m", nil, map[int]bool{0: true}, 900, 400)
	if idle == busy {
		t.Fatal("a busy agent's flowing wire would keep the grey idle wire under it")
	}
}

// liveGroupScenario is the stage the user saw: an earlier group request
// whose candidates are two qoder accounts on one model, and a new live
// request whose public trace names only qoder/dfmodel, not the account.
func liveGroupScenario(t *testing.T) (*Model, *routing.Store, time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.Local)
	day := now.Format("2006-01-02")
	store := routing.NewStore()
	store.ReplaceHistory([]routing.Route{{
		ID: 700, Time: now.Add(-time.Minute), Agent: "orca_desktop", Model: "group/auto-deepseek-flash",
		Provider: "qoder", Done: true, Status: 200, Millis: 6000, HistoryDay: day,
		Order: []routing.Weighed{
			{ID: "acct-1", Provider: "qoder", Model: "dfmodel", Kind: "account"},
			{ID: "acct-2", Provider: "qoder", Model: "dfmodel", Kind: "account"},
		},
		Tries: []routing.Try{{ID: "acct-1", Model: "qoder/dfmodel", Done: true, Status: 200, Millis: 6000}},
	}})
	store.ApplyActive([]routing.LiveRequest{{
		ID: 1, Agent: "orca_desktop", Model: "group/auto-deepseek-flash", Session: "s1",
		RouteID: 701, Started: now, Millis: 10,
	}})
	store.ApplySessionRoute(routing.SessionRouteLike{
		ID: 701, Time: now, Asked: "group/auto-deepseek-flash", Model: "qoder/dfmodel",
		Tries: []routing.TryLike{{Model: "qoder/dfmodel"}},
	})
	model, _ := testModel(t, store.Snapshot(), Actions{})
	model.mu.Lock()
	model.selectedID = 700 // picked when the app opened
	model.mu.Unlock()
	return model, store, now
}

func TestLiveBirdReachesTheAccountItsTraceNames(t *testing.T) {
	model, store, now := liveGroupScenario(t)
	s := store.Snapshot()
	model.prepareListSelection(model.visibleRoutes(s, false))
	if sel, ok := model.selectedRoute(model.visibleRoutes(s, false)); !ok || sel.ID != 701 {
		t.Fatalf("live stage tells about route %d, want the request under way (701)", sel.ID)
	}
	model.syncFlightTracks(s, s.Routes, now)
	geometry, stage := stageFor(t, model, s, now)
	f := trackFor(t, model, 701)
	if f.phases[0].toHub {
		t.Fatalf("bird flies to magpie only: %+v", f.phases)
	}
	if pi := geometry.providerIndex(targetForPhase(f, f.phases[0])); pi < 0 {
		t.Fatalf("no account row on the stage for %+v; rows %+v", targetForPhase(f, f.phases[0]), stage.rows)
	}
}

func TestPinnedLiveStageFliesNoNewRequests(t *testing.T) {
	model, store, now := liveGroupScenario(t)
	model.mu.Lock()
	model.pinnedID = 700 // picked in the list, as client.js's pick()
	model.mu.Unlock()
	s := store.Snapshot()
	model.syncFlightTracks(s, s.Routes, now)
	model.mu.RLock()
	n := len(model.topology.flights)
	model.mu.RUnlock()
	if n != 0 {
		t.Fatalf("a pinned live stage flew %d new requests, want none", n)
	}
}

func TestLiveStageKeepsTheGroupsRowsWhileARequestFlies(t *testing.T) {
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.Local)
	day := now.Format("2006-01-02")
	store := routing.NewStore()
	store.SetConfig(routing.ConfigSnapshot{Groups: []routing.GroupInfo{{
		ID: "auto-deepseek-flash", Name: "DeepSeek V4.1 Flash",
		Members: []string{"qoder/dfmodel", "deepseek/deepseek-flash"},
	}}})
	store.ApplyActive([]routing.LiveRequest{{
		ID: 1, Agent: "orca_desktop", Model: "group/auto-deepseek-flash", Session: "s1",
		RouteID: 801, Started: now, Millis: 10,
	}})
	// What the public route API says while the request flies: its group
	// and the one try made so far, no candidate order.
	store.ApplySessionRoute(routing.SessionRouteLike{
		ID: 801, Time: now, Asked: "group/auto-deepseek-flash", Model: "qoder/dfmodel",
		Group: "auto-deepseek-flash", Tries: []routing.TryLike{{Model: "qoder/dfmodel"}},
	})
	s := store.Snapshot()
	model, _ := testModel(t, s, Actions{})
	stageRows := func(s routing.Snapshot) (*routingStagePresentation, routing.RouteView) {
		routes := model.visibleRoutes(s, false)
		model.prepareListSelection(routes)
		sel, ok := model.selectedRoute(routes)
		sel = withConfigGroup(sel, s.Config)
		return model.routingStagePresentationAt(s, routes, sel, ok, now), sel
	}
	flying, sel := stageRows(s)
	if len(flying.rows) != 2 {
		t.Fatalf("rows while flying = %+v, want the group's two members", flying.rows)
	}
	if routeGroupName(sel) != "DeepSeek V4.1 Flash" || groupMembersShown(sel, s.Config) != 2 {
		t.Fatalf("title while flying = %q with %d members, want the group's name and 2", routeGroupName(sel), groupMembersShown(sel, s.Config))
	}
	model.syncFlightTracks(s, s.Routes, now)
	geometry, _ := stageFor(t, model, s, now)
	f := trackFor(t, model, 801)
	if pi := geometry.providerIndex(targetForPhase(f, f.phases[0])); pi != 0 {
		t.Fatalf("bird flies to row %d, want the qoder member (0)", pi)
	}

	// Over: the completed history brings the weighed candidates.
	store.ApplyActive(nil)
	store.MergeHistory([]routing.Route{{
		ID: 801, Time: now, Agent: "orca_desktop", Model: "group/auto-deepseek-flash",
		Provider: "qoder", Done: true, Status: 200, Millis: 6000, HistoryDay: day,
		Group: &routing.GroupRef{ID: "auto-deepseek-flash", Name: "DeepSeek V4.1 Flash",
			Members: []string{"qoder/dfmodel", "deepseek/deepseek-flash"}},
		Order: []routing.Weighed{
			{ID: "acct-q", Provider: "qoder", Model: "dfmodel", Kind: "account"},
			{ID: "acct-d", Provider: "deepseek", Model: "deepseek-flash", Kind: "key"},
		},
		Tries: []routing.Try{{ID: "acct-q", Model: "qoder/dfmodel", Done: true, Status: 200, Millis: 6000}},
	}})
	s2 := store.Snapshot()
	model.SetSnapshot(s2)
	done, sel2 := stageRows(s2)
	if len(done.rows) != len(flying.rows) || routeGroupName(sel2) != routeGroupName(sel) {
		t.Fatalf("rows %d → %d, title %q → %q: the stage changed shape when the request ended",
			len(flying.rows), len(done.rows), routeGroupName(sel), routeGroupName(sel2))
	}
}

func TestLiveRequestWithoutSessionStillShowsItsGroup(t *testing.T) {
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.Local)
	store := routing.NewStore()
	store.SetConfig(routing.ConfigSnapshot{Groups: []routing.GroupInfo{{
		ID: "auto-deepseek-flash", Name: "DeepSeek V4.1 Flash",
		Members: []string{"qoder/dfmodel", "deepseek/deepseek-flash"},
	}}})
	// No session (curl, a browser extension): the concurrency row is all
	// there is. Its model is what the agent asked, the group.
	store.ApplyActive([]routing.LiveRequest{{
		ID: 1, Agent: "curl", Model: "group/auto-deepseek-flash", Provider: "qoder",
		RouteID: 901, Started: now, Millis: 10,
	}})
	s := store.Snapshot()
	model, _ := testModel(t, s, Actions{})
	routes := model.visibleRoutes(s, false)
	model.prepareListSelection(routes)
	sel, ok := model.selectedRoute(routes)
	sel = withConfigGroup(sel, s.Config)
	stage := model.routingStagePresentationAt(s, routes, sel, ok, now)
	if len(stage.rows) != 2 || routeGroupName(sel) != "DeepSeek V4.1 Flash" {
		t.Fatalf("rows %+v, title %q: want the group's two members and its name", stage.rows, routeGroupName(sel))
	}
	// For a group, the row's provider is not the account being tried
	// (seen live: "opencode-go" while the try went to codex), so the bird
	// waits at magpie for the trace instead of guessing.
	model.syncFlightTracks(s, s.Routes, now)
	if f := trackFor(t, model, 901); !f.phases[0].toHub {
		t.Fatalf("legs %+v: want the bird waiting at magpie", f.phases)
	}
}

func TestTwoLiveGroupRequestsShareTheGroupsRows(t *testing.T) {
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.Local)
	store := routing.NewStore()
	store.SetConfig(routing.ConfigSnapshot{Groups: []routing.GroupInfo{{
		ID: "auto-deepseek-flash", Name: "DeepSeek V4.1 Flash",
		Members: []string{"qoder/dfmodel", "deepseek/deepseek-flash"},
	}}})
	store.ApplyActive([]routing.LiveRequest{
		{ID: 1, Agent: "curl", Model: "group/auto-deepseek-flash", Provider: "qoder", RouteID: 902, Started: now, Millis: 10},
		{ID: 2, Agent: "codex", Model: "group/auto-deepseek-flash", Provider: "deepseek", RouteID: 903, Started: now.Add(time.Second), Millis: 10},
	})
	s := store.Snapshot()
	model, _ := testModel(t, s, Actions{})
	routes := model.visibleRoutes(s, false)
	model.prepareListSelection(routes)
	sel, ok := model.selectedRoute(routes)
	stage := model.routingStagePresentationAt(s, routes, withConfigGroup(sel, s.Config), ok, now)
	if len(stage.rows) != 2 {
		t.Fatalf("rows %+v: want the group's two members, no group/... alias row", stage.rows)
	}
}

// translateScenario is a request captured from the real Gateway: no session,
// the concurrency row says provider "opencode-go", and the completed history
// shows the one try went to the codex account.
func translateScenario(t *testing.T) (*Model, *routing.Store, time.Time, routing.Route) {
	t.Helper()
	now := time.Date(2026, 10, 19, 23, 1, 0, 0, time.Local)
	store := routing.NewStore()
	store.SetConfig(routing.ConfigSnapshot{Groups: []routing.GroupInfo{{
		ID: "translate", Name: "translate", Sink: true, Levels: []string{"none"},
		Members: []string{"opencode-go/deepseek-v4.1-flash", "deepseek/deepseek-flash:none", "qoder/dfmodel:none",
			"opencode-go/gpt-6-luna:none", "codex/gpt-6-luna:none", "archlinux-ollama/selfllm:latest:none"},
	}}})
	store.ApplyActive([]routing.LiveRequest{{
		ID: 3297, Agent: "PureTranslate", Model: "group/translate", Provider: "opencode-go",
		Phase: "upstream", RouteID: 1792383455839, Started: now, Millis: 127,
	}})
	done := routing.Route{
		ID: 1792383455839, Time: now, Agent: "PureTranslate", Model: "group/translate",
		Provider: "opencode-go", Done: true, Status: 200, Millis: 7000, HistoryDay: now.Format("2006-01-02"),
		Group: &routing.GroupRef{ID: "translate", Name: "translate"},
		Order: []routing.Weighed{
			{ID: "codex", Provider: "codex", Model: "gpt-6-luna"},
			{ID: "opencode-go", Provider: "opencode-go", Model: "deepseek-v4.1-flash"},
			{ID: "opencode-go", Provider: "opencode-go", Model: "gpt-6-luna"},
			{ID: "deepseek", Provider: "deepseek", Model: "deepseek-flash"},
			{ID: "qoder", Provider: "qoder", Model: "dfmodel"},
			{ID: "archlinux-ollama", Provider: "archlinux-ollama", Model: "selfllm:latest"},
		},
		Tries: []routing.Try{{ID: "codex", Model: "gpt-6-luna", Done: true, Status: 200, Millis: 7000, Start: now}},
	}
	s := store.Snapshot()
	model, _ := testModel(t, s, Actions{})
	return model, store, now, done
}

func TestGroupMembersDropTheirEffortLevel(t *testing.T) {
	model, store, now, _ := translateScenario(t)
	s := store.Snapshot()
	routes := model.visibleRoutes(s, false)
	model.prepareListSelection(routes)
	sel, ok := model.selectedRoute(routes)
	stage := model.routingStagePresentationAt(s, routes, withConfigGroup(sel, s.Config), ok, now)
	var got []string
	for _, r := range stage.rows {
		got = append(got, r.Provider+"/"+r.Model)
	}
	want := []string{"opencode-go/deepseek-v4.1-flash", "deepseek/deepseek-flash", "qoder/dfmodel",
		"opencode-go/gpt-6-luna", "codex/gpt-6-luna", "archlinux-ollama/selfllm:latest"}
	if len(got) != len(want) {
		t.Fatalf("rows %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rows %v, want %v", got, want)
		}
	}
}

func TestBirdFliesToTheRecordedAccountOnceHistoryLands(t *testing.T) {
	model, store, now, done := translateScenario(t)
	s := store.Snapshot()
	model.syncFlightTracks(s, s.Routes, now)

	// Seven seconds later the request is over; history is written a moment
	// later, asynchronously. Meanwhile the bird keeps waiting at magpie.
	over := now.Add(7 * time.Second)
	store.ApplyActive(nil)
	s2 := store.Snapshot()
	model.SetSnapshot(s2)
	model.syncFlightTracks(s2, s2.Routes, over)
	f := trackFor(t, model, done.ID)
	if f.done || flightMomentAt(f, over).phase.kind == flightInbound {
		t.Fatalf("bird flew home before the trace said where it went: %+v", f.phases)
	}

	landed := over.Add(1500 * time.Millisecond)
	store.MergeHistory([]routing.Route{done})
	s3 := store.Snapshot()
	model.SetSnapshot(s3)
	model.syncFlightTracks(s3, s3.Routes, landed)
	geometry, _ := stageFor(t, model, s3, landed)
	f = trackFor(t, model, done.ID)
	var out flightPhase
	for _, ph := range f.phases {
		if ph.kind == flightHubOut {
			out = ph
		}
	}
	if out.kind != flightHubOut {
		t.Fatalf("legs %+v: want a hub-out leg to the recorded account", f.phases)
	}
	if pi := geometry.providerIndex(targetForPhase(f, out)); pi < 0 || geometry.accounts[pi].AccountID != "codex" {
		t.Fatalf("hub-out flies to row %d, want the codex account", pi)
	}
	if last := f.phases[len(f.phases)-1]; last.kind != flightInbound {
		t.Fatalf("legs end with %+v, want the answer flying home", last)
	}
}

func TestBirdWaitingForHistoryFliesHomeAfterAWhile(t *testing.T) {
	model, store, now, done := translateScenario(t)
	s := store.Snapshot()
	model.syncFlightTracks(s, s.Routes, now)
	store.ApplyActive(nil)
	s2 := store.Snapshot()
	model.SetSnapshot(s2)
	over := now.Add(7 * time.Second)
	model.syncFlightTracks(s2, s2.Routes, over)
	if wait, ok := model.liveTraceWaitIn(over); !ok || wait <= 0 || wait > liveTraceWait {
		t.Fatalf("next rebuild in %v (%t), want within the trace wait", wait, ok)
	}
	late := over.Add(liveTraceWait + time.Millisecond)
	model.syncFlightTracks(s2, s2.Routes, late)
	f := trackFor(t, model, done.ID)
	if !f.done || f.phases[len(f.phases)-1].kind != flightInbound || !f.phases[len(f.phases)-1].toHub {
		t.Fatalf("legs %+v: want the bird home from magpie once no trace came", f.phases)
	}
}
