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

func TestStoreSnapshotCopiesSlices(t *testing.T) {
	s := NewStore()
	s.MergeHistory([]Route{{ID: 1, Time: time.Now(), Order: []Weighed{{ID: "a"}}}})
	a := s.Snapshot()
	a.Routes[0].Order[0].ID = "mutated"
	b := s.Snapshot()
	if b.Routes[0].Order[0].ID != "a" {
		t.Fatal("snapshot shares mutable route slices")
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

func TestErrTextNoSecrets(t *testing.T) {
	e := &APIErrorText{Text: "gateway is unreachable"}
	if strings.Contains(SafeErrorText(e), "secret") {
		t.Fatal("unsafe")
	}
}

type APIErrorText struct{ Text string }

func (e *APIErrorText) Error() string { return e.Text }
