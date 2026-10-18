package routing

import (
	"sort"
	"strings"
	"time"
)

// RouteFilter selects route presentation rows without changing the observed
// data. Agent and Model compare case-insensitively; Query also searches the
// session, provider, group and complete candidate chain.
type RouteFilter struct {
	Query      string
	Agent      string
	Status     string
	ActiveOnly bool
	HistoryDay string
}

// Apply returns matching routes in the snapshot's newest-first order.
func (f RouteFilter) Apply(routes []RouteView) []RouteView {
	q := strings.ToLower(strings.TrimSpace(f.Query))
	agent := strings.ToLower(strings.TrimSpace(f.Agent))
	day := strings.TrimSpace(f.HistoryDay)
	out := make([]RouteView, 0, len(routes))
	for _, r := range routes {
		if f.ActiveOnly && !r.Active {
			continue
		}
		if day != "" && r.HistoryDay != day {
			continue
		}
		if agent != "" && !strings.EqualFold(r.Agent, agent) {
			continue
		}
		status := f.Status
		if status == "" {
			status = "all"
		}
		switch status {
		case "active":
			if !r.Active {
				continue
			}
		case "completed":
			if r.Active || !(r.Done || r.Completeness.History) {
				continue
			}
		case "failed":
			if r.Status < 400 && r.Error == "" {
				continue
			}
		}
		if q != "" && !routeContains(r, q) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func routeContains(r RouteView, q string) bool {
	if containsAny(q, r.Agent, r.Session, r.Model, r.Provider, r.Error) {
		return true
	}
	if r.Group != nil && containsAny(q, r.Group.ID, r.Group.Name) {
		return true
	}
	for _, w := range r.Order {
		if containsAny(q, w.ID, w.Provider, w.Name, w.Who, w.Model, w.Plan) {
			return true
		}
	}
	for _, t := range r.Tries {
		if containsAny(q, t.ID, t.Model, t.Fail, t.Error, t.Served) {
			return true
		}
	}
	return false
}

func containsAny(q string, values ...string) bool {
	for _, v := range values {
		if v != "" && strings.Contains(strings.ToLower(v), q) {
			return true
		}
	}
	return false
}

// FilterValue returns a stable value for a filter control.
func FilterValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// SelectRoute keeps a pinned route when it still exists; otherwise it picks
// the first visible route, then the newest route in the snapshot.
func SelectRoute(routes []RouteView, pinnedID int64) (RouteView, bool) {
	if pinnedID != 0 {
		for _, r := range routes {
			if r.ID == pinnedID {
				return r, true
			}
		}
	}
	if len(routes) > 0 {
		return routes[0], true
	}
	return RouteView{}, false
}

// CompleteRoutes returns only routes backed by completed Magpie history, in
// the order given.
func CompleteRoutes(routes []RouteView) []Route {
	out := make([]Route, 0, len(routes))
	for _, r := range routes {
		if r.Completeness.History && (r.Done || !r.Time.IsZero()) {
			out = append(out, r.Route)
		}
	}
	return out
}

// BuildDayReplay builds the true timeline for one history day. Routes not
// backed by that day's file are ignored, so a replay never includes live or
// another day's observations.
func BuildDayReplay(routes []RouteView, day string) ReplayTimeline {
	var rs []Route
	for _, r := range routes {
		if r.HistoryDay == day && r.Completeness.History {
			rs = append(rs, r.Route)
		}
	}
	return BuildReplay(rs)
}

// ReplayVisibleRoutes reports the routes that have begun at position, and
// the routes in flight there. Only Try.Start and Route.Time determine starts;
// the second value is inferred only from recorded durations and is labeled so
// in the UI.
func ReplayVisibleRoutes(tl ReplayTimeline, position time.Duration) (started []Route, inFlight []Route) {
	if position < 0 {
		position = 0
	}
	for _, r := range tl.Routes {
		if r.Time.IsZero() {
			continue
		}
		start := r.Time.Sub(tl.Start)
		if start > position {
			continue
		}
		started = append(started, r)
		elapsed := position - start
		if elapsed < time.Duration(max(r.Millis, 0))*time.Millisecond {
			inFlight = append(inFlight, r)
		}
	}
	return started, inFlight
}

// ReplayProgress returns the 0..1 position of a replay timeline.
func ReplayProgress(tl ReplayTimeline, position time.Duration) float64 {
	if tl.Duration <= 0 {
		return 1
	}
	return max(0, min(float64(position)/float64(tl.Duration), 1))
}

// ReplaySpeed cycles through the speeds the native player exposes.
func ReplaySpeed(current float64) float64 {
	speeds := []float64{0.5, 1, 2, 4, 8}
	for i, s := range speeds {
		if current < s {
			return s
		}
		if current == s {
			if i+1 < len(speeds) {
				return speeds[i+1]
			}
			return speeds[0]
		}
	}
	return 1
}

// GroupViews returns configured groups in stable display order, then groups
// observed only in route history. It is presentation-only: no dynamic state
// is invented for a configured group.
func GroupViews(cfg ConfigSnapshot, routes []RouteView) []GroupView {
	out := make([]GroupView, 0, len(cfg.Groups))
	seen := map[string]int{}
	for _, g := range cfg.Groups {
		key := strings.ToLower(g.ID)
		if key == "" {
			continue
		}
		seen[key] = len(out)
		out = append(out, GroupView{
			ID: g.ID, Name: GroupName(g), Members: append([]string(nil), g.Members...),
			Matched: append([]string(nil), g.Matched...),
			Match:   append([]string(nil), g.Match...), Routing: g.Routing,
			Affinity: g.Affinity, Auto: g.Auto, Sink: g.Sink, Disabled: g.Disabled,
			Pick: g.Pick, Off: append([]string(nil), g.Off...),
			Fast: append([]string(nil), g.Fast...), Levels: append([]string(nil), g.Levels...),
			Family: g.Family, Hidden: g.Hidden, Rules: g.Rules, FirstToken: g.FirstToken,
			Patience: g.Patience, Classifier: g.Classifier, Effort: g.Effort,
			Context: g.Context, Configured: true,
		})
	}
	for _, r := range routes {
		if r.Group == nil || r.Group.ID == "" {
			continue
		}
		key := strings.ToLower(r.Group.ID)
		if i, ok := seen[key]; ok {
			g := &out[i]
			g.Observed++
			if len(g.Members) == 0 {
				g.Members = append([]string(nil), r.Group.Members...)
			}
			if g.Routing == "" {
				g.Routing = r.Group.Routing
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, GroupView{
			ID: r.Group.ID, Name: FilterValue(r.Group.Name, r.Group.ID),
			Members: append([]string(nil), r.Group.Members...), Routing: r.Group.Routing,
			Affinity: r.Group.Affinity, Auto: r.Group.Auto, Sink: r.Group.Sink,
			Observed: 1, ObservedOnly: true,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Auto != out[j].Auto {
			return !out[i].Auto
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// GroupView is a configured or observed group, kept separate from the
// compact JSON shapes so the UI never needs to write back.
type GroupView struct {
	ID           string
	Name         string
	Members      []string
	Matched      []string
	Match        []string
	Routing      string
	Affinity     string
	Auto         bool
	Sink         bool
	Pick         string
	Off          []string
	Fast         []string
	Levels       []string
	Family       string
	Disabled     bool
	Hidden       bool
	Rules        int
	FirstToken   int
	Patience     int
	Classifier   string
	Effort       string
	Context      int
	Observed     int
	Configured   bool
	ObservedOnly bool
}

// ProviderViews returns provider facts read only from providers.json. The
// count of accounts remains unknown because the file's secret account shapes
// are deliberately not decoded.
func ProviderViews(cfg ConfigSnapshot) []ProviderInfo {
	out := append([]ProviderInfo(nil), cfg.Providers...)
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(FilterValue(out[i].Name, out[i].ID)) <
			strings.ToLower(FilterValue(out[j].Name, out[j].ID))
	})
	return out
}

// PhaseLabel is a stable translation key for a live concurrency phase.
func PhaseLabel(p Phase) string {
	switch p {
	case PhaseSlot:
		return "phase.slot"
	case PhaseRPM:
		return "phase.rpm"
	case PhaseUpstream:
		return "phase.upstream"
	default:
		return "phase.observed"
	}
}

// TryFailure is the truthful failure text: Magpie's reason first, then its
// error, then status. Missing reasons are not replaced by guesses.
func TryFailure(t Try) string {
	if t.Fail != "" {
		return t.Fail
	}
	if t.Error != "" {
		return t.Error
	}
	if t.Status >= 400 {
		return httpStatus(t.Status)
	}
	return ""
}

func httpStatus(code int) string {
	switch code {
	case 400:
		return "HTTP 400 Bad Request"
	case 401:
		return "HTTP 401 Unauthorized"
	case 403:
		return "HTTP 403 Forbidden"
	case 404:
		return "HTTP 404 Not Found"
	case 408:
		return "HTTP 408 Request Timeout"
	case 409:
		return "HTTP 409 Conflict"
	case 429:
		return "HTTP 429 Too Many Requests"
	case 500:
		return "HTTP 500 Internal Server Error"
	case 502:
		return "HTTP 502 Bad Gateway"
	case 503:
		return "HTTP 503 Service Unavailable"
	case 504:
		return "HTTP 504 Gateway Timeout"
	default:
		return "HTTP " + itoa(code)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
