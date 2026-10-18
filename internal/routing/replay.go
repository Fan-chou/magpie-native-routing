package routing

import (
	"sort"
	"time"
)

// ReplayState is UI state for a completed-history replay. It is deliberately
// separate from live observation.
type ReplayState struct {
	Day        string
	Playing    bool
	Speed      float64
	Position   time.Duration
	Duration   time.Duration
	RouteCount int
	UpdatedAt  time.Time
}

// ReplayEvent is one real trace point on a route's replay timeline.
type ReplayEvent struct {
	RouteID   int64
	At        time.Duration
	Kind      string
	TryIndex  int
	Label     string
	Completed bool
	Failed    bool
}

// ReplayTimeline holds events ordered by their actual relative timestamps.
type ReplayTimeline struct {
	Start    time.Time
	Duration time.Duration
	Events   []ReplayEvent
	Routes   []Route
}

// BuildReplay creates a timeline from completed routes. Relative time comes
// from Route.Time, Try.Start and durations recorded by Magpie. No events are
// invented for unobserved requests.
func BuildReplay(routes []Route) ReplayTimeline {
	rs := append([]Route(nil), routes...)
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].Time.Equal(rs[j].Time) {
			return rs[i].ID < rs[j].ID
		}
		return rs[i].Time.Before(rs[j].Time)
	})
	if len(rs) == 0 {
		return ReplayTimeline{}
	}
	start := rs[0].Time
	for _, r := range rs {
		if !r.Time.IsZero() && r.Time.Before(start) {
			start = r.Time
		}
	}
	var tl ReplayTimeline
	tl.Start = start
	tl.Routes = append(tl.Routes, rs...)
	for _, r := range rs {
		if r.Time.IsZero() {
			continue
		}
		base := r.Time.Sub(start)
		tl.Events = append(tl.Events, ReplayEvent{
			RouteID: r.ID, At: base, Kind: "accepted",
			Label: r.Agent + " → " + r.Model,
		})
		for i, t := range r.Tries {
			at := base
			if !t.Start.IsZero() {
				at = t.Start.Sub(start)
				if at < 0 {
					at = base
				}
			}
			kind := "try"
			if t.Done {
				kind = "try-done"
			}
			tl.Events = append(tl.Events, ReplayEvent{
				RouteID: r.ID, At: at, Kind: kind, TryIndex: i,
				Label: t.Model, Failed: t.Status >= 400 || t.Fail != "" || t.Error != "",
			})
		}
		end := base + time.Duration(max(r.Millis, 0))*time.Millisecond
		if r.Time.IsZero() && !r.Done {
			continue
		}
		tl.Events = append(tl.Events, ReplayEvent{
			RouteID: r.ID, At: end, Kind: "complete", Completed: true,
			Failed: r.Status >= 400 || r.Error != "", Label: r.StatusText(),
		})
		if end > tl.Duration {
			tl.Duration = end
		}
	}
	sort.SliceStable(tl.Events, func(i, j int) bool {
		if tl.Events[i].At == tl.Events[j].At {
			if tl.Events[i].RouteID == tl.Events[j].RouteID {
				return tl.Events[i].Kind < tl.Events[j].Kind
			}
			return tl.Events[i].RouteID < tl.Events[j].RouteID
		}
		return tl.Events[i].At < tl.Events[j].At
	})
	return tl
}

// StatusText is a compact truthful status label.
func (r Route) StatusText() string {
	switch {
	case !r.Done && !r.HasSource(SourceHistory):
		return "active"
	case r.Status >= 400 || r.Error != "":
		return "failed"
	case r.Done:
		return "completed"
	default:
		return "unknown"
	}
}

// DisplayStatus is truthful for both live-only and completed routes.
func (v RouteView) DisplayStatus() string {
	if v.Active {
		return "active"
	}
	if v.Status >= 400 || v.Error != "" {
		return "failed"
	}
	if v.Done || v.Completeness.History {
		return "completed"
	}
	return "observed"
}
