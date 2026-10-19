// Package routing contains Magpie's route data and the read-only model that
// combines live Gateway observations with completed history files.
package routing

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Source identifies where a fact came from. A route can carry several
// sources after live and completed data have been merged.
type Source string

const (
	SourceLiveAPI      Source = "live-api"
	SourceSessionRoute Source = "session-route"
	SourceHistory      Source = "completed-history"
	SourceStaticConfig Source = "static-config"
)

// Phase is where a live request currently is, as the Gateway reports it.
type Phase string

const (
	PhaseSlot     Phase = "slot"
	PhaseRPM      Phase = "rpm"
	PhaseUpstream Phase = "upstream"
)

// LiveRequest is one row returned by GET /v1/magpie/concurrency.
//
// ID is Magpie's transient in-flight counter. RouteID is the routing trace's
// ID when the request has already been routed. They are deliberately kept
// apart: ID must never be used as a Route ID.
type LiveRequest struct {
	ID          int64     `json:"id"`
	Agent       string    `json:"agent"`
	Model       string    `json:"model,omitempty"`
	Provider    string    `json:"provider,omitempty"`
	Session     string    `json:"session,omitempty"`
	Started     time.Time `json:"started"`
	Millis      int64     `json:"ms"`
	Phase       Phase     `json:"phase,omitempty"`
	RouteID     int64     `json:"route,omitempty"`
	ObservedAt  time.Time `json:"-"`
	Sequence    int64     `json:"-"`
	FromSession bool      `json:"-"`
}

// Route is an immutable afterParse Route. Unknown JSON fields are retained in
// Extra so diagnostics and newer Magpie versions do not silently lose data.
type Route struct {
	Seq       int64      `json:"seq,omitempty"`
	ID        int64      `json:"id"`
	Time      time.Time  `json:"time"`
	Agent     string     `json:"agent"`
	Session   string     `json:"session,omitempty"`
	Kind      string     `json:"kind,omitempty"`
	Parent    string     `json:"parentSession,omitempty"`
	TitleLink *TitleLink `json:"titleLink,omitempty"`
	// ParentMatched is Magpie's display inference, not an agent-supplied
	// parent session. Keep it separate from Parent.
	ParentMatched bool `json:"parentMatched,omitempty"`

	Model    string        `json:"model"`
	Effort   string        `json:"effort,omitempty"`
	Tier     string        `json:"tier,omitempty"`
	Provider string        `json:"provider,omitempty"`
	Group    *GroupRef     `json:"group,omitempty"`
	Rule     *RuleHit      `json:"rule,omitempty"`
	Nested   []NestedRule  `json:"nested,omitempty"`
	For      *CallFor      `json:"for,omitempty"`
	Subagent *SubagentPick `json:"subagent,omitempty"`
	Affinity *Affinity     `json:"affinity,omitempty"`

	Order  []Weighed `json:"order,omitempty"`
	Left   []Weighed `json:"left,omitempty"`
	Tries  []Try     `json:"tries,omitempty"`
	Done   bool      `json:"done"`
	Status int       `json:"status,omitempty"`
	Error  string    `json:"error,omitempty"`

	Millis          int64        `json:"ms,omitempty"`
	TTFT            int64        `json:"ttft,omitempty"`
	TTFTMS          int64        `json:"ttft_ms,omitempty"`
	FirstText       int64        `json:"firstText,omitempty"`
	FirstReasoning  int64        `json:"firstReasoning,omitempty"`
	ReasoningHidden bool         `json:"reasoningHidden,omitempty"`
	Flow            int64        `json:"flow,omitempty"`
	Sent            int64        `json:"sent,omitempty"`
	Own             int64        `json:"own,omitempty"`
	Retried         int64        `json:"retried,omitempty"`
	Queued          int64        `json:"queued,omitempty"`
	Tokens          int          `json:"tokens,omitempty"`
	Output          int          `json:"out,omitempty"`
	Reasoning       int          `json:"reasoning,omitempty"`
	Usage           []RouteUsage `json:"usage,omitempty"`
	Served          string       `json:"served,omitempty"`
	Swapped         bool         `json:"swapped,omitempty"`
	Routed          bool         `json:"routed,omitempty"`
	Upstream        string       `json:"upstream,omitempty"`

	Folder         string  `json:"folder,omitempty"`
	Prompt         *Prompt `json:"prompt,omitempty"`
	Archive        string  `json:"archive,omitempty"`
	Conv           string  `json:"conv,omitempty"`
	Pinned         string  `json:"pinned,omitempty"`
	Sub            string  `json:"sub,omitempty"`
	SubagentID     string  `json:"subagentId,omitempty"`
	SubagentEffort string  `json:"subagentEffort,omitempty"`
	SealedTask     bool    `json:"sealedTask,omitempty"`
	LeadAccount    string  `json:"leadAccount,omitempty"`
	Compaction     string  `json:"compaction,omitempty"`

	// Extra holds unknown top-level fields. It is never rendered as a prompt
	// or request body; callers may inspect it for diagnostics.
	Extra map[string]json.RawMessage `json:"-"`

	// Local metadata, filled by the reader and store.
	Sources    []Source  `json:"-"`
	ReadAt     time.Time `json:"-"`
	HistoryDay string    `json:"-"`
}

