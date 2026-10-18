package routing

import (
	"errors"
	"time"

	"github.com/Fan-chou/magpie-native-routing/internal/magpie"
)

// FromGatewayConcurrency converts the transport model without leaking HTTP
// types into UI state.
func FromGatewayConcurrency(c magpie.Concurrency) []LiveRequest {
	out := make([]LiveRequest, 0, len(c.Active))
	for _, a := range c.Active {
		out = append(out, LiveRequest{
			ID: a.ID, Agent: a.Agent, Model: a.Model, Provider: a.Provider,
			Session: a.Session, Started: a.Started, Millis: a.Millis,
			Phase: Phase(a.Phase), RouteID: a.RouteID, ObservedAt: c.ReadAt,
		})
	}
	return out
}

// FromGatewaySession converts a per-session route, returning false when the
// endpoint has no route yet.
func FromGatewaySession(resp magpie.RouteResponse) (SessionRouteLike, bool) {
	if resp.Route == nil || resp.Route.ID == 0 {
		return SessionRouteLike{}, false
	}
	r := resp.Route
	ttft := r.TTFT
	if ttft == 0 {
		ttft = r.TTFTOld
	}
	out := SessionRouteLike{
		ID: r.ID, Time: r.Time, Asked: r.Asked, Model: r.Model,
		Effort: r.Effort, Group: r.Group, Served: r.Served,
		Done: r.Done, Status: r.Status, Error: r.Error,
		Millis: r.Millis, TTFT: ttft,
	}
	for _, t := range r.Tries {
		tryTTFT := t.TTFT
		if tryTTFT == 0 {
			tryTTFT = t.TTFTMS
		}
		out.Tries = append(out.Tries, TryLike{
			Model: t.Model, Effort: t.Effort, Tier: t.Tier,
			TierServed: t.TierServed, Done: t.Done, Status: t.Status,
			Fail: t.Fail, Millis: t.Millis, TTFT: tryTTFT,
		})
	}
	return out, true
}

// DayFilesToDays makes UI-facing counts from history files. Counts are not
// fabricated: files are counted only by the reader that opened them.
func DayFilesToDays(files []DayFile, counts map[string]int) []HistoryDay {
	out := make([]HistoryDay, 0, len(files))
	for _, f := range files {
		out = append(out, HistoryDay{
			Day: f.Day, Requests: counts[f.Day], Size: f.Size, Modified: f.Modified,
		})
	}
	return out
}

// SafeErrorText keeps UI errors useful without including response bodies.
func SafeErrorText(err error) string {
	if err == nil {
		return ""
	}
	var api *magpie.APIError
	if errors.As(err, &api) {
		return api.Error()
	}
	return err.Error()
}

// RetryDelay gives a bounded exponential backoff, respecting Retry-After for
// server-throttled failures.
func RetryDelay(attempt int, err error) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 6 {
		attempt = 6
	}
	d := time.Second << attempt
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	var api *magpie.APIError
	if errors.As(err, &api) && api.RetryAfter > 0 && api.RetryAfter < 5*time.Minute {
		return api.RetryAfter
	}
	return d
}
