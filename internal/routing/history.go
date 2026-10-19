package routing

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var dayForm = "2006-01-02"

// DayFile is one readable routing history file. Day is local calendar day.
type DayFile struct {
	Day      string
	JSONL    string
	Gzip     string
	Size     int64
	Modified time.Time
}

// HistoryReader reads Magpie's routing directory without modifying it.
type HistoryReader struct {
	Dir string
}

// NewHistoryReader returns a reader for dir. It does not create directories.
func NewHistoryReader(dir string) *HistoryReader { return &HistoryReader{Dir: dir} }

// Days lists valid YYYY-MM-DD routing files, newest day first. If both a
// .jsonl and .jsonl.gz exist, both are reported for that day.
func (h *HistoryReader) Days() ([]DayFile, error) {
	entries, err := os.ReadDir(h.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []DayFile{}, nil
		}
		return nil, err
	}
	byDay := map[string]*DayFile{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		day, kind, ok := parseHistoryName(name)
		if !ok {
			continue
		}
		d := byDay[day]
		if d == nil {
			d = &DayFile{Day: day}
			byDay[day] = d
		}
		p := filepath.Join(h.Dir, name)
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		d.Size += info.Size()
		if info.ModTime().After(d.Modified) {
			d.Modified = info.ModTime()
		}
		if kind == "gz" {
			d.Gzip = p
		} else {
			d.JSONL = p
		}
	}
	out := make([]DayFile, 0, len(byDay))
	for _, d := range byDay {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day > out[j].Day })
	return out, nil
}

func parseHistoryName(name string) (day, kind string, ok bool) {
	base := name
	switch {
	case strings.HasSuffix(base, ".jsonl.gz"):
		base, kind = strings.TrimSuffix(base, ".jsonl.gz"), "gz"
	case strings.HasSuffix(base, ".jsonl"):
		base, kind = strings.TrimSuffix(base, ".jsonl"), "jsonl"
	default:
		return "", "", false
	}
	if _, err := time.Parse(dayForm, base); err != nil {
		return "", "", false
	}
	return base, kind, true
}

// ReadDay reads a day's completed routes. JSONL is read first, then both
// gzip members (which may contain a late-written JSONL route) and appended
// members. Duplicate Route IDs are collapsed, with the latest complete
// representation winning.
func (h *HistoryReader) ReadDay(day string) ([]Route, error) {
	if _, err := time.Parse(dayForm, day); err != nil {
		return nil, fmt.Errorf("invalid routing day %q", day)
	}
	seen := map[int64]Route{}
	var order []int64
	add := func(r Route) {
		if r.ID == 0 {
			return
		}
		r.HistoryDay = day
		r.ReadAt = time.Now()
		r.AddSource(SourceHistory)
		if old, ok := seen[r.ID]; ok {
			// A later line for the same ID is the more complete record. Keep
			// the latest one but merge a fuller source marker.
			for _, s := range old.Sources {
				r.AddSource(s)
			}
		} else {
			order = append(order, r.ID)
		}
		seen[r.ID] = r
	}

	// The gzip is the older, compacted copy. A JSONL beside it can hold a
	// late route written after the gzip, so it is read last and wins.
	gzPath := filepath.Join(h.Dir, day+".jsonl.gz")
	if err := readGzip(gzPath, add); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	jsonlPath := filepath.Join(h.Dir, day+".jsonl")
	if err := readJSONL(jsonlPath, add); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	out := make([]Route, 0, len(order))
	for _, id := range order {
		out = append(out, seen[id])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Time.Equal(out[j].Time) {
			return out[i].ID < out[j].ID
		}
		return out[i].Time.Before(out[j].Time)
	})
	return out, nil
}

