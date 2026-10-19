package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Fan-chou/magpie-native-routing/internal/routing"
)

// syncReplay rebuilds a history timeline only when the selected day's
// completed trace set changes. Playback itself advances from the UI clock in
// replayState, so this frame callback never performs I/O or mutates workers.
func (m *Model) syncReplay(s routing.Snapshot, now time.Time) {
	_ = now
	m.mu.RLock()
	day := m.historyDay
	tab := m.tab
	cachedKey := m.replay.builtKey
	speed := m.replay.speed
	m.mu.RUnlock()
	if tab != tabHistory || day == "" {
		return
	}

	filter := m.filters()
	key := replayFilterKey(s.Version, day, filter, speed)
	m.mu.Lock()
	if key != cachedKey {
		// Only filter and fingerprint the day when the identity of the
		// source can change. The previous implementation did this in every
		// View call; during playback that walked and serialized every
		// completed trace five times a second.
		_, routes := replaySource(s, day, filter)
		next := routing.BuildDayReplay(routes, day)
		next.Duration = max(next.Duration, replayPresentationDuration(next, routes, speed))
		m.replay.baseWindows = buildReplayFlightWindows(next, routes, speed)
		hadTimeline := m.replay.loaded && m.replay.builtKey != ""
		oldPosition := m.replay.position
		m.replay.timeline = next
		m.replay.applySeek()
		duration := m.replay.timeline.Duration
		if m.replay.playing && m.replay.position > duration {
			m.replay.position = 0
		}
		m.replay.builtKey = key
		m.replay.loaded = true
		m.replay.day = day
		if m.replay.position > duration {
			m.replay.position = 0
		}
		if m.replay.pendingID != 0 {
			offset := replayOffset(next, m.replay.pendingID)
			m.replay.position = offset
			m.replay.playing = false
			m.replay.lastTick = time.Time{}
			m.replay.pendingID = 0
			m.replay.seekAt = offset
			m.replay.applySeek()
		} else if !hadTimeline {
			m.replay.position = 0
		} else if oldPosition < duration {
			m.replay.position = oldPosition
		}
		if m.replay.position < m.replay.seekAt {
			m.replay.seekAt = 0
			m.replay.applySeek()
		}
	}
	m.mu.Unlock()
}

// replayFilterKey identifies the replay input without walking the routes.
// Version changes whenever the Store commits real data; day and filter are
// copied from the model's current controls. The version is part of the key so
// no caller has to remember to invalidate a separate cache on every state
// transition. The speed is in it too: a flight's legs last wall time, so how
// much replay time a bird spans depends on how fast the replay runs.
func replayFilterKey(version uint64, day string, f routing.RouteFilter, speed float64) string {
	return fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%t\x00%g", version, day, f.Query, f.Status, f.ActiveOnly, speed)
}

// buildReplayFlightWindows records the exact presentation interval of each
// route's animated flight. The recorded request may have ended while the
// answer is still flying home, so the window extends through the inbound
// phase. Seeking into that interval can therefore rebuild the return bird
// without having played the route before. Legs are wall time at any speed,
// so a window spans the flight's display time times the speed.
func buildReplayFlightWindows(tl routing.ReplayTimeline, routes []routing.RouteView, speed float64) []replayFlightWindow {
	if speed <= 0 {
		speed = 1
	}
	windows := make([]replayFlightWindow, 0, len(routes))
	for _, rv := range routes {
		if rv.ID == 0 || rv.Time.IsZero() || !hasFlightAttempt(rv) {
			continue
		}
		_, phases, display := replayFlightPlan(rv, speed, 0)
		if len(phases) == 0 {
			continue
		}
		start := rv.Time.Sub(tl.Start)
		if start < 0 {
			start = 0
		}
		_, recorded := traceFlightAttempts(rv)
		windows = append(windows, replayFlightWindow{
			routeID: rv.ID,
			start:   start,
			end:     start + time.Duration(float64(display)*speed),
			recEnd:  start + recorded,
			display: display,
		})
	}
	sort.SliceStable(windows, func(i, j int) bool {
		if windows[i].start == windows[j].start {
			return windows[i].routeID < windows[j].routeID
		}
		return windows[i].start < windows[j].start
	})
	return windows
}

