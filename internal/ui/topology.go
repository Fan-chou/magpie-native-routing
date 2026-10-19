package ui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/Fan-chou/magpie-native-routing/internal/routing"
)

type topologyFrameLayout struct {
	width, height float32
	agents        []nodeBox
	gateway       nodeBox
	providers     []nodeBox
}

type nodeBox struct {
	x, y, w, h float32
}

func (b nodeBox) rect() ui.Rect { return ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h} }
func (b nodeBox) rightCenter() (float32, float32) {
	return b.x + b.w, b.y + b.h/2
}
func (b nodeBox) leftCenter() (float32, float32) {
	return b.x, b.y + b.h/2
}

func point(x, y float32) [2]float32 { return [2]float32{x, y} }

const (
	routingStageMinHeight = float32(258)
	routingStagePadding   = float32(20)
)

type routingStageSizing struct {
	agentH   float32
	agentGap float32
	rowH     float32
	rowGap   float32
}

// routingStageSizing compacts node spacing as concurrency rises instead of
// hiding requests. The stage grows to fit every observed agent and provider
// row, with all active flights remaining addressable by their real identity.
func routingStageMetrics(agentCount, rowCount int) routingStageSizing {
	m := routingStageSizing{agentH: 78, agentGap: 8, rowH: 44, rowGap: 7}
	switch {
	case agentCount > 12:
		m.agentH, m.agentGap = 48, 3
	case agentCount > 8:
		m.agentH, m.agentGap = 58, 5
	case agentCount > 4:
		m.agentH, m.agentGap = 68, 6
	}
	switch {
	case rowCount > 12:
		m.rowH, m.rowGap = 30, 3
	case rowCount > 8:
		m.rowH, m.rowGap = 34, 4
	case rowCount > 5:
		m.rowH, m.rowGap = 38, 5
	}
	return m
}

func stageStackHeight(count int, itemH, gap float32) float32 {
	if count <= 0 {
		return 0
	}
	return float32(count)*itemH + float32(count-1)*gap
}

func routingStageContentHeight(agentCount, rowCount int) float32 {
	metrics := routingStageMetrics(agentCount, rowCount)
	return max(
		stageStackHeight(agentCount, metrics.agentH, metrics.agentGap),
		stageStackHeight(rowCount, metrics.rowH, metrics.rowGap),
		98,
	)
}

// layoutRoutingStage returns geometry in the coordinate space of r. A canvas
// pass calls it with a local rect and the live layer offsets the result to
// window coordinates.
func layoutRoutingStage(r ui.Rect, agents []agentNode, rows []providerNode) flightStage {
	agentW := float32(124)
	gap := min(float32(56), max(r.W*0.055, 24))
	hubW := float32(124)
	providerW := max(r.W-agentW-hubW-gap*2, 220)
	total := agentW + gap + hubW + gap + providerW
	startX := r.X + max((r.W-total)/2, 8)
	centerY := r.Y + r.H/2
	metrics := routingStageMetrics(len(agents), len(rows))

	agentStack := stageStackHeight(len(agents), metrics.agentH, metrics.agentGap)
	agentTop := centerY - agentStack/2
	agentBoxes := make([]nodeBox, 0, len(agents))
	for i := range agents {
		agentBoxes = append(agentBoxes, nodeBox{
			x: startX, y: agentTop + float32(i)*(metrics.agentH+metrics.agentGap),
			w: agentW, h: metrics.agentH,
		})
	}

	hubX := startX + agentW + gap
	hub := nodeBox{x: hubX, y: centerY - 49, w: hubW, h: 98}
	rowsX := hubX + hubW + gap
	rowStack := stageStackHeight(len(rows), metrics.rowH, metrics.rowGap)
	rowsTop := centerY - rowStack/2
	rowBoxes := make([]nodeBox, 0, len(rows))
	for i := range rows {
		rowBoxes = append(rowBoxes, nodeBox{
			x: rowsX, y: rowsTop + float32(i)*(metrics.rowH+metrics.rowGap),
			w: providerW, h: metrics.rowH,
		})
	}
	return flightStage{agents: agentBoxes, hub: hub, accounts: rows, rows: rowBoxes}
}

func stageIdentityKey(agents []agentNode, rows []providerNode, width, height float32) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%.1fx%.1f|", width, height)
	for _, agent := range agents {
		b.WriteString(agent.ID)
		b.WriteByte(0)
	}
	for _, row := range rows {
		b.WriteString(row.Provider)
		b.WriteByte(0)
		b.WriteString(row.Name)
		b.WriteByte(0)
		b.WriteString(row.Model)
		b.WriteByte(0)
		b.WriteString(row.AccountID)
		b.WriteByte(0)
		b.WriteString(row.Who)
		b.WriteByte(0)
	}
	return b.String()
}

// flightStage is the geometry and identity of the stage at this paint. It
// keeps provider names paired with their boxes, so a retry can never land
// on a different provider merely because two rows share a position.
type flightStage struct {
	agents   []nodeBox
	hub      nodeBox
	accounts []providerNode
	rows     []nodeBox
}

// flightGeometry owns every curve that can be flown on one stage layout.
// The paths are rebuilt only when a node moves, resizes, or changes
// identity; animation frames then reuse the exact same arc-length tables.
type flightGeometry struct {
	key string
	hub nodeBox

	agents   []nodeBox
	accounts []providerNode
	rows     []nodeBox

	agentHub       []flightPath
	agentHubCenter []flightPath
	providerHub    []flightPath
	hubProvider    []flightPath
	outbound       map[flightPair]flightPath
	inbound        map[flightPair]flightPath
}

type flightPair struct {
	agent    int
	provider int
}

// providerTarget is the exact endpoint a recorded attempt addresses. A
// provider name alone cannot distinguish two models, or two accounts, under
// the same provider.
type providerTarget struct {
	provider  string
	model     string
	accountID string
	who       string
}

func targetForPhase(f flightTrack, phase flightPhase) providerTarget {
	return providerTarget{
		provider:  firstNonEmpty(phase.provider, f.provider),
		model:     firstNonEmpty(phase.model, f.model),
		accountID: firstNonEmpty(phase.accountID, f.accountID),
		who:       firstNonEmpty(phase.who, f.who),
	}
}

func targetForRoute(rv routing.RouteView) providerTarget {
	if len(rv.Tries) > 0 {
		return traceAttemptTarget(rv, rv.Tries[len(rv.Tries)-1])
	}
	return providerTarget{
		provider: firstNonEmpty(rv.Provider, rv.DisplayProvider()),
		model:    rv.Model,
	}
}

func newFlightGeometry(stage flightStage) *flightGeometry {
	key := flightStageKey(stage)
	return newFlightGeometryKeyed(stage, key)
}

func newFlightGeometryKeyed(stage flightStage, key string) *flightGeometry {
	g := &flightGeometry{
		hub: stage.hub, agents: stage.agents,
		accounts: stage.accounts, rows: stage.rows,
		agentHub:       make([]flightPath, len(stage.agents)),
		agentHubCenter: make([]flightPath, len(stage.agents)),
		providerHub:    make([]flightPath, len(stage.rows)),
		hubProvider:    make([]flightPath, len(stage.rows)),
		outbound:       make(map[flightPair]flightPath),
		inbound:        make(map[flightPair]flightPath),
	}
	for i := range stage.agents {
		g.agentHub[i] = agentToHubPath(stage.agents[i], stage.hub)
		g.agentHubCenter[i] = g.agentHub[i]
	}
	for i := range stage.rows {
		g.providerHub[i] = providerToHubPath(stage.hub, &stage.rows[i])
		g.hubProvider[i] = hubToProviderPath(stage.hub, &stage.rows[i])
	}
	g.key = key
	return g
}

func (g *flightGeometry) providerIndex(target providerTarget) int {
	return indexProviderNode(g.accounts, target)
}

func (g *flightGeometry) outboundPath(agentIndex, providerIndex int) flightPath {
	if agentIndex < 0 || agentIndex >= len(g.agents) {
		return flightPath{}
	}
	if providerIndex < 0 || providerIndex >= len(g.rows) {
		return g.agentHubCenter[agentIndex]
	}
	key := flightPair{agent: agentIndex, provider: providerIndex}
	if path, ok := g.outbound[key]; ok {
		return path
	}
	path := stageOutboundPath(g.agents[agentIndex], g.hub, &g.rows[providerIndex])
	g.outbound[key] = path
	return path
}

func (g *flightGeometry) inboundPath(agentIndex, providerIndex int) flightPath {
	if agentIndex < 0 || agentIndex >= len(g.agents) {
		return flightPath{}
	}
	if providerIndex < 0 || providerIndex >= len(g.rows) {
		return g.agentHubCenter[agentIndex].reversed()
	}
	key := flightPair{agent: agentIndex, provider: providerIndex}
	if path, ok := g.inbound[key]; ok {
		return path
	}
	path := g.outboundPath(agentIndex, providerIndex).reversed()
	g.inbound[key] = path
	return path
}

func flightStageKey(stage flightStage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "hub:%.2f,%.2f,%.2f,%.2f|", stage.hub.x, stage.hub.y, stage.hub.w, stage.hub.h)
	for _, box := range stage.agents {
		fmt.Fprintf(&b, "a:%.2f,%.2f,%.2f,%.2f|", box.x, box.y, box.w, box.h)
	}
	for i, row := range stage.rows {
		name := ""
		if i < len(stage.accounts) {
			name = stage.accounts[i].Provider + "\x00" + stage.accounts[i].Name + "\x00" +
				stage.accounts[i].Model + "\x00" + stage.accounts[i].AccountID
		}
		fmt.Fprintf(&b, "r:%.2f,%.2f,%.2f,%.2f:%s|", row.x, row.y, row.w, row.h, name)
	}
	return b.String()
}

