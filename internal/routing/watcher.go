package routing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// HistoryWatcher keeps the Store's completed-history layer in sync with
// Magpie's routing directory. It only opens files for reading.
//
// The watcher combines a stateful tail of the newest JSONL with periodic
// directory scans. The tail gives low-cost appends; the scan catches a new
// day, gzip compaction, old-day late writes, truncation and replacement.
type HistoryWatcher struct {
	reader   *HistoryReader
	store    *Store
	maxDays  int
	interval time.Duration

	mu          sync.Mutex
	started     bool
	cancel      context.CancelFunc
	done        chan struct{}
	refresh     chan struct{}
	now         func() time.Time
	loaded      bool
	tail        *Tail
	tailDay     string
	stamps      map[string]fileStamp
	counts      map[string]int
	days        []HistoryDay
	selectedDay string
}

type fileStamp struct {
	path  string
	size  int64
	mtime int64
	inode uint64
}

func NewHistoryWatcher(reader *HistoryReader, store *Store, maxDays int, interval time.Duration) *HistoryWatcher {
	if maxDays <= 0 {
		maxDays = 7
	}
	if interval < 250*time.Millisecond {
		interval = 1500 * time.Millisecond
	}
	return &HistoryWatcher{
		reader: reader, store: store, maxDays: maxDays, interval: interval,
		done: make(chan struct{}), refresh: make(chan struct{}, 1),
		now:    time.Now,
		stamps: map[string]fileStamp{}, counts: map[string]int{},
	}
}

// SetSelectedDay sets the day whose completed history the viewer is showing.
// An empty or today's day keeps the default newest-days window. A past day is
// read alongside today's file so future records still arrive without
// replacing the selected day's detailed trace.
func (w *HistoryWatcher) SetSelectedDay(day string) {
	w.mu.Lock()
	if day != w.selectedDay {
		w.selectedDay = day
		w.loaded = false
	}
	w.mu.Unlock()
	w.Refresh()
}

func (w *HistoryWatcher) currentSelectedDay() string {
	day := w.selectedDay
	if day == "" || day == w.now().Local().Format(dayForm) {
		return ""
	}
	if _, err := time.Parse(dayForm, day); err != nil {
		return ""
	}
	return day
}

// Start performs the initial load and then watches until ctx ends. It is
// idempotent. Initial reads happen off the caller's UI thread.
func (w *HistoryWatcher) Start(ctx context.Context) {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return
	}
	w.started = true
	cctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.mu.Unlock()
	go w.run(cctx)
}

func (w *HistoryWatcher) Stop() {
	w.mu.Lock()
	cancel, started := w.cancel, w.started
	w.mu.Unlock()
	if !started {
		return
	}
	if cancel != nil {
		cancel()
	}
	<-w.done
}

// Refresh requests a scan on the next loop. Repeated requests coalesce.
func (w *HistoryWatcher) Refresh() {
	select {
	case w.refresh <- struct{}{}:
	default:
	}
}

func (w *HistoryWatcher) run(ctx context.Context) {
	defer close(w.done)
	if err := w.load(ctx, true); err != nil && ctx.Err() == nil {
		w.store.SetHistoryError(SafeErrorText(err))
	}
	t := time.NewTimer(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.refresh:
		case <-t.C:
		}
		if err := w.Scan(ctx); err != nil && ctx.Err() == nil {
			w.store.SetHistoryError(SafeErrorText(err))
		}
		t.Reset(w.interval)
	}
}

// Scan performs one incremental scan. It is exported for deterministic
// tests and for a manual refresh.
func (w *HistoryWatcher) Scan(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.scanLocked(ctx)
}

func (w *HistoryWatcher) load(ctx context.Context, initial bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.loadLocked(ctx, initial)
}

