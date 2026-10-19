package routing

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Fan-chou/magpie-native-routing/internal/magpie"
)

// Supervisor owns network/file background work and publishes snapshots into a
// Store. It retries with bounded exponential backoff after Gateway restarts.
type Supervisor struct {
	client    *magpie.Client
	store     *Store
	pollEvery time.Duration
	day       string
	maxDays   int
	config    *ConfigReader
	history   *HistoryReader

	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
	done    chan struct{}
	notify  chan struct{}
	// lastSeq is the newest route seq seen per session. The endpoint reports
	// the seq of that session's own latest route, not a global counter.
	lastSeq map[string]int64
	// lastConfig is the last successfully parsed providers.json.
	lastConfig ConfigSnapshot
	haveConfig bool
}

func NewSupervisor(client *magpie.Client, store *Store, pollEvery time.Duration, day string, maxDays int) *Supervisor {
	if pollEvery < 250*time.Millisecond {
		pollEvery = time.Second
	}
	if maxDays <= 0 {
		maxDays = 7
	}
	return &Supervisor{
		client: client, store: store, pollEvery: pollEvery, day: day,
		maxDays: maxDays, lastSeq: map[string]int64{}, done: make(chan struct{}), notify: make(chan struct{}, 1),
	}
}

// ConfigureHistory attaches a read-only history directory and the number
// of newest days to keep in memory.
func (s *Supervisor) ConfigureHistory(reader *HistoryReader, maxDays int) {
	if maxDays <= 0 {
		maxDays = 7
	}
	s.history = reader
	s.maxDays = maxDays
}

// ConfigureProviders attaches the read-only providers.json reader.
func (s *Supervisor) ConfigureProviders(reader *ConfigReader) {
	s.config = reader
}

// Start launches polling. It is idempotent.
func (s *Supervisor) Start(parent context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return
	}
	s.started = true
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	go s.run(ctx)
}

// Stop ends workers and waits for them.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	cancel, started := s.cancel, s.started
	s.mu.Unlock()
	if !started {
		return
	}
	if cancel != nil {
		cancel()
	}
	<-s.done
}

// Refresh asks the network loop to poll immediately.
func (s *Supervisor) Refresh() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *Supervisor) run(ctx context.Context) {
	defer close(s.done)
	netTimer := time.NewTimer(0)
	defer netTimer.Stop()
	cfgTimer := time.NewTimer(0)
	defer cfgTimer.Stop()
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.notify:
			if !netTimer.Stop() {
				select {
				case <-netTimer.C:
				default:
				}
			}
			netTimer.Reset(0)
		case <-netTimer.C:
		case <-cfgTimer.C:
			s.refreshProviders()
			cfgTimer.Reset(3 * time.Second)
			continue
		}
		err := s.pollOnce(ctx)
		var wait time.Duration
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			failures++
			s.store.SetConnectionError(SafeErrorText(err))
			wait = RetryDelay(failures-1, err)
		} else {
			failures = 0
			wait = s.pollEvery
		}
		netTimer.Reset(wait)
	}
}

func (s *Supervisor) refreshProviders() {
	if s.config == nil {
		return
	}
	cfg, err := s.config.Read()
	if err != nil {
		if s.haveConfig {
			s.store.SetConfigError(SafeErrorText(err))
			return
		}
		s.store.SetConfigError(SafeErrorText(err))
		return
	}
	s.mu.Lock()
	s.lastConfig = cfg
	s.haveConfig = true
	s.mu.Unlock()
	s.store.SetConfig(cfg)
}

func (s *Supervisor) pollOnce(ctx context.Context) error {
	cctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	c, err := s.client.Concurrency(cctx)
	if err != nil {
		return err
	}
	s.store.ApplyActive(FromGatewayConcurrency(c))

	// Poll known sessions at most once per cycle. The endpoint's long
	// polling is intentionally not used here: the supervisor must cancel
	// promptly on quit and not hold a worker for a minute.
	var warning string
	active := map[string]struct{}{}
	for _, a := range c.Active {
		if a.Session == "" {
			continue
		}
		active[a.Session] = struct{}{}
		last := s.lastSeq[a.Session]
		resp, err := s.client.SessionRoute(cctx, a.Session, last, 0)
		if err != nil {
			if magpie.IsAuthError(err) || magpie.IsNotFound(err) {
				if warning == "" {
					warning = SafeErrorText(err)
				}
				continue
			}
			// One session can disappear between the concurrency snapshot
			// and this request. Keep the successful global snapshot and
			// let the next cycle retry.
			if warning == "" {
				warning = SafeErrorText(err)
			}
			continue
		}
		if last > 0 && resp.Seq < last {
			// This session's trace sequence went backwards: the Gateway
			// restarted. ApplyActive above already replaced the live plane
			// wholesale, so only the seq state needs resetting.
			s.lastSeq[a.Session] = 0
		} else if resp.Seq > last {
			s.lastSeq[a.Session] = resp.Seq
		}
		if sr, ok := FromGatewaySession(resp); ok {
			s.store.MergeSession(sr)
		}
	}
	for id := range s.lastSeq {
		if _, ok := active[id]; !ok {
			delete(s.lastSeq, id)
		}
	}
	s.store.SetGatewayWarning(warning)
	return nil
}

// LoadHistory loads the newest limit days and merges completed routes. It is
// intended to run off the UI thread; it never writes Magpie's directory.
func (s *Supervisor) LoadHistory(ctx context.Context, reader *HistoryReader, limit int) error {
	if limit <= 0 {
		limit = s.maxDays
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	routes, days, err := reader.ReadDays(limit)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, r := range routes {
		if r.HistoryDay != "" {
			counts[r.HistoryDay]++
		}
	}
	s.store.SetDays(DayFilesToDays(days, counts))
	s.store.ReplaceHistory(routes)
	return nil
}

// LoadDay loads one day and replaces that day's merged records in the store.
func (s *Supervisor) LoadDay(ctx context.Context, reader *HistoryReader, day string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	rs, err := reader.ReadDay(day)
	if err != nil {
		return err
	}
	s.store.MergeHistory(rs)
	return nil
}

// ErrStopped is returned by tests that wait on a stopped supervisor.
var ErrStopped = errors.New("supervisor stopped")
