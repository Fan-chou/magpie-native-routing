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
	// .rt-stage's own 22px top and bottom padding plus the tallest default
	// node (the hub's 104px). Growing it further leaves the large empty
	// band the earlier native layout showed above and below the stage.
	routingStageMinHeight = float32(148)
	routingStagePadding   = float32(22)
	// routing narrow breakpoint follows the stage's own content box, as
	// client.js's ResizeObserver does on .rt (not the whole window).
	routingStageNarrowWidth = float32(560)
	routingStageRowWidth    = float32(460)
	routingStageAgentWidth  = float32(124)
	routingStageHubWidth    = float32(124)
	routingStageColumnGap   = float32(56)
	routingStageNarrowGap   = float32(28)
	routingStageAgentH      = float32(82)
	routingStageAgentGap    = float32(10)
	routingStageRowH        = float32(44)
	routingStageRowGap      = float32(7)
	routingStageHubH        = float32(104)
	// routingAnimationInterval follows the original's 60 FPS flight cadence.
	// The interval is the fast budget; MyGo's retained-tree repaint cost may
	// lower it through animationFrameInterval when the window cannot keep
	// that cadence without heating up.
	routingAnimationInterval = time.Second / 60
	routingAnimationSlowest  = time.Second / 30
	// flightAwayDuration is .rt-flier.away's 450ms transition. The return
	// wire is already over when it starts, but the bird must remain alive
	// for the exit animation.
	flightAwayDuration = 450 * time.Millisecond
)

type routingStageSizing struct {
	agentH   float32
	agentGap float32
	rowH     float32
	rowGap   float32
}

// routingStageSizing compacts rows only when there are more than fit the
// original proportions. Upstream's normal card is never changed merely to
// squeeze one more row in: the stage grows first, then the denser classes
// used by .rt-srcs.many and .rt-accts keep every endpoint addressable.
func routingStageMetrics(agentCount, rowCount int) routingStageSizing {
	m := routingStageSizing{
		agentH: routingStageAgentH, agentGap: routingStageAgentGap,
		rowH: routingStageRowH, rowGap: routingStageRowGap,
	}
	switch {
	case agentCount > 12:
		m.agentH, m.agentGap = 54, 4
	case agentCount > 8:
		m.agentH, m.agentGap = 62, 5
	case agentCount > 4:
		m.agentH, m.agentGap = 70, 7
	}
	switch {
	case rowCount > 12:
		m.rowH, m.rowGap = 32, 3
	case rowCount > 8:
		m.rowH, m.rowGap = 37, 4
	case rowCount > 5:
		m.rowH, m.rowGap = 41, 5
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
		routingStageHubH,
	)
}

// layoutRoutingStage returns geometry in the coordinate space of r. The
// wide layout is the original CSS grid exactly: 124px for agents, 124px for
// the hub, 56px gaps and the flexible column for accounts. At or below 560px
// the stage becomes two columns, agents beside the hub and accounts below,
// with the lane down the left that client.js's fromHub() draws.
//
// The same function feeds the static canvas, the live birds and the route
// wires, so no coordinate is recomputed for one layer and not another.
func layoutRoutingStage(r ui.Rect, agents []agentNode, rows []providerNode) flightStage {
	metrics := routingStageMetrics(len(agents), len(rows))
	// .rt-stage: 22px block padding, 2px inline padding.
	content := nodeBox{
		x: r.X + 2, y: r.Y + routingStagePadding,
		w: max(r.W-4, 0), h: max(r.H-2*routingStagePadding, 0),
	}
	if content.w <= routingStageNarrowWidth {
		return layoutNarrowRoutingStage(content, agents, rows, metrics)
	}
	return layoutWideRoutingStage(content, agents, rows, metrics)
}

func layoutWideRoutingStage(r nodeBox, agents []agentNode, rows []providerNode, metrics routingStageSizing) flightStage {
	providerW := max(r.w-routingStageAgentWidth-routingStageHubWidth-2*routingStageColumnGap, 160)
	startX := r.x + max((r.w-(routingStageAgentWidth+routingStageHubWidth+2*routingStageColumnGap+providerW))/2, 0)
	centerY := r.y + r.h/2

	agentStack := stageStackHeight(len(agents), metrics.agentH, metrics.agentGap)
	agentTop := centerY - agentStack/2
	agentBoxes := make([]nodeBox, 0, len(agents))
	for i := range agents {
		agentBoxes = append(agentBoxes, nodeBox{
			x: startX, y: agentTop + float32(i)*(metrics.agentH+metrics.agentGap),
			w: routingStageAgentWidth, h: metrics.agentH,
		})
	}
	hubX := startX + routingStageAgentWidth + routingStageColumnGap
	hub := nodeBox{x: hubX, y: centerY - routingStageHubH/2, w: routingStageHubWidth, h: routingStageHubH}
	rowsX := hubX + routingStageHubWidth + routingStageColumnGap
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

// layoutNarrowRoutingStage is the .rt.max560 grid: 1fr 1fr with 28px
// between them and 26px between its rows. Agents and hub share the first
// row; the provider list is grid-column 1 / -1 below, 22px in from the left
// for the lane that client.js's fromHub() draws.
func layoutNarrowRoutingStage(r nodeBox, agents []agentNode, rows []providerNode, metrics routingStageSizing) flightStage {
	colW := max((r.w-routingStageNarrowGap)/2, 96)
	agentStack := stageStackHeight(len(agents), metrics.agentH, metrics.agentGap)
	topRowH := max(agentStack, routingStageHubH)
	top := r.y
	agentTop := top + (topRowH-agentStack)/2
	agentBoxes := make([]nodeBox, 0, len(agents))
	for i := range agents {
		agentBoxes = append(agentBoxes, nodeBox{
			x: r.x, y: agentTop + float32(i)*(metrics.agentH+metrics.agentGap),
			w: colW, h: metrics.agentH,
		})
	}
	hub := nodeBox{x: r.x + colW + routingStageNarrowGap, y: top + (topRowH-routingStageHubH)/2, w: colW, h: routingStageHubH}

	rowsTop := top + topRowH + 26
	rowsX := r.x + 22
	rowsW := max(r.w-22, 160)
	rowBoxes := make([]nodeBox, 0, len(rows))
	for i := range rows {
		rowBoxes = append(rowBoxes, nodeBox{
			x: rowsX, y: rowsTop + float32(i)*(metrics.rowH+metrics.rowGap),
			w: rowsW, h: metrics.rowH,
		})
	}
	return flightStage{agents: agentBoxes, hub: hub, accounts: rows, rows: rowBoxes, narrow: true}
}

func stageIdentityKey(agents []agentNode, rows []providerNode, width, height float32) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%.1fx%.1f|", width, height)
	for _, agent := range agents {
		b.WriteString(agent.ID)
		b.WriteByte(0)
		b.WriteString(agent.Name)
		b.WriteByte(0)
		b.WriteString(agent.Model)
		b.WriteByte(0)
		b.WriteString(agent.Icon)
		b.WriteByte(0)
		fmt.Fprintf(&b, "%t\x00", agent.Active)
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
		b.WriteString(row.Plan)
		b.WriteByte(0)
		b.WriteString(row.Status)
		b.WriteByte(0)
		b.WriteString(row.Note)
		b.WriteByte(0)
		b.WriteString(row.Kind)
		b.WriteByte(0)
		b.WriteString(row.Weight)
		b.WriteByte(0)
		b.WriteString(row.Routing)
		b.WriteByte(0)
		fmt.Fprintf(&b, "%t/%t/%t/%t/%t/%t/%t/%t/%.4f\x00",
			row.Current, row.Failed, row.Known, row.Fallback,
			row.Rest, row.Answered, row.WentOn, row.Masked, row.Used)
		b.WriteString(row.RestWhy)
		b.WriteByte(0)
		b.WriteString(row.RestUntil.UTC().Format(time.RFC3339Nano))
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
	narrow   bool
}

func offsetNodeBox(b nodeBox, dx, dy float32) nodeBox {
	return nodeBox{x: b.x + dx, y: b.y + dy, w: b.w, h: b.h}
}

// offsetFlightStage translates one layout into window coordinates. Node
// identity and sizes are shared with the static nodes; only the origin
// changes, so static and live layers cannot drift apart.
func offsetFlightStage(stage flightStage, dx, dy float32) flightStage {
	out := flightStage{
		hub:      offsetNodeBox(stage.hub, dx, dy),
		accounts: stage.accounts,
		narrow:   stage.narrow,
		agents:   make([]nodeBox, len(stage.agents)),
		rows:     make([]nodeBox, len(stage.rows)),
	}
	for i := range stage.agents {
		out.agents[i] = offsetNodeBox(stage.agents[i], dx, dy)
	}
	for i := range stage.rows {
		out.rows[i] = offsetNodeBox(stage.rows[i], dx, dy)
	}
	return out
}

// flightGeometry owns every curve that can be flown on one stage layout.
// The paths are rebuilt only when a node moves, resizes, or changes
// identity; animation frames then reuse the exact same arc-length tables.
type flightGeometry struct {
	key    string
	hub    nodeBox
	narrow bool

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
		narrow:         stage.narrow,
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
		if stage.narrow {
			g.hubProvider[i] = hubToNarrowProviderPath(stage.hub, stage.agents, &stage.rows[i])
		} else {
			g.hubProvider[i] = hubToProviderPath(stage.hub, &stage.rows[i])
		}
		g.providerHub[i] = g.hubProvider[i].reversed()
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
	path := stageOutboundPath(g.agents[agentIndex], g.hub, &g.rows[providerIndex], g.narrow)
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
	// A return follows the outbound route backwards. The prior version
	// rebuilt a reversed pair of cubic legs with a different control
	// direction, so the bird visibly changed lane at the hub.
	path := g.outboundPath(agentIndex, providerIndex).reversed()
	g.inbound[key] = path
	return path
}

func flightStageKey(stage flightStage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "n:%t|hub:%.2f,%.2f,%.2f,%.2f|", stage.narrow, stage.hub.x, stage.hub.y, stage.hub.w, stage.hub.h)
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
	replay, replayActive := m.replayTopologyStateAt(now)
	// Build or reuse the Route ID index before taking topology's write lock:
	// routeViewIndex has its own model lock and must never be called under it.
	routeByID := m.routeViewIndex(s)
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
			displayEnd := flightPhaseEnd(phases) + flightAwayDuration
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
			addReplay(rv, replay.position-offset)
		}
	} else {
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
			speed := replay.speed
			if speed <= 0 {
				speed = 1
			}
			track.firstSeen = now.Add(-time.Duration(float64(in.observed) / speed))
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
		track.replayPlay = replayActive && replay.playing
		// Record a phase transition on the track itself so the account row
		// can shake (rthit) or pulse (rtlive) without a second identity map
		// that a retry to the same provider/model/account would collide in.
		if phase := currentFlightPhase(*track, now); phase.hasPhase {
			key := fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%d", in.id, phase.phase.provider, phase.phase.model, phase.phase.accountID, phase.phase.kind)
			if track.phaseKey != key {
				track.phaseKey = key
				track.phaseKind = phase.phase.kind
				track.phaseFailed = phase.phase.failed
				track.phaseAt = now
			}
		}
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
	return now.Before(f.doneAt.Add(max(f.returnDur, 420*time.Millisecond) + flightAwayDuration))
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
	// Try.ID is the account identity. When it is present, only an order row
	// with that exact id may identify the endpoint; a model alias must never
	// redirect the attempt to another account after an id mismatch.
	if tr.ID != "" {
		var accountRow *routing.Weighed
		for _, w := range rv.Order {
			if !strings.EqualFold(w.ID, tr.ID) {
				continue
			}
			row := w
			if accountRow == nil {
				accountRow = &row
			}
			// Model is only a tie-breaker within the one account Try named.
			if tr.Model != "" && modelMatches(w.Model, tr.Model, firstNonEmpty(w.Provider, rv.Provider)) {
				return traceTargetFromOrder(w, tr)
			}
		}
		if accountRow != nil {
			return traceTargetFromOrder(*accountRow, tr)
		}
		return providerTarget{
			provider:  firstNonEmpty(providerOf(tr.Model), rv.Provider),
			model:     tr.Model,
			accountID: tr.ID,
		}
	}
	// Without an account id, Model may identify a provider but must not be
	// promoted into the account id of whichever Order row happens to be
	// first. Leaving the account empty makes duplicate model rows ambiguous.
	provider := firstNonEmpty(providerOf(tr.Model), rv.Provider)
	if provider == "" && tr.Model != "" {
		providers := map[string]bool{}
		for _, w := range rv.Order {
			if !modelMatches(w.Model, tr.Model, firstNonEmpty(w.Provider, rv.Provider)) {
				continue
			}
			if p := firstNonEmpty(w.Provider, providerOf(w.Model)); p != "" {
				providers[strings.ToLower(p)] = true
			}
		}
		if len(providers) == 1 {
			for p := range providers {
				provider = p
			}
		}
	}
	return providerTarget{
		provider: provider,
		model:    firstNonEmpty(tr.Model, rv.Model),
	}
}

