package ui

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Fan-chou/magpie-native-routing/internal/routing"
)

// routeOutcome is client.js's outcome(): the thing that answered, or the
// request still in flight, and the class that colours its row. It is
// deliberately based on the last recorded try, not the first candidate.
func routeOutcome(rv routing.RouteView) (said, how string, tr routing.Try) {
	if !rv.Done && !rv.Completeness.History {
		if len(rv.Tries) > 0 {
			last := rv.Tries[len(rv.Tries)-1]
			return routeTryWho(rv, last) + " is answering…", "wait", last
		}
		if len(rv.Live) > 0 {
			last := rv.Live[len(rv.Live)-1]
			return firstNonEmpty(last.Provider, rv.DisplayProvider(), "routing…") + " is answering…", "wait", routing.Try{}
		}
		return "routing…", "wait", routing.Try{}
	}
	if rv.Status < 400 && rv.Error == "" {
		for _, try := range rv.Tries {
			if try.Done && !tryFailed(try) {
				said := routeEndpointText(rv, try)
				if said == "" {
					said = firstNonEmpty(rv.DisplayProvider(), "—")
				}
				how := "ok"
				if len(rv.Tries) > 1 {
					how = "moved"
				}
				return said, how, try
			}
		}
		if len(rv.Tries) == 0 {
			return firstNonEmpty(rv.DisplayProvider(), "—"), "ok", routing.Try{}
		}
	}
	last := routing.Try{}
	if len(rv.Tries) > 0 {
		last = rv.Tries[len(rv.Tries)-1]
	}
	if last.Status > 0 {
		return itoa(last.Status) + " · " + failWord(last.Fail), "bad", last
	}
	return strings.TrimSpace(itoa(rv.Status) + " " + routing.TryFailure(last)), "bad", last
}

// routeTryWho is the identity client.js's who() shows for a weighted seat.
// A provider-kind row is named by its provider; an account or key is named
// by Who, the human-facing account, never by its internal id.
func routeTryWho(rv routing.RouteView, tr routing.Try) string {
	if len(rv.Order) > 0 {
		target := traceAttemptTarget(rv, tr)
		for _, w := range rv.Order {
			if providerMatchScore(providerNode{
				Provider: firstNonEmpty(w.Provider, providerOf(w.Model)), Model: w.Model,
				AccountID: w.ID, Who: w.Who, Name: w.Name,
			}, target) <= 0 {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(w.Kind), "provider") {
				return firstNonEmpty(w.Name, w.Provider, w.ID)
			}
			if w.Kind == "key" {
				return firstNonEmpty(w.Who, w.Name, w.ID, w.Provider)
			}
			return firstNonEmpty(w.Who, w.Name, w.Provider, w.ID)
		}
		if target.who != "" {
			return target.who
		}
	}
	return firstNonEmpty(tr.ID, providerOf(tr.Model), tr.Model)
}

// routeEndpointText preserves the original row's two-part destination:
// the account (or provider) that answered, then the model that served it.
// It never emits the same token twice when the trace repeats an id as model.
func routeEndpointText(rv routing.RouteView, tr routing.Try) string {
	who := strings.TrimSpace(routeTryWho(rv, tr))
	model := strings.TrimSpace(firstNonEmpty(tr.Served, tr.Model, rv.Served))
	if model != "" {
		model = modelAfterProvider(model, firstNonEmpty(providerOf(tr.Model), rv.Provider))
	}
	if who == "" {
		return model
	}
	if model == "" || strings.EqualFold(who, model) {
		return who
	}
	return who + " · " + model
}

func modelAfterProvider(model, provider string) string {
	if provider != "" && strings.HasPrefix(strings.ToLower(model), strings.ToLower(provider)+"/") {
		return model[len(provider)+1:]
	}
	return model
}

// routePromptCache is client.js's promptOf(). Cache reads are counted
// across every billable try; the denominator includes cache writes.
func routePromptCache(rv routing.RouteView) (read, prompt int) {
	for _, u := range rv.Usage {
		read += u.CacheRead
		prompt += u.Input + u.CacheRead + u.CacheWrite
	}
	return read, prompt
}

type decodeValue struct {
	n int
	w int64
}

