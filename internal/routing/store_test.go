package routing

import (
	"strings"
	"testing"
	"time"

	"github.com/Fan-chou/magpie-native-routing/internal/testutil"
)

func syncParse(t *testing.T, line string) Route {
	t.Helper()
	var got []Route
	parseLine([]byte(line), func(r Route) { got = append(got, r) })
	if len(got) != 1 {
		t.Fatalf("parsed %d routes", len(got))
	}
	return got[0]
}

func TestStoreMergesOnlyByRouteID(t *testing.T) {
	s := NewStore()
	now := time.Now()
	s.ApplyActive([]LiveRequest{
		{ID: 100, Agent: "codex", Session: "same", Model: "p/m", Started: now, RouteID: 7001},
		{ID: 101, Agent: "codex", Session: "same", Model: "p/m", Started: now, RouteID: 0},
	})
	s.MergeSession(SessionRouteLike{ID: 7001, Asked: "p/m", Model: "p/m", Done: false, Tries: []TryLike{{Model: "p/m"}}})

	snap := s.Snapshot()
	if len(snap.Routes) != 1 {
		t.Fatalf("routes = %+v", snap.Routes)
	}
	rv := snap.Routes[0]
	if rv.ID != 7001 || len(rv.Live) != 1 || rv.Live[0].ID != 100 {
		t.Fatalf("route/live = %+v", rv)
	}
	if !rv.HasSource(SourceLiveAPI) || !rv.HasSource(SourceSessionRoute) {
		t.Fatalf("sources = %v", rv.Sources)
	}
	orphans := s.Orphans()
	if len(orphans) != 1 || orphans[0].ID != 101 {
		t.Fatalf("orphans = %+v", orphans)
	}
}

func TestStoreHistoryCompletesWithoutForcedMatching(t *testing.T) {
	s := NewStore()
	s.ApplyActive([]LiveRequest{{ID: 9, Session: "unknown", Model: "p/m", Started: time.Now(), RouteID: 0}})
	s.MergeHistory([]Route{{ID: 9000, Time: time.Now(), Agent: "codex", Session: "other", Model: "p/m", Done: true}})
	snap := s.Snapshot()
	if len(snap.Routes) != 1 || len(snap.Live) != 1 {
		t.Fatalf("snapshot = %+v", snap)
	}
	if snap.Routes[0].ID != 9000 || len(snap.Routes[0].Live) != 0 {
		t.Fatalf("forced merge occurred: %+v", snap.Routes[0])
	}
	if got := s.Orphans(); len(got) != 1 {
		t.Fatalf("orphans = %+v", got)
	}
}

func TestStoreHistoryOverridesFullerTrace(t *testing.T) {
	s := NewStore()
	s.ApplyActive([]LiveRequest{{ID: 1, RouteID: 7001, Model: "p/m", Started: time.Now()}})
	s.MergeSession(SessionRouteLike{ID: 7001, Asked: "p/m", Model: "p/m", Tries: []TryLike{{Model: "p/m", Done: false}}})
	r := syncParse(t, testutil.SyntheticRouteJSON)
	s.MergeHistory([]Route{r})
	snap := s.Snapshot()
	if len(snap.Routes) != 1 {
		t.Fatalf("routes = %d", len(snap.Routes))
	}
	got := snap.Routes[0]
	if !got.Done || len(got.Order) != 2 || len(got.Tries) != 2 || !got.Completeness.Order || !got.Completeness.History || !got.Completeness.Live {
		t.Fatalf("merged route = %+v", got)
	}
	if got.Order[1].Provider != "backup" {
		t.Fatalf("order = %+v", got.Order)
	}
}