// UnmarshalJSON decodes a stored trace and retains top-level fields this
// version does not know. Raw request and response bodies are not part of the
// routing trace and are never persisted by this app; unknown metadata remains
// available for diagnostics.
func (r *Route) UnmarshalJSON(data []byte) error {
	type routeAlias Route
	var decoded routeAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	known := []string{
		"seq", "id", "time", "agent", "session", "kind", "parentSession",
		"titleLink", "parentMatched", "model", "effort", "tier", "provider",
		"group", "rule", "nested", "for", "subagent", "affinity", "order",
		"left", "tries", "done", "status", "error", "ms", "ttft", "ttft_ms", "firstText",
		"firstReasoning", "reasoningHidden", "flow", "sent", "own", "retried",
		"queued", "tokens", "out", "reasoning", "usage", "served", "swapped",
		"routed", "upstream", "folder", "prompt", "archive", "conv", "pinned",
		"sub", "subagentId", "subagentEffort", "sealedTask", "leadAccount",
		"compaction",
	}
	for _, name := range known {
		delete(fields, name)
	}
	if len(fields) > 0 {
		decoded.Extra = fields
	}
	if decoded.TTFT == 0 && decoded.TTFTMS > 0 {
		decoded.TTFT = decoded.TTFTMS
	}
	for i := range decoded.Tries {
		if decoded.Tries[i].TTFT == 0 && decoded.Tries[i].TTFTMS > 0 {
			decoded.Tries[i].TTFT = decoded.Tries[i].TTFTMS
		}
	}
	*r = Route(decoded)
	return nil
}

// TitleLink is display evidence found in completed history.
type TitleLink struct {
	Prompt string `json:"prompt,omitempty"`
	Scope  string `json:"scope,omitempty"`
}

// GroupRef is a route's group as it was when the request ran.
type GroupRef struct {
	ID       string     `json:"id"`
	Name     string     `json:"name,omitempty"`
	Routing  string     `json:"routing,omitempty"`
	Affinity string     `json:"affinity,omitempty"`
	Auto     bool       `json:"auto,omitempty"`
	Sink     bool       `json:"sink,omitempty"`
	Members  []string   `json:"members,omitempty"`
	Via      []string   `json:"via,omitempty"`
	Fast     []string   `json:"fast,omitempty"`
	Subs     []SubGroup `json:"subs,omitempty"`
}

// SubGroup is a group nested in a route group.
type SubGroup struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Routing string `json:"routing,omitempty"`
	In      string `json:"in,omitempty"`
	Rules   int    `json:"rules,omitempty"`
}

