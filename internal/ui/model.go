package ui

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/Fan-chou/magpie-native-routing/internal/config"
	"github.com/Fan-chou/magpie-native-routing/internal/routing"
	"github.com/egoist/mygo/ui"
)

// Actions is the narrow control surface the native view may invoke. HTTP
// requests and file scans stay behind the application shell, never in a
// draw callback or the UI model.
type Actions struct {
	Refresh          func()
	OpenDay          func(day string)
	RefreshHistory   func()
	ReloadDay        func(day string)
	SaveSettings     func(config.AppSettings) error
	Restart          func()
	ChooseDataDir    func()
	ApplyTheme       func(config.ThemeMode)
	ConfigureDay     func(day string)
	LoadKnownHistory func()
}

type tabKind int

const (
	tabLive tabKind = iota
	tabHistory
	tabGroups
)

// liveRouteLimit is client.js's last 60 routes: the gateway's live trace
// window, not every request found in today's completed history file.
const liveRouteLimit = 60

// Model owns UI-only state. It never performs I/O and reads only immutable
// routing snapshots.
type Model struct {
	mu sync.RWMutex

	windowWidth    float32
	windowHeight   float32
	historyVersion uint64
	viewCache      uiViewCache

	store           *routing.Store
	settings        config.AppSettings
	paths           config.Paths
	actualMagpieDir string
	actions         Actions
	loc             Localization

	tab          tabKind
	query        string
	status       string
	statusIndex  int
	activeOnly   bool
	selectedID   int64
	pinnedID     int64
	maskAccounts bool
	// maskRevealAt starts upstream's 350ms rtpii transition after the user
	// turns masking on. It is zero for a restored preference: the first
	// frame of a saved masked session must already be blurred, not animate.
	maskRevealAt time.Time
	expandedTry  int
	listState    ui.ListState
	requestSel   int
	listRouteID  []int64
	syncListSel  bool

	historyDay string
	replay     replayController
	pageScroll ui.ScrollState

	settingsOpen bool
	draftURL     string
	draftKey     string
	draftDataDir string
	draftTheme   config.ThemeMode
	draftLang    string
	settingsErr  string
	showSignal   bool
	showOrphans  bool

	groupID      string
	groupNotice  string
	splitPercent float32
	contextOpen  bool
	ctxTab       string
	ctxShowAll   bool
	metrics      map[string]bool
	topology     topologyState
	lastSnapshot routing.Snapshot
}

// uiViewCache holds derivations that are pure functions of the snapshot and
// the user's filters. MyGo rebuilds the whole view for replay's low-rate
// clock ticks and for store updates; walking and filtering all 4,000+ routes
// each time was the dominant CPU cost in a continuous history replay.
type uiViewCache struct {
	filteredKey  string
	filtered     []routing.RouteView
	groupKey     string
	groups       []routing.GroupView
	routeByIDKey uint64
	routeByID    map[int64]routing.RouteView
	accountKey   string
	accounts     []routeAccountTally
}

type replayController struct {
	timeline   routing.ReplayTimeline
	day        string
	playing    bool
	speed      float64
	position   time.Duration
	lastTick   time.Time
	builtKey   string
	loaded     bool
	pendingID  int64
	windows    []replayFlightWindow
	windowEnds []time.Duration
}

// replayFlightWindow is the presentation interval during which a recorded
// route owns a bird. It starts at Route.Time and ends after the last inbound
// presentation leg, so seeking directly into the return still rebuilds it.
type replayFlightWindow struct {
	routeID int64
	start   time.Duration
	end     time.Duration
}

type topologyState struct {
	flights        []flightTrack
	frameAt        time.Time
	stageGeometry  *flightGeometry
	flightGeometry *flightGeometry
	reduceMotion   bool
	layout         topologyFrameLayout
	// animationCost is a slow estimate of how long one retained-tree
	// repaint takes. MyGo repaints the whole retained tree for
	// Painter.After, so spending 60 frames a second on a large history page
	// can burn CPU even when only a bird moves. The budget keeps the
	// original cadence when a frame is cheap and lowers it gradually when
	// the same window cannot paint comfortably.
	animationCost   time.Duration
	animationFrames int
	// staticStage caches the complete non-moving stage (cards, labels,
	// icons, idle wires) as a device-resolution bitmap. ui.Render draws it
	// in an independent, correctly scaled view, so the bitmap is safe under
	// the page's scroll clip and MyGo does not have to repaint every text
	// and icon on each animation frame.
	staticStage      *ui.Bitmap
	staticStageKey   string
	staticStageScale float32
}

type topologyFeedback struct {
	at     time.Time
	kind   flightPhaseKind
	failed bool
}