// applySeek derives the flight windows after a seek to seekAt. A route in
// flight there flies again from the agent, starting at seekAt; one already
// over does not fly; later routes keep their recorded start. The timeline
// runs on until the last restarted bird has landed.
func (r *replayController) applySeek() {
	speed := r.speed
	if speed <= 0 {
		speed = 1
	}
	out := make([]replayFlightWindow, 0, len(r.baseWindows))
	for _, w := range r.baseWindows {
		switch {
		case w.start >= r.seekAt:
			out = append(out, w)
		case w.recEnd > r.seekAt:
			w.shift = r.seekAt - w.start
			w.start = r.seekAt
			w.end = r.seekAt + time.Duration(float64(w.display)*speed)
			out = append(out, w)
			r.timeline.Duration = max(r.timeline.Duration, w.end)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].start == out[j].start {
			return out[i].routeID < out[j].routeID
		}
		return out[i].start < out[j].start
	})
	r.windows = out
	r.windowEnds = replayWindowEnds(out)
}

func replayWindowEnds(windows []replayFlightWindow) []time.Duration {
	ends := make([]time.Duration, len(windows))
	var maxEnd time.Duration
	for i, w := range windows {
		maxEnd = max(maxEnd, w.end)
		ends[i] = maxEnd
	}
	return ends
}