// routeDecodeFor is routeDecode with the route's output and reasoning
// fields. It mirrors usage.DecodeOf as client.js restates it.
func routeDecodeFor(rv routing.RouteView, ms, ttft, firstText, flow int64, reasoning int, reasoningHidden bool) (decodeValue, bool) {
	if reasoningHidden && firstText == 0 {
		return decodeValue{}, false
	}
	visible := reasoning > 0 && firstText == 0 && flow > 0 && reasoning <= rv.Output
	n := rv.Output
	if n == 0 && len(rv.Usage) > 0 {
		n = rv.Usage[len(rv.Usage)-1].Output
	}
	if reasoning > 0 && !visible {
		n -= reasoning
	}
	from := ttft
	if visible {
		from = rv.FirstReasoning
	} else if reasoning > 0 {
		from = firstText
	}
	w := ms - from
	if !(n > 0 && ttft > 0 && from > 0 && w >= 100 && int64(n)*1000 <= 10000*w) {
		return decodeValue{}, false
	}
	if flow > 0 && flow*4 < w {
		return decodeValue{}, false
	}
	if reasoning > 0 && firstText > 0 && int64(n)*(firstText-ttft) > 20*int64(reasoning)*w {
		return decodeValue{}, false
	}
	return decodeValue{n: n, w: w}, true
}

func routeSpeed(rv routing.RouteView, how string) int {
	if !rv.Done || how == "bad" {
		return 0
	}
	reasoning := rv.Reasoning
	if reasoning == 0 && len(rv.Usage) > 0 {
		reasoning = rv.Usage[len(rv.Usage)-1].Reasoning
	}
	d, ok := routeDecodeFor(rv, rv.Millis, rv.TTFT, rv.FirstText, rv.Flow, reasoning, rv.ReasoningHidden)
	if !ok {
		return 0
	}
	return int(math.Round(float64(d.n) / (float64(d.w) / 1000)))
}

func routeMetricTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return ctxNumber(float64(n)/1_000_000, 1) + "M"
	case n >= 1_000:
		return ctxNumber(float64(n)/1_000, 1) + "k"
	default:
		return itoa(n)
	}
}

type routeMetric struct {
	key, label, value string
}

type routeMetricOption struct {
	key       string
	labelKey  string
	available bool
}

var routeMetricOptions = []routeMetricOption{
	{key: "duration", labelKey: "metrics.duration", available: true},
	{key: "ttft", labelKey: "metrics.firstToken", available: true},
	{key: "tokens", labelKey: "metrics.tokens", available: true},
	{key: "cache", labelKey: "metrics.cache", available: true},
	{key: "speed", labelKey: "metrics.speed", available: true},
	// Magpie's public Route/history has no price catalog. Keep the original
	// menu entry visible but disabled instead of inventing a cost.
	{key: "cost", labelKey: "metrics.cost", available: false},
}

func routeMetrics(rv routing.RouteView, how string, loc Localization) []routeMetric {
	out := make([]routeMetric, 0, 6)
	if rv.Done || rv.Completeness.History {
		if rv.Millis > 0 {
			out = append(out, routeMetric{"duration", loc.T("metrics.duration"), routeTook(rv.Millis, loc)})
		}
		if rv.TTFT > 0 {
			out = append(out, routeMetric{"ttft", loc.T("metrics.firstToken"), routeTook(rv.TTFT, loc)})
		}
		if rv.Tokens > 0 {
			out = append(out, routeMetric{"tokens", loc.T("metrics.tokens"), routeMetricTokens(rv.Tokens)})
		}
		read, prompt := routePromptCache(rv)
		if prompt > 0 {
			out = append(out, routeMetric{"cache", loc.T("metrics.cache"), trimFixed(float64(read)/float64(prompt)*100, 0) + "%"})
		}
		if speed := routeSpeed(rv, how); speed > 0 {
			out = append(out, routeMetric{"speed", loc.T("metrics.speed"), loc.F("metrics.tokensPerSecond", speed)})
		}
	}
	return out
}

func (m *Model) visibleRouteMetrics(rv routing.RouteView, how string, loc Localization) []routeMetric {
	all := routeMetrics(rv, how, loc)
	out := make([]routeMetric, 0, len(all))
	for _, metric := range all {
		if metric.key != "cost" && m.metricVisible(metric.key) {
			out = append(out, metric)
		}
	}
	return out
}

// routeTook formats a duration as the original tok/cost display does.
func routeTook(ms int64, loc Localization) string {
	if ms <= 0 {
		return "—"
	}
	if ms < 1000 {
		return loc.F("metrics.ms", int(ms))
	}
	if ms < 10_000 {
		return loc.F("metrics.seconds", ctxNumber(float64(ms)/1000, 1))
	}
	if ms < 60_000 {
		return loc.F("metrics.seconds", ctxNumber(float64(ms)/1000, 0))
	}
	return loc.F("metrics.minutes", trimFixed(float64(ms)/60000, 0))
}