// ReadSelectedDay reads one day together with the newest tailed day. Magpie
// writes completed records to today's JSONL, while the view may be showing an
// older day. Keeping both lets the caller replace the displayed day's layer
// without dropping today's incrementally observed records.
func (h *HistoryReader) ReadSelectedDay(selected string) ([]Route, []DayFile, error) {
	days, err := h.Days()
	if err != nil {
		return nil, nil, err
	}
	today := time.Now().Local().Format(dayForm)
	tailDay := today
	if len(days) > 0 && days[0].Day > tailDay {
		tailDay = days[0].Day
	}
	wanted := []string{selected}
	if tailDay != selected {
		wanted = append(wanted, tailDay)
	}
	seen := map[string]bool{}
	var out []Route
	for _, day := range wanted {
		if day == "" || seen[day] {
			continue
		}
		seen[day] = true
		routes, err := h.ReadDay(day)
		if err != nil {
			return out, days, err
		}
		out = append(out, routes...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Time.Equal(out[j].Time) {
			return out[i].ID > out[j].ID
		}
		return out[i].Time.After(out[j].Time)
	})
	return out, days, nil
}

// ReadDaysIncluding reads the newest limit days and always adds selected when
// it is not already among them. Magpie's routing files are read only.
func (h *HistoryReader) ReadDaysIncluding(limitDays int, selected string) ([]Route, []DayFile, error) {
	days, err := h.Days()
	if err != nil {
		return nil, nil, err
	}
	if limitDays <= 0 || limitDays > len(days) {
		limitDays = len(days)
	}
	chosen := append([]DayFile(nil), days[:limitDays]...)
	if selected != "" {
		found := false
		for _, day := range chosen {
			if day.Day == selected {
				found = true
				break
			}
		}
		if !found {
			for _, day := range days {
				if day.Day == selected {
					chosen = append(chosen, day)
					break
				}
			}
		}
	}
	sort.SliceStable(chosen, func(i, j int) bool { return chosen[i].Day > chosen[j].Day })
	var out []Route
	for _, day := range chosen {
		routes, err := h.ReadDay(day.Day)
		if err != nil {
			return out, days, err
		}
		out = append(out, routes...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Time.Equal(out[j].Time) {
			return out[i].ID > out[j].ID
		}
		return out[i].Time.After(out[j].Time)
	})
	return out, days, nil
}