// syncFlightTracks advances the stage animation from the immutable snapshot.
// Only routes present in the live snapshot, or in flight at the history
// replay cursor, receive a track. The timestamps and attempt boundaries are
// copied from Magpie; no request or retry is synthesized here.
func (m *Model) syncFlightTracks(s routing.Snapshot, routes []routing.RouteView, now time.Time) {
	replay, replayActive := m.replayTopologyState()
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.topology.frameAt.IsZero() || now.Before(m.topology.frameAt) {
		m.topology.frameAt = now
	}
	byID := make(map[int64]*flightTrack, len(m.topology.flights))
	for i := range m.topology.flights {
		byID[m.topology.flights[i].routeID] = &m.topology.flights[i]
	}
	type input struct {
		id        int64
		agent     string
		model     string
		provider  string
		accountID string
		who       string
		failed    bool
		done      bool
		observed  time.Duration
		total     time.Duration
		attempts  []flightAttempt
		phases    []flightPhase
	}
	var inputs []input
	if replayActive {
		addReplay := func(rv routing.RouteView, elapsed time.Duration) {
			if rv.ID == 0 || rv.Time.IsZero() {
				return
			}
			target := targetForRoute(rv)
			attempts, total := traceFlightAttempts(rv)
			if elapsed < 0 {
				elapsed = 0
			}
			done := elapsed >= total
			phases := buildFlightPhases(attempts, total, true)
			recordedEnd := flightRecordedPhaseEnd(phases)
			displayEnd := flightPhaseEnd(phases)
			if elapsed > displayEnd {
				return
			}
			// The replay ghost is complete once the recorded trace is
			// over; the inbound leg below is presentation only. Keeping
			// this true lets flightMomentAt continue along that leg.
			done = elapsed >= recordedEnd
			inputs = append(inputs, input{
				id: rv.ID, agent: rv.Agent, model: rv.Model,
				provider: target.provider, accountID: target.accountID, who: target.who,
				failed:   rv.Status >= 400 || rv.Error != "",
				done:     done,
				observed: elapsed,
				total:    total,
				attempts: attempts,
				phases:   phases,
			})
		}
		for _, rv := range replay.routes {
			offset := rv.Time.Sub(replay.start)
			addReplay(rv, m.replay.position-offset)
		}
	} else {
		routeByID := make(map[int64]routing.RouteView, len(routes)+len(s.Routes))
		for _, rv := range s.Routes {
			routeByID[rv.ID] = rv
		}
		for _, rv := range routes {
			routeByID[rv.ID] = rv
		}
		byRoute := map[int64][]routing.LiveRequest{}
		for _, l := range s.Live {
			if l.RouteID == 0 {
				continue
			}
			byRoute[l.RouteID] = append(byRoute[l.RouteID], l)
		}
		for _, l := range s.Live {
			if l.RouteID == 0 || byRoute[l.RouteID] == nil {
				continue
			}
			delete(byRoute, l.RouteID)
			elapsed := liveElapsedAt(l, now)
			if rv, ok := routeByID[l.RouteID]; ok && len(rv.Tries) > 0 {
				target := targetForRoute(rv)
				attempts, total := traceFlightAttempts(rv)
				inputs = append(inputs, input{
					id: l.RouteID, agent: l.Agent, model: l.Model,
					provider:  firstNonEmpty(target.provider, l.Provider),
					accountID: target.accountID,
					who:       target.who,
					failed:    rv.Status >= 400 || rv.Error != "",
					// Concurrency says this route is still active; a
					// SessionRoute "done" race must not end its bird
					// before the snapshot drops the request.
					done:     false,
					observed: elapsed,
					total:    total,
					attempts: attempts,
					phases:   buildFlightPhases(attempts, total, false),
				})
				continue
			}
			outbound := clampDuration(elapsed, 620*time.Millisecond, 1400*time.Millisecond)
			if elapsed < outbound {
				outbound = max(elapsed, 620*time.Millisecond)
			}
			inputs = append(inputs, input{
				id: l.RouteID, agent: l.Agent, model: l.Model, provider: l.Provider,
				observed: elapsed,
				total:    max(elapsed+700*time.Millisecond, 1400*time.Millisecond),
				attempts: []flightAttempt{{
					provider: l.Provider, model: l.Model, start: 0,
					end: max(elapsed, 0),
				}},
				phases: []flightPhase{
					{kind: flightOutbound, provider: l.Provider, model: l.Model, start: 0, end: outbound},
					{kind: flightHold, provider: l.Provider, model: l.Model, start: outbound, end: max(elapsed, outbound)},
				},
			})
		}
	}
	next := make([]flightTrack, 0, len(inputs))
	for _, in := range inputs {
		track, ok := byID[in.id]
		if !ok {
			track = &flightTrack{
				routeID: in.id, agent: in.agent, provider: in.provider,
				accountID: in.accountID, who: in.who,
				firstSeen: now.Add(-in.observed),
			}
		} else if replayActive {
			// Replay position, not wall time, drives the bird. Re-anchor it
			// each frame so pause and seeking freeze or jump truthfully.
			track.firstSeen = now.Add(-in.observed)
		}
		track.agent = in.agent
		if in.model != "" {
			track.model = in.model
		}
		if in.provider != "" {
			track.provider = in.provider
		} else if fallback := firstNonEmpty(providerOf(track.model), track.provider); fallback != "" {
			// A live API row may omit provider while retaining the model
			// prefix. Keep the real provider identity used by the row.
			track.provider = fallback
		}
		track.accountID = firstNonEmpty(in.accountID, track.accountID)
		track.who = firstNonEmpty(in.who, track.who)
		track.lastSeen = now
		track.failed = in.failed
		track.done = in.done
		if !track.done {
			// A replay seek backwards or a still-running live request
			// must not inherit a timed return from an earlier completion.
			track.doneAt = time.Time{}
			track.removedAt = time.Time{}
		} else if !replayActive && track.doneAt.IsZero() {
			track.doneAt = now
		}
		track.observed = in.observed
		track.total = in.total
		track.attempts = in.attempts
		track.phases = in.phases
		track.returnDur = 700 * time.Millisecond
		if len(in.phases) > 0 {
			last := in.phases[len(in.phases)-1]
			if last.kind == flightInbound {
				track.returnDur = clampDuration(last.end-last.start, 420*time.Millisecond, 1400*time.Millisecond)
			}
		}
		track.inReplay = replayActive
		next = append(next, *track)
	}
	// A route that has left a live Gateway snapshot gets one return leg. It
	// is never re-created from history or guessed to be active.
	for i := range m.topology.flights {
		old := m.topology.flights[i]
		found := false
		for j := range next {
			if next[j].routeID == old.routeID {
				found = true
				break
			}
		}
		if !found {
			if replayActive || old.inReplay {
				continue
			}
			if !old.done {
				old.done = true
				old.removedAt = now
				old.doneAt = now
				old.phases = buildFlightPhases(old.attempts, old.total, true)
				old.returnDur = flightReturnDuration(old.phases)
			}
			if old.doneAt.IsZero() {
				old.doneAt = now
			}
			if flightAlive(old, now) {
				next = append(next, old)
			}
		}
	}
	m.topology.flights = next
}

func flightAlive(f flightTrack, now time.Time) bool {
	if !f.done || f.inReplay {
		return true
	}
	return now.Before(f.doneAt.Add(max(f.returnDur, 420*time.Millisecond) + 450*time.Millisecond))
}

// buildFlightPhases lays the recorded attempts onto the visual legs. The
// outbound carrier keeps upstream's 1.4s rhythm; its hold ends at the real
// Try end; a failed try returns through the gateway and a fresh carrier
// continues. withReturn appends the separate 1.4s answer flight home for a
// completed trace; a live request waits at its account until Gateway says it
// is finished.
func buildFlightPhases(attempts []flightAttempt, total time.Duration, withReturn bool) []flightPhase {
	if len(attempts) == 0 {
		return nil
	}
	out := make([]flightPhase, 0, len(attempts)*3+1)
	cursor := time.Duration(0)
	for i, a := range attempts {
		if i == 0 {
			// The request carrier leaves the agent at the route's start
			// and takes 1.4s to reach the first account, as upstream's
			// play() does even when the account answers sooner.
			out = append(out, flightPhase{
				kind: flightOutbound, provider: a.provider, model: a.model,
				accountID: a.accountID, who: a.who,
				start: 0, end: 1400 * time.Millisecond,
			})
			cursor = 1400 * time.Millisecond
		} else {
			// A failed answer goes back to the gateway in 620ms, then a
			// fresh carrier takes the request on in 900ms.
			prev := attempts[i-1]
			out = append(out,
				flightPhase{
					kind: flightRetryBack, provider: prev.provider, model: prev.model,
					accountID: prev.accountID, who: prev.who,
					start: cursor, end: cursor + 620*time.Millisecond, failed: prev.failed,
				},
				flightPhase{
					kind: flightRetryOut, provider: a.provider, model: a.model,
					accountID: a.accountID, who: a.who,
					start: cursor + 620*time.Millisecond, end: cursor + 1520*time.Millisecond,
				},
			)
			cursor += 1520 * time.Millisecond
		}
		// The dot waits at the account until the recorded try ends. When
		// a very short try has already ended before the bird arrives, the
		// hold is instantaneous and the bird turns around.
		holdEnd := max(a.end, cursor)
		out = append(out, flightPhase{
			kind: flightHold, provider: a.provider, model: a.model,
			accountID: a.accountID, who: a.who,
			start: cursor, end: holdEnd, failed: a.failed,
		})
		cursor = holdEnd
	}
	if withReturn {
		last := attempts[len(attempts)-1]
		out = append(out, flightPhase{
			kind: flightInbound, provider: last.provider, model: last.model,
			accountID: last.accountID, who: last.who,
			start: cursor, end: cursor + 1400*time.Millisecond,
			failed: last.failed,
		})
	}
	if len(out) == 0 {
		// Keep an anchor at the route duration so a caller with malformed
		// traces still has a bounded presentation interval.
		out = append(out, flightPhase{kind: flightHold, start: 0, end: max(total, time.Millisecond)})
	}
	return out
}

func flightReturnDuration(phases []flightPhase) time.Duration {
	for i := len(phases) - 1; i >= 0; i-- {
		if phases[i].kind == flightInbound {
			return clampDuration(phases[i].end-phases[i].start, 420*time.Millisecond, 1400*time.Millisecond)
		}
	}
	return 1400 * time.Millisecond
}

func flightPhaseEnd(phases []flightPhase) time.Duration {
	if len(phases) == 0 {
		return 0
	}
	end := time.Duration(0)
	for _, phase := range phases {
		end = max(end, phase.end)
	}
	return end
}

// flightRecordedPhaseEnd is the point where the recorded request is over:
// everything before the presentation-only inbound leg. The return flight
// itself still plays after this on the wall or replay clock.
func flightRecordedPhaseEnd(phases []flightPhase) time.Duration {
	end := time.Duration(0)
	for _, phase := range phases {
		if phase.kind == flightInbound {
			break
		}
		end = max(end, phase.end)
	}
	return end
}

// traceFlightAttempts returns the actual attempt chain in route-relative
// time. A try begins at Try.Start (or the route start when omitted), waits
// at its account for Try.Millis, and a failed try with another try after it
// returns to the gateway before the next one departs.
func traceFlightAttempts(rv routing.RouteView) ([]flightAttempt, time.Duration) {
	total := time.Duration(max(rv.Millis, 0)) * time.Millisecond
	out := make([]flightAttempt, 0, len(rv.Tries))
	for _, tr := range rv.Tries {
		start := time.Duration(0)
		if !tr.Start.IsZero() && tr.Start.After(rv.Time) {
			start = tr.Start.Sub(rv.Time)
		}
		end := start + time.Duration(max(tr.Millis, 0))*time.Millisecond
		failed := tr.Status >= 400 || tr.Fail != "" || tr.Error != ""
		target := traceAttemptTarget(rv, tr)
		out = append(out, flightAttempt{
			provider: target.provider, model: target.model,
			accountID: target.accountID, who: target.who,
			start: start, end: end, failed: failed, done: tr.Done,
		})
		if end > total {
			total = end
		}
	}
	if total <= 0 {
		total = 1400 * time.Millisecond
	}
	return out, total
}

