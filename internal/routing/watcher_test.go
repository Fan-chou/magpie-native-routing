package routing

import (
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Fan-chou/magpie-native-routing/internal/testutil"
)

func testWatcher(t *testing.T, dir string, now time.Time) (*HistoryWatcher, *Store) {
	t.Helper()
	store := NewStore()
	w := NewHistoryWatcher(NewHistoryReader(dir), store, 30, time.Second)
	w.now = func() time.Time { return now }
	return w, store
}

func appendFile(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func routeIDs(s Snapshot) map[int64]bool {
	out := map[int64]bool{}
	for _, r := range s.Routes {
		out[r.ID] = true
	}
	return out
}

func setAndScan(t *testing.T, w *HistoryWatcher, mtime time.Time) {
	t.Helper()
	_ = filepath.WalkDir(w.reader.Dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			_ = os.Chtimes(path, mtime, mtime)
		}
		return nil
	})
	if err := w.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryWatcherInitialLoadAndPartialAppend(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.Local)
	path := filepath.Join(dir, now.Format(dayForm)+".jsonl")
	write(t, path, []byte(testutil.SyntheticRouteJSON+"\n{\"id\":7002"))

	w, store := testWatcher(t, dir, now)
	if err := w.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	snap := store.Snapshot()
	if len(snap.Routes) != 1 || snap.Routes[0].ID != 7001 {
		t.Fatalf("initial routes = %+v", snap.Routes)
	}
	if len(snap.Days) == 0 || snap.Days[0].Day != "2026-10-18" {
		t.Fatalf("days = %+v", snap.Days)
	}

	appendFile(t, path, "}\n")
	setAndScan(t, w, now)
	if len(store.Snapshot().Routes) != 2 {
		t.Fatalf("partial append was lost: %+v", store.Snapshot().Routes)
	}

	if err := w.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.Snapshot().Routes) != 2 {
		t.Fatal("rescan duplicated a route")
	}
}

func TestHistoryWatcherOldDayLateWriteForcesReload(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.Local)
	today := filepath.Join(dir, "2026-10-18.jsonl")
	old := filepath.Join(dir, "2026-10-17.jsonl")
	write(t, today, []byte(testutil.SyntheticRouteJSON+"\n"))
	write(t, old, []byte(testutil.SyntheticSecondRouteJSON+"\n"))

	w, store := testWatcher(t, dir, now)
	if err := w.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	appendFile(t, old, `{"id":7003,"time":"2026-10-17T10:02:00Z","agent":"late","model":"synthetic/late","done":true,"status":200}`+"\n")
	setAndScan(t, w, now.Add(time.Second))
	ids := routeIDs(store.Snapshot())
	if !ids[7001] || !ids[7002] || !ids[7003] {
		t.Fatalf("late old-day route not loaded: %v", ids)
	}
}

func TestHistoryWatcherGzipCompactionAndTruncation(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.Local)
	path := filepath.Join(dir, "2026-10-18.jsonl")
	write(t, path, []byte(testutil.SyntheticRouteJSON+"\n"))

	w, store := testWatcher(t, dir, now)
	if err := w.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}

	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	if _, err := z.Write([]byte(testutil.SyntheticRouteJSON + "\n" + testutil.SyntheticSecondRouteJSON + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".gz", b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	setAndScan(t, w, now)
	snap := store.Snapshot()
	if ids := routeIDs(snap); !ids[7001] || !ids[7002] {
		t.Fatalf("gzip compaction routes = %v", ids)
	}

	if err := os.Remove(path + ".gz"); err != nil {
		t.Fatal(err)
	}
	write(t, path, []byte(`{"id":7004,"time":"2026-10-18T10:03:00Z","agent":"new","model":"synthetic/new","done":true,"status":200}`+"\n"))
	setAndScan(t, w, now.Add(time.Second))
	ids := routeIDs(store.Snapshot())
	if ids[7001] || ids[7002] || !ids[7004] {
		t.Fatalf("truncated/replaced file routes = %v", ids)
	}
}

func TestHistoryWatcherDaySwitchAndRemovedDays(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 18, 23, 59, 0, 0, time.Local)
	first := filepath.Join(dir, "2026-10-18.jsonl")
	write(t, first, []byte(testutil.SyntheticRouteJSON+"\n"))

	w, store := testWatcher(t, dir, now)
	if err := w.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}

	waiting := filepath.Join(dir, "2026-10-19.jsonl")
	write(t, waiting, []byte(testutil.SyntheticSecondRouteJSON+"\n"))
	w.now = func() time.Time { return now.Add(2 * time.Minute) }
	if err := w.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ids := routeIDs(store.Snapshot()); !ids[7001] || !ids[7002] {
		t.Fatalf("day switch routes = %v", ids)
	}

	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	setAndScan(t, w, now.Add(3*time.Minute))
	snap := store.Snapshot()
	if ids := routeIDs(snap); ids[7001] || !ids[7002] {
		t.Fatalf("pruned route remained: %v", ids)
	}
	if len(snap.Days) != 1 || snap.Days[0].Day != "2026-10-19" {
		t.Fatalf("day metadata = %+v", snap.Days)
	}
}

