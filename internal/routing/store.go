package routing

import (
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Observer is a UI-safe snapshot. All slices are copies and can be read
// without holding the store lock.
type Snapshot struct {
	Version           uint64
	Routes            []RouteView
	Live              []LiveRequest
	Orphans           []LiveRequest
	Config            ConfigSnapshot
	GatewayOnline     bool
	GatewayError      string
	GatewayWarning    string
	HistoryError      string
	ConfigError       string
	LastPoll          time.Time
	LastHistoryRead   time.Time
	LastConfigRead    time.Time
	Days              []HistoryDay
	LoadedHistoryDays []string
	Replay            ReplayState
}

type HistoryDay struct {
	Day      string
	Requests int
	Size     int64
	Modified time.Time
}

// Store merges Gateway observations and completed history by stable Route ID.
// It is safe for one writer goroutine and many UI readers.
type Store struct {
	mu      sync.RWMutex
	routes  map[int64]*Route
	live    map[int64]LiveRequest
	byRoute map[int64]map[int64]LiveRequest // route ID -> active counters
	orphans map[int64]LiveRequest
	config  ConfigSnapshot
	snap    Snapshot
	history map[int64]time.Time

	notifyMu   sync.Mutex
	notify     []func()
	version    uint64
	historyErr string
}

func NewStore() *Store {
	return &Store{
		routes:  map[int64]*Route{},
		live:    map[int64]LiveRequest{},
		byRoute: map[int64]map[int64]LiveRequest{},
		orphans: map[int64]LiveRequest{},
		history: map[int64]time.Time{},
	}
}

// OnChange registers a callback for any committed snapshot change. Callbacks
// run after the store lock is released and must stay short, usually just
// invalidating a native window on the main thread.
func (s *Store) OnChange(fn func()) {
	if fn == nil {
		return
	}
	s.notifyMu.Lock()
	s.notify = append(s.notify, fn)
	s.notifyMu.Unlock()
}

// Version increments once per committed change.
func (s *Store) Version() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

func (s *Store) changed() {
	s.version++
	s.notifyMu.Lock()
	callbacks := append([]func(){}, s.notify...)
	s.notifyMu.Unlock()
	for _, fn := range callbacks {
		fn()
	}
}

// ApplyConcurrency replaces the live snapshot and updates route records by
// RouteID only. A live request without a RouteID remains an orphan; it is
// never associated with a history request by model or timing heuristics.
func (s *Store) ApplyConcurrency(c ConcurrencyLike) {
	s.mu.Lock()
	now := time.Now()
	s.live = map[int64]LiveRequest{}
	s.byRoute = map[int64]map[int64]LiveRequest{}
	s.orphans = map[int64]LiveRequest{}
	for _, a := range c.Active {
		lr := LiveRequest{
			ID: a.ID, Agent: a.Agent, Model: a.Model, Provider: a.Provider,
			Session: a.Session, Started: a.Started, Millis: a.Millis,
			Phase: Phase(a.Phase), RouteID: a.RouteID, ObservedAt: now,
		}
		s.live[a.ID] = lr
		if a.RouteID > 0 {
			m := s.byRoute[a.RouteID]
			if m == nil {
				m = map[int64]LiveRequest{}
				s.byRoute[a.RouteID] = m
			}
			m[a.ID] = lr
			r := s.routes[a.RouteID]
			if r == nil {
				r = &Route{ID: a.RouteID, Agent: a.Agent, Session: a.Session, Model: a.Model,
					Provider: a.Provider, Time: a.Started}
				s.routes[a.RouteID] = r
			}
			r.AddSource(SourceLiveAPI)
			if r.Agent == "" {
				r.Agent = a.Agent
			}
			if r.Model == "" {
				r.Model = a.Model
			}
			if r.Session == "" {
				r.Session = a.Session
			}
			if r.Provider == "" {
				r.Provider = a.Provider
			}
			if r.Time.IsZero() {
				r.Time = a.Started
			}
		} else {
			s.orphans[a.ID] = lr
		}
	}
	s.snap.GatewayOnline = true
	s.snap.GatewayError = ""
	s.snap.LastPoll = now
	s.mu.Unlock()
	s.changed()
}

// ApplyActive is ApplyConcurrency for a transport-neutral active list.
func (s *Store) ApplyActive(active []LiveRequest) {
	items := make([]ActiveLike, 0, len(active))
	for _, a := range active {
		items = append(items, ActiveLike{
			ID: a.ID, Agent: a.Agent, Model: a.Model, Provider: a.Provider,
			Session: a.Session, Started: a.Started, Millis: a.Millis,
			Phase: string(a.Phase), RouteID: a.RouteID,
		})
	}
	s.ApplyConcurrency(ConcurrencyLike{Active: items})
}

// SetConnectionError records a safe error string. Existing live rows remain
// visible for one snapshot so the UI can show a disconnected state without
// pretending the requests are still being observed.
func (s *Store) SetConnectionError(msg string) {
	s.mu.Lock()
	if s.snap.GatewayOnline == false && s.snap.GatewayError == msg {
		s.mu.Unlock()
		return
	}
	s.snap.GatewayOnline = false
	s.snap.GatewayError = msg
	s.snap.LastPoll = time.Now()
	// A disconnected Gateway cannot confirm that any previous live row is
	// still in flight. Keep the route records as history, but clear the live
	// plane instead of showing stale requests as active.
	s.live = map[int64]LiveRequest{}
	s.byRoute = map[int64]map[int64]LiveRequest{}
	s.orphans = map[int64]LiveRequest{}
	s.mu.Unlock()
	s.changed()
}

// ApplySessionRoute merges the subset exposed by /v1/magpie/route. It never
// fabricates an order: the public endpoint exposes tries and model, not the
// gateway's full candidate list.
func (s *Store) ApplySessionRoute(sr SessionRouteLike) {
	s.mu.Lock()
	r := s.routes[sr.ID]
	if r == nil {
		r = &Route{ID: sr.ID}
		s.routes[sr.ID] = r
	}
	// Completed history is authoritative for completion, attempts and
	// final metrics. A later session-route poll must never replace its
	// fuller trace with the smaller public API shape.
	history := r.HasSource(SourceHistory)
	if sr.Time != (time.Time{}) && (r.Time.IsZero() || !history) {
		r.Time = sr.Time
	}
	if sr.Asked != "" && (r.Model == "" || !history) {
		r.Model = sr.Asked
	}
	if sr.Model != "" && (r.Provider == "" || !history) {
		r.Provider = providerOfModel(sr.Model)
	}
	if sr.Effort != "" && (r.Effort == "" || !history) {
		r.Effort = sr.Effort
	}
	if sr.Group != "" && (r.Group == nil || !history) {
		if r.Group == nil {
			r.Group = &GroupRef{ID: sr.Group}
		} else {
			r.Group.ID = sr.Group
		}
	}
	if !history {
		r.Served = sr.Served
		r.Done = sr.Done
		r.Status = sr.Status
		r.Error = sr.Error
		r.Millis = sr.Millis
		r.TTFT = sr.TTFT
	}
	if len(sr.Tries) > 0 && !history {
		r.Tries = make([]Try, 0, len(sr.Tries))
		for _, t := range sr.Tries {
			r.Tries = append(r.Tries, Try{
				Model: t.Model, Effort: t.Effort, Tier: t.Tier, TierServed: t.TierServed,
				Done: t.Done, Status: t.Status, Fail: t.Fail, Millis: t.Millis, TTFT: t.TTFT,
			})
		}
	}
	r.AddSource(SourceSessionRoute)
	s.mu.Unlock()
	s.changed()
}

// MergeSession merges a public SessionRoute-shaped observation.
func (s *Store) MergeSession(sr SessionRouteLike) { s.ApplySessionRoute(sr) }

// MergeHistory inserts completed routes, replacing a live/session record with
// the fuller history version while preserving all source labels.
func (s *Store) MergeHistory(rs []Route) {
	s.mu.Lock()
	for _, in := range rs {
		if in.ID == 0 {
			continue
		}
		c := in
		c.AddSource(SourceHistory)
		if old := s.routes[in.ID]; old != nil {
			for _, src := range old.Sources {
				c.AddSource(src)
			}
		}
		s.routes[in.ID] = &c
		s.history[in.ID] = time.Now()
	}
	s.snap.LastHistoryRead = time.Now()
	s.mu.Unlock()
	s.changed()
}

// ReplaceHistory rebuilds the completed-history layer. Routes that vanished
// from the loaded days are removed unless they also came from a live or
// session observation. This keeps retention/pruning from leaving stale
// history-only rows in the UI.
func (s *Store) ReplaceHistory(rs []Route) {
	s.mu.Lock()
	next := make(map[int64]*Route, len(rs)+len(s.routes))
	loadedDays := make([]string, 0)
	seenDay := map[string]struct{}{}
	for _, in := range rs {
		if in.ID == 0 {
			continue
		}
		c := in
		c.AddSource(SourceHistory)
		if old := s.routes[in.ID]; old != nil {
			for _, src := range old.Sources {
				c.AddSource(src)
			}
		}
		next[in.ID] = &c
		if _, ok := seenDay[in.HistoryDay]; !ok && in.HistoryDay != "" {
			seenDay[in.HistoryDay] = struct{}{}
			loadedDays = append(loadedDays, in.HistoryDay)
		}
	}
	for id, old := range s.routes {
		if _, ok := next[id]; ok {
			continue
		}
		if !old.HasSource(SourceLiveAPI) && !old.HasSource(SourceSessionRoute) {
			continue
		}
		c := cloneRoute(*old)
		c.Sources = c.Sources[:0]
		for _, src := range old.Sources {
			if src != SourceHistory {
				c.AddSource(src)
			}
		}
		c.HistoryDay = ""
		next[id] = &c
	}
	s.routes = next
	sort.Strings(loadedDays)
	s.snap.LoadedHistoryDays = loadedDays
	s.snap.LastHistoryRead = time.Now()
	s.mu.Unlock()
	s.changed()
}

// SetConfig replaces the static snapshot.
func (s *Store) SetConfig(c ConfigSnapshot) {
	s.mu.Lock()
	s.config = c
	s.snap.ConfigError = ""
	s.snap.LastConfigRead = c.ReadAt
	s.mu.Unlock()
	s.changed()
}

// SetConfigError retains the last successful configuration snapshot.
func (s *Store) SetConfigError(msg string) {
	s.mu.Lock()
	s.snap.ConfigError = msg
	s.snap.LastConfigRead = time.Now()
	s.mu.Unlock()
	s.changed()
}

// SetDays updates the history day list.
func (s *Store) SetDays(days []HistoryDay) {
	s.mu.Lock()
	if slices.Equal(s.snap.Days, days) {
		s.mu.Unlock()
		return
	}
	s.snap.Days = append([]HistoryDay(nil), days...)
	s.snap.LastHistoryRead = time.Now()
	s.mu.Unlock()
	s.changed()
}

// SetHistoryError records a safe reader error while retaining last-good data.
func (s *Store) SetHistoryError(msg string) {
	s.mu.Lock()
	if s.historyErr == msg {
		s.mu.Unlock()
		return
	}
	s.historyErr = msg
	s.snap.HistoryError = msg
	s.mu.Unlock()
	s.changed()
}

// SetGatewayWarning records a partial failure, such as the session-route
// endpoint being unavailable while concurrency still works.
func (s *Store) SetGatewayWarning(msg string) {
	s.mu.Lock()
	if s.snap.GatewayWarning == msg {
		s.mu.Unlock()
		return
	}
	s.snap.GatewayWarning = msg
	s.mu.Unlock()
	s.changed()
}

// SetReplay stores a copy of the replay state.
func (s *Store) SetReplay(r ReplayState) {
	s.mu.Lock()
	s.snap.Replay = r
	s.mu.Unlock()
	s.changed()
}

// Snapshot returns a deep-enough copy for the UI. Route slices are copied so
// future merges cannot race with a frame being built.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.snap
	out.Version = s.version
	out.Config = cloneConfig(s.config)
	out.Days = append([]HistoryDay(nil), s.snap.Days...)
	out.Live = make([]LiveRequest, 0, len(s.live))
	for _, l := range s.live {
		out.Live = append(out.Live, l)
	}
	sort.Slice(out.Live, func(i, j int) bool { return out.Live[i].ID < out.Live[j].ID })
	out.Routes = make([]RouteView, 0, len(s.routes))
	for _, r := range s.routes {
		if r.HistoryDay != "" {
			// loaded days are already recorded separately
		}
		rv := RouteView{Route: cloneRoute(*r)}
		if live := s.byRoute[r.ID]; len(live) > 0 {
			for _, l := range live {
				rv.Live = append(rv.Live, l)
			}
			sort.Slice(rv.Live, func(i, j int) bool { return rv.Live[i].ID < rv.Live[j].ID })
		}
		rv.Active = s.snap.GatewayOnline && len(rv.Live) > 0
		rv.Completeness = completenessOf(rv.Route, rv.Live)
		rv.CandidateOrderKnown = len(rv.Order) > 0
		out.Routes = append(out.Routes, rv)
	}
	SortRoutes(out.Routes)
	out.Orphans = make([]LiveRequest, 0, len(s.orphans))
	for _, l := range s.orphans {
		out.Orphans = append(out.Orphans, l)
	}
	sort.Slice(out.Orphans, func(i, j int) bool { return out.Orphans[i].ID < out.Orphans[j].ID })
	return out
}

