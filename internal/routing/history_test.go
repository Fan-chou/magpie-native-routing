package routing

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Fan-chou/magpie-native-routing/internal/testutil"
)

func write(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func push(t *testing.T, w *gzip.Writer, s string) {
	t.Helper()
	if _, err := w.Write([]byte(s + "\n")); err != nil {
		t.Fatal(err)
	}
}

func gzipped(t *testing.T, chunks ...string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	for _, s := range chunks {
		push(t, z, s)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestReadDayJSONLPartialLineAndDedup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-10-18.jsonl")
	write(t, path, []byte(testutil.SyntheticRouteJSON+"\n"+testutil.SyntheticSecondRouteJSON+"\n"+"{\"id\":7003"))
	r := NewHistoryReader(dir)
	rs, err := r.ReadDay("2026-10-18")
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].ID != 7001 || rs[1].ID != 7002 {
		t.Fatalf("routes = %+v", rs)
	}
	if rs[0].Order[0].Provider != "synthetic" || rs[0].Tries[1].Status != 200 {
		t.Fatalf("route fields = %+v", rs[0])
	}
}

func TestReadDayGzipAndLateJSONLWins(t *testing.T) {
	dir := t.TempDir()
	day := "2026-10-18"
	late := testutil.SyntheticRouteJSON
	old := `{"id":7001,"time":"2026-10-18T09:00:00Z","agent":"old","model":"old/m","done":true,"status":500}`
	write(t, filepath.Join(dir, day+".jsonl.gz"), gzipped(t, old))
	write(t, filepath.Join(dir, day+".jsonl"), []byte(late+"\n"))
	rs, err := NewHistoryReader(dir).ReadDay(day)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].Agent != "codex" {
		t.Fatalf("late JSONL did not win: %+v", rs)
	}
}

func TestReadDayConcatenatedGzipMembers(t *testing.T) {
	dir := t.TempDir()
	day := "2026-10-17"
	var b bytes.Buffer
	first := gzipped(t, testutil.SyntheticRouteJSON)
	second := gzipped(t, testutil.SyntheticSecondRouteJSON)
	b.Write(first)
	b.Write(second)
	write(t, filepath.Join(dir, day+".jsonl.gz"), b.Bytes())
	rs, err := NewHistoryReader(dir).ReadDay(day)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 {
		t.Fatalf("routes = %d", len(rs))
	}
}

func TestDaysIgnoresTemporaryAndInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"2026-10-18.jsonl", "2026-10-18.jsonl.gz", "bad.jsonl", "2026-10-17.jsonl.gz.tmp"} {
		write(t, filepath.Join(dir, name), []byte(""))
	}
	days, err := NewHistoryReader(dir).Days()
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 || days[0].Day != "2026-10-18" || days[0].JSONL == "" || days[0].Gzip == "" {
		t.Fatalf("days = %+v", days)
	}
}

func TestTailPartialAppendAndNoDuplicates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-10-18.jsonl")
	write(t, path, nil)
	tail, err := NewTail(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	line := testutil.SyntheticRouteJSON + "\n"
	cut := len(line) / 2
	if _, err := f.WriteString(line[:cut]); err != nil {
		t.Fatal(err)
	}
	got, err := tail.Poll()
	if err != nil || len(got) != 0 {
		t.Fatalf("partial poll = %v, %v", got, err)
	}
	if _, err := f.WriteString(line[cut:]); err != nil {
		t.Fatal(err)
	}
	got, err = tail.Poll()
	if err != nil || len(got) != 1 || got[0].ID != 7001 {
		t.Fatalf("completed poll = %v, %v", got, err)
	}
	got, err = tail.Poll()
	if err != nil || len(got) != 0 {
		t.Fatalf("duplicate poll = %v, %v", got, err)
	}
	if _, err := f.WriteString(testutil.SyntheticRouteJSON + "\n"); err != nil {
		t.Fatal(err)
	}
	got, err = tail.Poll()
	if err != nil || len(got) != 0 {
		t.Fatalf("tail duplicate = %v, %v", got, err)
	}
	f.Close()
}

func TestTailTruncationRestarts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-10-18.jsonl")
	write(t, path, []byte(testutil.SyntheticRouteJSON+"\n"))
	tail, _ := NewTail(path)
	got, _ := tail.Poll()
	if len(got) != 0 {
		t.Fatalf("NewTail should start at end: %v", got)
	}
	write(t, path, []byte(testutil.SyntheticSecondRouteJSON+"\n"))
	got, err := tail.Poll()
	if err != nil || len(got) != 1 || got[0].ID != 7002 {
		t.Fatalf("truncated poll = %v, %v", got, err)
	}
}

func TestRealHistoryIfPresent(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	dir := filepath.Join(home, ".config", "magpie", "routing")
	h := NewHistoryReader(dir)
	days, err := h.Days()
	if err != nil || len(days) == 0 {
		t.Skip("no real Magpie routing history")
	}
	for i := 0; i < len(days) && i < 3; i++ {
		rs, err := h.ReadDay(days[i].Day)
		if err != nil {
			t.Fatalf("%s: %v", days[i].Day, err)
		}
		if len(rs) == 0 {
			t.Fatalf("%s: no routes", days[i].Day)
		}
		for _, r := range rs {
			if r.ID == 0 || r.Time.IsZero() || r.Agent == "" {
				t.Fatalf("%s: incomplete route %+v", days[i].Day, r)
			}
		}
	}
}

func TestBuildReplayUsesTraceTimes(t *testing.T) {
	rs := []Route{
		{ID: 1, Time: time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC), Agent: "a", Model: "p/m", Done: true, Status: 200, Millis: 100,
			Tries: []Try{{Model: "p/m", Start: time.Date(2026, 10, 18, 10, 0, 0, 20_000_000, time.UTC), Done: true, Status: 200}}},
		{ID: 2, Time: time.Date(2026, 10, 18, 10, 0, 1, 0, time.UTC), Agent: "b", Model: "q/m", Done: true, Status: 500, Millis: 50},
	}
	tl := BuildReplay(rs)
	if len(tl.Events) < 5 || tl.Duration != 1050*time.Millisecond {
		t.Fatalf("timeline = %+v", tl)
	}
	if tl.Events[0].RouteID != 1 || tl.Events[len(tl.Events)-1].At > tl.Duration {
		t.Fatalf("events = %+v", tl.Events)
	}
}

func TestStatusTextMarksActive(t *testing.T) {
	r := Route{ID: 1, Model: "p/m"}
	r.AddSource(SourceLiveAPI)
	if r.StatusText() != "active" {
		t.Fatalf("status = %q", r.StatusText())
	}
	r.Done = true
	r.Status = 500
	if r.StatusText() != "failed" {
		t.Fatalf("status = %q", r.StatusText())
	}
}
