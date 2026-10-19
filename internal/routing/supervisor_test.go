package routing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Fan-chou/magpie-native-routing/internal/magpie"
)

// Two active sessions whose latest routes carry different seqs must not look
// like a Gateway restart: each session is compared against its own last seq.
func TestPollOnceDoesNotTreatPerSessionSeqAsRestart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/magpie/concurrency":
			_, _ = w.Write([]byte(`{"object":"concurrency","data":{},"active":[` +
				`{"id":1,"agent":"codex","session":"A","started":"2026-10-18T10:00:00Z","route":100},` +
				`{"id":2,"agent":"codex","session":"B","started":"2026-10-18T10:00:00Z","route":90}]}`))
		case "/v1/magpie/route":
			if r.URL.Query().Get("session") == "A" {
				_, _ = w.Write([]byte(`{"session":"A","seq":100,"route":{"id":100,"asked":"m","tries":[]}}`))
				return
			}
			_, _ = w.Write([]byte(`{"session":"B","seq":90,"route":{"id":90,"asked":"m","tries":[]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c, err := magpie.NewClient(magpie.Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	st := NewStore()
	sup := NewSupervisor(c, st, time.Second, "", 1)
	var notified atomic.Int64
	st.OnChange(func() { notified.Add(1) })
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := sup.pollOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	settled := notified.Load()
	for i := 0; i < 5; i++ {
		if err := sup.pollOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if extra := notified.Load() - settled; extra != 0 {
		t.Fatalf("%d extra notifications from identical polls", extra)
	}
}