func TestStoreSnapshotSharesImmutableRoutes(t *testing.T) {
	s := NewStore()
	s.MergeHistory([]Route{{
		ID: 1, Time: time.Now(), Order: []Weighed{{ID: "a"}}, Tries: []Try{{ID: "a"}},
		Group: &GroupRef{ID: "g", Members: []string{"member-a"}},
	}})
	a := s.Snapshot()
	oldOrder := a.Routes[0].Order
	// A later update clones only the changed route. The earlier snapshot
	// remains stable, and updating a caller-owned input does not mutate the
	// route that was already stored.
	input := Route{ID: 1, Time: time.Now(), Order: []Weighed{{ID: "b"}}, Tries: []Try{{ID: "b"}}}
	s.MergeHistory([]Route{input})
	b := s.Snapshot()
	if a.Routes[0].Order[0].ID != oldOrder[0].ID || b.Routes[0].Order[0].ID != "b" {
		t.Fatalf("copy-on-write route snapshot changed: old=%+v new=%+v", a.Routes[0].Order, b.Routes[0].Order)
	}
	input.Order[0].ID = "caller-mutated"
	input.Tries[0].ID = "caller-mutated"
	c := s.Snapshot()
	if c.Routes[0].Order[0].ID != "b" || c.Routes[0].Tries[0].ID != "b" {
		t.Fatalf("stored route shares caller-owned slices: %+v", c.Routes[0])
	}
}

func TestStoreConcurrencySnapshotReplacesLive(t *testing.T) {
	s := NewStore()
	s.ApplyActive([]LiveRequest{{ID: 1, RouteID: 10}})
	s.ApplyActive([]LiveRequest{{ID: 2, RouteID: 20}})
	snap := s.Snapshot()
	if len(snap.Live) != 1 || snap.Live[0].ID != 2 {
		t.Fatalf("live = %+v", snap.Live)
	}
	if len(snap.Routes) != 2 {
		// The route records remain as a trace of what was observed, but only
		// ID 20 carries a live marker now.
		t.Fatalf("routes = %+v", snap.Routes)
	}
	if len(snap.Routes[0].Live)+len(snap.Routes[1].Live) != 1 {
		t.Fatalf("stale live marker: %+v", snap.Routes)
	}
}

func TestStoreSkipsNotificationsForVolatileOnlyPolls(t *testing.T) {
	s := NewStore()
	started := time.Now()
	notifications := 0
	s.OnChange(func() { notifications++ })
	s.ApplyActive([]LiveRequest{{
		ID: 1, RouteID: 10, Agent: "codex", Provider: "p",
		Started: started, Millis: 100, Phase: PhaseUpstream,
	}})
	if notifications != 1 {
		t.Fatalf("initial poll notifications = %d, want 1", notifications)
	}
	s.ApplyActive([]LiveRequest{{
		ID: 1, RouteID: 10, Agent: "codex", Provider: "p",
		Started: started, Millis: 1100, Phase: PhaseUpstream,
	}})
	if notifications != 1 {
		t.Fatalf("volatile ms poll rebuilt the view: %d notifications", notifications)
	}
	s.ApplyActive([]LiveRequest{{
		ID: 1, RouteID: 10, Agent: "codex", Provider: "p",
		Started: started, Millis: 1100, Phase: PhaseRPM,
	}})
	if notifications != 2 {
		t.Fatalf("phase change notifications = %d, want 2", notifications)
	}
	if got := s.Snapshot().Live[0].Millis; got != 1100 {
		t.Fatalf("latest observed ms = %d, want 1100", got)
	}
}