// flightTrack is deliberately UI-only animation state. Its route id is the
// exact Magpie Route ID and every timing input comes from the observed route.
// It never creates a request that is absent from the snapshot.
type flightTrack struct {
	routeID     int64
	agent       string
	model       string
	provider    string
	accountID   string
	who         string
	firstSeen   time.Time
	lastSeen    time.Time
	removedAt   time.Time
	doneAt      time.Time
	returnDur   time.Duration
	observed    time.Duration
	total       time.Duration
	attempts    []flightAttempt
	phases      []flightPhase
	inReplay    bool
	replayPlay  bool
	failed      bool
	done        bool
	phaseKey    string
	phaseKind   flightPhaseKind
	phaseFailed bool
	phaseAt     time.Time
}

// flightAttempt is the geometry-independent part of one recorded attempt.
// Times are relative to the route's start and come from Try.Start/Millis;
// the painter maps the provider name to the account row currently on screen.
type flightAttempt struct {
	provider  string
	model     string
	accountID string
	who       string
	start     time.Duration
	end       time.Duration
	failed    bool
	done      bool
}

type flightPhaseKind uint8

const (
	flightOutbound flightPhaseKind = iota
	flightHold
	flightRetryBack
	flightRetryOut
	flightInbound
)

// flightPhase is a presentation interval ordered from the recorded attempts.
// Travel legs use short animation durations; the time left over is spent at
// the account, preserving the real ordering and relative attempt lengths.
type flightPhase struct {
	kind      flightPhaseKind
	provider  string
	model     string
	accountID string
	who       string
	start     time.Duration
	end       time.Duration
	failed    bool
}

// NewModel constructs the native presentation model.
func NewModel(store *routing.Store, settings config.AppSettings, paths config.Paths, actualMagpieDir string, actions Actions) *Model {
	m := &Model{
		store: store, settings: settings, paths: paths,
		actualMagpieDir: actualMagpieDir, actions: actions,
		loc: NewLocalization(settings.Language), status: "all",
		draftURL: settings.GatewayURL, draftKey: settings.APIKey,
		draftDataDir: settings.MagpieDir, draftTheme: settings.Theme,
		draftLang:    settings.Language,
		topology:     topologyState{},
		splitPercent: 58,
		requestSel:   -1,
		ctxTab:       "all",
		metrics:      defaultVisibleMetrics(),
		maskAccounts: settings.HideAccounts,
	}
	if settings.Theme == "" {
		m.settings.Theme = config.ThemeSystem
	}
	m.replay.speed = 1
	m.historyDay = time.Now().Format("2006-01-02")
	m.replay.day = m.historyDay
	return m
}

// SetSnapshot is called on the UI thread with a copied snapshot.
func (m *Model) SetSnapshot(s routing.Snapshot) {
	m.mu.Lock()
	if m.historyVersion != s.Version {
		// A committed store version is the only event that can add,
		// replace or remove a completed trace. Keep the filtered history
		// slice for the replay builder so a playing replay does not walk
		// and fingerprint all 3,500+ rows on every frame.
		m.historyVersion = s.Version
	}
	m.lastSnapshot = s
	if m.selectedID == 0 && len(s.Routes) > 0 {
		m.selectedID = s.Routes[0].ID
		m.syncListSel = true
	}
	if m.historyDay == "" {
		m.historyDay = time.Now().Format("2006-01-02")
		m.replay.day = m.historyDay
	}
	m.mu.Unlock()
}

// Select sets the route the detail pane should show.
func (m *Model) Select(id int64) {
	m.mu.Lock()
	if m.selectedID != id {
		m.selectedID = id
		m.syncListSel = true
	}
	m.mu.Unlock()
}

// SetHistoryDay selects a local calendar day for the history view.
func (m *Model) SetHistoryDay(day string) {
	m.mu.Lock()
	m.historyDay = day
	if m.replay.day != day {
		m.replay = replayController{speed: 1, day: day}
	}
	m.mu.Unlock()
}

// PlayRoute switches to the completed history, selects the exact Route ID
// and positions the real timeline at that request's recorded start. It is
// used by the story's Replay action; it never fabricates a timestamp.
func (m *Model) PlayRoute(day string, id int64) {
	m.mu.Lock()
	if day != "" {
		m.historyDay = day
	}
	if id != 0 {
		m.selectedID = id
		m.pinnedID = id
		m.syncListSel = true
	}
	m.tab = tabHistory
	if m.replay.day != day {
		m.replay = replayController{speed: 1, day: day}
	}
	m.replay.pendingID = id
	m.replay.loaded = false
	m.mu.Unlock()
}

// SetDraftDataDir updates the settings draft after the native folder picker.
func (m *Model) SetDraftDataDir(dir string) {
	m.mu.Lock()
	m.draftDataDir = dir
	m.mu.Unlock()
}

// SetDraftGateway updates the settings draft, used by command-line overrides
// and diagnostics.
func (m *Model) SetDraftGateway(url, key string) {
	m.mu.Lock()
	m.draftURL = url
	if key != "" {
		m.draftKey = key
	}
	m.mu.Unlock()
}