// traceAttemptTarget finds the exact account row that actually seated a Try.
// Try.ID identifies the account; Model identifies the model when the same
// account can serve several. The recorded candidate order is authoritative.
func traceAttemptTarget(rv routing.RouteView, tr routing.Try) providerTarget {
	for _, w := range rv.Order {
		if tr.ID != "" && !strings.EqualFold(w.ID, tr.ID) {
			continue
		}
		if tr.Model != "" && w.Model != "" && !strings.EqualFold(w.Model, tr.Model) {
			continue
		}
		if provider := firstNonEmpty(w.Provider, providerOf(w.Model), w.ID); provider != "" {
			return providerTarget{
				provider:  provider,
				model:     firstNonEmpty(w.Model, tr.Model),
				accountID: firstNonEmpty(w.ID, tr.ID),
				who:       w.Who,
			}
		}
	}
	for _, w := range rv.Order {
		if tr.Model == "" || w.Model == "" || !strings.EqualFold(w.Model, tr.Model) {
			continue
		}
		if provider := firstNonEmpty(w.Provider, providerOf(w.Model), w.ID); provider != "" {
			return providerTarget{
				provider:  provider,
				model:     w.Model,
				accountID: firstNonEmpty(w.ID, tr.ID),
				who:       w.Who,
			}
		}
	}
	return providerTarget{
		provider:  firstNonEmpty(providerOf(tr.Model), rv.Provider),
		model:     firstNonEmpty(tr.Model, rv.Model),
		accountID: firstNonEmpty(tr.ID, tr.Served),
	}
}

func clampDuration(v, lo, hi time.Duration) time.Duration {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func liveElapsedAt(l routing.LiveRequest, now time.Time) time.Duration {
	if l.Millis > 0 {
		return time.Duration(l.Millis) * time.Millisecond
	}
	if l.Started.IsZero() || now.Before(l.Started) {
		return 0
	}
	return now.Sub(l.Started)
}

func (m *Model) drawTopology(p *ui.Painter, r ui.Rect, s routing.Snapshot, routes []routing.RouteView, sel routing.RouteView, hasSel bool, pal palette, now time.Time) {
	if r.W < 80 || r.H < 100 {
		return
	}
	drawTopologyGrid(p, r, pal)
	agents := m.visibleAgents(routes, s.Live)
	if len(agents) == 0 && hasSel {
		agents = m.visibleAgents([]routing.RouteView{sel}, nil)
	}
	replay, replayActive := m.replayTopologyState()
	if replayActive && len(replay.routes) > 0 {
		agents = m.visibleAgents(replay.routes, nil)
	}
	topologyRoutes := append([]routing.RouteView(nil), routes...)
	if replayActive {
		// The stage shows the same trace subset that owns a flight window.
		// This keeps every replayed bird addressable by its real agent and
		// provider, including routes whose recorded request already ended
		// while their return animation is still in progress.
		topologyRoutes = replay.routes
	}
	providers := providerRowsForTopology(sel, hasSel, topologyRoutes, s.Live, m.accountsMasked())
	if len(providers) == 0 {
		providers = providerRowsFromRoutes(topologyRoutes)
	}
	if len(providers) > 5 {
		providers = providers[:5]
	}

	pad := float32(20)
	colW := min((r.W-pad*2-72)/3, 150)
	colW = max(colW, 112)
	cardH := float32(52)
	agentGap := float32(12)
	agentTop := r.Y + max((r.H-float32(len(agents))*cardH-float32(max(len(agents)-1, 0))*agentGap)/2, 54)
	if len(agents) == 0 {
		agentTop = r.Y + r.H/2 - 24
	}
	leftX := r.X + pad
	centerX := r.X + r.W/2 - colW/2
	rightX := r.X + r.W - pad - colW

	layout := topologyFrameLayout{width: r.W, height: r.H, gateway: nodeBox{x: centerX, y: r.Y + r.H/2 - cardH/2, w: colW, h: cardH + 16}}
	for i := range agents {
		layout.agents = append(layout.agents, nodeBox{x: leftX, y: agentTop + float32(i)*(cardH+agentGap), w: colW, h: cardH})
	}
	providerTop := r.Y + max((r.H-float32(len(providers))*cardH-float32(max(len(providers)-1, 0))*agentGap)/2, 54)
	if len(providers) == 0 {
		providerTop = r.Y + r.H/2 - 24
	}
	for i := range providers {
		layout.providers = append(layout.providers, nodeBox{x: rightX, y: providerTop + float32(i)*(cardH+agentGap), w: colW, h: cardH})
	}
	m.mu.Lock()
	m.topology.layout = layout
	m.mu.Unlock()

	// Draw idle links first so activity paints above them.
	if len(layout.agents) == 0 {
		p.Fill(ui.Rect{X: leftX, Y: layout.gateway.y + layout.gateway.h/2 - 1, W: layout.gateway.x - leftX, H: 2}, pal.RouteIdle.Alpha(0.5), 1)
	} else {
		for _, a := range layout.agents {
			ax, ay := a.rightCenter()
			gx, gy := layout.gateway.leftCenter()
			drawCubic(p, point(ax, ay), point(gx, gy), pal.RouteIdle.Alpha(0.45), 1.2)
		}
	}
	if len(layout.providers) == 0 {
		p.Fill(ui.Rect{X: layout.gateway.x + layout.gateway.w, Y: layout.gateway.y + layout.gateway.h/2 - 1, W: rightX - (layout.gateway.x + layout.gateway.w), H: 2}, pal.RouteIdle.Alpha(0.5), 1)
	} else {
		for _, prov := range layout.providers {
			gx, gy := layout.gateway.rightCenter()
			px, py := prov.leftCenter()
			drawCubic(p, point(gx, gy), point(px, py), pal.RouteIdle.Alpha(0.45), 1.2)
		}
	}

	// Draw only paths backed by an observation in this snapshot or by the
	// completed trace currently being replayed. A provider endpoint is used
	// only when its exact id is present in the card layout; otherwise the
	// truthful path stops at Gateway.
	activities := make([]topologyActivity, 0, len(s.Live))
	for _, l := range s.Live {
		activities = append(activities, topologyActivity{
			agent: l.Agent, color: pal.Accent,
			target: providerTarget{provider: l.Provider, model: l.Model},
			seed:   int(l.ID % 997), animate: true,
		})
	}
	for _, rv := range replay.routes {
		target := targetForRoute(rv)
		activity := topologyActivity{
			agent: rv.Agent, target: target, color: agentHue(rv.Agent),
			seed: int(rv.ID % 997), animate: replay.playing,
			failed: rv.Status >= 400 || rv.Error != "",
		}
		if activity.target.provider == "" {
			activity.target.provider = rv.DisplayProvider()
		}
		activities = append(activities, activity)
	}
	for _, activity := range activities {
		ai := indexAgentNode(agents, activity.agent)
		if ai < 0 {
			continue
		}
		anchor := layout.agents[ai]
		ax, ay := anchor.rightCenter()
		gx, gy := layout.gateway.leftCenter()
		drawCubic(p, point(ax, ay), point(gx, gy), activity.color.Alpha(0.9), 2.2)
		drawFlier(p, point(ax, ay), point(gx, gy), activity.color, now, activity.seed, activity.failed)
		pi := indexProviderNode(providers, activity.target)
		if pi >= 0 {
			gx, gy := layout.gateway.rightCenter()
			px, py := layout.providers[pi].leftCenter()
			drawCubic(p, point(gx, gy), point(px, py), activity.color.Alpha(0.9), 2.2)
			drawFlier(p, point(gx, gy), point(px, py), activity.color, now.Add(650*time.Millisecond), activity.seed+1, activity.failed)
		}
		if activity.animate {
			p.AnimationFrame()
		}
	}

	for i, a := range agents {
		localPalette := pal
		localPalette.Accent = a.Hue
		drawAgentCard(p, layout.agents[i].rect(), a, localPalette)
	}
	drawGatewayCard(p, layout.gateway.rect(), s, pal)
	for i, prov := range providers {
		drawProviderCard(p, layout.providers[i].rect(), prov, pal)
	}
	if len(agents) == 0 {
		p.Text(leftX+2, r.Y+r.H/2+12, m.localization().T("routing.noAgents"), 9, pal.Faint)
	}
	_ = sel
}

type topologyActivity struct {
	agent   string
	target  providerTarget
	color   ui.Color
	seed    int
	animate bool
	failed  bool
}

// drawRoutingStageCanvas keeps the stage's static topology in a MyGo Canvas.
// The canvas is drawn again only when the agent/account layout or a lit
// route changes, while the moving birds and packets remain live painters.
// This follows MyGo's own Canvas guidance for content that seldom changes.
func (m *Model) drawRoutingStageCanvas(p *ui.Painter, r ui.Rect, s routing.Snapshot, routes []routing.RouteView, sel routing.RouteView, hasSel bool, pal palette, now time.Time, history bool) {
	if r.W < 200 || r.H < 140 {
		return
	}
	replay, replayActive := m.replayTopologyState()
	agents := m.stageAgents(s, sel, hasSel, routes, replay, replayActive)
	if len(agents) == 0 {
		p.Text(r.X+8, r.Y+r.H/2, m.localization().T("routing.noAgents"), 10, pal.Faint)
		return
	}
	rows := m.stageAccountRows(sel, hasSel, s.Live, replay.routes, replayActive)
	if len(rows) == 0 {
		rows = []providerNode{{Name: m.localization().T("routing.noTrace")}}
	}
	// The Canvas has its own origin: all static geometry is in local card
	// coordinates, while live flights below use the window's coordinates.
	localStage := layoutRoutingStage(ui.Rect{W: r.W, H: r.H}, agents, rows)

	target := providerTarget{}
	if hasSel {
		target = targetForRoute(sel)
	}
	gatewayURL := m.Settings().GatewayURL
	litAgents := map[string]bool{}
	litRows := map[int]bool{}
	m.mu.Lock()
	flights := append([]flightTrack(nil), m.topology.flights...)
	for _, f := range flights {
		if !flightAlive(f, now) {
			continue
		}
		if ai := indexAgentNode(agents, f.agent); ai >= 0 {
			litAgents[strings.ToLower(agents[ai].ID)] = true
		}
		phase := flightPhase{}
		if len(f.phases) > 0 {
			phase = f.phases[len(f.phases)-1]
		}
		if pi := indexProviderNode(rows, targetForPhase(f, phase)); pi >= 0 {
			litRows[pi] = true
		}
	}
	if !activityVisible(flights, now) && hasSel {
		if ai := indexAgentNode(agents, sel.Agent); ai >= 0 {
			litAgents[strings.ToLower(agents[ai].ID)] = true
		}
		if pi := indexProviderNode(rows, target); pi >= 0 {
			litRows[pi] = true
		}
	}
	stageKey := stageCanvasKey(agents, rows, target.provider, hasSel, pal, s.GatewayOnline, gatewayURL, litAgents, litRows)
	scale := p.Scale()
	pixelW, pixelH := int(math.Ceil(float64(r.W*scale))), int(math.Ceil(float64(r.H*scale)))
	if m.topology.stageCanvas == nil || m.topology.stageCanvasW != pixelW ||
		m.topology.stageCanvasH != pixelH || m.topology.stageCanvasScale != scale {
		// MyGo exposes NewCanvas, not Canvas.resize. Creating a canvas at the
		// current raster size is the supported way to resize the cached layer.
		m.topology.stageCanvas = ui.NewCanvas(pixelW, pixelH)
		m.topology.stageCanvasKey = ""
		m.topology.stageCanvasW = pixelW
		m.topology.stageCanvasH = pixelH
		m.topology.stageCanvasScale = scale
	}
	redraw := m.topology.stageCanvasKey != stageKey
	m.mu.Unlock()
	if redraw {
		m.topology.stageCanvas.Draw(p, func(cp *ui.Painter) {
			cp.Transform(ui.Scaling(scale, scale), 0, 0, func() {
				drawRoutingStatic(cp, ui.Rect{W: r.W, H: r.H}, localStage.agents, localStage.hub, rows, localStage.rows, agents, litAgents, litRows, s, gatewayURL, pal)
			})
		})
		m.mu.Lock()
		m.topology.stageCanvasKey = stageKey
		m.mu.Unlock()
	}
	p.Image(m.topology.stageCanvas.Bitmap(), r, ui.FillBox)

	// Draw the wires and their highlights over the static node layer, then
	// the birds above both. This is the same layer order as upstream's
	// rt-wires and rt-sky and keeps all moving geometry in the live painter.
	activeFlight := false
	stageGeometryKey := stageIdentityKey(agents, rows, r.W, r.H)
	m.mu.Lock()
	geometry := m.topology.flightGeometry
	if geometry == nil || geometry.key != stageGeometryKey {
		geometry = newFlightGeometryKeyed(localStage, stageGeometryKey)
		m.topology.flightGeometry = geometry
	}
	m.mu.Unlock()
	// The canvas and flight geometry share local card coordinates. One
	// painter transform moves the live layer into place without allocating
	// offset copies of every node on every animation frame.
	p.Transform(ui.Translation(r.X, r.Y), 0, 0, func() {
		for _, f := range flights {
			if !flightAlive(f, now) {
				continue
			}
			ai := indexAgentNode(agents, f.agent)
			if ai < 0 {
				continue
			}
			drawFlightWire(p, geometry, ai, f, agentHue(f.agent), now)
			activeFlight = true
		}
		if !activeFlight && hasSel {
			ai := indexAgentNode(agents, sel.Agent)
			if ai >= 0 {
				drawStillRoute(p, geometry.agents[ai], geometry.hub, geometry.rows, indexProviderNode(rows, target), agentHue(sel.Agent))
			}
		}
		for _, f := range flights {
			if !flightAlive(f, now) {
				continue
			}
			ai := indexAgentNode(agents, f.agent)
			if ai < 0 {
				continue
			}
			drawFlightActor(p, geometry, ai, f, agentHue(f.agent), pal, now, m.reduceMotionEnabled())
		}
	})
	replayPlaying := false
	if replayActive {
		replayPlaying = replay.playing
	}
	if !m.reduceMotionEnabled() && (activeFlight && !replayActive || replayActive && replayPlaying) {
		// The replay clock itself moves even between flights; draw again so
		// pause, seek and newly started returns stay in step with it.
		p.AnimationFrame()
	}
}

func activityVisible(flights []flightTrack, now time.Time) bool {
	for _, f := range flights {
		if flightAlive(f, now) {
			return true
		}
	}
	return false
}

func (m *Model) reduceMotionEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.topology.reduceMotion
}

