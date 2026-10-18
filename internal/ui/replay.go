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
	m.mu.RUnlock()
	if tab != tabHistory || day == "" {
		return
	}

	key, routes := replaySource(s, day, m.filters())
	m.mu.Lock()
	if m.replay.builtKey != key {
		next := routing.BuildDayReplay(routes, day)
		next.Duration = max(next.Duration, replayPresentationDuration(next, routes))
		m.replay.windows = buildReplayFlightWindows(next, routes)
		m.replay.windowEnds = replayWindowEnds(m.replay.windows)
		hadTimeline := m.replay.loaded && m.replay.builtKey != ""
		oldPosition := m.replay.position
		m.replay.timeline = next
		if m.replay.playing && m.replay.position > next.Duration {
			m.replay.position = 0
		}
		m.replay.builtKey = key
		m.replay.loaded = true
		m.replay.day = day
		if m.replay.position > m.replay.timeline.Duration {
			m.replay.position = 0
		}
		if m.replay.pendingID != 0 {
			offset := replayOffset(next, m.replay.pendingID)
			m.replay.position = offset
			m.replay.playing = false
			m.replay.lastTick = time.Time{}
			m.replay.pendingID = 0
		} else if !hadTimeline {
			m.replay.position = 0
		} else if oldPosition < next.Duration {
			m.replay.position = oldPosition
		}
	}
	m.mu.Unlock()
}

// buildReplayFlightWindows records the exact presentation interval of each
// route's animated flight. The recorded request may have ended while the
// answer is still flying home, so the window extends through the inbound
// phase. Seeking into that interval can therefore rebuild the return bird
// without having played the route before.
func buildReplayFlightWindows(tl routing.ReplayTimeline, routes []routing.RouteView) []replayFlightWindow {
	windows := make([]replayFlightWindow, 0, len(routes))
	for _, rv := range routes {
		if rv.ID == 0 || rv.Time.IsZero() || !hasFlightAttempt(rv) {
			continue
		}
		attempts, total := traceFlightAttempts(rv)
		phases := buildFlightPhases(attempts, total, true)
		if len(phases) == 0 {
			continue
		}
		start := rv.Time.Sub(tl.Start)
		if start < 0 {
			start = 0
		}
		windows = append(windows, replayFlightWindow{
			routeID: rv.ID,
			start:   start,
			end:     start + flightPhaseEnd(phases),
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
func replayPresentationDuration(tl routing.ReplayTimeline, routes []routing.RouteView) time.Duration {
	end := tl.Duration
	if tl.Start.IsZero() {
		return end
	}
	for _, rv := range routes {
		if rv.ID == 0 || rv.Time.IsZero() || !hasFlightAttempt(rv) {
			continue
		}
		attempts, total := traceFlightAttempts(rv)
		phases := buildFlightPhases(attempts, total, true)
		at := rv.Time.Sub(tl.Start)
		if at < 0 {
			at = 0
		}
		end = max(end, at+flightPhaseEnd(phases))
	}
	return end
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
		fmt.Fprintf(&key, "%d/%d/%t/%d/%d/%s/%d/%s;", r.ID, len(r.Tries), r.Done,
			r.Status, len(r.Order), r.Time.UTC().Format(time.RFC3339Nano), r.Millis, r.HistoryDay)
	}
	return key.String(), routes
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
	}
	m.mu.Unlock()
}

func (m *Model) cycleSpeed() {
	m.mu.Lock()
	m.replay.speed = routing.ReplaySpeed(m.replay.speed)
	m.mu.Unlock()
}
