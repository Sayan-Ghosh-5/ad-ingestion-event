package worker

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/event"
	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/storage"
)

// Config tunes the worker pool behaviour.
type Config struct {
	Workers     int
	QueueSize   int
	BatchSize   int
	FlushPeriod time.Duration
}

func DefaultConfig() Config {
	return Config{Workers: 10, QueueSize: 10_000, BatchSize: 100, FlushPeriod: time.Second}
}

// Pool is a set of goroutines that drain a buffered channel of events and
// flush them to storage in batches.
type Pool struct {
	cfg     Config
	queue   chan event.Event
	store   storage.Store
	log     *slog.Logger
	wg      sync.WaitGroup
	dropped atomic.Int64
	stored  atomic.Int64
}

func New(cfg Config, store storage.Store, log *slog.Logger) *Pool {
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	return &Pool{
		cfg:   cfg,
		queue: make(chan event.Event, cfg.QueueSize),
		store: store,
		log:   log,
	}
}

// Start spawns worker goroutines. They stop when ctx is cancelled.
func (p *Pool) Start(ctx context.Context) {
	for i := 0; i < p.cfg.Workers; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}
}

// Submit enqueues an event without blocking. Returns false if the queue is full
// (backpressure: we drop rather than block the HTTP handler).
func (p *Pool) Submit(e event.Event) bool {
	select {
	case p.queue <- e:
		return true
	default:
		p.dropped.Add(1)
		return false
	}
}

// QueueDepth returns the current number of buffered events.
func (p *Pool) QueueDepth() int { return len(p.queue) }
func (p *Pool) Dropped() int64  { return p.dropped.Load() }
func (p *Pool) Stored() int64   { return p.stored.Load() }

// Stop waits for all workers to drain and finish.
func (p *Pool) Stop() { p.wg.Wait() }

func (p *Pool) worker(ctx context.Context, id int) {
	defer p.wg.Done()
	batch := make([]event.Event, 0, p.cfg.BatchSize)
	ticker := time.NewTicker(p.cfg.FlushPeriod)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		// Use a short standalone context so flushes succeed even during shutdown.
		fctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.store.BatchInsert(fctx, batch); err != nil {
			p.log.Error("batch insert failed", "worker", id, "size", len(batch), "err", err)
		} else {
			p.stored.Add(int64(len(batch)))
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			// Drain whatever is left in the channel before exiting.
			for {
				select {
				case e := <-p.queue:
					batch = append(batch, e)
					if len(batch) >= p.cfg.BatchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case e := <-p.queue:
			batch = append(batch, e)
			if len(batch) >= p.cfg.BatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