func TestStoreSkipsUnchangedHistoryAndConfigRevisions(t *testing.T) {
	s := NewStore()
	notifications := 0
	s.OnChange(func() { notifications++ })
	route := Route{
		ID: 17, Time: time.Date(2026, 10, 19, 2, 0, 0, 0, time.UTC),
		Agent: "codex", Model: "openai/gpt-6", Done: true, Status: 200,
	}
	s.MergeHistory([]Route{route})
	if notifications != 1 {
		t.Fatalf("first history merge notifications = %d, want 1", notifications)
	}
	// A tail scan and a full-day read can both return the same record. The
	// ReadAt and accumulated source metadata change, but no rendered field
	// does.
	repeated := cloneRoute(route)
	repeated.ReadAt = time.Now().Add(time.Minute)
	repeated.AddSource(SourceHistory)
	s.MergeHistory([]Route{repeated})
	if notifications != 1 {
		t.Fatalf("identical history append rebuilt the view: %d notifications", notifications)
	}
	s.ReplaceHistory([]Route{route})
	if notifications != 1 {
		t.Fatalf("identical history replacement rebuilt the view: %d notifications", notifications)
	}

	cfg := ConfigSnapshot{
		Providers: []ProviderInfo{{ID: "openai", Name: "OpenAI"}},
		Groups:    []GroupInfo{{ID: "g", Name: "Group", Members: []string{"openai/gpt-6"}}},
	}
	s.SetConfig(cfg)
	if notifications != 2 {
		t.Fatalf("first config read notifications = %d, want 2", notifications)
	}
	same := cloneConfig(cfg)
	same.ReadAt = time.Now().Add(time.Second)
	same.ModifiedAt = time.Now().Add(time.Second)
	s.SetConfig(same)
	if notifications != 2 {
		t.Fatalf("identical providers.json refresh rebuilt the view: %d notifications", notifications)
	}
	s.SetConfigError("temporary config read error")
	if notifications != 3 {
		t.Fatalf("config error notifications = %d, want 3", notifications)
	}
	s.SetConfig(same)
	if notifications != 4 {
		t.Fatalf("recovered identical config did not clear the stale error: %d notifications", notifications)
	}
	if got := s.Snapshot().ConfigError; got != "" {
		t.Fatalf("config error remained after recovery: %q", got)
	}
	changed := cloneConfig(cfg)
	changed.Groups[0].Members = append(changed.Groups[0].Members, "backup/gpt-6")
	s.SetConfig(changed)
	if notifications != 5 {
		t.Fatalf("changed config notifications = %d, want 5", notifications)
	}
}

func TestStoreRepeatedHistoryMergePreservesLiveSourcesWithoutRebuild(t *testing.T) {
	s := NewStore()
	notifications := 0
	s.OnChange(func() { notifications++ })

	started := time.Date(2026, 10, 19, 2, 0, 0, 0, time.UTC)
	s.ApplyActive([]LiveRequest{{
		ID: 1, RouteID: 17, Agent: "codex", Model: "openai/gpt-6",
		Started: started, Phase: PhaseUpstream,
	}})
	history := Route{
		ID: 17, Time: started, Agent: "codex", Model: "openai/gpt-6",
		Done: true, Status: 200,
	}
	s.MergeHistory([]Route{history})
	if notifications != 2 {
		t.Fatalf("initial live+history notifications = %d, want 2", notifications)
	}

	// The history watcher re-reads the same completed record while the live
	// concurrency poll still owns the same route. The incoming history copy
	// only carries its own source label; the store must union old provenance
	// before deciding whether anything visible changed.
	for range 3 {
		repeated := cloneRoute(history)
		repeated.ReadAt = time.Now()
		s.MergeHistory([]Route{repeated})
	}
	if notifications != 2 {
		t.Fatalf("repeated live+history merge rebuilt the view: %d notifications", notifications)
	}
	got := s.Snapshot().Routes[0]
	if !got.HasSource(SourceLiveAPI) || !got.HasSource(SourceHistory) {
		t.Fatalf("merged sources = %v", got.Sources)
	}
}

func TestStoreConcurrencyKeepsCompletedHistory(t *testing.T) {
	s := NewStore()
	s.MergeHistory([]Route{{
		ID: 7, Time: time.Now().Add(-time.Minute), Agent: "codex",
		Model: "p/m", Done: true, Status: 200,
	}})
	s.ApplyActive([]LiveRequest{{ID: 2, RouteID: 20, Model: "p/m", Started: time.Now()}})
	snap := s.Snapshot()
	if len(snap.Routes) != 2 {
		t.Fatalf("routes after poll = %d, want history plus live", len(snap.Routes))
	}
}

func TestErrTextNoSecrets(t *testing.T) {
	e := &APIErrorText{Text: "gateway is unreachable"}
	if strings.Contains(SafeErrorText(e), "secret") {
		t.Fatal("unsafe")
	}
}

type APIErrorText struct{ Text string }

func (e *APIErrorText) Error() string { return e.Text }