// GroupNotice is a short message shown in the read-only group view.
func (m *Model) GroupNotice() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.groupNotice
}

func (m *Model) setGroupNotice(s string) {
	m.mu.Lock()
	m.groupNotice = s
	m.mu.Unlock()
}

func (m *Model) setTab(tab tabKind) {
	m.mu.Lock()
	m.tab = tab
	m.mu.Unlock()
}

func (m *Model) setStatus(status string, index int) {
	m.mu.Lock()
	m.status = status
	m.statusIndex = index
	m.mu.Unlock()
}

func (m *Model) queryValue() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.query
}

func (m *Model) statusValue() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.statusIndex
}

func (m *Model) currentHistoryDay() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.historyDay
}

func (m *Model) isPinned(id int64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pinnedID == id
}

func (m *Model) accountsMasked() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.maskAccounts
}

func (m *Model) toggleAccountMask() {
	m.mu.Lock()
	m.maskAccounts = !m.maskAccounts
	if m.maskAccounts {
		m.maskRevealAt = time.Now()
	} else {
		m.maskRevealAt = time.Time{}
	}
	m.mu.Unlock()
}

// accountMaskStyle is the animated part of upstream .pii. It is measured
// from the UI clock by callers, never from a frame count.
func (m *Model) accountMaskStyle(now time.Time) (blur, opacity float32) {
	m.mu.RLock()
	masked, revealAt := m.maskAccounts, m.maskRevealAt
	m.mu.RUnlock()
	if !masked {
		return 0, 1
	}
	// CSS rtpii: from blur(0), opacity 1 to blur(2.2px), opacity .5 over
	// .35s ease-out. Before the clock reaches revealAt (a Test's frozen
	// clock) show the final .pii state; a restored preference never sets
	// revealAt and starts there too.
	if revealAt.IsZero() || now.Before(revealAt) {
		return 2.2, 0.5
	}
	t := clamp01(float32(now.Sub(revealAt)) / float32(350*time.Millisecond))
	if t >= 1 {
		return 2.2, 0.5
	}
	k := easeOut(t)
	return 2.2 * k, 1 - 0.5*k
}

// accountMaskAnimating reports whether the short reveal still needs
// frames. Outside it, masked text is static.
func (m *Model) accountMaskAnimating(now time.Time) bool {
	m.mu.RLock()
	masked, revealAt := m.maskAccounts, m.maskRevealAt
	m.mu.RUnlock()
	return masked && !revealAt.IsZero() && now.Sub(revealAt) < 350*time.Millisecond
}

// setAccountMask restores the app-local preference without touching
// Magpie's settings or localStorage. Upstream keeps this switch in
// magpie.maskEmails; this app stores it in its own settings file.
func (m *Model) setAccountMask(on bool) {
	m.mu.Lock()
	m.maskAccounts = on
	m.maskRevealAt = time.Time{}
	m.mu.Unlock()
}

func (m *Model) toggleContextCard() {
	m.mu.Lock()
	m.contextOpen = !m.contextOpen
	m.mu.Unlock()
}

func (m *Model) contextCardOpen() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.contextOpen
}

func (m *Model) contextTab() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ctxTab
}

func (m *Model) setContextTab(tab string) {
	m.mu.Lock()
	m.ctxTab = tab
	m.mu.Unlock()
}

func (m *Model) contextShowAll() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ctxShowAll
}

func (m *Model) toggleContextShowAll() {
	m.mu.Lock()
	m.ctxShowAll = !m.ctxShowAll
	m.mu.Unlock()
}

// metricVisible reports whether the request list shows one of the original
// metric classes. Costs stay excluded until Magpie's public history has a
// price source; choosing an unavailable metric must not invent a value.
func (m *Model) metricVisible(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.metrics[key]
}

func (m *Model) toggleMetric(key string) {
	m.mu.Lock()
	if m.metrics == nil {
		m.metrics = defaultVisibleMetrics()
	}
	// Upstream's metric menu can leave every class off. Rebuild the map so
	// metricVisible's "absent means hidden" rule is unambiguous.
	next := make(map[string]bool, len(m.metrics)+1)
	for k, on := range m.metrics {
		next[k] = on
	}
	next[key] = !next[key]
	m.metrics = next
	m.mu.Unlock()
}

func defaultVisibleMetrics() map[string]bool {
	return map[string]bool{
		"duration": true,
		"ttft":     true,
		"tokens":   true,
		"cache":    true,
		"speed":    true,
	}
}

func (m *Model) setSettingsOpen(open bool) {
	m.mu.Lock()
	m.settingsOpen = open
	if !open {
		m.settingsErr = ""
	}
	m.mu.Unlock()
}

func (m *Model) settingsState() (open bool, errText string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settingsOpen, m.settingsErr
}