// CallFor is the request a side call Magpie made on its own served.
type CallFor struct {
	Agent        string `json:"agent"`
	Model        string `json:"model"`
	Unknown      bool   `json:"unknown,omitempty"`
	Missing      string `json:"missing,omitempty"`
	MCP          bool   `json:"mcp,omitempty"`
	Instead      string `json:"instead,omitempty"`
	Why          string `json:"why,omitempty"`
	NothingFound bool   `json:"nothingFound,omitempty"`
	NotSearched  bool   `json:"notSearched,omitempty"`
	Picked       bool   `json:"picked,omitempty"`
}

// SubagentPick records a model override for a subagent request.
type SubagentPick struct {
	Asked string `json:"asked"`
	To    string `json:"to"`
	Kept  string `json:"kept,omitempty"`
}

// Affinity describes the conversation stickiness Magpie observed.
type Affinity struct {
	Mode       string    `json:"mode"`
	Turn       int       `json:"turn"`
	Within     bool      `json:"within,omitempty"`
	Last       string    `json:"last,omitempty"`
	LastTurn   int       `json:"lastTurn,omitempty"`
	At         time.Time `json:"at,omitempty"`
	CacheRead  int       `json:"cacheRead,omitempty"`
	CacheWrite int       `json:"cacheWrite,omitempty"`
	Kept       bool      `json:"kept,omitempty"`
	Why        string    `json:"why"`
}

// Prompt is the non-text context breakdown recorded by Magpie. Item names
// and paths are retained; no prompt text is present in history.
type Prompt struct {
	Window  int          `json:"window,omitempty"`
	Tokens  int          `json:"tokens"`
	Counted bool         `json:"counted,omitempty"`
	Held    bool         `json:"held,omitempty"`
	Turns   int          `json:"turns,omitempty"`
	Parts   []PromptPart `json:"parts,omitempty"`
}

type PromptPart struct {
	Kind   string       `json:"kind"`
	Tokens int          `json:"tokens"`
	Items  []PromptItem `json:"items,omitempty"`
}

type PromptItem struct {
	Name   string `json:"name"`
	Tag    string `json:"tag,omitempty"`
	N      int    `json:"n,omitempty"`
	Tokens int    `json:"tokens"`
}

// RuleHit describes the group rule that matched a request.
type RuleHit struct {
	N          int         `json:"n"`
	Use        string      `json:"use,omitempty"`
	When       []string    `json:"when,omitempty"`
	Held       bool        `json:"held,omitempty"`
	Waits      bool        `json:"waits,omitempty"`
	Grown      bool        `json:"grown,omitempty"`
	Turn       int         `json:"turn,omitempty"`
	Tokens     int         `json:"tokens,omitempty"`
	Images     bool        `json:"images,omitempty"`
	Effort     string      `json:"effort,omitempty"`
	Folder     string      `json:"folder,omitempty"`
	Unready    bool        `json:"unready,omitempty"`
	Then       []string    `json:"then,omitempty"`
	Instead    string      `json:"instead,omitempty"`
	Pick       string      `json:"pick,omitempty"`
	Wanted     string      `json:"wanted,omitempty"`
	Compact    bool        `json:"compact,omitempty"`
	Small      []string    `json:"small,omitempty"`
	Bare       bool        `json:"bare,omitempty"`
	UseEffort  string      `json:"useEffort,omitempty"`
	Classified *Classified `json:"classified,omitempty"`
}

// Classified is a routing classifier's observed decision.
type Classified struct {
	By      string   `json:"by,omitempty"`
	Intents []string `json:"intents,omitempty"`
	Intent  string   `json:"intent,omitempty"`
	After   string   `json:"after,omitempty"`
	Effort  string   `json:"effort,omitempty"`
	Score   float64  `json:"score,omitempty"`
	Sure    float64  `json:"sure,omitempty"`
	Err     string   `json:"error,omitempty"`
}

// NestedRule is a nested group's rule outcome.
type NestedRule struct {
	Group string   `json:"group"`
	Name  string   `json:"name,omitempty"`
	Rule  *RuleHit `json:"rule,omitempty"`
}