func stageCanvasKey(agents []agentNode, rows []providerNode, provider string, hasSel bool, pal palette, online bool, gatewayURL string, litAgents map[string]bool, litRows map[int]bool) string {
	var b strings.Builder
	for _, a := range agents {
		b.WriteString(a.ID)
		b.WriteByte(0)
		b.WriteString(a.Model)
		b.WriteByte(0)
		b.WriteString(fmt.Sprintf("%t/%d", a.Active, a.Requests))
		b.WriteByte(0)
	}
	for _, r := range rows {
		b.WriteString(r.Provider)
		b.WriteByte(0)
		b.WriteString(r.Name)
		b.WriteByte(0)
		b.WriteString(r.Model)
		b.WriteByte(0)
		b.WriteString(r.AccountID)
		b.WriteByte(0)
		b.WriteString(r.Who)
		b.WriteByte(0)
		b.WriteString(r.Plan)
		b.WriteByte(0)
		b.WriteString(r.Note)
		b.WriteByte(0)
		b.WriteString(r.Weight)
		b.WriteByte(0)
		b.WriteString(r.Status)
		b.WriteByte(0)
		b.WriteString(fmt.Sprintf("%t/%t/%t/%t/%t/%t", r.Current, r.Failed, r.Rest, r.Fallback, r.Answered, r.WentOn))
		b.WriteByte(0)
	}
	b.WriteString(provider)
	b.WriteByte(0)
	if hasSel {
		b.WriteByte(1)
	}
	for _, a := range agents {
		if litAgents[strings.ToLower(a.ID)] {
			b.WriteByte('A')
			b.WriteString(a.ID)
			b.WriteByte(0)
		}
	}
	for i := range rows {
		if litRows[i] {
			b.WriteByte('R')
			b.WriteString(itoa(i))
			b.WriteByte(0)
		}
	}
	b.WriteString(fmt.Sprintf("%v|%v|%v|%v|%t|%s", pal.Canvas, pal.Panel, pal.Line, pal.Text, online, gatewayURL))
	return b.String()
}

func drawRoutingStatic(p *ui.Painter, r ui.Rect, agentsBoxes []nodeBox, hub nodeBox, rows []providerNode, rowsBoxes []nodeBox, agents []agentNode, litAgents map[string]bool, litRows map[int]bool, s routing.Snapshot, gatewayURL string, pal palette) {
	for _, a := range agentsBoxes {
		ax, ay := a.rightCenter()
		hx, hy := hub.leftCenter()
		drawCubic(p, point(ax, ay), point(hx, hy), pal.RouteIdle, 1.3)
	}
	for _, row := range rowsBoxes {
		hx, hy := hub.rightCenter()
		rx, ry := row.leftCenter()
		drawCubic(p, point(hx, hy), point(rx, ry), pal.RouteIdle, 1.3)
	}
	for i, a := range agents {
		drawRoutingAgentBox(p, agentsBoxes[i].rect(), a, litAgents[strings.ToLower(a.ID)], pal)
	}
	drawRoutingHub(p, hub.rect(), s, gatewayURL, pal)
	for i, row := range rows {
		drawRoutingAccountRow(p, rowsBoxes[i].rect(), row, litRows[i], pal)
	}
	_ = r
}

func drawStillRoute(p *ui.Painter, agent nodeBox, hub nodeBox, rows []nodeBox, providerIndex int, color ui.Color) {
	ax, ay := agent.rightCenter()
	hx, hy := hub.leftCenter()
	drawCubic(p, point(ax, ay), point(hx, hy), color, 1.8)
	if providerIndex >= 0 && providerIndex < len(rows) {
		hx, hy = hub.rightCenter()
		rx, ry := rows[providerIndex].leftCenter()
		drawCubic(p, point(hx, hy), point(rx, ry), color, 1.8)
	}
}

// stageAgents is the upstream stage's agent set, not the whole request
// history: every agent with a request active in this exact snapshot, or the
// one agent of the request told about when the gateway is idle. During a
// replay it is every agent actually in flight at the replay cursor.
func (m *Model) stageAgents(s routing.Snapshot, sel routing.RouteView, hasSel bool, routes []routing.RouteView, replay replayTopology, replayActive bool) []agentNode {
	var out []agentNode
	seen := map[string]bool{}
	add := func(id, model string, active bool) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		key := strings.ToLower(id)
		if seen[key] {
			for i := range out {
				if strings.EqualFold(out[i].ID, id) {
					out[i].Active = out[i].Active || active
					if out[i].Model == "" {
						out[i].Model = model
					}
				}
			}
			return
		}
		seen[key] = true
		out = append(out, agentNode{
			ID: id, Name: agentName(id), Model: model, Active: active, Hue: agentHue(id),
		})
	}
	if replayActive {
		for _, rv := range replay.routes {
			add(rv.Agent, rv.Model, true)
		}
	} else {
		for _, l := range s.Live {
			add(l.Agent, l.Model, true)
		}
	}
	if len(out) == 0 && hasSel {
		add(sel.Agent, sel.Model, sel.Active)
	}
	if len(out) == 0 && len(routes) > 0 {
		add(routes[0].Agent, routes[0].Model, false)
	}
	return out
}

// stageAccountRows is exactly the accounts of the request told about plus
// the providers of requests currently active. It never blends every row in
// the history into one fictitious candidate set.
func (m *Model) stageAccountRows(sel routing.RouteView, hasSel bool, live []routing.LiveRequest, replay []routing.RouteView, replayActive bool) []providerNode {
	var rows []providerNode
	seen := map[string]bool{}
	add := func(items []providerNode) {
		for _, n := range items {
			key := strings.ToLower(n.Provider + "\x00" + n.Name + "\x00" + n.Model + "\x00" + n.AccountID)
			if key == "\x00\x00\x00" || seen[key] {
				continue
			}
			seen[key] = true
			rows = append(rows, n)
		}
	}
	if hasSel {
		add(providerRows(sel, m.accountsMasked()))
	}
	if replayActive {
		for _, rv := range replay {
			add(providerRows(rv, m.accountsMasked()))
		}
	} else {
		for _, l := range live {
			if l.Provider == "" && l.Model == "" {
				continue
			}
			add([]providerNode{{
				Provider: l.Provider, Name: l.Provider, Model: l.Model,
				Status: string(l.Phase), Current: true,
			}})
		}
	}
	return rows
}

func drawRoutingAgentBox(p *ui.Painter, r ui.Rect, a agentNode, lit bool, pal palette) {
	border := pal.Line
	if lit {
		border = a.Hue
	}
	p.Fill(r, pal.Panel, 10)
	width := float32(1)
	if lit {
		width = 1.6
	}
	p.Stroke(r, border, 10, width)
	cx := r.X + r.W/2
	compact := r.H < 70
	if compact {
		p.Fill(ui.Rect{X: r.X + 9, Y: r.Y + r.H/2 - 11, W: 22, H: 22}, a.Hue.Alpha(0.16), 6)
		p.Text(r.X+14, r.Y+r.H/2-6, agentInitials(a.ID), 8, a.Hue)
		p.Text(r.X+39, r.Y+r.H/2-16, truncate(agentName(a.ID), 16), 11, pal.Text)
		p.Text(r.X+39, r.Y+r.H/2-1, truncate(a.Model, 15), 9, pal.Muted)
		if lit {
			p.Fill(ui.Rect{X: r.X + r.W - 9, Y: r.Y + 7, W: 5, H: 5}, a.Hue, 3)
		}
		return
	}
	p.Fill(ui.Rect{X: cx - 13, Y: r.Y + 8, W: 26, H: 26}, a.Hue.Alpha(0.16), 7)
	p.Text(cx-8, r.Y+15, agentInitials(a.ID), 9, a.Hue)
	drawTextCentered(p, agentName(a.ID), cx, r.Y+43, 12, pal.Text)
	drawTextCentered(p, truncate(a.Model, 19), cx, r.Y+59, 10, pal.Muted)
	if lit {
		p.Fill(ui.Rect{X: r.X + r.W - 10, Y: r.Y + 8, W: 5, H: 5}, a.Hue, 3)
	}
}

func drawTextCentered(p *ui.Painter, s string, cx, y, size float32, color ui.Color) {
	width := float32(0)
	for _, r := range s {
		if r > 0x2e80 {
			width += size
		} else {
			width += size * 0.56
		}
	}
	p.Text(cx-width/2, y, s, size, color)
}