func (m *Model) setSettingsError(err error) {
	m.mu.Lock()
	if err != nil {
		m.settingsErr = err.Error()
	} else {
		m.settingsErr = ""
	}
	m.mu.Unlock()
}

// historyDays returns the day chips. Today always comes from the store even
// when no file exists yet.
func (m *Model) historyDays(s routing.Snapshot) []routing.HistoryDay {
	m.mu.RLock()
	current := m.historyDay
	m.mu.RUnlock()
	days := append([]routing.HistoryDay(nil), s.Days...)
	if current == "" {
		return days
	}
	for _, d := range days {
		if d.Day == current {
			return days
		}
	}
	return append([]routing.HistoryDay{{Day: current}}, days...)
}

// prepareListSelection preserves an explicit selection across list content
// changes, while treating a changed list index as a user choice when the
// rows stayed the same. It avoids guessing any route identity.
func (m *Model) prepareListSelection(routes []routing.RouteView) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]int64, len(routes))
	for i := range routes {
		ids[i] = routes[i].ID
	}
	contentChanged := !slices.Equal(ids, m.listRouteID)
	if contentChanged || m.syncListSel {
		m.requestSel = indexRoute(routes, m.selectedID)
		if m.requestSel < 0 && m.pinnedID != 0 {
			m.requestSel = indexRoute(routes, m.pinnedID)
		}
		if m.requestSel < 0 && len(routes) > 0 {
			m.requestSel = 0
			m.selectedID = routes[0].ID
		}
		m.syncListSel = false
	} else if m.requestSel >= 0 && m.requestSel < len(routes) {
		m.selectedID = routes[m.requestSel].ID
	}
	m.listRouteID = ids
}

func (m *Model) commitListSelection(routes []routing.RouteView) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.requestSel >= 0 && m.requestSel < len(routes) {
		m.selectedID = routes[m.requestSel].ID
	}
}

func indexRoute(routes []routing.RouteView, id int64) int {
	if id == 0 {
		return -1
	}
	for i := range routes {
		if routes[i].ID == id {
			return i
		}
	}
	return -1
}

func (m *Model) Settings() config.AppSettings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settings
}

func (m *Model) SetSettings(s config.AppSettings) {
	m.mu.Lock()
	m.settings = s
	m.maskAccounts = s.HideAccounts
	m.maskRevealAt = time.Time{}
	m.loc = NewLocalization(s.Language)
	m.draftURL, m.draftKey = s.GatewayURL, s.APIKey
	m.draftDataDir, m.draftTheme, m.draftLang = s.MagpieDir, s.Theme, s.Language
	m.mu.Unlock()
}

// SetPaths updates the configured and detected data directories after a
// settings change. Workers apply the new paths asynchronously.
func (m *Model) SetPaths(paths config.Paths, actualMagpieDir string) {
	m.mu.Lock()
	m.paths = paths
	m.actualMagpieDir = actualMagpieDir
	m.mu.Unlock()
}

func (m *Model) Paths() config.Paths {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.paths
}

func (m *Model) ActualMagpieDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.actualMagpieDir
}

func (m *Model) localization() Localization {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loc
}

func (m *Model) snapshot() routing.Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastSnapshot
}

func (m *Model) filters() routing.RouteFilter {
	m.mu.RLock()
	defer m.mu.RUnlock()
	f := routing.RouteFilter{Query: m.query, Status: m.status}
	if m.tab == tabHistory && m.historyDay != "" {
		f.HistoryDay = m.historyDay
	}
	return f
}

func (m *Model) visibleRoutes(s routing.Snapshot, replayOnly bool) []routing.RouteView {
	m.mu.RLock()
	f := routing.RouteFilter{Query: m.query, Status: m.status}
	tab := m.tab
	if tab == tabHistory {
		f.HistoryDay = m.historyDay
	}
	replay := m.replay
	selected := m.selectedID
	m.mu.RUnlock()
	key := fmt.Sprintf("%d\x00%d\x00%s\x00%s\x00%s", s.Version, tab, f.Query, f.Status, f.HistoryDay)
	m.mu.RLock()
	cached, ok := m.viewCache.filtered, m.viewCache.filteredKey == key
	m.mu.RUnlock()
	routes := cached
	if !ok {
		routes = f.Apply(s.Routes)
		m.mu.Lock()
		if m.viewCache.filteredKey != key {
			m.viewCache.filteredKey = key
			m.viewCache.filtered = routes
		}
		m.mu.Unlock()
	}
	if tab == tabLive && f.HistoryDay == "" {
		// The original keeps 60 routes in its live map. Today's JSONL may
		// contain thousands; those belong to the day view and must not
		// pollute live stats or the account tally.
		if len(routes) > liveRouteLimit {
			routes = routes[:liveRouteLimit]
		}
	}
	if replayOnly && replay.loaded && replay.timeline.Duration > 0 {
		started, _ := routing.ReplayVisibleRoutes(replay.timeline, replay.position)
		allowed := make(map[int64]bool, len(started))
		for _, r := range started {
			allowed[r.ID] = true
		}
		filtered := make([]routing.RouteView, 0, len(routes))
		for _, r := range routes {
			if r.HistoryDay != replay.day || allowed[r.ID] {
				filtered = append(filtered, r)
			}
		}
		routes = filtered
	}
	_ = selected
	return routes
}