func (w *HistoryWatcher) scanLocked(ctx context.Context) error {
	if !w.loaded {
		return w.loadLocked(ctx, true)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	days, err := w.reader.Days()
	if err != nil {
		return err
	}
	days = selectNewestDays(days, w.maxDays)
	selected := w.currentSelectedDay()
	if selected != "" {
		days = selectDaysWith(days, selected)
	}
	currentTail := w.newestTailPath(days)
	nextStamps, err := stampDays(days, currentTail)
	if err != nil {
		return err
	}
	if currentTail != w.tailPath() {
		return w.loadLocked(ctx, false)
	}
	if changed, tailAppend := stampsChanged(w.stamps, nextStamps, currentTail); changed {
		if !tailAppend {
			return w.loadLocked(ctx, false)
		}
	}
	if w.tail == nil {
		return w.loadLocked(ctx, false)
	}
	rs, err := w.tail.Poll()
	if err != nil {
		return err
	}
	if len(rs) > 0 {
		day := w.tailDay
		for i := range rs {
			rs[i].HistoryDay = day
			rs[i].ReadAt = time.Now()
			rs[i].AddSource(SourceHistory)
			w.counts[day]++
		}
		w.store.MergeHistory(rs)
	}
	w.stamps = nextStamps
	w.days = mergeDayMetadata(days, w.counts, w.now())
	w.store.SetDays(w.days)
	w.store.SetHistoryError("")
	return nil
}

func (w *HistoryWatcher) loadLocked(ctx context.Context, initial bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	days, err := w.reader.Days()
	if err != nil {
		return err
	}
	days = selectNewestDays(days, w.maxDays)
	selected := w.currentSelectedDay()
	if selected != "" {
		days = selectDaysWith(days, selected)
	}
	tailPath := w.newestTailPath(days)

	// Create the tail before reading the day. Bytes appended during the
	// full read are then picked up by Poll, and Route ID merging removes
	// any overlap.
	if w.tail == nil || w.tailPath() != tailPath {
		t, err := NewTail(tailPath)
		if err != nil {
			return err
		}
		w.tail = t
		w.tailDay = dayOfPath(tailPath)
	}

	routes, allDays, err := w.reader.ReadDaysIncluding(w.maxDays, selected)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, r := range routes {
		if r.HistoryDay != "" {
			counts[r.HistoryDay]++
		}
	}
	w.counts = counts
	w.days = mergeDayMetadata(allDays, counts, w.now())
	w.store.ReplaceHistory(routes)
	w.store.SetDays(w.days)

	// The full read and the tail both start at byte zero. Seed the tail's
	// ID set with what the full read already delivered so a file created
	// between the directory scan and NewTail cannot be counted twice.
	if w.tail != nil {
		w.tail.MarkSeen(routes, w.tailDay)
	}

	stampDaysList := selectNewestDays(allDays, w.maxDays)
	if selected != "" {
		stampDaysList = selectDaysWith(stampDaysList, selected)
	}
	stamps, err := stampDays(stampDaysList, tailPath)
	if err != nil {
		return err
	}
	w.stamps = stamps
	w.loaded = true
	w.store.SetHistoryError("")

	rs, err := w.tail.Poll()
	if err != nil {
		return err
	}
	if len(rs) > 0 {
		for i := range rs {
			rs[i].HistoryDay = w.tailDay
			rs[i].ReadAt = time.Now()
			rs[i].AddSource(SourceHistory)
			w.counts[w.tailDay]++
		}
		w.store.MergeHistory(rs)
	}
	_ = initial
	return nil
}

func (w *HistoryWatcher) tailPath() string {
	if w.tail == nil {
		return ""
	}
	return w.tail.Path
}

// newestTailPath names the newest real day's JSONL, or today's expected
// path when there is no file yet. This lets a first record create the
// file without restarting the watcher.
func (w *HistoryWatcher) newestTailPath(days []DayFile) string {
	today := w.now().Local().Format(dayForm)
	day := today
	if len(days) > 0 && days[0].Day > day {
		day = days[0].Day
	}
	for _, d := range days {
		if d.Day == day && d.JSONL != "" {
			return d.JSONL
		}
	}
	return filepath.Join(w.reader.Dir, day+".jsonl")
}

func selectNewestDays(days []DayFile, limit int) []DayFile {
	if limit <= 0 || limit > len(days) {
		limit = len(days)
	}
	out := append([]DayFile(nil), days[:limit]...)
	sort.Slice(out, func(i, j int) bool { return out[i].Day > out[j].Day })
	return out
}

func selectDaysWith(days []DayFile, selected string) []DayFile {
	// The selected historical day is read with the newest tail day. Both are
	// real files; no derived day is added to the reader.
	out := append([]DayFile(nil), days...)
	for _, day := range days {
		if day.Day == selected {
			return out
		}
	}
	if _, err := time.Parse(dayForm, selected); err == nil {
		out = append(out, DayFile{Day: selected})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day > out[j].Day })
	return out
}