// routeAccountTally is renderActs' data: every account or key the listed
// requests weighed, how often it was tried and answered, and the latest
// rest state. It is derived only from order and tries; no quota or account
// is invented.
type routeAccountTally struct {
	Provider string
	Name     string
	Who      string
	Model    string
	Plan     string
	Kind     string
	Icon     string
	Routing  string
	Known    bool
	Used     float64
	Current  bool
	Failed   bool
	Answered bool
	Rest     *routing.Rest
	Seen     int
	Tried    int
	OK       int
	Failures int
	LastAt   time.Time
	// ObservedAt is Route.Time of the latest request that weighed this
	// seat, the moment its quota reading belongs to.
	ObservedAt time.Time
}

func accountTally(routes []routing.RouteView) []routeAccountTally {
	by := map[string]*routeAccountTally{}
	// Oldest first so the latest observation wins, exactly as renderActs'
	// reverse loop does.
	ordered := append([]routing.RouteView(nil), routes...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Time.Before(ordered[j].Time) })
	for _, rv := range ordered {
		cand := map[string]*routeAccountTally{}
		for i, w := range rv.Order {
			provider := firstNonEmpty(w.Provider, providerOf(w.Model))
			if provider == "" && w.ID == "" {
				continue
			}
			key := accountTallyKey(provider, w.ID, w.Kind, w.Model)
			a := by[key]
			if a == nil {
				a = &routeAccountTally{
					Provider: provider, Name: w.Name, Who: w.Who, Model: w.Model,
					Icon: firstNonEmpty(w.Icon, w.Preset), Kind: w.Kind, Routing: w.Routing,
				}
				by[key] = a
			}
			a.Seen++
			a.Current = i == 0 && rv.Active
			// renderActs keeps the latest seat it saw: its plan, model,
			// allowance reading and rest state all describe that moment.
			a.Name, a.Who, a.Model = w.Name, w.Who, w.Model
			a.Plan, a.Kind, a.Routing = w.Plan, w.Kind, w.Routing
			a.Icon = firstNonEmpty(w.Icon, w.Preset)
			a.Known, a.Used = w.Known, w.Used
			a.ObservedAt = rv.Time
			a.Rest = w.Rest
			k := w.ID + "\x00" + w.Model
			cand[k] = a
		}
		for _, try := range rv.Tries {
			key := matchAccountTry(rv, try)
			a := cand[key]
			if a == nil {
				// renderActs only updates accounts that appeared in the
				// recorded Order. A try without a seat is not a new
				// account; inventing one produced the duplicate
				// "qoder · dfmodel" row.
				continue
			}
			if !try.Done || try.Fail == "canceled" {
				continue
			}
			a.Tried++
			if !tryFailed(try) {
				a.OK++
				a.Answered = true
				at := try.Start.Add(time.Duration(max(try.Millis, 0)) * time.Millisecond)
				if at.After(a.LastAt) {
					a.LastAt = at
				}
				if try.Model != "" && a.Model == "" {
					a.Model = try.Model
				}
			} else {
				a.Failures++
				a.Failed = true
			}
		}
	}
	out := make([]routeAccountTally, 0, len(by))
	for _, a := range by {
		if a.Tried == 0 && a.Seen == 0 {
			continue
		}
		out = append(out, *a)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func accountTallyKey(provider, accountID, kind, model string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if accountID = strings.TrimSpace(accountID); accountID != "" {
		return provider + "\x00" + strings.ToLower(accountID)
	}
	return provider + "\x00" + strings.ToLower(strings.TrimSpace(kind)) + "\x00" + strings.ToLower(strings.TrimSpace(model))
}

// matchAccountTry maps the try to the seat it used. The same identity
// rules as traceAttemptTarget prevent same-provider models or different
// account IDs from being folded together.
func matchAccountTry(rv routing.RouteView, tr routing.Try) string {
	target := traceAttemptTarget(rv, tr)
	for _, w := range rv.Order {
		if providerMatchScore(providerNode{
			Provider: firstNonEmpty(w.Provider, providerOf(w.Model)), Model: w.Model,
			AccountID: w.ID, Who: w.Who, Name: w.Name,
		}, target) > 0 {
			return w.ID + "\x00" + w.Model
		}
	}
	return ""
}