// routeViewIndex returns the snapshot's Route ID index, rebuilt only when a
// new store version arrives. Replay and live flight reconciliation need it
// every view build, and indexing 4,000+ routes each time dominated CPU in a
// continuous replay.
func (m *Model) routeViewIndex(s routing.Snapshot) map[int64]routing.RouteView {
	m.mu.RLock()
	if m.viewCache.routeByIDKey == s.Version && m.viewCache.routeByID != nil {
		byID := m.viewCache.routeByID
		m.mu.RUnlock()
		return byID
	}
	m.mu.RUnlock()
	byID := make(map[int64]routing.RouteView, len(s.Routes))
	for _, rv := range s.Routes {
		byID[rv.ID] = rv
	}
	m.mu.Lock()
	if m.viewCache.routeByIDKey != s.Version {
		m.viewCache.routeByIDKey = s.Version
		m.viewCache.routeByID = byID
	} else {
		byID = m.viewCache.routeByID
	}
	m.mu.Unlock()
	return byID
}

// cachedAccountTally reuses renderActs' projection across the view builds
// caused by UI-only changes. The route slice is immutable for a store
// version; the key also covers the visible IDs so a filtered list cannot
// share another filter's tally.
func (m *Model) cachedAccountTally(routes []routing.RouteView) []routeAccountTally {
	var b strings.Builder
	for i := range routes {
		fmt.Fprintf(&b, "%d:%d\x00", routes[i].ID, routeTallyRevision(routes[i]))
	}
	key := b.String()
	m.mu.RLock()
	cached, ok := m.viewCache.accounts, m.viewCache.accountKey == key
	m.mu.RUnlock()
	if ok {
		return cached
	}
	tally := accountTally(routes)
	m.mu.Lock()
	if m.viewCache.accountKey != key {
		m.viewCache.accountKey = key
		m.viewCache.accounts = tally
	}
	m.mu.Unlock()
	return tally
}

// routeTallyRevision identifies the only Route fields accountTally reads.
func routeTallyRevision(r routing.RouteView) uint64 {
	var h uint64 = 1469598103934665603
	mix := func(v uint64) {
		h ^= v
		h *= 1099511628211
	}
	mix(uint64(r.Time.UnixNano()))
	if r.Active {
		h ^= 1 << 8
	}
	mix(uint64(len(r.Order)))
	for _, w := range r.Order {
		for _, s := range []string{w.Provider, w.ID, w.Kind, w.Model, w.Name, w.Who, w.Plan, w.Icon, w.Preset, w.Routing} {
			for i := 0; i < len(s); i++ {
				mix(uint64(s[i]))
			}
		}
		if w.Known {
			mix(1)
		}
		mix(uint64(math.Float64bits(w.Used)))
	}
	mix(uint64(len(r.Tries)))
	for _, tr := range r.Tries {
		for _, s := range []string{tr.Model, tr.Served, tr.ID, tr.Fail, tr.Error} {
			for i := 0; i < len(s); i++ {
				mix(uint64(s[i]))
			}
		}
		if tr.Done {
			mix(2)
		}
		mix(uint64(tr.Status))
	}
	return h
}

// groupViews caches the read-only group projection. GroupView carries
// copies, so callers may keep it without exposing mutable config storage.
func (m *Model) groupViews(s routing.Snapshot) []routing.GroupView {
	key := fmt.Sprintf("%d\x00%d\x00%d", s.Version, len(s.Config.Groups), len(s.Routes))
	m.mu.RLock()
	cached, ok := m.viewCache.groups, m.viewCache.groupKey == key
	m.mu.RUnlock()
	if ok {
		return cached
	}
	groups := routing.GroupViews(s.Config, s.Routes)
	m.mu.Lock()
	if m.viewCache.groupKey != key {
		m.viewCache.groupKey = key
		m.viewCache.groups = groups
	}
	m.mu.Unlock()
	return groups
}

func (m *Model) selectedRoute(routes []routing.RouteView) (routing.RouteView, bool) {
	m.mu.RLock()
	id := m.pinnedID
	if id == 0 {
		id = m.selectedID
	}
	m.mu.RUnlock()
	return routing.SelectRoute(routes, id)
}

func (m *Model) selectRoute(id int64) {
	m.mu.Lock()
	m.selectedID = id
	m.mu.Unlock()
}

func (m *Model) togglePin(id int64) {
	m.mu.Lock()
	if m.pinnedID == id {
		m.pinnedID = 0
	} else {
		m.pinnedID = id
		m.selectedID = id
		m.syncListSel = true
	}
	m.mu.Unlock()
}