func drawRoutingHub(p *ui.Painter, r ui.Rect, s routing.Snapshot, gatewayURL string, pal palette) {
	p.Fill(r, pal.Panel, 12)
	p.Stroke(r, pal.Text, 12, 1.2)
	cx := r.X + r.W/2
	drawMagpieMark(p, ui.Rect{X: cx - 13, Y: r.Y + 9, W: 26, H: 26}, pal.Text, pal.Panel)
	drawTextCentered(p, "magpie", cx, r.Y+45, 12, pal.Text)
	drawTextCentered(p, gatewayURL, cx, r.Y+61, 10, pal.Muted)
	chipW := float32(50)
	status := "online"
	color := pal.Success
	if !s.GatewayOnline {
		status = "offline"
		color = pal.Danger
	}
	p.Fill(ui.Rect{X: cx - chipW/2, Y: r.Y + 69, W: chipW, H: 20}, pal.Text, 10)
	drawTextCentered(p, status, cx, r.Y+73, 9, pal.Panel)
	_ = color
}

func drawRoutingAccountRow(p *ui.Painter, r ui.Rect, n providerNode, active bool, pal palette) {
	border := pal.Line
	if n.Failed {
		border = pal.Danger.Alpha(0.8)
	} else if active {
		border = pal.Text
	} else if n.Answered {
		border = pal.Success.Alpha(0.65)
	} else if n.Rest {
		border = pal.LineSoft
	} else if n.Fallback {
		border = pal.LineSoft
	}
	p.Fill(r, pal.Panel, 9)
	width := float32(1)
	if active || n.Answered || n.Failed {
		width = 1.5
	}
	p.Stroke(r, border, 9, width)
	dotColor := pal.Faint
	if n.Failed {
		dotColor = pal.Danger
	} else if n.Answered {
		dotColor = pal.Success
	} else if active || n.Current {
		dotColor = pal.Text
	}
	p.Fill(ui.Rect{X: r.X + 11, Y: r.Y + r.H/2 - 4, W: 8, H: 8}, dotColor, 4)
	if r.H < 40 {
		p.Text(r.X+28, r.Y+4, truncate(firstNonEmpty(n.Name, n.Provider), 20), 10.5, pal.Text)
		model := firstNonEmpty(n.Model, n.Plan)
		if model != "" {
			model = " · " + truncate(model, 20)
		}
		p.Text(r.X+28, r.Y+17, truncate(firstNonEmpty(n.Who, "—"), 16)+model, 9, pal.Muted)
		if n.Status != "" {
			p.Text(r.X+r.W-72, r.Y+8, truncate(n.Status, 16), 9, boolColor(n.Failed, pal.Danger, pal.Muted))
		}
		return
	}
	p.Text(r.X+28, r.Y+9, truncate(firstNonEmpty(n.Name, n.Provider), 24), 12, pal.Text)
	model := firstNonEmpty(n.Model, n.Plan)
	if model != "" {
		model = " · " + truncate(model, 28)
	}
	p.Text(r.X+28, r.Y+25, truncate(firstNonEmpty(n.Who, "—"), 22)+model, 10.5, pal.Muted)
	if n.Status != "" {
		p.Text(r.X+r.W-82, r.Y+15, truncate(n.Status, 18), 10.5, boolColor(n.Failed, pal.Danger, pal.Muted))
	}
	if n.Fallback || n.Rest || n.WentOn {
		tag := "fallback"
		if n.Rest {
			tag = "resting"
		} else if n.WentOn {
			tag = "re-route"
		}
		p.Text(r.X+r.W-68, r.Y+r.H-15, tag, 8.5, pal.Faint)
	}
}

// flightPath is a polyline with cumulative arc lengths. Following distance
// rather than Bézier parameter keeps the bird's speed even through bends.
type flightPath struct {
	pts []flightPoint
	cum []float32
}

type flightPoint struct {
	x, y float32
}

func newFlightPath(raw []flightPoint) flightPath {
	var p flightPath
	for _, pt := range raw {
		if len(p.pts) > 0 {
			prev := p.pts[len(p.pts)-1]
			if math.Hypot(float64(pt.x-prev.x), float64(pt.y-prev.y)) < 0.05 {
				continue
			}
			p.cum = append(p.cum, p.cum[len(p.cum)-1]+float32(math.Hypot(float64(pt.x-prev.x), float64(pt.y-prev.y))))
		} else {
			p.cum = append(p.cum, 0)
		}
		p.pts = append(p.pts, pt)
	}
	return p
}

func (p flightPath) length() float32 {
	if len(p.cum) == 0 {
		return 0
	}
	return p.cum[len(p.cum)-1]
}

func (p flightPath) at(distance float32) (x, y, angle float32, ok bool) {
	if len(p.pts) == 0 {
		return 0, 0, 0, false
	}
	if len(p.pts) == 1 {
		return p.pts[0].x, p.pts[0].y, 0, true
	}
	distance = max(0, min(distance, p.length()))
	i := sort.Search(len(p.cum), func(i int) bool { return p.cum[i] >= distance })
	if i == 0 {
		i = 1
	}
	if i >= len(p.pts) {
		i = len(p.pts) - 1
	}
	from, to := p.pts[i-1], p.pts[i]
	seg := max(p.cum[i]-p.cum[i-1], 0.001)
	t := max(0, min((distance-p.cum[i-1])/seg, 1))
	x = from.x + (to.x-from.x)*t
	y = from.y + (to.y-from.y)*t
	angle = float32(math.Atan2(float64(to.y-from.y), float64(to.x-from.x)) * 180 / math.Pi)
	return x, y, angle, true
}

func (p flightPath) reversed() flightPath {
	raw := make([]flightPoint, len(p.pts))
	for i := range p.pts {
		raw[i] = p.pts[len(p.pts)-1-i]
	}
	return newFlightPath(raw)
}

func cubicFlightPoints(ax, ay, bx, by float32) []flightPoint {
	const steps = 40
	dx := max((bx-ax)*0.48, 22)
	out := make([]flightPoint, 0, steps+1)
	for i := 0; i <= steps; i++ {
		t := float32(i) / steps
		u := 1 - t
		x := u*u*u*ax + 3*u*u*t*(ax+dx) + 3*u*t*t*(bx-dx) + t*t*t*bx
		y := u*u*u*ay + 3*u*u*t*ay + 3*u*t*t*by + t*t*t*by
		out = append(out, flightPoint{x, y})
	}
	return out
}

func stageOutboundPath(agent, hub nodeBox, provider *nodeBox) flightPath {
	ax, ay := agent.rightCenter()
	hlx, hly := hub.leftCenter()
	hcx, hcy := hub.x+hub.w/2, hub.y+hub.h/2
	raw := cubicFlightPoints(ax, ay, hlx, hly)
	// The via across the hub is covered by the node, but gives the bird a
	// continuous arc through it instead of jumping between two paths.
	raw = append(raw[1:], cubicFlightPoints(hlx, hly, hcx, hcy)...)
	if provider == nil {
		return newFlightPath(raw)
	}
	hrx, hry := hub.rightCenter()
	raw = append(raw[1:], cubicFlightPoints(hcx, hcy, hrx, hry)...)
	px, py := provider.leftCenter()
	raw = append(raw[1:], cubicFlightPoints(hrx, hry, px, py)...)
	return newFlightPath(raw)
}

func agentToHubPath(agent, hub nodeBox) flightPath {
	ax, ay := agent.rightCenter()
	hlx, hly := hub.leftCenter()
	return newFlightPath(cubicFlightPoints(ax, ay, hlx, hly))
}

func providerToHubPath(hub nodeBox, provider *nodeBox) flightPath {
	if provider == nil {
		return flightPath{}
	}
	px, py := provider.leftCenter()
	hrx, hry := hub.rightCenter()
	return newFlightPath(cubicFlightPoints(px, py, hrx, hry))
}

func hubToProviderPath(hub nodeBox, provider *nodeBox) flightPath {
	if provider == nil {
		return flightPath{}
	}
	hrx, hry := hub.rightCenter()
	px, py := provider.leftCenter()
	return newFlightPath(cubicFlightPoints(hrx, hry, px, py))
}

type flightMoment struct {
	phase     flightPhase
	elapsed   time.Duration
	local     time.Duration
	progress  float32
	hasPhase  bool
	returning bool
	finished  bool
}

// flightMomentAt maps a track to its current real phase. Replay follows the
// replay cursor; a live request follows monotonic frame time and then a
// short return leg once the Gateway drops it. No untracked request is made.
func flightMomentAt(f flightTrack, now time.Time) flightMoment {
	elapsed := f.observed
	if !f.inReplay {
		if f.done {
			if len(f.phases) > 0 {
				elapsed = flightPhaseEnd(f.phases)
			}
			ret := now.Sub(f.doneAt)
			if f.doneAt.IsZero() {
				ret = now.Sub(f.lastSeen)
			}
			returnDur := max(f.returnDur, 420*time.Millisecond)
			return flightMoment{
				phase: flightPhase{
					kind: flightInbound, provider: f.provider, model: f.model,
					accountID: f.accountID, who: f.who, failed: f.failed,
				},
				elapsed:  elapsed + max(ret, 0),
				local:    max(ret, 0),
				progress: cosineEase(clamp01(float32(ret) / float32(returnDur))),
				hasPhase: true, returning: true,
			}
		}
		elapsed = max(now.Sub(f.firstSeen), 0)
	} else if f.done && len(f.phases) > 0 {
		// Replay time keeps advancing after the recorded completion so
		// the answer carrier can fly home. It is still the replay clock,
		// never a fresh live observation.
		elapsed = f.observed
	}
	for _, ph := range f.phases {
		if elapsed < ph.start {
			continue
		}
		if elapsed <= ph.end || ph == f.phases[len(f.phases)-1] {
			span := max(ph.end-ph.start, time.Millisecond)
			local := elapsed - ph.start
			return flightMoment{
				phase: ph, elapsed: elapsed, local: local,
				progress: cosineEase(clamp01(float32(local) / float32(span))),
				hasPhase: true, returning: ph.kind == flightInbound || ph.kind == flightRetryBack,
			}
		}
	}
	if f.done {
		return flightMoment{finished: true}
	}
	if len(f.phases) > 0 {
		ph := f.phases[len(f.phases)-1]
		return flightMoment{phase: ph, elapsed: elapsed, progress: 1, hasPhase: true}
	}
	return flightMoment{}
}

// drawFlightWire paints the real route above the static node layer so the
// lit agent-to-hub segment remains visible, as upstream's rt-wires do. The
// source wire is solid once the route is over and a flowing dashed line
// while the request or retry is actually in progress.
func drawFlightWire(p *ui.Painter, geometry *flightGeometry, agentIndex int, f flightTrack, color ui.Color, now time.Time) {
	if geometry == nil || agentIndex < 0 || agentIndex >= len(geometry.agents) {
		return
	}
	moment := flightMomentAt(f, now)
	providerIndex := geometry.providerIndex(targetForPhase(f, moment.phase))
	var wire [2]flightPath
	count := 0
	if moment.hasPhase {
		switch moment.phase.kind {
		case flightRetryBack:
			if providerIndex >= 0 && providerIndex < len(geometry.providerHub) {
				wire[count] = geometry.providerHub[providerIndex]
				count++
			}
		case flightRetryOut:
			if providerIndex >= 0 && providerIndex < len(geometry.hubProvider) {
				wire[count] = geometry.hubProvider[providerIndex]
				count++
			}
		case flightInbound:
			if providerIndex >= 0 && providerIndex < len(geometry.providerHub) {
				wire[count] = geometry.providerHub[providerIndex]
				count++
			}
			wire[count] = geometry.agentHub[agentIndex]
			count++
		default:
			wire[count] = geometry.agentHub[agentIndex]
			count++
			if providerIndex >= 0 && providerIndex < len(geometry.hubProvider) {
				wire[count] = geometry.hubProvider[providerIndex]
				count++
			}
		}
	} else {
		wire[count] = geometry.agentHub[agentIndex]
		count++
		if providerIndex >= 0 && providerIndex < len(geometry.hubProvider) {
			wire[count] = geometry.hubProvider[providerIndex]
			count++
		}
	}
	live := !moment.finished && (!f.done || (f.inReplay && moment.phase.kind != flightInbound))
	for _, path := range wire[:count] {
		if path.length() <= 0 {
			continue
		}
		if live {
			drawFlowingPath(p, path, color, 1.45, now)
		} else {
			drawFlightPath(p, path, color.Alpha(0.9), 1.55)
		}
	}
}