// ReadAll reads all available days, newest first. limit, when positive,
// limits the number of routes rather than days; callers that show a number
// of days should use ReadDays.
func (h *HistoryReader) ReadAll(limit int) ([]Route, []DayFile, error) {
	days, err := h.Days()
	if err != nil {
		return nil, nil, err
	}
	var out []Route
	for _, d := range days {
		if limit > 0 && len(out) >= limit {
			break
		}
		rs, err := h.ReadDay(d.Day)
		if err != nil {
			return out, days, err
		}
		out = append(out, rs...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Time.Equal(out[j].Time) {
			return out[i].ID > out[j].ID
		}
		return out[i].Time.After(out[j].Time)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, days, nil
}

// ReadDays reads the newest limitDays day files without truncating the
// routes of a day. A day with both JSONL and gzip data is read once.
func (h *HistoryReader) ReadDays(limitDays int) ([]Route, []DayFile, error) {
	days, err := h.Days()
	if err != nil {
		return nil, nil, err
	}
	if limitDays <= 0 {
		limitDays = len(days)
	}
	if limitDays > len(days) {
		limitDays = len(days)
	}
	var out []Route
	for _, d := range days[:limitDays] {
		rs, err := h.ReadDay(d.Day)
		if err != nil {
			return out, days, err
		}
		out = append(out, rs...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Time.Equal(out[j].Time) {
			return out[i].ID > out[j].ID
		}
		return out[i].Time.After(out[j].Time)
	})
	return out, days, nil
}

func readJSONL(path string, add func(Route)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return scanJSONLines(f, add)
}

func scanJSONLines(r io.Reader, add func(Route)) error {
	br := bufio.NewReaderSize(r, 256<<10)
	var pending []byte
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			pending = append(pending, line...)
			// A trailing fragment without newline is incomplete: Magpie may
			// still be writing it. It is deliberately ignored here.
			if line[len(line)-1] == '\n' {
				parseLine(pending, add)
				pending = pending[:0]
			}
		}
		if err == io.EOF {
			// Magpie accepts a final complete JSON object without newline in
			// gzip history, but live JSONL is append-only. Keeping the same
			// tolerant behavior for regular files is safe because json.Valid
			// is checked before decoding.
			if len(pending) > 0 && json.Valid(bytes.TrimSpace(pending)) {
				parseLine(pending, add)
			}
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func parseLine(line []byte, add func(Route)) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || !json.Valid(line) {
		return
	}
	var r Route
	if err := json.Unmarshal(line, &r); err != nil || r.ID == 0 {
		return
	}
	add(r)
}

func readGzip(path string, add func(Route)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	// gzip.Reader handles concatenated members.
	z, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer z.Close()
	return scanJSONLines(z, add)
}

// Tail reads only complete newline-terminated records from a growing JSONL
// file. It is intentionally stateful so a UI can poll cheaply.
type Tail struct {
	Path   string
	Offset int64
	Inode  uint64
	// Seen prevents duplicate routes when files overlap or a reader restarts.
	Seen map[int64]struct{}
}

// NewTail creates a tailer positioned at the end of the current file.
func NewTail(path string) (*Tail, error) {
	t := &Tail{Path: path, Seen: map[int64]struct{}{}}
	if _, err := t.Poll(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return t, nil
}

// ResetSeen clears the de-duplication set (usually on a day switch).
func (t *Tail) ResetSeen() {
	t.Seen = map[int64]struct{}{}
}

// MarkSeen records IDs that a full read has already delivered for this tail's
// day. It prevents the first Poll from replaying those records into counters
// when a file appeared after the watcher started.
func (t *Tail) MarkSeen(routes []Route, day string) {
	if t.Seen == nil {
		t.Seen = map[int64]struct{}{}
	}
	for _, r := range routes {
		if r.ID != 0 && (day == "" || r.HistoryDay == "" || r.HistoryDay == day) {
			t.Seen[r.ID] = struct{}{}
		}
	}
}

// Poll reads bytes appended since the previous call, returning newly
// completed routes. Partial trailing lines stay unread for the next call.
func (t *Tail) Poll() ([]Route, error) {
	f, err := os.Open(t.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Offset = 0
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	// Detect truncation / replacement. Offset past EOF starts over.
	if fi.Size() < t.Offset {
		t.Offset = 0
	}
	if t.Inode != 0 && !sameFileIdentity(t.Inode, fi) {
		t.Offset = 0
	}
	t.Inode = fileIdentity(fi)
	if t.Offset > 0 {
		if _, err := f.Seek(t.Offset, io.SeekStart); err != nil {
			return nil, err
		}
	}
	var out []Route
	br := bufio.NewReaderSize(f, 256<<10)
	var consumed int64
	for {
		line, rerr := br.ReadBytes('\n')
		if len(line) > 0 {
			consumed += int64(len(line))
			if line[len(line)-1] == '\n' {
				before := len(out)
				parseLine(line, func(r Route) { out = append(out, r) })
				if len(out) == before {
					// Keep malformed complete lines consumed: they should
					// not hold up later valid records.
				}
			} else {
				// Put the incomplete fragment back by not advancing over
				// it; the next poll will read it with any continuation.
				consumed -= int64(len(line))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, rerr
		}
	}
	if consumed > 0 {
		t.Offset += consumed
	}
	filtered := out[:0]
	for _, r := range out {
		if _, ok := t.Seen[r.ID]; ok {
			continue
		}
		t.Seen[r.ID] = struct{}{}
		filtered = append(filtered, r)
	}
	return filtered, nil
}

// ResetOnSwitch handles a day change and starts at the new file's end.
func (t *Tail) ResetOnSwitch(path string) error {
	if filepath.Clean(t.Path) == filepath.Clean(path) {
		return nil
	}
	t.Path = path
	t.Offset = 0
	t.Inode = 0
	t.ResetSeen()
	return nil
}