func (m *Model) visibleAgents(routes []routing.RouteView, live []routing.LiveRequest) []agentNode {
	var out []agentNode
	add := func(id, model, folder string, active bool, sessions, requests int) {
		id = strings.TrimSpace(id)
		if id == "" {
			id = "agent"
		}
		key := strings.ToLower(id)
		if i := indexAgent(out, key); i >= 0 {
			out[i].Active = out[i].Active || active
			out[i].Requests += requests
			out[i].Sessions += sessions
			if out[i].Model == "" {
				out[i].Model = model
			}
			return
		}
		out = append(out, agentNode{
			ID: id, Name: agentName(id), Model: model, Folder: folder, Icon: agentIcon(id),
			Active: active, Requests: requests, Sessions: sessions, Hue: agentHue(id),
		})
	}
	for _, l := range live {
		add(l.Agent, l.Model, "", true, 0, 1)
	}
	for _, r := range routes {
		if r.Active {
			add(r.Agent, r.Model, r.Folder, true, 0, 1)
			continue
		}
		add(r.Agent, r.Model, r.Folder, false, 1, 1)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active
		}
		return out[i].Requests > out[j].Requests
	})
	if len(out) > 4 {
		out = out[:4]
	}
	return out
}

func indexAgent(out []agentNode, key string) int {
	for i := range out {
		if strings.ToLower(out[i].ID) == key {
			return i
		}
	}
	return -1
}

type agentNode struct {
	ID       string
	Name     string
	Model    string
	Folder   string
	Icon     string
	Active   bool
	Requests int
	Sessions int
	Hue      ui.Color
}

func agentName(id string) string {
	switch strings.ToLower(id) {
	case "codex":
		return "Codex"
	case "claude", "claude-code":
		return "Claude Code"
	case "opencode":
		return "OpenCode"
	case "cursor":
		return "Cursor"
	case "gemini":
		return "Gemini"
	case "copilot":
		return "Copilot"
	case "crush":
		return "Crush"
	case "goose":
		return "Goose"
	case "pi":
		return "Pi"
	case "qoder":
		return "Qoder"
	case "other", "":
		return "Agent"
	default:
		return id
	}
}

func agentIcon(id string) string {
	switch strings.ToLower(id) {
	case "codex":
		return "codex-color"
	case "claude", "claude-code":
		return "claudecode-color"
	case "opencode":
		return "opencode"
	case "cursor":
		return "cursor"
	case "gemini":
		return "gemini-color"
	case "copilot":
		return "copilot-color"
	case "crush":
		return "crush"
	case "goose":
		return "goose"
	case "pi":
		return "pi"
	case "qoder":
		return "qoder"
	}
	return ""
}

func agentHue(id string) ui.Color {
	switch strings.ToLower(id) {
	case "codex":
		return ui.Hex("#6366f1")
	case "claude":
		return ui.Hex("#d97757")
	case "gemini":
		return ui.Hex("#0ea5e9")
	case "copilot":
		return ui.Hex("#a855f7")
	case "opencode":
		return ui.Hex("#eab308")
	case "cursor":
		return ui.Hex("#14b8a6")
	case "crush":
		return ui.Hex("#ec4899")
	case "goose":
		return ui.Hex("#84cc16")
	case "pi":
		return ui.Hex("#10b981")
	}
	colors := []ui.Color{
		ui.Hex("#8b5cf6"), ui.Hex("#22c55e"), ui.Hex("#0d9488"),
		ui.Hex("#0891b2"), ui.Hex("#ca8a04"), ui.Hex("#db2777"),
		ui.Hex("#2563eb"), ui.Hex("#65a30d"),
	}
	var n uint32
	for _, r := range id {
		n = n*31 + uint32(r)
	}
	return colors[int(n%uint32(len(colors)))]
}