// Weighed is an account or key in the actual candidate order recorded by
// Magpie. Used is the vendor's percentage from 0 to 100, as the routing
// history records it; unavailable fields are zero values.
type Weighed struct {
	ID         string      `json:"id"`
	Provider   string      `json:"provider,omitempty"`
	Name       string      `json:"name,omitempty"`
	Icon       string      `json:"icon,omitempty"`
	Preset     string      `json:"preset,omitempty"`
	Who        string      `json:"who,omitempty"`
	Kind       string      `json:"kind,omitempty"`
	Agent      string      `json:"agent,omitempty"`
	Plan       string      `json:"plan,omitempty"`
	Model      string      `json:"model,omitempty"`
	Fixed      string      `json:"fixed,omitempty"`
	Fast       bool        `json:"fast,omitempty"`
	Routing    string      `json:"routing,omitempty"`
	Fallback   bool        `json:"fallback,omitempty"`
	Shared     bool        `json:"shared,omitempty"`
	Known      bool        `json:"known,omitempty"`
	Used       float64     `json:"used,omitempty"`
	Limit      float64     `json:"limit,omitempty"`
	Unit       string      `json:"unit,omitempty"`
	Turn       bool        `json:"turn,omitempty"`
	Barred     bool        `json:"barred,omitempty"`
	Held       bool        `json:"held,omitempty"`
	Capped     int         `json:"capped,omitempty"`
	NoCredits  bool        `json:"noCredits,omitempty"`
	Unlisted   bool        `json:"unlisted,omitempty"`
	Learns     bool        `json:"learns,omitempty"`
	Amount     float64     `json:"amount,omitempty"`
	Pace       float64     `json:"pace,omitempty"`
	Tokens     float64     `json:"tokens,omitempty"`
	Fit        int         `json:"fit,omitempty"`
	Speaks     string      `json:"speaks,omitempty"`
	Rank       int         `json:"rank,omitempty"`
	Aside      bool        `json:"aside,omitempty"`
	NoURL      string      `json:"noURL,omitempty"`
	Via        []string    `json:"via,omitempty"`
	Renews     []time.Time `json:"renews,omitempty"`
	Restarts   *time.Time  `json:"restarts,omitempty"`
	Sunk       *time.Time  `json:"sunk,omitempty"`
	CapBack    *time.Time  `json:"capBack,omitempty"`
	Rest       *Rest       `json:"rest,omitempty"`
	Due        *time.Time  `json:"due,omitempty"`
	UsedRenews *time.Time  `json:"usedRenews,omitempty"`
}

// Try is one actual attempt recorded for a route.
type Try struct {
	ID              string     `json:"id"`
	Model           string     `json:"model,omitempty"`
	Effort          string     `json:"effort,omitempty"`
	Picked          bool       `json:"picked,omitempty"`
	Fixed           string     `json:"fixed,omitempty"`
	Fast            bool       `json:"fast,omitempty"`
	Start           time.Time  `json:"start,omitempty"`
	Done            bool       `json:"done"`
	Status          int        `json:"status,omitempty"`
	Millis          int64      `json:"ms,omitempty"`
	TTFT            int64      `json:"ttft,omitempty"`
	TTFTMS          int64      `json:"ttft_ms,omitempty"`
	FirstText       int64      `json:"firstText,omitempty"`
	Flow            int64      `json:"flow,omitempty"`
	Served          string     `json:"served,omitempty"`
	Swapped         bool       `json:"swapped,omitempty"`
	Routed          bool       `json:"routed,omitempty"`
	Upstream        string     `json:"upstream,omitempty"`
	Tier            string     `json:"tier,omitempty"`
	TierServed      string     `json:"tierServed,omitempty"`
	Fail            string     `json:"fail,omitempty"`
	Error           string     `json:"error,omitempty"`
	Rest            *Rest      `json:"rest,omitempty"`
	Again           int64      `json:"again,omitempty"`
	Replan          int        `json:"replan,omitempty"`
	Patience        int        `json:"patience,omitempty"`
	Gone            bool       `json:"gone,omitempty"`
	Earlier         bool       `json:"earlier,omitempty"`
	Queued          int64      `json:"queued,omitempty"`
	RuleEffort      string     `json:"ruleEffort,omitempty"`
	FirstReasoning  int64      `json:"firstReasoning,omitempty"`
	ReasoningHidden bool       `json:"reasoningHidden,omitempty"`
	Net             *TryNet    `json:"net,omitempty"`
	Reset           *AutoReset `json:"reset,omitempty"`
	Auto            []AutoPick `json:"auto,omitempty"`
}