// activeReplayWindows returns every flight whose route-time window contains
// position. The prefix maximum lets a backward scan stop as soon as no
// earlier window can still be alive.
func activeReplayWindows(windows []replayFlightWindow, prefixEnd []time.Duration, position time.Duration) []replayFlightWindow {
	if len(windows) == 0 || len(prefixEnd) != len(windows) {
		return nil
	}
	firstFuture := sort.Search(len(windows), func(i int) bool {
		return windows[i].start > position
	})
	var out []replayFlightWindow
	for i := firstFuture - 1; i >= 0; i-- {
		if prefixEnd[i] < position {
			break
		}
		if position >= windows[i].start && position <= windows[i].end {
			out = append(out, windows[i])
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// replayPresentationDuration keeps the replay clock running until every
// bird that the last request launched has landed. It extends presentation
// time only; recorded route durations and the replay provenance label do not
// change.
func replayPresentationDuration(tl routing.ReplayTimeline, routes []routing.RouteView, speed float64) time.Duration {
	end := tl.Duration
	if tl.Start.IsZero() {
		return end
	}
	if speed <= 0 {
		speed = 1
	}
	for _, rv := range routes {
		if rv.ID == 0 || rv.Time.IsZero() || !hasFlightAttempt(rv) {
			continue
		}
		_, _, display := replayFlightPlan(rv, speed, 0)
		at := rv.Time.Sub(tl.Start)
		if at < 0 {
			at = 0
		}
		end = max(end, at+time.Duration(float64(display)*speed))
	}
	return end
}

// replayNextFlightIn is how long until the replay reaches the next route's
// start. The view is built again then, so that route's bird is made with its
// leg just begun and leaves from the agent, not part way along a wire.
func (m *Model) replayNextFlightIn(now time.Time) (time.Duration, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.replay.playing || m.replay.speed <= 0 {
		return 0, false
	}
	position := m.replayPositionAtLocked(now)
	windows := m.replay.windows
	i := sort.Search(len(windows), func(i int) bool { return windows[i].start > position })
	if i == len(windows) {
		return 0, false
	}
	return time.Duration(float64(windows[i].start-position) / m.replay.speed), true
}

func hasFlightAttempt(rv routing.RouteView) bool {
	if len(rv.Tries) > 0 {
		return true
	}
	return rv.Active && !rv.Time.IsZero()
}

// replayOffset returns the exact elapsed time at which a Route ID begins.
// It returns zero only when that route is not in this completed day.
func replayOffset(tl routing.ReplayTimeline, id int64) time.Duration {
	for _, r := range tl.Routes {
		if r.ID != id || r.Time.IsZero() || tl.Start.IsZero() {
			continue
		}
		offset := r.Time.Sub(tl.Start)
		if offset < 0 {
			return 0
		}
		if tl.Duration > 0 && offset > tl.Duration {
			return tl.Duration
		}
		return offset
	}
	return 0
}

func replaySource(s routing.Snapshot, day string, f routing.RouteFilter) (string, []routing.RouteView) {
	f.HistoryDay = day
	routes := make([]routing.RouteView, 0)
	var key strings.Builder
	for _, r := range f.Apply(s.Routes) {
		routes = append(routes, r)
		appendReplayTraceKey(&key, r)
	}
	return key.String(), routes
}

// appendReplayTraceKey fingerprints every field that can change a replay
// flight phase or window. Try count alone is insufficient: a late history
// append can fill in an existing try's start, target, duration or failure
// while the route's count and status stay unchanged.
func appendReplayTraceKey(b *strings.Builder, r routing.RouteView) {
	fmt.Fprintf(b, "route:%d/%t/%t/%d/%d/%s/%d/%s/%s/%s/%s/%s;",
		r.ID, r.Done, r.Active, r.Status, len(r.Order), r.Time.UTC().Format(time.RFC3339Nano),
		r.Millis, r.HistoryDay, r.Agent, r.Model, r.Provider, r.Error)
	for i, w := range r.Order {
		fmt.Fprintf(b, "o%d:%s/%s/%s/%s/%s;",
			i, w.ID, w.Provider, w.Name, w.Who, w.Model)
	}
	for i, tr := range r.Tries {
		fmt.Fprintf(b, "t%d:%s/%s/%s/%t/%d/%d/%d/%d/%s/%s/%s/%s/%t;",
			i, tr.ID, tr.Model, tr.Served, tr.Done, tr.Status, tr.Millis,
			tr.TTFT, tr.Again, tr.Start.UTC().Format(time.RFC3339Nano),
			tr.Fail, tr.Error, tr.Fixed, tr.Gone)
	}
}

func (m *Model) replayState(now time.Time) (routing.ReplayTimeline, bool, float64, time.Duration, routing.ReplayState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.replay.playing {
		if m.replay.lastTick.IsZero() {
			m.replay.lastTick = now
		} else {
			d := now.Sub(m.replay.lastTick)
			if d > 0 {
				m.replay.position += time.Duration(float64(d) * m.replay.speed)
			}
		}
		m.replay.lastTick = now
		if m.replay.timeline.Duration <= 0 || m.replay.position >= m.replay.timeline.Duration {
			m.replay.position = m.replay.timeline.Duration
			m.replay.playing = false
		}
	}
	state := routing.ReplayState{
		Day: m.replay.day, Playing: m.replay.playing, Speed: m.replay.speed,
		Position: m.replay.position, Duration: m.replay.timeline.Duration,
		RouteCount: len(m.replay.timeline.Routes),
	}
	return m.replay.timeline, m.replay.playing, m.replay.speed, m.replay.position, state
}

func (m *Model) replayPositionAtLocked(now time.Time) time.Duration {
	position := m.replay.position
	if !m.replay.playing || m.replay.lastTick.IsZero() {
		return position
	}
	if delta := now.Sub(m.replay.lastTick); delta > 0 {
		position += time.Duration(float64(delta) * m.replay.speed)
	}
	if m.replay.timeline.Duration <= 0 || position >= m.replay.timeline.Duration {
		return m.replay.timeline.Duration
	}
	if position < 0 {
		return 0
	}
	return position
}

func (m *Model) toggleReplay(now time.Time) {
	m.mu.Lock()
	switch {
	case m.replay.timeline.Duration <= 0:
		m.replay.position = 0
	case m.replay.playing:
		m.replay.playing = false
		m.replay.lastTick = time.Time{}
	case m.replay.position >= m.replay.timeline.Duration:
		m.replay.position = 0
		m.replay.playing = true
		m.replay.lastTick = now
		m.replay.seekAt = 0
		m.replay.applySeek()
	default:
		m.replay.playing = true
		m.replay.lastTick = now
	}
	m.mu.Unlock()
}

func (m *Model) restartReplay(now time.Time) {
	m.mu.Lock()
	m.replay.position = 0
	m.replay.playing = m.replay.timeline.Duration > 0
	m.replay.lastTick = now
	m.replay.seekAt = 0
	m.replay.applySeek()
	m.mu.Unlock()
}

func (m *Model) seekReplay(seconds float64) {
	m.mu.Lock()
	if m.replay.timeline.Duration > 0 {
		m.replay.position = time.Duration(seconds * float64(time.Second))
		if m.replay.position > m.replay.timeline.Duration {
			m.replay.position = m.replay.timeline.Duration
		}
		if m.replay.position < 0 {
			m.replay.position = 0
		}
		// As client.js's seek(): the flights end, and the requests in
		// flight here fly again from where they are in their story.
		m.replay.seekAt = m.replay.position
		m.replay.applySeek()
		if m.replay.playing {
			m.replay.lastTick = time.Time{}
		}
	}
	m.mu.Unlock()
}

func (m *Model) cycleSpeed() {
	m.mu.Lock()
	m.replay.speed = routing.ReplaySpeed(m.replay.speed)
	m.mu.Unlock()
}
