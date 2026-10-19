package ui

import (
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

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

// Model owns UI-only state. It never performs I/O and reads only immutable
// routing snapshots.
type Model struct {
	mu sync.RWMutex

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
	expandedTry  int
	listState    ui.ListState
	requestSel   int
	listRouteID  []int64
	syncListSel  bool

	historyDay string
	replay     replayController

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
	topology     topologyState
	lastSnapshot routing.Snapshot
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
	flights          []flightTrack
	frameAt          time.Time
	stageCanvas      *ui.Canvas
	stageCanvasKey   string
	stageCanvasW     int
	stageCanvasH     int
	stageCanvasScale float32
	flightGeometry   *flightGeometry
	reduceMotion     bool
	layout           topologyFrameLayout
}

// flightTrack is deliberately UI-only animation state. Its route id is the
// exact Magpie Route ID and every timing input comes from the observed route.
// It never creates a request that is absent from the snapshot.
type flightTrack struct {
	routeID   int64
	agent     string
	model     string
	provider  string
	accountID string
	who       string
	firstSeen time.Time
	lastSeen  time.Time
	removedAt time.Time
	doneAt    time.Time
	returnDur time.Duration
	observed  time.Duration
	total     time.Duration
	attempts  []flightAttempt
	phases    []flightPhase
	inReplay  bool
	failed    bool
	done      bool
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
	m.mu.Unlock()
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
	if m.tab == tabHistory {
		f.HistoryDay = m.historyDay
	}
	replay := m.replay
	selected := m.selectedID
	m.mu.RUnlock()
	routes := f.Apply(s.Routes)
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
			ID: id, Name: agentName(id), Model: model, Folder: folder,
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
	Active   bool
	Requests int
	Sessions int
	Hue      ui.Color
}

func agentName(id string) string {
	switch strings.ToLower(id) {
	case "codex":
		return "Codex"
	case "claude":
		return "Claude Code"
	case "opencode":
		return "OpenCode"
	case "other", "":
		return "Agent"
	default:
		return id
	}
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
	add := func(provider, name, who, model, accountID, plan, status, note string, weight string, current, failed bool) {
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
			return
		}
		seen[key] = len(out)
		out = append(out, providerNode{
			Provider: provider, Name: name, Who: who, Model: model, Plan: plan,
			AccountID: accountID, Status: status, Note: note, Weight: weight,
			Current: current, Failed: failed,
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
		if i < len(r.Tries) {
			tr := r.Tries[i]
			failed = tryFailed(tr)
			status = tryStatus(tr)
			note = routing.TryFailure(tr)
			current = !tr.Done && tr.Fail == "" && tr.Error == ""
		}
		if w.Barred {
			note = "barred"
		}
		add(provider, name, w.Who, w.Model, w.ID, w.Plan, status, note, w.Fixed, current, failed)
		if len(out) > i {
			out[i].Fallback = w.Fallback
			out[i].Rest = w.Rest != nil
			out[i].Answered = i < len(r.Tries) && r.Tries[i].Done && !tryFailed(r.Tries[i])
			out[i].WentOn = i < len(r.Tries) && r.Tries[i].Done && tryFailed(r.Tries[i]) && i+1 < len(r.Tries)
		}
	}
	for _, tr := range r.Tries {
		provider := providerOf(tr.Model)
		if provider == "" {
			provider = tr.ID
		}
		if len(r.Order) > 0 && providerMatchesOrder(r.Order, provider, tr.Model) {
			continue
		}
		status := tryStatus(tr)
		add(provider, provider, tr.Served, tr.Model, tr.ID, "", status, routing.TryFailure(tr), "",
			r.Active && !tr.Done && !tryFailed(tr), tryFailed(tr))
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
			add(provider, provider, "", r.Model, "", "", status, "", "",
				r.Active, r.Status >= 400 || r.Error != "")
		}
	}
	if len(r.Live) > 0 && len(out) == 0 {
		for _, l := range r.Live {
			// Concurrency exposes a session, not the provider account id.
			add(l.Provider, l.Provider, "", l.Model, "", "", string(l.Phase), "", "",
				true, false)
		}
	}
	if hidden {
		for i := range out {
			if out[i].Who != "" {
				out[i].Who = maskWho(out[i].Who)
			}
		}
	}
	return out
}

func providerMatchesOrder(order []routing.Weighed, provider, model string) bool {
	for _, w := range order {
		p := w.Provider
		if p == "" {
			p = providerOf(w.Model)
		}
		if p != "" && strings.EqualFold(p, provider) && (model == "" || w.Model == "" || strings.EqualFold(w.Model, model)) {
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
	Status    string
	Note      string
	Weight    string
	Current   bool
	Failed    bool
	// Fallback and Rest are actual fields of Magpie's Weighed/Try trace;
	// they let the stage distinguish fallback members and resting accounts.
	Fallback bool
	Rest     bool
	Answered bool
	WentOn   bool
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

func maskWho(s string) string {
	rs := []rune(s)
	if len(rs) <= 2 {
		return "••"
	}
	return string(rs[:1]) + strings.Repeat("•", min(len(rs)-1, 6))
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