func stampDays(days []DayFile, tailPath string) (map[string]fileStamp, error) {
	out := map[string]fileStamp{}
	paths := []string{tailPath}
	for _, d := range days {
		if d.JSONL != "" {
			paths = append(paths, d.JSONL)
		}
		if d.Gzip != "" {
			paths = append(paths, d.Gzip)
		}
	}
	seen := map[string]struct{}{}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		st, err := statStamp(p)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				st = fileStamp{path: p, size: -1}
			} else {
				return nil, err
			}
		}
		out[p] = st
	}
	return out, nil
}

func statStamp(path string) (fileStamp, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStamp{}, err
	}
	return fileStamp{
		path: path, size: fi.Size(), mtime: fi.ModTime().UnixNano(), inode: fileIdentity(fi),
	}, nil
}

// stampsChanged reports whether any file changed and, if so, whether the
// only change was an append to the exact JSONL the tail follows. A late
// write to an older day must force a full reread: the tail cannot see it.
func stampsChanged(old, next map[string]fileStamp, tailPath string) (changed, tailAppend bool) {
	if len(old) != len(next) {
		return true, false
	}
	appends := 0
	for p, n := range next {
		o, ok := old[p]
		if !ok {
			return true, false
		}
		if o == n {
			continue
		}
		changed = true
		if p != tailPath {
			return true, false
		}
		if o.inode != 0 && n.inode != 0 && o.inode != n.inode {
			return true, false
		}
		// Equal size with a changed mtime can be an in-place rewrite with
		// the same byte count. Only strict growth is safe as a tail append.
		if o.size < 0 || n.size <= o.size {
			return true, false
		}
		appends++
	}
	if !changed {
		return false, false
	}
	return true, appends == 1
}

func mergeDayMetadata(files []DayFile, counts map[string]int, now time.Time) []HistoryDay {
	out := DayFilesToDays(files, counts)
	today := now.Local().Format(dayForm)
	if len(out) == 0 || out[0].Day != today {
		out = append([]HistoryDay{{Day: today}}, out...)
	}
	return out
}

func dayOfPath(path string) string {
	name := filepath.Base(path)
	name = trimHistorySuffix(name)
	if _, err := time.Parse(dayForm, name); err != nil {
		return ""
	}
	return name
}

func trimHistorySuffix(name string) string {
	if len(name) > len(".jsonl.gz") && name[len(name)-len(".jsonl.gz"):] == ".jsonl.gz" {
		return name[:len(name)-len(".jsonl.gz")]
	}
	if len(name) > len(".jsonl") && name[len(name)-len(".jsonl"):] == ".jsonl" {
		return name[:len(name)-len(".jsonl")]
	}
	return name
}

// Reload forces a complete reread, useful after a known external change.
func (w *HistoryWatcher) Reload(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.loadLocked(ctx, false)
}

func (w *HistoryWatcher) String() string {
	return fmt.Sprintf("history watcher: %s", w.reader.Dir)
}