// providerRows constructs the right-side stages from the selected route.
// Candidate rows come only from an observed order or concrete tries. A
// live-only row exposes the provider the Gateway reported, not a guessed
// order.
func providerRows(r routing.RouteView, hidden bool) []providerNode {
	var out []providerNode
	seen := map[string]int{}
	add := func(provider, name, who, model, accountID, plan, icon, status, note, weight, kind, routingMode string, current, failed bool) {
		// Fold rows for the same provider and model (the gateway may expose
		// the candidate order and a try separately). Accounts of the same
		// provider serving different models stay separate.
		key := strings.ToLower(strings.Join([]string{provider, model, accountID}, "\x00"))
		if i, ok := seen[key]; ok {
			if current {
				out[i].Current = true
			}
			if failed {
				out[i].Failed = true
			}
			if status != "" {
				out[i].Status = status
			}
			if note != "" {
				out[i].Note = note
			}
			if icon != "" {
				out[i].Icon = icon
			}
			if kind != "" {
				out[i].Kind = kind
			}
			if routingMode != "" {
				out[i].Routing = routingMode
			}
			return
		}
		seen[key] = len(out)
		out = append(out, providerNode{
			Provider: provider, Name: name, Who: who, Model: model, Plan: plan,
			AccountID: accountID, Icon: icon, Status: status, Note: note, Weight: weight,
			Kind: kind, Routing: routingMode, Current: current, Failed: failed,
		})
	}
	for i, w := range r.Order {
		provider := w.Provider
		if provider == "" {
			provider = providerOf(w.Model)
		}
		name := w.Name
		if name == "" {
			name = w.Provider
		}
		current := false
		failed := false
		status := ""
		note := ""
		if w.Barred {
			note = "barred"
		}
		add(provider, name, w.Who, w.Model, w.ID, w.Plan, firstNonEmpty(w.Icon, w.Preset), status, note, w.Fixed, w.Kind, w.Routing, current, failed)
		if len(out) > i {
			out[i].Fallback = w.Fallback
			out[i].Rest = w.Rest != nil
			if w.Rest != nil {
				out[i].RestWhy = w.Rest.Why
				out[i].RestUntil = w.Rest.Until
			}
			out[i].Known = w.Known
			out[i].Used = w.Used
		}
	}
	for ti, tr := range r.Tries {
		provider := providerOf(tr.Model)
		if provider == "" {
			provider = tr.ID
		}
		// traceAttemptTarget is the authoritative seat mapping used by the
		// flight layer. Resolve the Try against the already-built order rows
		// with that same function; providerMatchesOrder alone was too weak
		// and could leave an alias/account mismatch as a duplicate row.
		target := traceAttemptTarget(r, tr)
		row := -1
		for ri := range out {
			if providerMatchScore(out[ri], target) > 0 {
				row = ri
				break
			}
		}
		if row < 0 {
			add(provider, provider, tr.Served, tr.Model, tr.ID, "", providerIconKey(r, provider, tr),
				tryStatus(tr), routing.TryFailure(tr), "", "", "",
				r.Active && !tr.Done && !tryFailed(tr), tryFailed(tr))
			row = len(out) - 1
		} else {
			out[row].Status = tryStatus(tr)
			out[row].Note = routing.TryFailure(tr)
			out[row].Current = r.Active && !tr.Done && !tryFailed(tr)
			out[row].Failed = tryFailed(tr)
			if icon := providerIconKey(r, provider, tr); icon != "" && out[row].Icon == "" {
				out[row].Icon = icon
			}
		}
		if row >= 0 {
			done := tr.Done
			failed := tryFailed(tr)
			out[row].Answered = done && !failed
			out[row].WentOn = failed && ti+1 < len(r.Tries)
		}
	}
	if len(out) == 0 {
		provider := r.DisplayProvider()
		if provider != "" || r.Model != "" {
			status := ""
			if r.Active {
				status = "active"
			} else if r.Done {
				status = "done"
			}
			add(provider, provider, "", r.Model, "", "", "", status, "", "", "", "",
				r.Active, r.Status >= 400 || r.Error != "")
		}
	}
	if len(r.Live) > 0 && len(out) == 0 {
		for _, l := range r.Live {
			// Concurrency exposes a session, not the provider account id.
			add(l.Provider, l.Provider, "", l.Model, "", "", "", string(l.Phase), "", "", "", "",
				true, false)
		}
	}
	if hidden {
		for i := range out {
			if out[i].Who != "" {
				out[i].Who = maskWho(out[i].Who)
				out[i].Masked = true
			}
		}
	}
	return out
}

// providerRowsWithConfig fills the fields that completed traces often omit:
// providers.json is Magpie's own read-only source for a provider's logo and
// display name. A trace value remains authoritative whenever it is present.
func providerRowsWithConfig(r routing.RouteView, hidden bool, cfg routing.ConfigSnapshot) []providerNode {
	rows := providerRows(r, hidden)
	for i := range rows {
		info, ok := providerInfoByID(cfg, rows[i].Provider)
		if !ok {
			continue
		}
		if rows[i].Name == "" || strings.EqualFold(rows[i].Name, rows[i].Provider) {
			if info.Name != "" {
				rows[i].Name = info.Name
			}
		}
		if rows[i].Icon == "" {
			rows[i].Icon = firstNonEmpty(info.Icon, info.Preset)
		}
	}
	return rows
}

func providerInfoByID(cfg routing.ConfigSnapshot, id string) (routing.ProviderInfo, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return routing.ProviderInfo{}, false
	}
	for _, p := range cfg.Providers {
		if strings.EqualFold(p.ID, id) {
			return p, true
		}
	}
	return routing.ProviderInfo{}, false
}