func drawFlightPath(p *ui.Painter, path flightPath, color ui.Color, width float32) {
	if len(path.pts) < 2 {
		return
	}
	var outline ui.Path
	outline.MoveTo(path.pts[0].x, path.pts[0].y)
	for _, pt := range path.pts[1:] {
		outline.LineTo(pt.x, pt.y)
	}
	p.StrokePath(&outline, width, color)
}

// drawFlightActor paints the bird and its packet above the nodes: a request
// dot out, a held request while an account answers, a failure back through
// the gateway, and the answer home.
func drawFlightActor(p *ui.Painter, geometry *flightGeometry, agentIndex int, f flightTrack, agentColor ui.Color, pal palette, now time.Time, reduceMotion bool) {
	if geometry == nil || agentIndex < 0 || agentIndex >= len(geometry.agents) {
		return
	}
	moment := flightMomentAt(f, now)
	providerIndex := geometry.providerIndex(targetForPhase(f, moment.phase))
	path := geometry.outboundPath(agentIndex, providerIndex)
	carry := magpieCarryRequest
	outcome := packetOutcomeUnknown
	if f.inReplay {
		// History knows only the final outcome, so the result dot is a
		// ring for success and red-filled for the final failure.
		outcome = packetOutcomeOK
		if f.failed {
			outcome = packetOutcomeFailed
		}
	} else if moment.phase.failed {
		outcome = packetOutcomeFailed
	}
	switch moment.phase.kind {
	case flightRetryBack:
		if providerIndex >= 0 && providerIndex < len(geometry.providerHub) {
			path = geometry.providerHub[providerIndex]
		}
		carry = magpieCarryResult
	case flightRetryOut:
		if providerIndex >= 0 && providerIndex < len(geometry.hubProvider) {
			path = geometry.hubProvider[providerIndex]
		}
	case flightInbound:
		path = geometry.inboundPath(agentIndex, providerIndex)
		carry = magpieCarryResult
	}
	if path.length() <= 0 {
		return
	}
	switch moment.phase.kind {
	case flightHold:
		x, y, _, _ := path.at(path.length())
		drawHeldPacket(p, x, y, agentColor, outcome, now)
		// The carrier leaves as soon as it lets go of the request. Its
		// 450ms exit mirrors .rt-flier.away.
		if moment.local < 450*time.Millisecond {
			pose := magpiePose{
				x: x, y: y, scale: 0.72, aim: 0, flapAt: now,
				body: pal.BirdBody, belly: pal.BirdBelly, far: pal.BirdWing,
				appear:    1,
				away:      float32(moment.local) / float32(450*time.Millisecond),
				flapPhase: float32(moment.local%420) / 420,
			}
			drawMagpie(p, pose, agentColor, magpieCarryNone, outcome, reduceMotion)
		}
		return
	case flightInbound:
		if moment.progress >= 1 && !f.inReplay {
			return
		}
		if moment.progress >= 1 && f.inReplay {
			return
		}
	}
	d := moment.progress * path.length()
	x, y, _, ok := path.at(d)
	if !ok {
		return
	}
	aim, flip := pathAim(path, d)
	// Upstream eases b._a 12% toward the path's tangent each frame. A
	// 131ms time constant gives the same response at 60fps and holds it at
	// other refresh rates, without depending on a fixed frame count.
	if startAim, _ := pathAim(path, 0); ok {
		const tau = 131 * time.Millisecond
		k := 1 - float32(math.Exp(-float64(moment.local)/float64(tau)))
		aim = startAim + shortestAngle(startAim, aim)*k
	}
	// Every flight leg gets a new bird, as upstream's bird("req") and
	// bird("res") do; rtcome eases it in over 300ms.
	appear := easeOut(clamp01(float32(moment.local) / float32(300*time.Millisecond)))
	pose := magpiePose{
		x: x, y: y, scale: 0.72, aim: aim, flip: flip, flapAt: now,
		body: pal.BirdBody, belly: pal.BirdBelly, far: pal.BirdWing,
		appear: appear, flapPhase: float32(moment.local%420) / 420,
	}
	drawMagpie(p, pose, agentColor, carry, outcome, reduceMotion)
}

func pathAim(path flightPath, distance float32) (aim, flip float32) {
	distance = max(0, min(distance, path.length()))
	prev := max(0, distance-3)
	next := min(path.length(), distance+3)
	x0, y0, _, ok0 := path.at(prev)
	x1, y1, _, ok1 := path.at(next)
	if !ok0 || !ok1 {
		return 0, 1
	}
	vx, vy := x1-x0, y1-y0
	flip = 1
	if vx < 0 {
		flip = -1
	}
	aim = float32(math.Atan2(float64(vy), math.Abs(float64(vx))) * 180 / math.Pi)
	return max(-24, min(24, aim)), flip
}

func shortestAngle(from, to float32) float32 {
	d := float32(math.Mod(float64(to-from+180), 360))
	if d < 0 {
		d += 360
	}
	return d - 180
}

func smoothstep(t float32) float32 {
	t = max(0, min(t, 1))
	return t * t * (3 - 2*t)
}

// cubicBezierEase evaluates CSS's cubic-bezier(x1,y1,x2,y2) timing
// function at progress t. The x axis is solved by Newton's method with a
// bisection fallback, as browsers do for a monotone timing curve.
func cubicBezierEase(x1, y1, x2, y2, t float32) float32 {
	t = clamp01(t)
	if t == 0 || t == 1 {
		return t
	}
	bezier := func(a, b, u float32) float32 {
		v := 1 - u
		return 3*v*v*u*a + 3*v*u*u*b + u*u*u
	}
	slope := func(a, b, u float32) float32 {
		v := 1 - u
		return 3*v*v*a + 6*v*u*(b-a) + 3*u*u*(1-b)
	}
	u := t
	for range 6 {
		x := bezier(x1, x2, u) - t
		if math.Abs(float64(x)) < 1e-5 {
			return bezier(y1, y2, u)
		}
		d := slope(x1, x2, u)
		if math.Abs(float64(d)) < 1e-5 {
			break
		}
		u -= x / d
		if u < 0 || u > 1 {
			break
		}
	}
	lo, hi := float32(0), float32(1)
	for range 20 {
		u = (lo + hi) / 2
		if bezier(x1, x2, u) < t {
			lo = u
		} else {
			hi = u
		}
	}
	return bezier(y1, y2, (lo+hi)/2)
}

func easeOut(t float32) float32 {
	return cubicBezierEase(0, 0, 0.58, 1, t)
}

func easeDefault(t float32) float32 {
	return cubicBezierEase(0.25, 0.1, 0.25, 1, t)
}

func easeAway(t float32) float32 {
	return cubicBezierEase(0.3, 0, 0.2, 1, t)
}

func cosineEase(t float32) float32 {
	t = clamp01(t)
	return (1 - float32(math.Cos(float64(t)*math.Pi))) / 2
}

func clamp01(v float32) float32 { return max(0, min(v, 1)) }

type magpiePacket int

const (
	magpieCarryRequest magpiePacket = iota
	magpieCarryResult
	magpieCarryNone
)

// magpiePose is the complete per-frame pose of the native FLIER. Keeping it
// independent of route state makes the drawing deterministic and testable.
type magpiePose struct {
	x, y   float32
	scale  float32
	aim    float32
	flip   float32
	flapAt time.Time
	// flapPhase is this carrier's progress through rtflap's 420ms cycle.
	// Each leg gets a fresh bird, so concurrent routes do not share one
	// page-wide wing phase.
	flapPhase float32
	appear    float32
	away      float32
	body      ui.Color
	belly     ui.Color
	far       ui.Color
	bob       float32
}

type packetOutcome uint8

const (
	packetOutcomeUnknown packetOutcome = iota
	packetOutcomeOK
	packetOutcomeFailed
)

// drawMagpie ports the upstream FLIER geometry and layer order: far wing,
// tail, elliptical body and belly, round head, beak, then the near wing.
// Both wings scale vertically around the shoulder, matching rtflap, rather
// than moving independently shaped tips. Agent hue belongs to the packet,
// not the bird; the bird itself uses the interface ink and card colors.
func drawMagpie(p *ui.Painter, pose magpiePose, agentColor ui.Color, carry magpiePacket, outcome packetOutcome, reduceMotion bool) {
	if pose.scale <= 0 {
		return
	}
	aim := pose.aim
	flip := pose.flip
	if flip == 0 {
		flip = 1
	}
	if aim > 90 || aim < -90 {
		flip = -flip
		if aim > 90 {
			aim -= 180
		} else {
			aim += 180
		}
	}
	aim = max(-24, min(24, aim))
	appear := pose.appear
	// CSS's transform order applies scale first and translate last. The
	// native transform stack below is T * R * S, so an appearance zoom must
	// also scale the rtcome offset to keep the same path.
	scale := pose.scale * (0.85 + 0.15*appear)
	offsetY := pose.bob
	opacity := appear
	if pose.away > 0 {
		k := easeAway(pose.away)
		scale *= 1 - 0.2*k
		opacity *= 1 - k
	}
	if opacity <= 0.01 {
		return
	}
	body := pose.body
	if body == (ui.Color{}) {
		body = ui.Hex("#ededf1")
	}
	belly := pose.belly
	if belly == (ui.Color{}) {
		belly = ui.Hex("#232327")
	}
	far := pose.far
	if far == (ui.Color{}) {
		far = body
	}
	// The upstream flier is authored around x=-6.5, y=3.2 and then scaled
	// by 1.2. Bake that origin into the transform so the packet at the beak
	// remains on the moving path.
	phase := float32(0)
	if !reduceMotion {
		phase = pose.flapPhase
		if phase == 0 && !pose.flapAt.IsZero() {
			ms := pose.flapAt.UnixMilli()
			if ms < 0 {
				ms = -ms
			}
			phase = float32(ms%420) / 420
		}
	}
	wing, skew := wingFrame(phase)
	farWing, farSkew := wingFrame(phase - 0.03/0.42)
	appearX := -6 * (1 - appear) * scale
	appearY := -10 * (1 - appear) * scale
	away := easeAway(pose.away)
	awayX := 14 * away
	awayY := -16 * away
	// Match the original SVG's coordinate space: translate the whole bird to
	// the path point, rotate there, then scale the local artwork. Scaling
	// around pose.x/pose.y would push the negative local coordinates off the
	// canvas, which made the earlier native FLIER invisible.
	p.Transform(ui.Translation(pose.x+appearX+awayX, pose.y+offsetY+appearY+awayY), 0, 0, func() {
		p.Transform(ui.Rotation(aim), 0, 0, func() {
			p.Transform(ui.Scaling(scale*flip, scale), 0, 0, func() {
				p.Transform(ui.Translation(-6.5, 3.2).Then(ui.Scaling(1.2, 1.2)), 0, 0, func() {
					drawMagpieShape(p, body, belly, far, wing, skew, farWing, farSkew, opacity)
				})
			})
		})
	})
	drawMagpiePacket(p, pose.x, pose.y, agentColor, belly, carry, outcome)
}