func traceTargetFromOrder(w routing.Weighed, tr routing.Try) providerTarget {
	return providerTarget{
		provider:  firstNonEmpty(w.Provider, providerOf(w.Model), w.ID),
		model:     firstNonEmpty(tr.Model, w.Model),
		accountID: w.ID,
		who:       w.Who,
	}
}

// modelMatches compares a model id. When both sides carry provider
// prefixes, those providers must agree; the prefixed and bare forms are
// compatible only when the known provider context agrees as well.
func modelMatches(a, b, provider string) bool {
	aProvider, aModel := splitModel(a)
	bProvider, bModel := splitModel(b)
	a, b = strings.TrimSpace(aModel), strings.TrimSpace(bModel)
	if a == "" || b == "" {
		return false
	}
	if strings.EqualFold(a, b) {
		if aProvider != "" && bProvider != "" {
			return strings.EqualFold(aProvider, bProvider)
		}
		known := firstNonEmpty(aProvider, bProvider)
		context := firstNonEmpty(strings.TrimSpace(provider), known)
		return known == "" || context == "" || strings.EqualFold(known, context)
	}
	return false
}

func splitModel(model string) (provider, name string) {
	model = strings.TrimSpace(model)
	if i := strings.IndexByte(model, '/'); i >= 0 && i+1 < len(model) {
		return strings.TrimSpace(model[:i]), model[i+1:]
	}
	return "", model
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
	replay, replayActive := m.replayTopologyStateAt(now)
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

// routingStagePresentation is the immutable input shared by the stage's
// static and live painters for one frame. Both retained Draw callbacks run
// on every view build and on every Painter.After repaint; resolving agents,
// accounts, replay membership and geometry twice per frame was pure repeat
// work during a flight.
type routingStagePresentation struct {
	agents        []agentNode
	rows          []providerNode
	selection     routing.RouteView
	hasSelection  bool
	replayPlaying bool
	target        providerTarget
	flights       []flightTrack
	stageIdentity string
	replayActive  bool
}

func (m *Model) routingStagePresentationAt(s routing.Snapshot, routes []routing.RouteView, sel routing.RouteView, hasSel bool, now time.Time) *routingStagePresentation {
	replay, replayActive := m.replayTopologyStateAt(now)
	agents := m.stageAgents(s, sel, hasSel, routes, replay, replayActive, now)
	rows := m.stageAccountRows(sel, hasSel, s.Live, replay.routes, replayActive)
	if len(rows) == 0 {
		rows = []providerNode{{Name: m.localization().T("routing.noTrace")}}
	}
	target := providerTarget{}
	if hasSel {
		target = targetForRoute(sel)
	}
	m.mu.RLock()
	flights := append([]flightTrack(nil), m.topology.flights...)
	m.mu.RUnlock()
	return &routingStagePresentation{
		agents: agents, rows: rows, selection: sel, hasSelection: hasSel,
		target: target, flights: flights,
		stageIdentity: stageIdentityKey(agents, rows, 0, 0),
		replayActive:  replayActive, replayPlaying: replay.playing,
	}
}

// drawRoutingStageCanvas paints the routing stage. Static nodes and wires
// are drawn first, then the live wires and the birds that follow the exact
// same geometry.
func (m *Model) drawRoutingStageCanvas(p *ui.Painter, r ui.Rect, s routing.Snapshot, routes []routing.RouteView, sel routing.RouteView, hasSel bool, pal palette, now time.Time, history bool) {
	now = p.Now()
	if r.W < 200 || r.H < 140 {
		return
	}
	replay, replayActive := m.replayTopologyStateAt(now)
	agents := m.stageAgents(s, sel, hasSel, routes, replay, replayActive, now)
	if len(agents) == 0 {
		p.Text(r.X+8, r.Y+r.H/2, m.localization().T("routing.noAgents"), 10, pal.Faint)
		return
	}
	rows := m.stageAccountRows(sel, hasSel, s.Live, replay.routes, replayActive)
	if len(rows) == 0 {
		rows = []providerNode{{Name: m.localization().T("routing.noTrace")}}
	}
	geometry := m.routingStageGeometry(r, agents, rows)

	target := providerTarget{}
	if hasSel {
		target = targetForRoute(sel)
	}
	mode := stageRoutingMode(sel, hasSel, s.Config)
	gatewayURL := m.Settings().GatewayURL
	litAgents := map[string]bool{}
	litRows := map[int]bool{}
	activityNow := map[int]bool{}
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
			if moment := flightMomentAt(f, now); moment.hasPhase {
				switch moment.phase.kind {
				case flightOutbound, flightRetryOut, flightHold, flightRetryBack:
					activityNow[pi] = true
				}
			}
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
	m.mu.Unlock()
	loc := m.localization()
	maskBlur, maskOpacity := m.accountMaskStyle(now)
	m.mu.Lock()
	m.topology.flightGeometry = geometry
	m.mu.Unlock()
	activeFlight := false
	// The two Draw elements in routingStageCard call these two layers in
	// order. This branch is retained for direct callers and tests that paint
	// a complete stage into one callback.
	drawRoutingStatic(p, r, geometry.agents, geometry.hub, rows, geometry.rows, agents, litRows, s, gatewayURL, mode, pal, now, geometry.narrow, loc, maskBlur, maskOpacity)
	drawRoutingStageContent(p, geometry.agents, geometry.hub, rows, geometry.rows, agents, litAgents, litRows, activityNow, s, gatewayURL, mode, pal, now, flights, loc, maskBlur, maskOpacity)
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
			drawStillRoute(p, geometry, ai, indexProviderNode(rows, target), agentHue(sel.Agent))
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
		m.drawFlightActor(p, geometry, ai, f, agentHue(f.agent), pal, now, m.reduceMotionEnabled())
	}
	replayPlaying := false
	if replayActive {
		replayPlaying = replay.playing
	}
	animate := !m.reduceMotionEnabled() && (activeFlight && !replayActive || replayActive && replayPlaying)
	traveling := activityTraveling(flights, now)
	if animate {
		// Painter.After repaints the retained element tree instead of
		// rebuilding the view. A held dot is static; waiting does not
		// request frames, so long upstream calls do not keep the GPU busy.
		if traveling {
			p.After(routingAnimationInterval)
		}
	}
	if !animate && !m.reduceMotionEnabled() && feedbackVisible(flights, now) && (!replayActive || replayPlaying) {
		p.After(routingAnimationInterval)
	}
	if restingCountdownVisible(rows, now) && (!replayActive || replayPlaying) {
		// A countdown changes once a second even when nothing flies.
		p.After(time.Second)
	}
}

// drawRoutingStageStatic paints the cards, labels, idle wires and icons
// into a device-resolution bitmap through MyGo's public offscreen Render.
// The bitmap is built only when its inputs change; flight frames merely
// sample it, like any other image. Keeping it out of a Canvas matters:
// MyGo 0.5.6 samples nested transformed Canvas text and images unreliably
// under a scrolling clip.
func (m *Model) drawRoutingStageStatic(p *ui.Painter, r ui.Rect, stage *routingStagePresentation, s routing.Snapshot, pal palette, now time.Time) {
	if r.W < 200 || r.H < 140 {
		return
	}
	agents, rows := stage.agents, stage.rows
	if len(agents) == 0 {
		p.Text(r.X+8, r.Y+r.H/2, m.localization().T("routing.noAgents"), 10, pal.Faint)
		return
	}
	localStage := layoutRoutingStage(ui.Rect{W: r.W, H: r.H}, agents, rows)
	_, litRows, _ := m.stageHighlights(agents, rows, stage.target, stage.hasSelection, stage.selection.Agent, now, stage.flights)
	loc := m.localization()
	maskBlur, maskOpacity := m.accountMaskStyle(now)
	gatewayURL := m.Settings().GatewayURL
	mode := stageRoutingMode(stage.selection, stage.hasSelection, s.Config)
	key := staticStageBitmapKey(stage, localStage, pal, rows, now, maskBlur, maskOpacity)
	scale := p.Scale()
	m.mu.Lock()
	cached, cachedKey, cachedScale := m.topology.staticStage, m.topology.staticStageKey, m.topology.staticStageScale
	m.mu.Unlock()
	if cached == nil || cachedKey != key || cachedScale != scale {
		img := ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Box(c).Fill().Draw(func(cp *ui.Painter, drawRect ui.Rect) {
				drawRoutingStatic(cp, drawRect, localStage.agents, localStage.hub, rows, localStage.rows,
					agents, litRows, s, gatewayURL,
					mode, pal, now, localStage.narrow,
					loc, maskBlur, maskOpacity)
				drawRoutingStageContent(cp, localStage.agents, localStage.hub, rows, localStage.rows,
					agents, nil, litRows, nil, s, gatewayURL,
					mode, pal, now, nil, loc,
					maskBlur, maskOpacity)
			})
		}, max(1, int(math.Ceil(float64(r.W)))), max(1, int(math.Ceil(float64(r.H)))), scale)
		cached = ui.NewBitmap(img)
		m.mu.Lock()
		if m.topology.staticStageKey != key || m.topology.staticStageScale != scale ||
			m.topology.staticStage == nil {
			m.topology.staticStage = cached
			m.topology.staticStageKey = key
			m.topology.staticStageScale = scale
		} else {
			cached = m.topology.staticStage
		}
		m.mu.Unlock()
	}
	if cached != nil {
		p.Image(cached, r, ui.FillBox)
	}
}

// staticStageBitmapKey covers every input that changes the offscreen
// bitmap. Only a resting account's countdown is time-driven in the static
// layer; selection and activity rings live on the dynamic layer. The
// countdown advances at second granularity, so ordinary frame time does not
// invalidate the bitmap.
func staticStageBitmapKey(stage *routingStagePresentation, local flightStage, pal palette, rows []providerNode, now time.Time, maskBlur, maskOpacity float32) string {
	var b strings.Builder
	b.WriteString(stage.stageIdentity)
	b.WriteByte(0)
	b.WriteString(pal.stageKey())
	fmt.Fprintf(&b, "|%.1fx%.1f|%t|%.3f/%.3f|", local.hub.w, local.hub.h, local.narrow, maskBlur, maskOpacity)
	countdown := false
	for i := range rows {
		if i < len(local.rows) && rows[i].Rest && !rows[i].RestUntil.IsZero() && rows[i].RestUntil.After(now) {
			countdown = true
			break
		}
	}
	if countdown && !now.IsZero() {
		fmt.Fprintf(&b, "%d", now.Unix())
	}
	return b.String()
}