func TestHistoryWatcherRestartStartsFresh(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-10-18.jsonl")
	write(t, path, []byte(testutil.SyntheticRouteJSON+"\n"))

	firstStore := NewStore()
	first := NewHistoryWatcher(NewHistoryReader(dir), firstStore, 7, time.Second)
	if err := first.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}

	secondStore := NewStore()
	second := NewHistoryWatcher(NewHistoryReader(dir), secondStore, 7, time.Second)
	if err := second.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(secondStore.Snapshot().Routes) != 1 {
		t.Fatalf("restart routes = %+v", secondStore.Snapshot().Routes)
	}
}

func TestHistoryWatcherEmptyDirectoryShowsToday(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.Local)
	w, store := testWatcher(t, dir, now)
	if err := w.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	days := store.Snapshot().Days
	if len(days) != 1 || days[0].Day != "2026-10-18" || days[0].Requests != 0 {
		t.Fatalf("empty days = %+v", days)
	}
}

func TestHistoryWatcherFileAppearsAfterEmptyScanCountsOnce(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.Local)
	w, store := testWatcher(t, dir, now)
	if err := w.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, now.Format(dayForm)+".jsonl")
	write(t, path, []byte(testutil.SyntheticRouteJSON+"\n"))
	setAndScan(t, w, now.Add(time.Second))

	snap := store.Snapshot()
	if !routeIDs(snap)[7001] {
		t.Fatalf("routes = %+v", snap.Routes)
	}
	if len(snap.Days) == 0 || snap.Days[0].Day != "2026-10-18" || snap.Days[0].Requests != 1 {
		t.Fatalf("day count = %+v", snap.Days)
	}
}

func TestHistoryWatcherSameSizeReplacementForcesReload(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.Local)
	path := filepath.Join(dir, now.Format(dayForm)+".jsonl")
	write(t, path, []byte(testutil.SyntheticRouteJSON+"\n"))

	w, store := testWatcher(t, dir, now)
	if err := w.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}

	size := len(testutil.SyntheticRouteJSON) + 1
	record := `{"id":7009,"time":"2026-10-18T10:04:00Z","agent":"same","model":"synthetic/same","done":true,"status":200}`
	replacement := append([]byte(record), bytes.Repeat([]byte{' '}, size-len(record)-1)...)
	replacement = append(replacement, '\n')
	write(t, path, replacement)
	setAndScan(t, w, now.Add(time.Second))

	ids := routeIDs(store.Snapshot())
	if ids[7001] || !ids[7009] {
		t.Fatalf("same-size replacement routes = %v", ids)
	}
}

func TestHistoryWatcherSelectedPastDayKeepsLiveTail(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 18, 23, 59, 0, 0, time.Local)
	past := filepath.Join(dir, "2026-10-17.jsonl")
	live := filepath.Join(dir, "2026-10-18.jsonl")
	write(t, past, []byte(testutil.SyntheticRouteJSON+"\n"))
	write(t, live, []byte(testutil.SyntheticSecondRouteJSON+"\n"))

	w, store := testWatcher(t, dir, now)
	if err := w.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.SetSelectedDay("2026-10-17")
	if err := w.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	ids := routeIDs(store.Snapshot())
	if !ids[7001] || !ids[7002] {
		t.Fatalf("selected day load lost a real day: %v", ids)
	}

	appendFile(t, live, `{"id":7003,"time":"2026-10-18T23:59:30+08:00","agent":"live","model":"synthetic/live","done":true,"status":200}`+"\n")
	setAndScan(t, w, now.Add(time.Minute))
	ids = routeIDs(store.Snapshot())
	if !ids[7001] || !ids[7002] || !ids[7003] {
		t.Fatalf("live tail was lost while viewing a past day: %v", ids)
	}
}