// Orphans returns live rows without a RouteID, for an explicitly labeled
// live-only list.
func (s *Store) Orphans() []LiveRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]LiveRequest, 0, len(s.orphans))
	for _, l := range s.orphans {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ResetLive clears stale active state after a gateway restart.
func (s *Store) ResetLive() {
	s.mu.Lock()
	s.live = map[int64]LiveRequest{}
	s.byRoute = map[int64]map[int64]LiveRequest{}
	s.orphans = map[int64]LiveRequest{}
	s.snap.GatewayWarning = ""
	s.mu.Unlock()
	s.changed()
}

func completenessOf(r Route, live []LiveRequest) Completeness {
	c := Completeness{
		Live:         len(live) > 0 || r.HasSource(SourceLiveAPI),
		SessionTrace: r.HasSource(SourceSessionRoute),
		History:      r.HasSource(SourceHistory),
		Order:        len(r.Order) > 0,
		Tries:        len(r.Tries) > 0,
		Usage:        len(r.Usage) > 0 || r.Tokens > 0,
	}
	return c
}

func cloneRoute(r Route) Route {
	r.Sources = append([]Source(nil), r.Sources...)
	r.Order = append([]Weighed(nil), r.Order...)
	r.Left = append([]Weighed(nil), r.Left...)
	r.Tries = append([]Try(nil), r.Tries...)
	r.Usage = append([]RouteUsage(nil), r.Usage...)
	r.Nested = append([]NestedRule(nil), r.Nested...)
	for i := range r.Tries {
		r.Tries[i].Net = cloneTryNet(r.Tries[i].Net)
		r.Tries[i].Reset = cloneAutoReset(r.Tries[i].Reset)
		r.Tries[i].Auto = append([]AutoPick(nil), r.Tries[i].Auto...)
		r.Tries[i].Rest = cloneRest(r.Tries[i].Rest)
	}
	for i := range r.Order {
		r.Order[i].Rest = cloneRest(r.Order[i].Rest)
		r.Order[i].Renews = append([]time.Time(nil), r.Order[i].Renews...)
		r.Order[i].Via = append([]string(nil), r.Order[i].Via...)
		r.Order[i].Restarts = cloneTime(r.Order[i].Restarts)
		r.Order[i].Sunk = cloneTime(r.Order[i].Sunk)
		r.Order[i].CapBack = cloneTime(r.Order[i].CapBack)
	}
	for i := range r.Left {
		r.Left[i].Rest = cloneRest(r.Left[i].Rest)
		r.Left[i].Renews = append([]time.Time(nil), r.Left[i].Renews...)
		r.Left[i].Via = append([]string(nil), r.Left[i].Via...)
	}
	if r.Group != nil {
		g := *r.Group
		g.Members = append([]string(nil), g.Members...)
		g.Via = append([]string(nil), g.Via...)
		g.Fast = append([]string(nil), g.Fast...)
		g.Subs = append([]SubGroup(nil), g.Subs...)
		r.Group = &g
	}
	if r.Rule != nil {
		x := *r.Rule
		x.When = append([]string(nil), x.When...)
		x.Then = append([]string(nil), x.Then...)
		x.Small = append([]string(nil), x.Small...)
		r.Rule = &x
	}
	if r.TitleLink != nil {
		x := *r.TitleLink
		r.TitleLink = &x
	}
	if r.For != nil {
		x := *r.For
		r.For = &x
	}
	if r.Subagent != nil {
		x := *r.Subagent
		r.Subagent = &x
	}
	if r.Affinity != nil {
		x := *r.Affinity
		r.Affinity = &x
	}
	if r.Prompt != nil {
		x := *r.Prompt
		x.Parts = append([]PromptPart(nil), r.Prompt.Parts...)
		for i := range x.Parts {
			x.Parts[i].Items = append([]PromptItem(nil), x.Parts[i].Items...)
		}
		r.Prompt = &x
	}
	return r
}

func cloneRest(r *Rest) *Rest {
	if r == nil {
		return nil
	}
	x := *r
	return &x
}

func cloneTryNet(n *TryNet) *TryNet {
	if n == nil {
		return nil
	}
	x := *n
	return &x
}

func cloneAutoReset(r *AutoReset) *AutoReset {
	if r == nil {
		return nil
	}
	x := *r
	return &x
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	x := *t
	return &x
}

func cloneConfig(c ConfigSnapshot) ConfigSnapshot {
	out := c
	out.Providers = append([]ProviderInfo(nil), c.Providers...)
	for i := range out.Providers {
		out.Providers[i].Models = append([]string(nil), out.Providers[i].Models...)
	}
	out.Groups = append([]GroupInfo(nil), c.Groups...)
	for i := range out.Groups {
		out.Groups[i].Members = append([]string(nil), out.Groups[i].Members...)
		out.Groups[i].Match = append([]string(nil), out.Groups[i].Match...)
	}
	out.ProviderOrder = append([]string(nil), c.ProviderOrder...)
	out.GroupOrder = append([]string(nil), c.GroupOrder...)
	return out
}

func providerOfModel(model string) string {
	if i := strings.IndexByte(model, '/'); i > 0 {
		return model[:i]
	}
	return ""
}

// ConcurrencyLike is the subset the store needs, kept independent of the
// HTTP package so the routing package stays UI- and transport-neutral.
type ConcurrencyLike struct {
	Active []ActiveLike
}

type ActiveLike struct {
	ID       int64
	Agent    string
	Model    string
	Provider string
	Session  string
	Started  time.Time
	Millis   int64
	Phase    string
	RouteID  int64
}

type SessionRouteLike struct {
	ID     int64
	Time   time.Time
	Asked  string
	Model  string
	Effort string
	Group  string
	Served string
	Tries  []TryLike
	Done   bool
	Status int
	Error  string
	Millis int64
	TTFT   int64
}

type TryLike struct {
	Model      string
	Effort     string
	Tier       string
	TierServed string
	Done       bool
	Status     int
	Fail       string
	Millis     int64
	TTFT       int64
}
