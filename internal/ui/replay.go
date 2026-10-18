package ui

import (
	"fmt"
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
		if rv.ID == 0 || rv.Time.IsZero() {
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