func providerIconKey(r routing.RouteView, provider string, tr routing.Try) string {
	for _, w := range r.Order {
		rowProvider := firstNonEmpty(w.Provider, providerOf(w.Model))
		if !strings.EqualFold(rowProvider, provider) && !strings.EqualFold(w.ID, tr.ID) {
			continue
		}
		if icon := firstNonEmpty(w.Icon, w.Preset); icon != "" {
			return icon
		}
	}
	return ""
}

func providerMatchesOrder(order []routing.Weighed, provider, model string) bool {
	for _, w := range order {
		p := w.Provider
		if p == "" {
			p = providerOf(w.Model)
		}
		if p != "" && strings.EqualFold(p, provider) &&
			(model == "" || w.Model == "" || modelMatches(w.Model, model, provider)) {
			return true
		}
	}
	return false
}

type providerNode struct {
	Provider  string
	Name      string
	Who       string
	Model     string
	AccountID string
	Plan      string
	Icon      string
	Status    string
	Note      string
	Weight    string
	Kind      string
	Routing   string
	Known     bool
	Used      float64
	Current   bool
	Failed    bool
	// Fallback and Rest are actual fields of Magpie's Weighed/Try trace;
	// they let the stage distinguish fallback members and resting accounts.
	Fallback  bool
	Rest      bool
	RestWhy   string
	RestUntil time.Time
	Answered  bool
	WentOn    bool
	Masked    bool
}

func providerOf(model string) string {
	if i := strings.IndexByte(model, '/'); i > 0 {
		return model[:i]
	}
	return ""
}

func tryFailed(t routing.Try) bool {
	return t.Status >= 400 || t.Fail != "" || t.Error != ""
}

func tryStatus(t routing.Try) string {
	if t.Status > 0 {
		return "HTTP " + itoa(t.Status)
	}
	if t.Fail != "" || t.Error != "" {
		return "failed"
	}
	if t.Done {
		return "done"
	}
	return "active"
}

// maskWho is app.js's stand-in for an account: letters of the same shape,
// deterministic per address/name, so an address stays one recognisable
// blurred shape without exposing a single real character. The caller draws
// the result with the upstream 2.2px blur and 0.5 opacity.
func maskWho(s string) string {
	const alphabet = "aeiounrstlcmdh"
	h := uint32(7)
	var b strings.Builder
	// app.js replaces each UTF-16 code unit, not each Go rune. Iterating
	// runes made the stand-ins of emoji and other non-BMP names differ from
	// Magpie even though ASCII addresses happened to match.
	for _, unit := range utf16.Encode([]rune(s)) {
		if unit == '@' || unit == '.' {
			b.WriteRune(rune(unit))
			continue
		}
		h = h*31 + uint32(unit)
		b.WriteByte(alphabet[h%uint32(len(alphabet))])
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func durationText(ms int64) string {
	if ms <= 0 {
		return "—"
	}
	if ms < 1000 {
		return itoa(int(ms)) + " ms"
	}
	if ms < 10_000 {
		return trimFixed(float64(ms)/1000, 1) + " s"
	}
	if ms < 60_000 {
		return trimFixed(float64(ms)/1000, 0) + " s"
	}
	m := ms / 60_000
	if m < 60 {
		return itoa(int(m)) + " min"
	}
	h := m / 60
	return itoa(int(h)) + " h " + itoa(int(m%60)) + " min"
}

func trimFixed(v float64, decimals int) string {
	s := strconvFormat(v, decimals)
	return s
}

func strconvFormat(v float64, decimals int) string {
	if decimals == 0 {
		return itoa(int(v + 0.5))
	}
	n := int(v*10 + 0.5)
	return itoa(n/10) + "." + itoa(n%10)
}

func clockText(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Local().Format("15:04:05")
	}
	return t.Local().Format("01-02 15:04")
}

func shortID(id int64) string {
	if id == 0 {
		return "—"
	}
	return "#" + itoa(int(id))
}

func sessionText(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	if len(s) <= 16 {
		return s
	}
	return s[:8] + "…" + s[len(s)-4:]
}

func sourceLabels(r routing.RouteView) []string {
	var out []string
	if r.Active {
		out = append(out, "live")
	}
	if r.Completeness.SessionTrace {
		out = append(out, "session")
	}
	if r.Completeness.History {
		out = append(out, "history")
	}
	return out
}

func statusKey(r routing.RouteView) string {
	switch r.DisplayStatus() {
	case "active":
		return "active"
	case "failed":
		return "failed"
	case "completed":
		return "completed"
	default:
		return "observed"
	}
}

func selected(loc Localization, key string) string {
	switch key {
	case "active":
		return loc.T("app.active")
	case "completed":
		return loc.T("app.completed")
	case "failed":
		return loc.T("app.failed")
	case "observed":
		return loc.T("app.observed")
	}
	return key
}

func elapsedLive(l routing.LiveRequest) int64 {
	if l.Millis > 0 {
		return l.Millis
	}
	if l.Started.IsZero() {
		return 0
	}
	return time.Since(l.Started).Milliseconds()
}