func wingFrame(phase float32) (scale, skew float32) {
	// rtflap is cubic-bezier(.45,0,.3,1) with 0%, 100% at scaleY(1) and
	// 55% at scaleY(-.62) skewX(-8deg).
	phase -= float32(math.Floor(float64(phase)))
	k := clamp01(phase)
	if k <= 0.55 {
		t := cubicBezierEase(0.45, 0, 0.3, 1, k/0.55)
		return 1 - 1.62*t, -8 * t
	}
	t := cubicBezierEase(0.45, 0, 0.3, 1, (k-0.55)/0.45)
	return -0.62 + 1.62*t, -8 * (1 - t)
}

func drawMagpieShape(p *ui.Painter, body, belly, far ui.Color, wing, skew, farWing, farSkew, opacity float32) {
	alpha := func(c ui.Color) ui.Color { return c.Alpha(opacity) }
	// far wing: transform around its shoulder (its bottom edge), then skew.
	p.Transform(ui.Scaling(1, farWing).Then(ui.Skewing(farSkew, 0)), -12.5, -3.2, func() {
		var wing ui.Path
		wing.MoveTo(-14.5, -3.4)
		wing.CubeTo(-16.5, -9.5, -14.2, -15, -8.6, -18.6)
		wing.CubeTo(-9.4, -12.6, -9.6, -7.4, -9.2, -3.2)
		wing.Close()
		p.FillPath(&wing, alpha(body).Alpha(0.45))
	})
	var tail ui.Path
	tail.MoveTo(-31.5, 3.1)
	tail.LineTo(-18.6, -1.8)
	tail.LineTo(-17.4, 1.2)
	tail.LineTo(-30.8, 4.6)
	tail.Close()
	p.FillPath(&tail, alpha(body))

	// Draw the upstream ellipse exactly as SVG does, using four cubic
	// segments.
	p.FillPath(ellipsePath(-12.2, -1.4, 7.6, 3.9), alpha(body))
	p.FillPath(ellipsePath(-12.8, 0.4, 4.3, 1.6), alpha(belly))
	var head ui.Path
	head.Circle(-4.9, -3.7, 3.1)
	p.FillPath(&head, alpha(body))
	var beak ui.Path
	beak.MoveTo(-2.3, -4.8)
	beak.LineTo(1.4, -3.3)
	beak.LineTo(-2.3, -2.2)
	beak.Close()
	p.FillPath(&beak, alpha(body))

	p.Transform(ui.Scaling(1, wing).Then(ui.Skewing(skew, 0)), -13.1, -2.8, func() {
		var near ui.Path
		near.MoveTo(-15.6, -3.2)
		near.CubeTo(-17.8, -10.2, -15.2, -16.4, -8.4, -20.4)
		near.CubeTo(-9.3, -13.6, -9.4, -7.8, -8.8, -2.8)
		near.Close()
		p.FillPath(&near, alpha(body))
		var bar ui.Path
		bar.MoveTo(-14.6, -5.6)
		bar.CubeTo(-15.4, -10.4, -13.8, -14.4, -10.4, -17.2)
		p.StrokePath(&bar, 1.3, alpha(belly))
	})
}

func ellipsePath(cx, cy, rx, ry float32) *ui.Path {
	const k = 0.5522847498
	var p ui.Path
	p.MoveTo(cx+rx, cy)
	p.CubeTo(cx+rx, cy+ry*k, cx+rx*k, cy+ry, cx, cy+ry)
	p.CubeTo(cx-rx*k, cy+ry, cx-rx, cy+ry*k, cx-rx, cy)
	p.CubeTo(cx-rx, cy-ry*k, cx-rx*k, cy-ry, cx, cy-ry)
	p.CubeTo(cx+rx*k, cy-ry, cx+rx, cy-ry*k, cx+rx, cy)
	p.Close()
	return &p
}

func drawMagpiePacket(p *ui.Painter, x, y float32, agentColor, card ui.Color, carry magpiePacket, outcome packetOutcome) {
	if carry == magpieCarryNone {
		return
	}
	if agentColor == (ui.Color{}) {
		agentColor = ui.Hex("#8b8b94")
	}
	r := float32(4.5)
	if carry == magpieCarryResult {
		r = 4
	}
	box := ui.Rect{X: x - r, Y: y - r, W: r * 2, H: r * 2}
	p.Shadow(box, r, 0, 0, 3, 0, agentColor.Alpha(0.55))
	switch carry {
	case magpieCarryRequest:
		p.Fill(box, agentColor, r)
	case magpieCarryResult:
		fill, stroke := card, agentColor
		switch outcome {
		case packetOutcomeFailed:
			fill, stroke = agentColor, ui.Hex("#f28b82")
		}
		p.Fill(box, fill, r)
		p.Stroke(box, stroke, r, 2.25)
	}
}

// drawStillMagpie renders a deterministic frame of the same FLIER for tests
// and snapshots. It carries no packet and does not depend on wall time.
func drawStillMagpie(p *ui.Painter, x, y, scale float32, pal palette) {
	drawMagpie(p, magpiePose{
		x: x, y: y, scale: scale, flapAt: time.Unix(0, 0),
		body: pal.BirdBody, belly: pal.BirdBelly, far: pal.BirdWing,
		appear: 1,
	}, ui.Hex("#6366f1"), magpieCarryNone, packetOutcomeUnknown, true)
}

func drawHeldPacket(p *ui.Painter, x, y float32, color ui.Color, outcome packetOutcome, now time.Time) {
	pulse := float32(1)
	if now.UnixMilli()%1200 > 600 {
		pulse = 0.55
	}
	r := float32(4.5)
	stroke := color
	if outcome == packetOutcomeFailed {
		stroke = ui.Hex("#f28b82")
	}
	p.Fill(ui.Rect{X: x - r - 2, Y: y - r - 2, W: (r + 2) * 2, H: (r + 2) * 2}, color.Alpha(0.10*pulse), r+2)
	p.Fill(ui.Rect{X: x - r, Y: y - r, W: r * 2, H: r * 2}, color.Alpha(0.42*pulse), r)
	p.Stroke(ui.Rect{X: x - r, Y: y - r, W: r * 2, H: r * 2}, stroke.Alpha(pulse), r, 2)
}

// drawFlowingPath paints a short dash that travels along the actual wire.
// This is the native equivalent of rt-wires path.live's stroke-dashoffset
// animation, and costs only the visible dash geometry per frame.
func drawFlowingPath(p *ui.Painter, path flightPath, color ui.Color, width float32, now time.Time) {
	const dash, gap float32 = 3, 5
	period := dash + gap
	offset := float32(now.UnixMilli()%700) / float32(700) * period
	length := path.length()
	var flow ui.Path
	added := false
	for d := -offset; d < length; d += period {
		from := max(d, 0)
		to := min(d+dash, length)
		if appendFlightArc(&flow, path, from, to) {
			added = true
		}
	}
	if added {
		p.StrokePath(&flow, width, color)
	}
}

func appendFlightArc(out *ui.Path, path flightPath, from, to float32) bool {
	if to-from < 0.1 {
		return false
	}
	x, y, _, ok := path.at(from)
	if !ok {
		return false
	}
	const step = 2.0
	attached := false
	out.MoveTo(x, y)
	for d := from; d < to; d += step {
		px, py, _, ok := path.at(d)
		if !ok {
			continue
		}
		out.LineTo(px, py)
		attached = true
	}
	x, y, _, ok = path.at(to)
	if ok {
		out.LineTo(x, y)
		attached = true
	}
	return attached
}

type replayTopology struct {
	routes  []routing.RouteView
	playing bool
	start   time.Time
	active  bool
}

// replayTopologyState returns the completed routes whose presentation flight
// covers the replay cursor. Flight windows are derived from the exact trace
// phases, so a seek into the return leg reconstructs it without relying on a
// previous playback pass.
func (m *Model) replayTopologyState() (replayTopology, bool) {
	m.mu.RLock()
	tab := m.tab
	replay := m.replay
	snapshot := m.lastSnapshot
	m.mu.RUnlock()
	if tab != tabHistory || !replay.loaded || replay.timeline.Duration <= 0 {
		return replayTopology{}, false
	}
	byID := make(map[int64]routing.RouteView, len(snapshot.Routes))
	for _, rv := range snapshot.Routes {
		byID[rv.ID] = rv
	}
	state := replayTopology{
		playing: replay.playing, start: replay.timeline.Start, active: true,
	}
	for _, window := range activeReplayWindows(replay.windows, replay.windowEnds, replay.position) {
		if rv, ok := byID[window.routeID]; ok {
			state.routes = append(state.routes, rv)
			continue
		}
		for _, route := range replay.timeline.Routes {
			if route.ID != window.routeID {
				continue
			}
			state.routes = append(state.routes, routing.RouteView{
				Route:        route,
				Completeness: routing.Completeness{History: true},
			})
			break
		}
	}
	return state, true
}

func indexAgentNode(agents []agentNode, id string) int {
	for i := range agents {
		if strings.EqualFold(agents[i].ID, id) {
			return i
		}
	}
	return -1
}

func indexProviderNode(providers []providerNode, target providerTarget) int {
	best, bestScore := -1, 0
	ambiguous := false
	for i := range providers {
		score := providerMatchScore(providers[i], target)
		if score <= 0 {
			continue
		}
		switch {
		case score > bestScore:
			best, bestScore, ambiguous = i, score, false
		case score == bestScore:
			ambiguous = true
		}
	}
	if ambiguous {
		return -1
	}
	return best
}

// providerMatchScore favors exact recorded account identity, then provider
// and model. Who is display text and may be masked, so matching never relies
// on it. A provider-only target can still resolve uniquely, but it never
// silently chooses the first of two models or accounts under that provider.
func providerMatchScore(row providerNode, target providerTarget) int {
	provider := strings.TrimSpace(target.provider)
	model := strings.TrimSpace(target.model)
	account := strings.TrimSpace(target.accountID)
	rowProvider := firstNonEmpty(row.Provider, providerOf(row.Model))
	if provider != "" && rowProvider != "" && !strings.EqualFold(rowProvider, provider) {
		return 0
	}
	score := 0
	accountMatch := account != "" && row.AccountID != "" && strings.EqualFold(row.AccountID, account)
	if accountMatch {
		score += 100
	}
	if model != "" && row.Model != "" && strings.EqualFold(row.Model, model) {
		score += 30
	} else if model != "" && row.Model != "" && !accountMatch {
		// A recorded account id is stronger evidence than a display model
		// alias; without it, a different model row is not this endpoint.
		return 0
	}
	if provider != "" && rowProvider != "" && strings.EqualFold(rowProvider, provider) {
		score += 10
	}
	if score == 0 && provider == "" && model == "" {
		return 0
	}
	return score
}