// TryNet is where a try's time went up to its first byte, in milliseconds.
type TryNet struct {
	Own     float64 `json:"own,omitempty"`
	Before  float64 `json:"before,omitempty"`
	Asks    int     `json:"asks,omitempty"`
	Pool    float64 `json:"pool,omitempty"`
	DNS     float64 `json:"dns,omitempty"`
	Connect float64 `json:"connect,omitempty"`
	TLS     float64 `json:"tls,omitempty"`
	Reused  bool    `json:"reused,omitempty"`
	Send    float64 `json:"send,omitempty"`
	Wait    float64 `json:"wait,omitempty"`
	Head    float64 `json:"head,omitempty"`
}

type AutoReset struct {
	Who   string `json:"who"`
	Text  string `json:"text"`
	Agent string `json:"agent,omitempty"`
}

type AutoPick struct {
	Model   string `json:"model"`
	Via     string `json:"via"`
	Skipped string `json:"skipped,omitempty"`
	Refused string `json:"refused,omitempty"`
	API     string `json:"api,omitempty"`
	Session bool   `json:"session,omitempty"`
}

// Rest records why an account or try sits out.
type Rest struct {
	Why      string    `json:"why,omitempty"`
	Status   int       `json:"status,omitempty"`
	Until    time.Time `json:"until,omitempty"`
	By       string    `json:"by,omitempty"`
	Failures int       `json:"failures,omitempty"`
	Model    string    `json:"model,omitempty"`
	Link     string    `json:"link,omitempty"`
}

// RouteUsage is token usage for one billable attempt.
type RouteUsage struct {
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Input      int    `json:"in"`
	Output     int    `json:"out"`
	CacheRead  int    `json:"cache_read,omitempty"`
	CacheWrite int    `json:"cache_write,omitempty"`
	Reasoning  int    `json:"reasoning,omitempty"`
}

// RouteView is the UI-independent route presentation with provenance.
type RouteView struct {
	Route
	Live         []LiveRequest
	Completeness Completeness
	// Active is true only when this frame's Gateway snapshot currently
	// contains this route. A stale SourceLiveAPI label alone is not enough.
	Active bool
	// CandidateOrderKnown is true only when the completed history or a
	// session trace actually supplied the order. A live-only row leaves it
	// false even when the gateway knows which provider is currently serving.
	CandidateOrderKnown bool
}

// Completeness describes what was actually observed.
type Completeness struct {
	Live         bool
	SessionTrace bool
	History      bool
	Order        bool
	Tries        bool
	Usage        bool
}

// HasSource reports whether s is present.
func (r Route) HasSource(s Source) bool {
	for _, x := range r.Sources {
		if x == s {
			return true
		}
	}
	return false
}

// AddSource adds s once.
func (r *Route) AddSource(s Source) {
	if !r.HasSource(s) {
		r.Sources = append(r.Sources, s)
	}
}

// SortRoutes orders newest first, then by ID.
func SortRoutes(rs []RouteView) {
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].Time.Equal(rs[j].Time) {
			return rs[i].ID > rs[j].ID
		}
		return rs[i].Time.After(rs[j].Time)
	})
}

// DisplayProvider returns the provider or the provider part of a model name.
func (r Route) DisplayProvider() string {
	if r.Provider != "" {
		return r.Provider
	}
	if i := strings.IndexByte(r.Model, '/'); i > 0 {
		return r.Model[:i]
	}
	return ""
}