// drawRoutingStageLive paints only moving route geometry above the static
// layer. It uses the same geometry pointer as the static callback and the
// real snapshot/replay flight tracks; it never creates an activity that is
// absent from the snapshot.
func (m *Model) drawRoutingStageLive(p *ui.Painter, r ui.Rect, stage *routingStagePresentation, pal palette, now time.Time) {
	if r.W < 200 || r.H < 140 {
		return
	}
	agents, rows := stage.agents, stage.rows
	if len(agents) == 0 {
		return
	}
	geometry := m.routingStageGeometryKeyed(r, agents, rows, stage.stageIdentity)
	m.drawRoutingStageLiveOn(p, geometry, stage, pal, now)
}

// drawRoutingStageLiveOn paints the dynamic layer against an already
// resolved geometry. The static canvas and the live callback share this
// path, so their node anchors cannot drift apart.
func (m *Model) drawRoutingStageLiveOn(p *ui.Painter, geometry *flightGeometry, stage *routingStagePresentation, pal palette, now time.Time) {
	if geometry == nil || stage == nil {
		return
	}
	agents, rows := stage.agents, stage.rows
	if len(agents) == 0 {
		return
	}
	flights := stage.flights
	litAgents, litRows, activityNow := m.stageHighlights(agents, rows, stage.target, stage.hasSelection, stage.selection.Agent, now, flights)

	activeFlight := false
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
	if !activeFlight && stage.hasSelection {
		if ai := indexAgentNode(agents, stage.selection.Agent); ai >= 0 {
			drawStillRoute(p, geometry, ai, indexProviderNode(rows, stage.target), agentHue(stage.selection.Agent))
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
		m.drawFlightActor(p, geometry, ai, f, agentHue(f.agent), pal, now, m.reduceMotionEnabled())
	}
	// Draw the time-driven card feedback above the static texture. Keeping
	// these pixels out of the texture lets the rtlive rings, rthit shake and
	// failure chip animate without invalidating the stage bitmap.
	for i, a := range agents {
		if litAgents[strings.ToLower(a.ID)] {
			drawAgentPulse(p, geometry.agents[i].rect(), a, true)
		}
	}
	for i, row := range rows {
		feedback := feedbackForRow(flights, row)
		if litRows[i] || activityNow[i] || row.Failed || feedback.failed {
			drawAccountDynamicState(p, geometry.rows[i].rect(), row, litRows[i], activityNow[i], pal, now, feedback, m.localization())
		}
	}
	replayPlaying := !stage.replayActive || stage.replayPlaying
	animate := !m.reduceMotionEnabled() && (!stage.replayActive && activeFlight || stage.replayActive && replayPlaying)
	interval := m.animationFrameInterval(now)
	if animate && activityTraveling(flights, now) {
		m.requestAnimationFrame(p, interval)
	} else if !m.reduceMotionEnabled() && feedbackVisible(flights, now) && replayPlaying {
		m.requestAnimationFrame(p, interval)
	} else if restingCountdownVisible(rows, now) && replayPlaying {
		m.requestAnimationFrame(p, time.Second)
	}
}

func (m *Model) requestAnimationFrame(p *ui.Painter, interval time.Duration) {
	if interval <= 0 {
		interval = routingAnimationInterval
	}
	p.After(interval)
}

// animationFrameInterval adapts the retained-tree repaint rate to what the
// previous frames actually cost. The first frames use the original 60 Hz
// cadence. A frame that takes more than half of a 30 Hz budget lowers the
// rate one step; recovery is deliberately slower, so a single expensive
// list or layout frame does not make the flight stutter. Timing comes from
// the monotonic UI clock, never from a fixed frame count.
func (m *Model) animationFrameInterval(now time.Time) time.Duration {
	m.mu.RLock()
	cost, frames := m.topology.animationCost, m.topology.animationFrames
	m.mu.RUnlock()
	// A frame costs roughly paint + present; the observed built-frame
	// budget is 8.3ms at 120 Hz panels. Since MyGo gives us a frame timestamp
	// in nanoseconds and not the previous painter duration, use a bounded
	// warm-up based on consecutive active frames: the first 18 frames stay
	// at 60 Hz, then settle to 45 Hz, and only a long-running window with a
	// large retained tree drops to 30 Hz. This keeps a short request flight
	// visually identical to upstream while a continuously animating
	// history page does not redraw a 700-element tree 60 times a second.
	if frames < 18 || cost <= 0 {
		return routingAnimationInterval
	}
	if cost > 12*time.Millisecond {
		return routingAnimationSlowest
	}
	return routingAnimationInterval + (routingAnimationSlowest-routingAnimationInterval)/2
}

// noteAnimationPaint records the cost of the last repaint for the adaptive
// budget. The live painter runs on the UI thread; this state is model-local
// and guarded like the rest of topology.
func (m *Model) noteAnimationPaint(start time.Time, moving bool) {
	if !moving || start.IsZero() {
		return
	}
	cost := time.Since(start)
	m.mu.Lock()
	if m.topology.animationCost == 0 {
		m.topology.animationCost = cost
	} else {
		m.topology.animationCost = (m.topology.animationCost*7 + cost) / 8
	}
	m.topology.animationFrames++
	m.mu.Unlock()
}

// stageHighlights computes the lit agent/account indices from the same
// window of real flights used by both stage layers.
func (m *Model) stageHighlights(agents []agentNode, rows []providerNode, target providerTarget, hasSel bool, selAgent string, now time.Time, flights []flightTrack) (map[string]bool, map[int]bool, map[int]bool) {
	litAgents := map[string]bool{}
	litRows := map[int]bool{}
	activityNow := map[int]bool{}
	if flights == nil {
		m.mu.Lock()
		flights = append([]flightTrack(nil), m.topology.flights...)
		m.mu.Unlock()
	}
	activity := false
	for _, f := range flights {
		if !flightAlive(f, now) {
			continue
		}
		activity = true
		if ai := indexAgentNode(agents, f.agent); ai >= 0 {
			litAgents[strings.ToLower(agents[ai].ID)] = true
		}
		phase := flightPhase{}
		if len(f.phases) > 0 {
			phase = f.phases[len(f.phases)-1]
		}
		if pi := indexProviderNode(rows, targetForPhase(f, phase)); pi >= 0 {
			litRows[pi] = true
			if moment := flightMomentAt(f, now); moment.hasPhase {
				switch moment.phase.kind {
				case flightOutbound, flightRetryOut, flightHold, flightRetryBack:
					activityNow[pi] = true
				}
			}
		}
	}
	if !activity && hasSel {
		if ai := indexAgentNode(agents, selAgent); ai >= 0 {
			litAgents[strings.ToLower(agents[ai].ID)] = true
		}
		if pi := indexProviderNode(rows, target); pi >= 0 {
			litRows[pi] = true
		}
	}
	return litAgents, litRows, activityNow
}

// routingStageGeometry resolves the one geometry shared by the stage's
// static cards, its idle wires and every moving flight. The cache key covers
// the full node identity as well as the stage's origin and size, so a resize,
// DPI change or data update cannot leave stale curves behind.
func (m *Model) routingStageGeometry(r ui.Rect, agents []agentNode, rows []providerNode) *flightGeometry {
	return m.routingStageGeometryKeyed(r, agents, rows, stageIdentityKey(agents, rows, r.W, r.H))
}

func (m *Model) routingStageGeometryKeyed(r ui.Rect, agents []agentNode, rows []providerNode, identity string) *flightGeometry {
	key := fmt.Sprintf("%s|%.1fx%.1f@%.1f,%.1f", identity, r.W, r.H, r.X, r.Y)
	m.mu.Lock()
	defer m.mu.Unlock()
	if geometry := m.topology.stageGeometry; geometry != nil && geometry.key == key {
		m.topology.flightGeometry = geometry
		return geometry
	}
	localStage := layoutRoutingStage(ui.Rect{W: r.W, H: r.H}, agents, rows)
	geometry := newFlightGeometryKeyed(offsetFlightStage(localStage, r.X, r.Y), key)
	m.topology.stageGeometry = geometry
	m.topology.flightGeometry = geometry
	return geometry
}

func restingCountdownVisible(rows []providerNode, now time.Time) bool {
	if now.IsZero() {
		return false
	}
	for _, row := range rows {
		if row.Rest && !row.RestUntil.IsZero() && row.RestUntil.After(now) {
			return true
		}
	}
	return false
}

func activityVisible(flights []flightTrack, now time.Time) bool {
	for _, f := range flights {
		if flightAlive(f, now) {
			return true
		}
	}
	return false
}

func activityTraveling(flights []flightTrack, now time.Time) bool {
	for _, f := range flights {
		if flightAlive(f, now) && flightTraveling(f, now) {
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

func drawRoutingStatic(p *ui.Painter, r ui.Rect, agentsBoxes []nodeBox, hub nodeBox, rows []providerNode, rowsBoxes []nodeBox, agents []agentNode, litRows map[int]bool, s routing.Snapshot, gatewayURL, mode string, pal palette, now time.Time, narrow bool, loc Localization, maskBlur, maskOpacity float32) {
	for _, a := range agentsBoxes {
		ax, ay := a.rightCenter()
		hx, hy := hub.leftCenter()
		drawCubic(p, point(ax, ay), point(hx, hy), pal.RouteIdle, 1.3)
	}
	for i := range rowsBoxes {
		path := hubToProviderPath(hub, &rowsBoxes[i])
		if narrow {
			path = hubToNarrowProviderPath(hub, agentsBoxes, &rowsBoxes[i])
		}
		drawFlightPath(p, path, pal.RouteIdle, 1.5)
	}
	for i, a := range agents {
		// Agent activity is a pulsing dot, drawn by the live layer. Keeping
		// it out of this bitmap means no animation frame rebuilds the
		// static card texture.
		drawRoutingAgentBox(p, agentsBoxes[i].rect(), a, false, pal, false)
	}
	drawRoutingHub(p, hub.rect(), s, gatewayURL, mode, pal, false)
	for i, row := range rows {
		drawRoutingAccountRow(p, rowsBoxes[i].rect(), row, litRows[i], false, pal, now, false, topologyFeedback{}, loc, maskBlur, maskOpacity)
	}
	_ = r
}

// drawRoutingStageContent paints the text, provider logos and state chips
// over the card fill and border layer. It is split from drawRoutingStatic so
// state text and icons can reuse the exact same node geometry.
func drawRoutingStageContent(p *ui.Painter, agentsBoxes []nodeBox, hub nodeBox, rows []providerNode, rowsBoxes []nodeBox, agents []agentNode, litAgents map[string]bool, litRows, activityNow map[int]bool, s routing.Snapshot, gatewayURL, mode string, pal palette, now time.Time, flights []flightTrack, loc Localization, maskBlur, maskOpacity float32) {
	for i, a := range agents {
		drawRoutingAgentBox(p, agentsBoxes[i].rect(), a, litAgents[strings.ToLower(a.ID)], pal, true)
	}
	drawRoutingHub(p, hub.rect(), s, gatewayURL, mode, pal, true)
	for i, row := range rows {
		drawRoutingAccountRow(p, rowsBoxes[i].rect(), row, litRows[i], activityNow[i], pal, now, true, feedbackForRow(flights, row), loc, maskBlur, maskOpacity)
	}
}

func drawAgentPulse(p *ui.Painter, r ui.Rect, a agentNode, lit bool) {
	if !lit {
		return
	}
	compact := r.H < 70
	iconSize := float32(26)
	iconY := r.Y + 10
	nameSize := float32(13)
	if compact {
		iconSize = 20
		iconY = r.Y + 8
		nameSize = 11
	}
	nameY := iconY + iconSize + 3
	if compact {
		nameY = iconY + iconSize + 2
	}
	nameW, _ := p.MeasureText(0, ui.Span{Text: agentName(a.ID), Size: nameSize, Weight: 600})
	dot := float32(7)
	dotCX := r.X + r.W/2 - (nameW+dot+6)/2 + dot/2
	dotCY := nameY + nameSize*0.48 + dot/2
	phase := float32(p.Now().UnixMilli()%1800) / 1800
	k := clamp01(phase / 0.7)
	ring := dot + 6*k
	if alpha := 0.4 * (1 - k); alpha > 0.005 {
		p.Stroke(ui.Rect{X: dotCX - ring/2, Y: dotCY - ring/2, W: ring, H: ring}, a.Hue.Alpha(alpha), ring/2, 1)
	}
}

// drawAccountDynamicState paints the row's time-driven state. It is called
// by the live layer, after the cached static cards, so the rtlive ring and
// rthit shake never force the whole stage texture to be rebuilt.
func drawAccountDynamicState(p *ui.Painter, r ui.Rect, n providerNode, lit, active bool, pal palette, now time.Time, feedback topologyFeedback, loc Localization) {
	offsetX := float32(0)
	if !feedback.at.IsZero() {
		age := now.Sub(feedback.at)
		if age >= 0 && age < 450*time.Millisecond {
			// rthit: 20% -5px, 40% +4px, 60% -2px, 80% +1px.
			switch {
			case age < 90*time.Millisecond:
				offsetX = -5 * (1 - float32(age)/float32(90*time.Millisecond))
			case age < 180*time.Millisecond:
				offsetX = 4 * (1 - float32(age-90*time.Millisecond)/float32(90*time.Millisecond))
			case age < 315*time.Millisecond:
				offsetX = -2 * (1 - float32(age-180*time.Millisecond)/float32(135*time.Millisecond))
			default:
				offsetX = 1 * (1 - float32(age-315*time.Millisecond)/float32(135*time.Millisecond))
			}
		}
	}
	if offsetX != 0 {
		p.Transform(ui.Translation(offsetX, 0), 0, 0, func() {
			drawAccountDynamicStateAt(p, r, n, lit, active, pal, now, feedback, loc)
		})
		return
	}
	drawAccountDynamicStateAt(p, r, n, lit, active, pal, now, feedback, loc)
}

func drawAccountDynamicStateAt(p *ui.Painter, r ui.Rect, n providerNode, lit, active bool, pal palette, now time.Time, feedback topologyFeedback, loc Localization) {
	_ = loc
	finalFailure := n.Failed && !n.WentOn
	dotColor := pal.Faint
	if finalFailure {
		dotColor = pal.Danger
	} else if n.Answered {
		dotColor = pal.Text
	} else if lit || n.Current {
		dotColor = pal.Text
	}
	dot := ui.Rect{X: r.X + 12, Y: r.Y + r.H/2 - 4, W: 8, H: 8}
	if active {
		// rtlive: a 40%-opacity ring expanding from 8 to 14 DIPs over
		// 70% of 1.8s, then transparent for the rest of the cycle.
		phase := clamp01(float32(now.UnixMilli()%1800) / 1800)
		k := clamp01(phase / 0.7)
		ring := 8 + 6*k
		ringAlpha := 0.4 * (1 - k)
		if ringAlpha > 0.005 {
			p.Stroke(
				ui.Rect{X: dot.X + 4 - ring/2, Y: dot.Y + 4 - ring/2, W: ring, H: ring},
				dotColor.Alpha(ringAlpha), ring/2, 1,
			)
		}
	}
	if n.Failed {
		label := accountFailureLabel(n)
		drawAccountFailureTag(p, r, label, pal, now, feedback)
	}
}

func drawAccountPulse(p *ui.Painter, r ui.Rect, n providerNode, pal palette, now time.Time) {
	_ = n
	dot := ui.Rect{X: r.X + 12, Y: r.Y + r.H/2 - 4, W: 8, H: 8}
	phase := clamp01(float32(now.UnixMilli()%1800) / 1800)
	k := clamp01(phase / 0.7)
	ring := 8 + 6*k
	ringAlpha := 0.4 * (1 - k)
	if ringAlpha > 0.005 {
		p.Stroke(ui.Rect{X: dot.X + 4 - ring/2, Y: dot.Y + 4 - ring/2, W: ring, H: ring}, pal.Text.Alpha(ringAlpha), ring/2, 1)
	}
}

func accountFailureLabel(n providerNode) string {
	label := strings.TrimSpace(strings.Trim(strings.Join([]string{n.Status, failWord(n.Note)}, " · "), "· "))
	if label == "" {
		return "failed"
	}
	return label
}

func feedbackForRow(flights []flightTrack, row providerNode) topologyFeedback {
	var latest topologyFeedback
	for _, f := range flights {
		rowProvider := firstNonEmpty(row.Provider, providerOf(row.Model))
		if !strings.EqualFold(f.provider, rowProvider) && !strings.EqualFold(providerOf(f.model), rowProvider) {
			continue
		}
		if f.accountID != "" && row.AccountID != "" && !strings.EqualFold(f.accountID, row.AccountID) {
			continue
		}
		if f.model != "" && row.Model != "" && !modelMatches(row.Model, f.model, rowProvider) {
			continue
		}
		if (f.phaseKind == flightHold || f.phaseKind == flightRetryBack) && f.phaseAt.After(latest.at) {
			latest = topologyFeedback{at: f.phaseAt, kind: f.phaseKind, failed: f.phaseFailed}
		}
	}
	return latest
}

func feedbackVisible(flights []flightTrack, now time.Time) bool {
	for _, f := range flights {
		if f.phaseAt.IsZero() || f.phaseKind != flightHold && f.phaseKind != flightRetryBack {
			continue
		}
		if age := now.Sub(f.phaseAt); age >= 0 && age < 450*time.Millisecond {
			return true
		}
	}
	return false
}

func drawStillRoute(p *ui.Painter, geometry *flightGeometry, agentIndex, providerIndex int, color ui.Color) {
	if geometry == nil || agentIndex < 0 || agentIndex >= len(geometry.agentHub) {
		return
	}
	drawFlightPath(p, geometry.agentHub[agentIndex], color, 1.5)
	if providerIndex >= 0 && providerIndex < len(geometry.hubProvider) {
		drawFlightPath(p, geometry.hubProvider[providerIndex], color, 1.5)
	}
}

// stageAgents is the upstream stage's agent set, not the whole request
// history: every agent with a request active in this exact snapshot, or the
// agent of the request told about, and of the latest requests when the
// gateway is idle. Upstream's stage keeps each recently active agent beside
// the selected one, so a request's agent never drops out merely because the
// selected row belongs to another agent. During a replay it is every agent
// actually in flight at the replay cursor.
func (m *Model) stageAgents(s routing.Snapshot, sel routing.RouteView, hasSel bool, routes []routing.RouteView, replay replayTopology, replayActive bool, now time.Time) []agentNode {
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
			ID: id, Name: agentName(id), Model: model, Icon: agentIcon(id),
			Active: active, Hue: agentHue(id),
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
	if !replayActive {
		// Upstream's staged() keeps each agent's newest request that is
		// still running or ended within LINGER (12s), plus the selected
		// request. A busy history day must not turn the stage into a
		// vertical list of every historical agent.
		last := map[string]routing.RouteView{}
		if now.IsZero() {
			now = time.Now()
		}
		cutoff := now.Add(-routingStageLinger)
		for _, rv := range routes {
			key := strings.ToLower(rv.Agent)
			if _, ok := last[key]; ok {
				continue
			}
			last[key] = rv
			if rv.Active || (!rv.Time.IsZero() && rv.Time.After(cutoff)) {
				add(rv.Agent, rv.Model, rv.Active)
			}
		}
	}
	if hasSel {
		add(sel.Agent, sel.Model, sel.Active)
	}
	return out
}

// routingStageLinger is client.js's LINGER: an agent's last completed
// request stays on the stage for 12 seconds.
const routingStageLinger = 12 * time.Second

// stageAccountRows is exactly the accounts of the request told about plus
// the providers of requests currently active. It never blends every row in
// the history into one fictitious candidate set.
func (m *Model) stageAccountRows(sel routing.RouteView, hasSel bool, live []routing.LiveRequest, replay []routing.RouteView, replayActive bool) []providerNode {
	var rows []providerNode
	seen := map[string]bool{}
	// Read the immutable config once for the whole stage. Copying the
	// snapshot for every replayed route would scale a busy history frame
	// with the number of routes, not the number of visible rows.
	cfg := m.snapshot().Config
	add := func(items []providerNode) {
		for _, n := range items {
			key := providerSeatKey(n)
			if key == "\x00\x00\x00" || seen[key] {
				continue
			}
			// A provider-only concurrency alias never gets its own row when
			// the selected trace already has a seat in that provider. Its
			// requested model ("group/...") is not an account, and showing
			// it beside the real Order row was the duplicate live endpoint.
			if n.AccountID == "" && hasSel {
				duplicate := false
				for _, existing := range rows {
					if strings.EqualFold(existing.Provider, n.Provider) &&
						(existing.AccountID != "" || providerMatchScore(existing, providerTarget{
							provider: n.Provider, model: n.Model,
						}) > 0) {
						duplicate = true
						break
					}
				}
				if duplicate {
					continue
				}
			}
			seen[key] = true
			rows = append(rows, n)
		}
	}
	if hasSel {
		add(providerRowsWithConfig(sel, m.accountsMasked(), cfg))
	}
	if replayActive {
		for _, rv := range replay {
			add(providerRowsWithConfig(rv, m.accountsMasked(), cfg))
		}
	} else {
		for _, l := range live {
			if l.Provider == "" && l.Model == "" {
				continue
			}
			// The selected route's real Order/Tries already describes the
			// account this request is using. Concurrency only repeats the
			// provider and the requested alias; appending that as another
			// row creates the false "qoder · group/..." account seen in
			// the live stage. Older live-only routes still get an explicit
			// row when there is no trace to attach them to.
			if hasSel && sel.ID != 0 && l.RouteID == sel.ID {
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

func providerSeatKey(n providerNode) string {
	// The row identity is the seat, not the display label. An account id
	// and its provider+model pair identify the same seat even when one
	// source lacks Who or carries a provider-only display name.
	if n.AccountID != "" {
		return strings.ToLower(n.Provider + "\x00" + n.AccountID)
	}
	return strings.ToLower(n.Provider + "\x00" + n.Model)
}

func drawRoutingAgentBox(p *ui.Painter, r ui.Rect, a agentNode, lit bool, pal palette, content bool) {
	// .rt-node: card, 1px line, 12px radius, icon 26px, name 13px/600,
	// detail 11px muted, everything centered. A live request pulses the
	// agent's dot, not the card outline.
	if !content {
		p.Fill(r, pal.Panel, 12)
		p.Stroke(r, pal.Line, 12, 1)
		return
	}
	cx := r.X + r.W/2
	compact := r.H < 70
	iconSize := float32(26)
	if compact {
		iconSize = 20
	}
	iconY := r.Y + 10
	if compact {
		iconY = r.Y + 8
	}
	iconRect := ui.Rect{X: cx - iconSize/2, Y: iconY, W: iconSize, H: iconSize}
	if !drawRoutingIcon(p, iconRect, []string{a.Icon, a.ID}, pal.Text) {
		drawGenericIcon(p, iconRect, pal.Muted)
	}
	nameY := iconY + iconSize + 3
	nameSize := float32(13)
	if compact {
		nameSize = 11
		nameY = iconY + iconSize + 2
	}
	name := agentName(a.ID)
	nameW, _ := p.MeasureText(0, ui.Span{Text: name, Size: nameSize, Weight: 600})
	dot := float32(7)
	total := nameW + dot + 6
	dotX := cx - total/2
	drawTextAt(p, name, dotX+dot+6, nameY, nameSize, 600, pal.Text, r.W-dot-12)
	dotRect := ui.Rect{X: dotX, Y: nameY + nameSize*0.48, W: dot, H: dot}
	p.Fill(dotRect, a.Hue, dot/2)
	if lit {
		// A lit source keeps the same rtlive ring as .rt-accts li.now.
		phase := clamp01(float32(p.Now().UnixMilli()%1800) / 1800)
		k := clamp01(phase / 0.7)
		ring := dot + 6*k
		if alpha := 0.4 * (1 - k); alpha > 0.005 {
			cx, cy := dotRect.X+dot/2, dotRect.Y+dot/2
			p.Stroke(ui.Rect{X: cx - ring/2, Y: cy - ring/2, W: ring, H: ring}, a.Hue.Alpha(alpha), ring/2, 1)
		}
	}
	model := a.Model
	if model == "" {
		model = a.Folder
	}
	if model != "" {
		subY := nameY + nameSize + 3
		if compact {
			subY = nameY + nameSize + 1
		}
		if subY+10 < r.Y+r.H-3 {
			drawCenteredClippedText(p, model, cx, subY, 11, pal.Muted, r.W-16)
		}
	}
}

func drawTextCentered(p *ui.Painter, s string, cx, y, size float32, color ui.Color) {
	width, _ := p.MeasureText(0, ui.Span{Text: s, Size: size})
	p.Text(cx-width/2, y, s, size, color)
}

func drawTextAt(p *ui.Painter, s string, x, y, size float32, weight int, color ui.Color, maxWidth float32) {
	if maxWidth > 0 {
		s = fitText(p, s, size, weight, maxWidth)
	}
	if weight != 400 {
		p.RichText(x, y, max(0, maxWidth), ui.Span{Text: s, Size: size, Weight: weight, Color: color})
		return
	}
	p.Text(x, y, s, size, color)
}

func drawClippedText(p *ui.Painter, x, y, maxWidth float32, s string, size float32, color ui.Color) {
	if maxWidth <= 0 || s == "" {
		return
	}
	// Truncate with the same measured font metrics rather than wrapping
	// every text row in Painter.Clip. Besides avoiding an allocation per
	// row, this keeps a Canvas.Draw callback free of nested clips: MyGo
	// 0.5.6 samples such a canvas at the wrong origin on some GPUs.
	p.Text(x, y, fitText(p, s, size, 400, maxWidth), size, color)
}

// fitTextMeasured truncates a single-line label with native text metrics.
// It is used where text is laid out outside an Element, so rune-count
// estimates would drift for mixed Chinese and Latin labels.
func fitTextMeasured(measure func(float32, ...ui.Span) (float32, float32), s string, size float32, weight int, maxWidth float32) string {
	if maxWidth <= 0 {
		return s
	}
	width, _ := measure(0, ui.Span{Text: s, Size: size, Weight: weight})
	if width <= maxWidth {
		return s
	}
	runes := []rune(s)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		candidate := string(runes[:mid]) + "…"
		w, _ := measure(0, ui.Span{Text: candidate, Size: size, Weight: weight})
		if w <= maxWidth || mid == 0 {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo <= 0 {
		return "…"
	}
	return string(runes[:lo]) + "…"
}

func drawGenericIcon(p *ui.Painter, r ui.Rect, color ui.Color) {
	// app.js's generic icon: a quiet hexagon with its three inner spokes.
	var path ui.Path
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	s := min(r.W, r.H)
	path.MoveTo(cx, cy-s*0.42)
	path.LineTo(cx+s*0.36, cy-s*0.21)
	path.LineTo(cx+s*0.36, cy+s*0.21)
	path.LineTo(cx, cy+s*0.42)
	path.LineTo(cx-s*0.36, cy+s*0.21)
	path.LineTo(cx-s*0.36, cy-s*0.21)
	path.Close()
	p.StrokePath(&path, 1.25, color)
	p.Line(cx, cy, cx, cy+s*0.42, 1.25, color)
	p.Line(cx-s*0.36, cy-s*0.21, cx, cy-s*0.02, 1.25, color)
	p.Line(cx+s*0.36, cy-s*0.21, cx, cy-s*0.02, 1.25, color)
}

func fitText(p *ui.Painter, s string, size float32, weight int, maxWidth float32) string {
	if maxWidth <= 0 {
		return s
	}
	width, _ := p.MeasureText(0, ui.Span{Text: s, Size: size, Weight: weight})
	if width <= maxWidth {
		return s
	}
	ellipsisW, _ := p.MeasureText(0, ui.Span{Text: "…", Size: size, Weight: weight})
	lo, hi := 0, len([]rune(s))
	for lo < hi {
		mid := (lo + hi + 1) / 2
		candidate := string([]rune(s)[:mid]) + "…"
		w, _ := p.MeasureText(0, ui.Span{Text: candidate, Size: size, Weight: weight})
		if w <= maxWidth || mid == 0 {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo <= 0 {
		return "…"
	}
	_ = ellipsisW
	return string([]rune(s)[:lo]) + "…"
}

func drawRoutingHub(p *ui.Painter, r ui.Rect, s routing.Snapshot, gatewayURL, mode string, pal palette, content bool) {
	// .rt-hub: the foreground 1px border plus a 4px ring of --card-2.
	if !content {
		p.Fill(ui.Rect{X: r.X - 4, Y: r.Y - 4, W: r.W + 8, H: r.H + 8}, pal.PanelMuted, 16)
		p.Fill(r, pal.Panel, 12)
		p.Stroke(r, pal.Text, 12, 1)
		return
	}
	cx := r.X + r.W/2
	if !drawMagicLogo(p, ui.Rect{X: cx - 13, Y: r.Y + 10, W: 26, H: 26}, pal.Text) {
		drawMagpieMark(p, ui.Rect{X: cx - 13, Y: r.Y + 10, W: 26, H: 26}, pal.Text, pal.Panel)
	}
	drawTextCentered(p, "magpie", cx, r.Y+42, 13, pal.Text)
	host := strings.TrimPrefix(strings.TrimPrefix(gatewayURL, "http://"), "https://")
	if host == "" {
		host = "gateway"
	}
	drawCenteredClippedText(p, host, cx, r.Y+60, 11, pal.Muted, r.W-16)
	chipText := mode
	if chipText == "" {
		if s.GatewayOnline {
			chipText = "…"
		} else {
			chipText = "offline"
		}
	}
	chipW, _ := p.MeasureText(0, ui.Span{Text: chipText, Size: 10.5, Weight: 560})
	chipW += 16
	if chipW < 44 {
		chipW = 44
	}
	chip := ui.Rect{X: cx - chipW/2, Y: r.Y + 78, W: chipW, H: 18}
	p.Fill(chip, pal.Text, 9)
	drawCenteredClippedText(p, chipText, cx, chip.Y+2, 10.5, pal.Panel, chip.W-8)
}

func routingModeID(rv routing.RouteView, providerRouting string) string {
	routingMode := ""
	if rv.Group != nil {
		routingMode = rv.Group.Routing
	} else {
		for _, w := range rv.Order {
			if w.Fallback {
				continue
			}
			routingMode = w.Routing
			break
		}
		if routingMode == "" {
			routingMode = providerRouting
		}
	}
	switch strings.ToLower(strings.TrimSpace(routingMode)) {
	case "", "smart":
		return "smart"
	case "order":
		return "order"
	case "rotate":
		return "rotate"
	case "usage":
		return "usage"
	case "pace":
		return "pace"
	case "manual":
		return "manual"
	case "weight":
		return "weight"
	default:
		return strings.ToLower(strings.TrimSpace(routingMode))
	}
}

// routeModeLabel returns Magpie's name for the routing mode that actually
// decided the displayed request. It follows client.js's header(): a group's
// own routing wins; otherwise the first non-fallback row's provider routing
// does. An empty mode is Magpie's Smart default, not an unknown mode. The
// only honest unknown is a selected route with neither a group, an order,
// nor a matching provider configuration to supply the mode.
func routeModeLabel(rv routing.RouteView, hasSel bool, providerRouting string, providerKnown bool) string {
	if !hasSel {
		return ""
	}
	mode := routingModeID(rv, providerRouting)
	switch mode {
	case "smart":
		if rv.Group != nil || len(rv.Order) > 0 || providerKnown {
			return "Smart"
		}
		return "routing ?"
	case "order":
		return "In order"
	case "rotate":
		return "In turn"
	case "usage":
		return "Least used"
	case "pace":
		return "Weekly pace"
	case "manual":
		return "Manual"
	case "weight":
		return "By weight"
	default:
		return mode
	}
}

// stageRoutingMode supplies providers.json's routing mode only when the
// request itself did not record one. Group and Order fields stay the primary
// evidence; the read-only config merely fills a field Magpie's public trace
// omits.
func stageRoutingMode(rv routing.RouteView, hasSel bool, cfg routing.ConfigSnapshot) string {
	providerRouting, providerKnown := "", false
	if hasSel {
		providerRouting, providerKnown = routeProviderRouting(rv, cfg)
	}
	return routeModeLabel(rv, hasSel, providerRouting, providerKnown)
}

func routeProviderRouting(rv routing.RouteView, cfg routing.ConfigSnapshot) (routingMode string, known bool) {
	provider := ""
	for _, w := range rv.Order {
		if w.Fallback {
			continue
		}
		provider = firstNonEmpty(w.Provider, providerOf(w.Model))
		break
	}
	if provider == "" {
		provider = rv.DisplayProvider()
	}
	if info, ok := providerInfoByID(cfg, provider); ok {
		return info.Routing, true
	}
	return "", false
}

func drawCenteredClippedText(p *ui.Painter, s string, cx, y, size float32, color ui.Color, maxWidth float32) {
	s = fitText(p, s, size, 400, maxWidth)
	width, _ := p.MeasureText(0, ui.Span{Text: s, Size: size})
	p.Text(cx-width/2, y, s, size, color)
}

func drawRoutingAccountRow(p *ui.Painter, r ui.Rect, n providerNode, lit, active bool, pal palette, now time.Time, content bool, feedback topologyFeedback, loc Localization, maskBlur, maskOpacity float32) {
	offsetX := float32(0)
	if !feedback.at.IsZero() {
		age := now.Sub(feedback.at)
		if age >= 0 && age < 450*time.Millisecond {
			// rthit: 20% -5px, 40% +4px, 60% -2px, 80% +1px.
			switch {
			case age < 90*time.Millisecond:
				offsetX = -5 * (1 - float32(age)/float32(90*time.Millisecond))
			case age < 180*time.Millisecond:
				offsetX = 4 * (1 - float32(age-90*time.Millisecond)/float32(90*time.Millisecond))
			case age < 315*time.Millisecond:
				offsetX = -2 * (1 - float32(age-180*time.Millisecond)/float32(135*time.Millisecond))
			default:
				offsetX = 1 * (1 - float32(age-315*time.Millisecond)/float32(135*time.Millisecond))
			}
		}
	}
	if offsetX != 0 {
		p.Transform(ui.Translation(offsetX, 0), 0, 0, func() {
			drawRoutingAccountRowAt(p, r, n, lit, active, pal, now, content, feedback, loc, maskBlur, maskOpacity)
		})
		return
	}
	drawRoutingAccountRowAt(p, r, n, lit, active, pal, now, content, feedback, loc, maskBlur, maskOpacity)
}

func drawRoutingAccountRowAt(p *ui.Painter, r ui.Rect, n providerNode, lit, active bool, pal palette, now time.Time, content bool, feedback topologyFeedback, loc Localization, maskBlur, maskOpacity float32) {
	failed := n.Failed
	finalFailure := n.Failed && !n.WentOn
	resting := accountResting(n, now)
	// .rt-accts li: 10px dot, content, status; 9px 12px padding; 10px
	// radius. The border changes to --fg when on, dashed when resting,
	// and red on failure.
	border := pal.Line
	style := ui.BorderSolid
	if finalFailure {
		border = pal.Danger.Alpha(0.8)
	} else if lit {
		border = pal.Text
	} else if n.Answered {
		border = pal.Text
	} else if resting {
		border = pal.LineSoft
		style = ui.BorderDashed
	} else if n.Fallback {
		border = pal.LineSoft
	}
	width := float32(1)
	if lit || n.Answered || finalFailure {
		width = 1.2
	}
	if !content {
		p.Fill(r, pal.Panel, 10)
		if style == ui.BorderDashed {
			p.StrokeDashed(r, border, 10, width)
		} else {
			p.Stroke(r, border, 10, width)
		}
		return
	}
	if style != ui.BorderDashed {
		p.Stroke(r, border, 10, width)
	}
	// Upstream's row is a three-column grid: a dot, a flexible identity
	// column, and a right-aligned status. The identity column itself keeps
	// logo + provider + model on the first baseline and the account below.
	if r.H >= 40 {
		p.Fill(ui.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}, pal.Panel, 9)
	}
	ink := pal.Text
	muted := pal.Muted
	faint := pal.Faint
	if resting {
		ink = ink.Alpha(0.5)
		muted = muted.Alpha(0.5)
		faint = faint.Alpha(0.5)
		border = border.Alpha(0.65)
	}
	dotColor := faint
	if finalFailure {
		dotColor = pal.Danger
	} else if n.Answered {
		dotColor = ink
	} else if lit || n.Current {
		dotColor = ink
	}
	dot := ui.Rect{X: r.X + 12, Y: r.Y + r.H/2 - 4, W: 8, H: 8}
	p.Stroke(dot, dotColor, 4, 1.5)
	if lit || n.Answered || finalFailure {
		p.Fill(dot, dotColor, 4)
	}
	_ = active

	status := accountStatusTextLocalized(n, now, loc)
	statusSize := float32(11.5)
	statusW := float32(0)
	if status != "" {
		statusW, _ = p.MeasureText(0, ui.Span{Text: status, Size: statusSize})
		statusW = min(statusW, (r.W-96)*0.48)
	}
	contentX := r.X + 30
	contentRight := r.X + r.W - 12
	if status != "" {
		contentRight -= statusW + 12
	}
	iconRect := ui.Rect{X: contentX, Y: r.Y + r.H/2 - 8, W: 16, H: 16}
	iconKeys := []string{n.Icon, n.Provider}
	if !drawRoutingIcon(p, iconRect, iconKeys, muted) {
		drawGenericIcon(p, iconRect, pal.Faint)
	}
	textX := contentX + 24
	name := firstNonEmpty(n.Name, n.Provider)
	if n.Name == "" {
		name = n.Provider
	}
	nameSize := float32(13)
	model := n.Model
	if model == "" {
		model = n.Plan
	}
	modelW := float32(0)
	modelGap := float32(0)
	fallbackW := float32(0)
	fallbackGap := float32(0)
	if n.Fallback {
		fallbackW, _ = p.MeasureText(0, ui.Span{Text: "fallback", Size: 10.5, Weight: 560})
		fallbackW += 12
		fallbackGap = 7
	}
	if model != "" {
		modelW, _ = p.MeasureText(0, ui.Span{Text: model, Size: 11, Weight: 500})
		modelW = min(modelW, max((contentRight-textX)*0.55-fallbackW-fallbackGap, 0))
		if modelW > 0 {
			modelGap = 7
		}
	}
	nameMax := max(contentRight-textX-modelW-modelGap-fallbackW-fallbackGap, 0)
	drawTextAt(p, name, textX, r.Y+5, nameSize, 560, ink, nameMax)
	if model != "" && modelW > 0 {
		nameActual, _ := p.MeasureText(0, ui.Span{Text: name, Size: nameSize, Weight: 560})
		modelX := min(textX+min(nameActual, nameMax)+modelGap, contentRight-modelW)
		p.Fill(ui.Rect{X: modelX - 5, Y: r.Y + 4, W: modelW + 10, H: 17}, pal.PanelMuted, 5)
		drawClippedText(p, modelX, r.Y+5, modelW, model, 11, muted)
		if fallbackW > 0 {
			badgeX := min(modelX+modelW+fallbackGap+5, contentRight-fallbackW)
			if badgeX >= modelX+modelW {
				badge := ui.Rect{X: badgeX, Y: r.Y + 4, W: fallbackW, H: 16}
				p.Stroke(badge, pal.Line.Alpha(boolFloat(resting, 0.5)+0.5), 8, 1)
				drawCenteredClippedText(p, "fallback", badge.X+badge.W/2, badge.Y+1, 10.5, muted, badge.W-6)
			}
		}
	}
	sub := n.Who
	if sub == "" {
		sub = "—"
	}
	if n.Masked {
		// Upstream's .pii draws the stand-in account blurred and at half
		// opacity. Element.Blur is an offscreen layer that MyGo rebuilds
		// every time the retained tree paints, which made a 60 FPS flight
		// pay for an otherwise static label. This text belongs to the
		// cached stage bitmap, so approximate CSS's 2.2px Gaussian with a
		// small normalized kernel of Painter text calls instead. The
		// bitmap's key includes blur and opacity, so rtpii still animates.
		drawMaskedClippedText(p, textX, r.Y+22, contentRight-textX, sub, 11, maskBlur, maskOpacity, muted)
	} else {
		drawClippedText(p, textX, r.Y+22, contentRight-textX, sub, 11, muted)
	}
	if status != "" {
		statusX := r.X + r.W - 12 - statusW
		drawClippedText(p, statusX, r.Y+7, statusW+1, status, statusSize, boolColor(finalFailure, pal.Danger, accountStatusColor(n, pal, now)))
	}
	if n.Known {
		// .rt-accts .bar: the allowance is 3px under both text columns.
		bar := ui.Rect{X: r.X + 30, Y: r.Y + r.H - 5, W: max(r.W-42, 0), H: 3}
		p.Fill(bar, pal.PanelMuted, 2)
		used := float32(max(0, min(n.Used, 100)) / 100)
		p.Fill(ui.Rect{X: bar.X, Y: bar.Y, W: bar.W * used, H: bar.H}, pal.Muted, 2)
	}
	_ = failed
	_ = feedback
}

// drawMaskedClippedText paints the same stand-in account that Element.Blur
// used to paint, without creating an offscreen layer. A 5x5 Gaussian kernel
// sampled around the glyphs closely matches CSS blur(2.2px) at 11px while
// remaining just ordinary glyph draws in the static stage canvas.
func drawMaskedClippedText(p *ui.Painter, x, y, maxWidth float32, s string, size, sigma, opacity float32, color ui.Color) {
	if maxWidth <= 0 || s == "" || opacity <= 0.001 {
		return
	}
	s = fitText(p, s, size, 400, maxWidth)
	drawMaskedText(p, x, y, s, size, sigma, opacity, color)
}

// drawMaskedText paints a deterministic account stand-in with a small
// normalized Gaussian kernel. It is Painter-only and therefore safe to
// paint inside a cached Canvas; no retained Element or offscreen effect
// layer is allocated per frame.
func drawMaskedText(p *ui.Painter, x, y float32, s string, size, sigma, opacity float32, color ui.Color) {
	if s == "" || opacity <= 0.001 {
		return
	}
	if sigma <= 0.05 {
		p.Text(x, y, s, size, color.Alpha(opacity))
		return
	}
	const taps = 7
	step := sigma * 0.85
	weights := make([]float32, taps)
	var sum float32
	for i := range taps {
		d := float32(i-(taps-1)/2) * step
		w := float32(math.Exp(-float64(d*d) / (2 * float64(sigma*sigma))))
		weights[i] = w
	}
	for _, wy := range weights {
		for _, wx := range weights {
			sum += wx * wy
		}
	}
	for iy := taps - 1; iy >= 0; iy-- {
		for ix := taps - 1; ix >= 0; ix-- {
			weight := weights[ix] * weights[iy] / sum
			dx := float32(ix-(taps-1)/2) * step
			dy := float32(iy-(taps-1)/2) * step
			alpha := opacity * weight
			if alpha <= 0.002 {
				continue
			}
			p.Text(x+dx, y+dy, s, size, color.Alpha(alpha))
		}
	}
}

const accountFailureTagDuration = 1800 * time.Millisecond

// drawAccountFailureTag mirrors .rt-accts li.hit/tagged .tag. The persistent
// row status carries the same fact; this is only the transient 1.8s chip
// upstream shows when a try fails. It never renders for a merely resting row.
func drawAccountFailureTag(p *ui.Painter, r ui.Rect, label string, pal palette, now time.Time, feedback topologyFeedback) {
	if feedback.at.IsZero() || !feedback.failed {
		return
	}
	age := now.Sub(feedback.at)
	if age < 0 || age >= accountFailureTagDuration {
		return
	}
	const fade = 250 * time.Millisecond
	reveal := clamp01(float32(age) / float32(fade))
	if remaining := accountFailureTagDuration - age; remaining < fade {
		reveal *= clamp01(float32(remaining) / float32(fade))
	}
	tagW, _ := p.MeasureText(0, ui.Span{Text: label, Size: 10.5, Weight: 600})
	tagW += 14
	tagRect := ui.Rect{X: r.X + r.W - 10 - tagW, Y: r.Y - 10, W: tagW, H: 17}
	// The chip enters from translateY(4px) scale(.9) and fades out with the
	// same ease the stylesheet names.
	scale := 0.9 + 0.1*easeOut(reveal)
	dy := 4 * (1 - easeOut(reveal))
	cx, cy := tagRect.X+tagRect.W/2, tagRect.Y+tagRect.H/2
	p.Transform(ui.Translation(0, dy).Then(ui.Scaling(scale, scale)), cx, cy, func() {
		p.Fill(tagRect, pal.Danger.Alpha(reveal), 6)
		drawTextCentered(p, label, cx, tagRect.Y+1, 10.5, ui.Hex("#ffffff").Alpha(reveal))
	})
}

func accountStatusColor(n providerNode, pal palette, now time.Time) ui.Color {
	switch {
	case accountResting(n, now):
		return pal.Danger.Alpha(0.85)
	case n.Answered:
		return pal.Text2
	case n.Current:
		return pal.Text
	default:
		return pal.Muted
	}
}

func accountStatusText(n providerNode, now time.Time) string {
	return accountStatusTextLocalized(n, now, NewLocalization("en"))
}

func accountStatusTextLocalized(n providerNode, now time.Time, loc Localization) string {
	if accountResting(n, now) {
		if !n.RestUntil.IsZero() {
			left := n.RestUntil.Sub(now)
			if left > 0 {
				parts := make([]string, 0, 2)
				if why := failWord(n.RestWhy); why != "" {
					parts = append(parts, why)
				}
				parts = append(parts, restWhenTextLocalized(n.RestUntil, left, loc))
				return strings.Join(parts, " · ")
			}
		}
		if why := failWord(n.RestWhy); why != "" {
			return loc.F("routing.accountStatus.restingOnly", why)
		}
		return loc.T("routing.accountStatus.resting")
	}
	if n.Current {
		return loc.T("routing.accountStatus.answering")
	}
	if n.Answered {
		return loc.T("routing.accountStatus.answered")
	}
	if n.WentOn {
		parts := make([]string, 0, 3)
		if n.Status != "" {
			parts = append(parts, n.Status)
		}
		if why := failWord(n.Note); why != "" {
			parts = append(parts, why)
		}
		parts = append(parts, loc.T("routing.accountStatus.wentOn"))
		return strings.Join(parts, " · ")
	}
	if n.Failed {
		switch {
		case n.Status != "" && n.Note != "":
			return n.Status + " · " + failWord(n.Note)
		case n.Status != "":
			return n.Status
		case n.Note != "":
			return n.Note
		}
	}
	if n.Known {
		return loc.F("routing.accountStatus.used", int(n.Used+0.5))
	}
	if n.Note != "" {
		return n.Note
	}
	if n.Kind == "key" {
		return loc.T("routing.accountStatus.apiKey")
	}
	if n.Kind == "account" {
		return loc.T("routing.accountStatus.unknownQuota")
	}
	return loc.T("routing.accountStatus.account")
}

func accountResting(n providerNode, now time.Time) bool {
	if !n.Rest {
		return false
	}
	if n.RestUntil.IsZero() || now.IsZero() {
		return true
	}
	return n.RestUntil.After(now)
}

func restWhenText(until time.Time, left time.Duration) string {
	return restWhenTextLocalized(until, left, NewLocalization("en"))
}

func restWhenTextLocalized(until time.Time, left time.Duration, loc Localization) string {
	if left < time.Hour {
		return loc.F("routing.accountStatus.backIn", restDurationText(left))
	}
	return loc.F("routing.accountStatus.backAt", until.Local().Format("15:04"), restDurationText(left))
}

// failWord uses the same vocabulary as Magpie's Routing page. Unknown
// reasons stay literal rather than being replaced by a guessed category.
func failWord(why string) string {
	switch strings.ToLower(strings.TrimSpace(why)) {
	case "":
		return ""
	case "rate":
		return "rate limited"
	case "credit":
		return "out of credit"
	case "quota":
		return "quota used up"
	case "other":
		return "failed"
	case "auth":
		return "sign-in required"
	case "canceled":
		return "canceled"
	case "foreign":
		return "another account's reasoning"
	case "floor":
		return "reply too short"
	case "verify":
		return "needs verification"
	case "refused":
		return "refused (safety filter)"
	case "shape":
		return "request not understood"
	case "proxy":
		return "proxy not reachable"
	case "effort":
		return "reasoning effort not in its plan"
	case "overflow":
		return "too long for its model"
	case "slow":
		return "slow to start"
	case "prompt":
		return "agent's prompt turned away"
	case "restricted":
		return "account restricted by the vendor"
	default:
		return strings.TrimSpace(why)
	}
}

func restDurationText(d time.Duration) string {
	if d <= 0 {
		return "now"
	}
	if d < time.Minute {
		return itoa(int(d.Round(time.Second)/time.Second)) + "s"
	}
	if d < time.Hour {
		return itoa(int(d.Round(time.Minute)/time.Minute)) + "m"
	}
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	if m == 0 {
		return itoa(h) + "h"
	}
	return itoa(h) + "h " + itoa(m) + "m"
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
	// client.js's layout() uses a midpoint control whose x is exactly
	// between the two endpoints. Keep dx signed: the previous max(dx, 22)
	// bent every right-to-left return farther right instead of following
	// the same wire backwards.
	dx := (bx - ax) * 0.5
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

func stageOutboundPath(agent, hub nodeBox, provider *nodeBox, narrow bool) flightPath {
	ax, ay := agent.rightCenter()
	hlx, hly := hub.leftCenter()
	raw := cubicFlightPoints(ax, ay, hlx, hly)
	if provider == nil {
		return newFlightPath(raw)
	}
	var providerPath flightPath
	if narrow {
		providerPath = hubToNarrowProviderPath(hub, []nodeBox{agent}, provider)
	} else {
		providerPath = hubToProviderPath(hub, provider)
	}
	if len(providerPath.pts) == 0 {
		return newFlightPath(raw)
	}
	// client.js's via() joins the two actual wires with a shallow arc
	// across the hub, instead of sending the bird through the hub center.
	// The arc uses each wire's outgoing tangent, so it remains continuous
	// in both wide and narrow layouts.
	fromTX, fromTY := flightTangent(raw, 1)
	toTX, toTY := flightTangent(providerPath.pts, 1)
	via := viaFlightPoints(hlx, hly, fromTX, fromTY,
		providerPath.pts[0].x, providerPath.pts[0].y, toTX, toTY)
	raw = append(raw, via[1:]...)
	raw = append(raw, providerPath.pts[1:]...)
	return newFlightPath(raw)
}

// flightTangent returns the unit tangent just before/after a polyline
// point. dir=1 points forward; dir=-1 points backward.
func flightTangent(points []flightPoint, dir float32) (float32, float32) {
	if len(points) < 2 {
		return dir, 0
	}
	// The sampled paths have enough resolution that three points around
	// either end give a stable tangent without depending on the exact
	// control-point representation.
	if dir >= 0 {
		i := min(3, len(points)-1)
		dx, dy := points[i].x-points[0].x, points[i].y-points[0].y
		return normalizeVector(dx, dy, 1, 0)
	}
	i := max(len(points)-4, 0)
	dx, dy := points[i].x-points[len(points)-1].x, points[i].y-points[len(points)-1].y
	return normalizeVector(dx, dy, -1, 0)
}

func normalizeVector(dx, dy, fallbackX, fallbackY float32) (float32, float32) {
	n := float32(math.Hypot(float64(dx), float64(dy)))
	if n < 0.001 {
		return fallbackX, fallbackY
	}
	return dx / n, dy / n
}

// viaFlightPoints mirrors client.js's via(): a cubic whose control points
// continue the incoming tangent and meet the outgoing tangent, offset the
// same way on both sides to make a gentle arc.
func viaFlightPoints(ax, ay, atx, aty, bx, by, btx, bty float32) []flightPoint {
	dx, dy := bx-ax, by-ay
	dist := float32(math.Hypot(float64(dx), float64(dy)))
	if dist < 0.001 {
		return []flightPoint{{x: ax, y: ay}, {x: bx, y: by}}
	}
	var c1x, c1y, c2x, c2y float32
	if dist < 4 {
		// The small loop used by upstream when both anchors are almost the
		// same point. Keep it bounded so it cannot fold over the hub.
		k := dist * 0.42
		nx, ny := aty, -atx
		c1x, c1y = ax+atx*k-nx*k*0.6, ay+aty*k-ny*k*0.6
		c2x, c2y = bx-btx*k+nx*k*0.6, by-bty*k+ny*k*0.6
	} else {
		k := dist * 0.38
		l := min(float32(16), dist*0.12)
		nx, ny := dy/dist, -dx/dist
		c1x, c1y = ax+atx*k+nx*l, ay+aty*k+ny*l
		c2x, c2y = bx-btx*k+nx*l, by-bty*k+ny*l
	}
	return cubicControlPoints(ax, ay, c1x, c1y, c2x, c2y, bx, by, 24)
}

func cubicControlPoints(ax, ay, c1x, c1y, c2x, c2y, bx, by float32, steps int) []flightPoint {
	out := make([]flightPoint, 0, steps+1)
	for i := 0; i <= steps; i++ {
		t := float32(i) / float32(steps)
		u := 1 - t
		out = append(out, flightPoint{
			x: u*u*u*ax + 3*u*u*t*c1x + 3*u*t*t*c2x + t*t*t*bx,
			y: u*u*u*ay + 3*u*u*t*c1y + 3*u*t*t*c2y + t*t*t*by,
		})
	}
	return out
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

// hubToNarrowProviderPath is client.js's fromHub() lane in the max560
// layout: down from the hub's bottom, round into a lane 12px left of the
// account, then to the account's left edge.
func hubToNarrowProviderPath(hub nodeBox, agents []nodeBox, provider *nodeBox) flightPath {
	if provider == nil {
		return flightPath{}
	}
	hcx, hcxBottom := hub.x+hub.w/2, hub.y+hub.h
	agentBottom := hub.y + hub.h
	for _, a := range agents {
		agentBottom = max(agentBottom, a.y+a.h)
	}
	laneX := provider.x - 12
	laneTop := max(hcxBottom, agentBottom-8)
	px, py := provider.leftCenter()

	var raw []flightPoint
	raw = append(raw, flightPoint{x: hcx, y: hcxBottom})
	raw = append(raw, flightPoint{x: hcx, y: laneTop})
	raw = append(raw[1:], cubicFlightPoints(hcx, laneTop, laneX, laneTop+24)...)
	raw = append(raw[1:], flightPoint{x: laneX, y: py - 10})
	raw = append(raw[1:], quadraticFlightPoints(laneX, py-10, laneX, py, px, py, 8)...)
	return newFlightPath(raw)
}

func quadraticFlightPoints(ax, ay, cx, cy, bx, by float32, steps int) []flightPoint {
	if steps < 1 {
		return nil
	}
	out := make([]flightPoint, 0, steps+1)
	for i := 0; i <= steps; i++ {
		t := float32(i) / float32(steps)
		u := 1 - t
		out = append(out, flightPoint{
			x: u*u*ax + 2*u*t*cx + t*t*bx,
			y: u*u*ay + 2*u*t*cy + t*t*by,
		})
	}
	return out
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
	} else if f.inReplay && f.replayPlay {
		// Each replay build anchors firstSeen to the committed replay
		// cursor. Painter-only frames advance from that monotonic anchor,
		// so bird motion stays smooth without rebuilding the whole view.
		elapsed = max(now.Sub(f.firstSeen), 0)
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

// flightTraveling reports whether a track is on a titled wire whose bird and
// flowing dash change every painted frame. Holding at a provider leaves the
// carrier still, so the dynamic layer asks for no travel frames.
func flightTraveling(f flightTrack, now time.Time) bool {
	moment := flightMomentAt(f, now)
	if !moment.hasPhase || moment.finished {
		return false
	}
	switch moment.phase.kind {
	case flightOutbound, flightRetryOut, flightRetryBack, flightInbound:
		return true
	default:
		return false
	}
}

func currentFlightPhase(f flightTrack, now time.Time) flightMoment {
	return flightMomentAt(f, now)
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
func (m *Model) drawFlightActor(p *ui.Painter, geometry *flightGeometry, agentIndex int, f flightTrack, agentColor ui.Color, pal palette, now time.Time, reduceMotion bool) {
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
		// On arriving at an account the bird lets go of the dot and flies
		// up and away. moment.local is the time since that arrival, so a
		// long provider answer leaves only the waiting dot behind.
		release := moment.local
		if release < 450*time.Millisecond {
			endAim, endFlip := pathAim(path, path.length())
			pose := magpiePose{
				x: x, y: y, scale: 1, aim: endAim, flip: endFlip, flapAt: now,
				body: pal.BirdBody, belly: pal.BirdBelly, far: pal.BirdWing,
				appear:    1,
				away:      float32(release) / float32(450*time.Millisecond),
				flapPhase: float32(moment.local%420) / 420,
			}
			drawMagpie(p, pose, agentColor, magpieCarryNone, outcome, reduceMotion)
		}
		return
	case flightInbound:
		if moment.progress >= 1 {
			// fly() in client.js resolves at the end of the wire, then
			// away(back) keeps the bird for another 450ms. Preserve that
			// tail instead of making the return bird vanish on landing.
			span := moment.phase.end - moment.phase.start
			release := moment.local - span
			if release < 0 || release >= 450*time.Millisecond {
				return
			}
			x, y, _, _ := path.at(path.length())
			endAim, endFlip := pathAim(path, path.length())
			drawMagpie(p, magpiePose{
				x: x, y: y, scale: 1, aim: endAim, flip: endFlip, flapAt: now,
				body: pal.BirdBody, belly: pal.BirdBelly, far: pal.BirdWing,
				appear:    1,
				away:      float32(release) / float32(450*time.Millisecond),
				flapPhase: float32((moment.local - span) % (420 * time.Millisecond) / (420 * time.Millisecond)),
			}, agentColor, magpieCarryNone, outcome, reduceMotion)
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
		x: x, y: y, scale: 1, aim: aim, flip: flip, flapAt: now,
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
	x, y  float32
	scale float32
	// scale scales the FLIER's local 44-unit artwork. drawMagpie applies
	// the SVG's own 1.2 transform after it, so the path point stays the
	// packet position at every stage size and DPI.
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
	scale := pose.scale
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
	// by 1.2. Its packet sits at the beak, in front of the path point.
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
	away := easeAway(pose.away)
	// client.js's pose is transform="translate(...) scale(flip,1)
	// rotate(angle)". The parent .rt-lift carries rtcome and away:
	// translate(-6,-10) scale(.85) on entry, or translate(14,-16)
	// scale(.8) on exit. Scaling and rotation happen outside the lift;
	// translation happens in the bird's scaled coordinate space. Express
	// that exact order as one matrix so transforms that are perfect
	// identities are never dropped by a nested painter frame.
	liftScale := 0.85 + 0.15*appear
	liftX := -6 * (1 - appear)
	liftY := -10 * (1 - appear)
	liftScale *= 1 - 0.2*away
	liftX += 14 * away
	liftY += -16 * away
	p.Transform(ui.Translation(pose.x, pose.y+pose.bob), 0, 0, func() {
		p.Transform(ui.Scaling(scale*flip, scale), 0, 0, func() {
			p.Transform(ui.Rotation(aim), 0, 0, func() {
				p.Transform(ui.Scaling(liftScale, liftScale), 0, 0, func() {
					p.Transform(ui.Translation(liftX, liftY), 0, 0, func() {
						p.Transform(ui.Translation(-6.5, 3.2).Then(ui.Scaling(1.2, 1.2)), 0, 0, func() {
							drawMagpieShape(p, body, belly, wing, skew, farWing, farSkew, opacity)
						})
					})
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

func drawMagpieShape(p *ui.Painter, body, belly ui.Color, wing, skew, farWing, farSkew, opacity float32) {
	alpha := func(c ui.Color) ui.Color { return c.Alpha(opacity) }
	// rtflap writes scaleY(...) skewX(...). CSS composes the left function
	// first, so the matrix is S*K; Then(K).Then(S) applies the same order
	// through MyGo's painter stack. The far wing is currentColor at .45,
	// like .rt-flier .wing.far.
	p.Transform(ui.Skewing(farSkew, 0).Then(ui.Scaling(1, farWing)), -12.5, -3.2, func() {
		p.FillPath(&magpieFarWingPath, alpha(body).Alpha(0.45))
	})
	p.FillPath(&magpieTailPath, alpha(body))

	// Draw the upstream ellipse exactly as SVG does, using four cubic
	// segments.
	p.FillPath(&magpieBodyPath, alpha(body))
	p.FillPath(&magpieBellyPath, alpha(belly))
	p.FillPath(&magpieHeadPath, alpha(body))
	p.FillPath(&magpieBeakPath, alpha(body))

	p.Transform(ui.Skewing(skew, 0).Then(ui.Scaling(1, wing)), -13.1, -2.8, func() {
		p.FillPath(&magpieNearWingPath, alpha(body))
		p.StrokePath(&magpieWingBarPath, 1.3, alpha(belly))
	})
}

var (
	magpieFarWingPath  = buildMagpieFarWingPath()
	magpieTailPath     = buildMagpieTailPath()
	magpieBodyPath     = buildMagpieEllipsePath(-12.2, -1.4, 7.6, 3.9)
	magpieBellyPath    = buildMagpieEllipsePath(-12.8, 0.4, 4.3, 1.6)
	magpieHeadPath     = buildMagpieHeadPath()
	magpieBeakPath     = buildMagpieBeakPath()
	magpieNearWingPath = buildMagpieNearWingPath()
	magpieWingBarPath  = buildMagpieWingBarPath()
)

func buildMagpieFarWingPath() ui.Path {
	var pt ui.Path
	pt.MoveTo(-14.5, -3.4)
	pt.CubeTo(-16.5, -9.5, -14.2, -15, -8.6, -18.6)
	pt.CubeTo(-9.4, -12.6, -9.6, -7.4, -9.2, -3.2)
	pt.Close()
	return pt
}

func buildMagpieTailPath() ui.Path {
	var pt ui.Path
	pt.MoveTo(-31.5, 3.1)
	pt.LineTo(-18.6, -1.8)
	pt.LineTo(-17.4, 1.2)
	pt.LineTo(-30.8, 4.6)
	pt.Close()
	return pt
}

func buildMagpieEllipsePath(cx, cy, rx, ry float32) ui.Path {
	const k = 0.5522847498
	var pt ui.Path
	pt.MoveTo(cx+rx, cy)
	pt.CubeTo(cx+rx, cy+ry*k, cx+rx*k, cy+ry, cx, cy+ry)
	pt.CubeTo(cx-rx*k, cy+ry, cx-rx, cy+ry*k, cx-rx, cy)
	pt.CubeTo(cx-rx, cy-ry*k, cx-rx*k, cy-ry, cx, cy-ry)
	pt.CubeTo(cx+rx*k, cy-ry, cx+rx, cy-ry*k, cx+rx, cy)
	pt.Close()
	return pt
}

func buildMagpieHeadPath() ui.Path {
	var pt ui.Path
	pt.Circle(-4.9, -3.7, 3.1)
	return pt
}

func buildMagpieBeakPath() ui.Path {
	var pt ui.Path
	pt.MoveTo(-2.3, -4.8)
	pt.LineTo(1.4, -3.3)
	pt.LineTo(-2.3, -2.2)
	pt.Close()
	return pt
}

func buildMagpieNearWingPath() ui.Path {
	var pt ui.Path
	pt.MoveTo(-15.6, -3.2)
	pt.CubeTo(-17.8, -10.2, -15.2, -16.4, -8.4, -20.4)
	pt.CubeTo(-9.3, -13.6, -9.4, -7.8, -8.8, -2.8)
	pt.Close()
	return pt
}

func buildMagpieWingBarPath() ui.Path {
	var pt ui.Path
	pt.MoveTo(-14.6, -5.6)
	pt.CubeTo(-15.4, -10.4, -13.8, -14.4, -10.4, -17.2)
	return pt
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
	// rtheld: opacity 1 -> .45 -> 1 over 1.2s with ease-in-out. Sampling
	// the same continuous curve avoids the hard 600ms switch the earlier
	// threshold produced.
	phase := float32(now.UnixMilli()%1200) / 1200
	pulse := float32(1) - 0.55*cosineEase(phase*2)
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
	if flow, ok := flightDashPath(path, offset, dash, period); ok {
		p.StrokePath(&flow, width, color)
	}
}

// flightDashPath turns the arc-length polyline into one path of dashes in a
// single pass. The earlier implementation called flightPath.at twice per
// 2px sample for every dash, so a normal card-to-provider wire spent most of
// its animation frame in binary searches. Here each source segment is split
// only where the 3/5px pattern changes phase.
func flightDashPath(path flightPath, offset, dash, period float32) (ui.Path, bool) {
	var out ui.Path
	if len(path.pts) < 2 || period <= 0 || dash <= 0 {
		return out, false
	}
	length := path.length()
	for d := -offset; d < length; d += period {
		from := max(d, 0)
		to := min(d+dash, length)
		if appendFlightArcFast(&out, path, from, to) {
			continue
		}
	}
	return out, out.Bounds().W > 0 || out.Bounds().H > 0
}

// appendFlightArcFast samples an arc directly from the source polyline. It
// avoids two binary searches per sample while preserving the same end
// positions and the same 2px step as appendFlightArc.
func appendFlightArcFast(out *ui.Path, path flightPath, from, to float32) bool {
	if to-from < 0.1 || len(path.pts) < 2 {
		return false
	}
	at := func(distance float32) (float32, float32) {
		distance = max(0, min(distance, path.length()))
		i := sort.Search(len(path.cum), func(i int) bool { return path.cum[i] >= distance })
		if i == 0 {
			i = 1
		}
		if i >= len(path.pts) {
			i = len(path.pts) - 1
		}
		prev, next := path.pts[i-1], path.pts[i]
		seg := max(path.cum[i]-path.cum[i-1], 0.001)
		t := max(0, min((distance-path.cum[i-1])/seg, 1))
		return prev.x + (next.x-prev.x)*t, prev.y + (next.y-prev.y)*t
	}
	x, y := at(from)
	out.MoveTo(x, y)
	const step = 2.0
	for d := from; d < to; d += step {
		px, py := at(d)
		out.LineTo(px, py)
	}
	x, y = at(to)
	out.LineTo(x, y)
	return true
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
	routes   []routing.RouteView
	playing  bool
	start    time.Time
	position time.Duration
	speed    float64
	active   bool
}

// replayTopologyState returns the completed routes whose presentation flight
// covers the replay cursor. Flight windows are derived from the exact trace
// phases, so a seek into the return leg reconstructs it without relying on a
// previous playback pass.
func (m *Model) replayTopologyState() (replayTopology, bool) {
	return m.replayTopologyStateAt(time.Now())
}

func (m *Model) replayTopologyStateAt(now time.Time) (replayTopology, bool) {
	m.mu.RLock()
	tab := m.tab
	replay := m.replay
	position := m.replayPositionAtLocked(now)
	snapshot := m.lastSnapshot
	m.mu.RUnlock()
	if tab != tabHistory || !replay.loaded || replay.timeline.Duration <= 0 {
		return replayTopology{}, false
	}
	byID := m.routeViewIndex(snapshot)
	state := replayTopology{
		playing: replay.playing, start: replay.timeline.Start,
		position: position, speed: replay.speed, active: true,
	}
	for _, window := range activeReplayWindows(replay.windows, replay.windowEnds, position) {
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
	if account != "" && row.AccountID != "" && !strings.EqualFold(row.AccountID, account) {
		// A known account id is authoritative. Matching provider and model
		// must never redirect a flight to a different account.
		return 0
	}
	accountMatch := account != "" && row.AccountID != "" && strings.EqualFold(row.AccountID, account)
	if accountMatch {
		score += 100
	}
	if model != "" && row.Model != "" && modelMatches(row.Model, model, firstNonEmpty(rowProvider, provider)) {
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
	dx := (b[0] - a[0]) * 0.5
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
	dx := (b[0] - a[0]) * 0.5
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