func providerRowsForTopology(sel routing.RouteView, hasSel bool, routes []routing.RouteView, live []routing.LiveRequest, hidden bool) []providerNode {
	var out []providerNode
	seen := map[string]bool{}
	add := func(rows []providerNode) {
		for _, row := range rows {
			key := strings.ToLower(row.Provider + "\x00" + row.Name + "\x00" + row.Model + "\x00" + row.AccountID)
			if key == "\x00\x00\x00" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, row)
		}
	}
	if hasSel {
		add(providerRows(sel, hidden))
	}
	for _, rv := range routes {
		add(providerRows(rv, hidden))
	}
	for _, l := range live {
		if l.Provider == "" {
			continue
		}
		add([]providerNode{{
			Provider: l.Provider, Name: l.Provider, Model: l.Model,
			Status: string(l.Phase), Current: true,
		}})
	}
	return out
}

func drawTopologyGrid(p *ui.Painter, r ui.Rect, pal palette) {
	step := float32(28)
	for x := r.X; x < r.X+r.W; x += step {
		p.Line(x, r.Y, x, r.Y+r.H, 0.5, pal.Grid.Alpha(0.5))
	}
	for y := r.Y; y < r.Y+r.H; y += step {
		p.Line(r.X, y, r.X+r.W, y, 0.5, pal.Grid.Alpha(0.5))
	}
}

func drawAgentCard(p *ui.Painter, r ui.Rect, a agentNode, pal palette) {
	if a.Active {
		p.Shadow(r, 10, 0, 4, 14, 0, pal.Shadow)
	}
	p.Fill(r, pal.PanelRaised, 12)
	p.Stroke(r, a.Hue.Alpha(0.35+boolFloat(a.Active, 0.3)), 12, 1)
	circle := ui.Rect{X: r.X + 10, Y: r.Y + 12, W: 28, H: 28}
	p.Fill(circle, a.Hue.Alpha(0.14), 14)
	p.Stroke(circle, a.Hue.Alpha(0.55), 14, 1)
	p.Text(r.X+16, r.Y+20, agentInitials(a.ID), 9, a.Hue)
	p.Text(r.X+46, r.Y+10, truncate(a.Name, 18), 10.5, pal.Text)
	sub := a.Model
	if a.Active {
		sub = "● " + sub
	}
	if sub == "" {
		sub = "—"
	}
	p.Text(r.X+46, r.Y+27, truncate(sub, 19), 8.5, pal.Muted)
	if a.Requests > 0 {
		p.Text(r.X+r.W-26, r.Y+10, itoa(a.Requests), 9, pal.Faint)
	}
	if a.Active {
		p.Fill(ui.Rect{X: r.X + r.W - 12, Y: r.Y + 8, W: 5, H: 5}, pal.Accent, 3)
	}
}

func drawGatewayCard(p *ui.Painter, r ui.Rect, s routing.Snapshot, pal palette) {
	color := pal.Warning
	if s.GatewayOnline {
		color = pal.Accent
	}
	p.Shadow(r, 14, 0, 8, 24, 0, pal.Shadow)
	p.FillGradient(r, ui.LinearGradient{From: pal.PanelRaised, To: pal.PanelMuted, Angle: 120}, 14)
	p.Stroke(r, color.Alpha(0.75), 14, 1.4)
	cx := r.X + r.W/2
	p.Fill(ui.Rect{X: cx - 19, Y: r.Y + 10, W: 38, H: 38}, color.Alpha(0.14), 19)
	drawMagpieMark(p, ui.Rect{X: cx - 14, Y: r.Y + 14, W: 28, H: 28}, color, pal.PanelRaised)
	p.Text(r.X+12, r.Y+52, "MAGPIE GATEWAY", 8, pal.Muted)
	p.Text(r.X+12, r.Y+65, gatewayStatusText(s), 9.5, color)
	p.Text(r.X+r.W-12, r.Y+65, m2(s), 8, pal.Faint)
}

func gatewayStatusText(s routing.Snapshot) string {
	if s.GatewayOnline {
		return "online · " + itoa(len(s.Live)) + " live"
	}
	return "offline"
}

func m2(s routing.Snapshot) string {
	if len(s.Live) == 0 {
		return "idle"
	}
	return itoa(len(s.Live)) + " req"
}

func drawProviderCard(p *ui.Painter, r ui.Rect, n providerNode, pal palette) {
	color := pal.PanelRaised
	border := pal.LineSoft
	if n.Failed {
		border = pal.Danger.Alpha(0.7)
	} else if n.Current {
		border = pal.Accent.Alpha(0.8)
	}
	p.Fill(r, color, 12)
	p.Stroke(r, border, 12, 1.1)
	p.Text(r.X+10, r.Y+9, truncate(firstNonEmpty(n.Name, n.Provider, "provider"), 18), 10, pal.Text)
	model := firstNonEmpty(n.Model, n.Who, n.Plan)
	p.Text(r.X+10, r.Y+25, truncate(model, 20), 8.5, pal.Muted)
	if n.Status != "" {
		statusColor := pal.Success
		if n.Failed {
			statusColor = pal.Danger
		} else if n.Current {
			statusColor = pal.Accent
		}
		p.Text(r.X+r.W-40, r.Y+9, truncate(n.Status, 8), 8, statusColor)
	}
	if n.Note != "" {
		p.Text(r.X+10, r.Y+40, truncate(n.Note, 24), 7.5, pal.Faint)
	}
	if n.Current {
		p.Fill(ui.Rect{X: r.X + r.W - 9, Y: r.Y + r.H - 9, W: 4, H: 4}, pal.Accent, 2)
	}
}

func providerRowsFromRoutes(routes []routing.RouteView) []providerNode {
	var out []providerNode
	seen := map[string]bool{}
	for _, r := range routes {
		for _, n := range providerRows(r, false) {
			key := n.Provider + "\x00" + n.Name + "\x00" + n.Model
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, n)
			if len(out) >= 5 {
				return out
			}
		}
	}
	return out
}

func drawCubic(p *ui.Painter, a, b [2]float32, color ui.Color, width float32) {
	if a[0] == b[0] && a[1] == b[1] {
		return
	}
	dx := max((b[0]-a[0])*0.48, 22)
	var path ui.Path
	path.MoveTo(a[0], a[1])
	path.CubeTo(a[0]+dx, a[1], b[0]-dx, b[1], b[0], b[1])
	p.StrokePath(&path, width, color)
}

func drawPacket(p *ui.Painter, a, b [2]float32, progress float32, color ui.Color, now time.Time, seed int) {
	t := progress
	if t <= 0 || t >= 1 {
		t = float32((now.UnixMilli()+int64(seed*137))%1800) / 1800
	}
	x, y := cubicPoint(a, b, t)
	drawBirdAt(p, x, y, 5.2, color, now, seed)
	p.Fill(ui.Rect{X: x - 8, Y: y - 8, W: 16, H: 16}, color.Alpha(0.10), 8)
}

func drawFlier(p *ui.Painter, a, b [2]float32, color ui.Color, now time.Time, seed int, failed bool) {
	drawPacket(p, a, b, 0, color, now, seed)
	if failed {
		t := float32((now.UnixMilli()+int64(seed*137))%1800) / 1800
		x, y := cubicPoint(a, b, t)
		p.Stroke(ui.Rect{X: x - 9, Y: y - 9, W: 18, H: 18}, ui.Hex("#b94a44"), 9, 1.2)
	}
}

// drawBirdAt draws a small native magpie in flight: a body, a tail, and two
// wings whose span changes with the frame clock. It is intentionally built
// from UI paths rather than an SVG or web asset.
func drawBirdAt(p *ui.Painter, x, y, size float32, color ui.Color, now time.Time, seed int) {
	beat := float32((now.UnixMilli()/55 + int64(seed)*7) % 8)
	if beat > 4 {
		beat = 8 - beat
	}
	wingLift := -size * (0.35 + beat*0.09)
	body := ui.Rect{X: x - size*0.75, Y: y - size*0.28, W: size * 1.5, H: size * 0.56}
	p.Fill(body, color, size*0.28)
	var tail ui.Path
	tail.MoveTo(x-size*0.55, y)
	tail.LineTo(x-size*1.18, y-size*0.32)
	tail.LineTo(x-size*0.92, y+size*0.2)
	p.FillPath(&tail, color)
	var leftWing ui.Path
	leftWing.MoveTo(x-size*0.12, y-size*0.12)
	leftWing.CubeTo(x-size*0.48, wingLift-size*0.18, x-size*0.82, wingLift, x-size*0.95, wingLift+size*0.2)
	leftWing.CubeTo(x-size*0.58, wingLift+size*0.46, x-size*0.26, y+size*0.18, x-size*0.12, y-size*0.12)
	p.FillPath(&leftWing, color.Alpha(0.82))
	var rightWing ui.Path
	rightWing.MoveTo(x+size*0.12, y-size*0.12)
	rightWing.CubeTo(x+size*0.5, wingLift-size*0.12, x+size*0.82, wingLift, x+size*0.95, wingLift+size*0.2)
	rightWing.CubeTo(x+size*0.58, wingLift+size*0.46, x+size*0.26, y+size*0.18, x+size*0.12, y-size*0.12)
	p.FillPath(&rightWing, color.Alpha(0.68))
}

func cubicPoint(a, b [2]float32, t float32) (float32, float32) {
	dx := max((b[0]-a[0])*0.48, 22)
	c1x, c1y := a[0]+dx, a[1]
	c2x, c2y := b[0]-dx, b[1]
	u := 1 - t
	x := u*u*u*a[0] + 3*u*u*t*c1x + 3*u*t*t*c2x + t*t*t*b[0]
	y := u*u*u*a[1] + 3*u*u*t*c1y + 3*u*t*t*c2y + t*t*t*b[1]
	return x, y
}

func drawMagpieMark(p *ui.Painter, r ui.Rect, body, belly ui.Color) {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	s := min(r.W, r.H)
	var wing ui.Path
	wing.MoveTo(cx-s*0.32, cy-s*0.02)
	wing.CubeTo(cx-s*0.58, cy-s*0.24, cx-s*0.44, cy-s*0.42, cx-s*0.18, cy-s*0.22)
	wing.CubeTo(cx-s*0.08, cy-s*0.13, cx-s*0.12, cy+s*0.08, cx-s*0.32, cy-s*0.02)
	p.FillPath(&wing, body)
	p.Fill(ui.Rect{X: cx - s*0.16, Y: cy - s*0.18, W: s * 0.38, H: s * 0.26}, body, s*0.13)
	p.Fill(ui.Rect{X: cx + s*0.09, Y: cy - s*0.22, W: s * 0.18, H: s * 0.17}, belly, s*0.07)
	var tail ui.Path
	tail.MoveTo(cx+s*0.2, cy+s*0.02)
	tail.LineTo(cx+s*0.36, cy+s*0.2)
	tail.LineTo(cx+s*0.12, cy+s*0.08)
	p.FillPath(&tail, body)
	p.Fill(ui.Rect{X: cx + s*0.2, Y: cy - s*0.17, W: s * 0.025, H: s * 0.025}, belly, 1)
}

func agentInitials(id string) string {
	if id == "" {
		return "A"
	}
	name := agentName(id)
	if name == "" {
		return "A"
	}
	rs := []rune(name)
	if len(rs) >= 2 {
		return string(rs[:2])
	}
	return string(rs)
}

func truncate(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	if n <= 1 {
		return string(rs[:n])
	}
	return string(rs[:n-1]) + "…"
}

func boolFloat(v bool, f float32) float32 {
	if v {
		return f
	}
	return 0
}
