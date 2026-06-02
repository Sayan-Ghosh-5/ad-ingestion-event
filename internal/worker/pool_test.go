package worker

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/event"
	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/storage"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestPoolFlushesByBatchSize(t *testing.T) {
	mock := storage.NewMock()
	cfg := Config{Workers: 1, QueueSize: 1000, BatchSize: 5, FlushPeriod: time.Hour}
	p := New(cfg, mock, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)

	for i := 0; i < 12; i++ {
		if !p.Submit(event.Event{Type: event.TypeClick, CampaignID: "c1", Timestamp: time.Now()}) {
			t.Fatal("submit unexpectedly dropped")
		}
	}

	// Give workers a moment, then shut down to flush the remainder.
	time.Sleep(100 * time.Millisecond)
	cancel()
	p.Stop()

	if got := mock.Count(); got != 12 {
		t.Fatalf("expected 12 stored events, got %d", got)
	}
}

func TestPoolDropsWhenQueueFull(t *testing.T) {
	mock := storage.NewMock()
	cfg := Config{Workers: 0, QueueSize: 2, BatchSize: 100, FlushPeriod: time.Hour}
	// Workers=0 normalizes to 1, but we don't Start() it, so nothing drains.
	p := New(cfg, mock, testLogger())

	ok1 := p.Submit(event.Event{Type: event.TypeClick, CampaignID: "c1"})
	ok2 := p.Submit(event.Event{Type: event.TypeClick, CampaignID: "c1"})
	ok3 := p.Submit(event.Event{Type: event.TypeClick, CampaignID: "c1"})

	if !ok1 || !ok2 {
		t.Fatal("first two submits should succeed")
	}
	if ok3 {
		t.Fatal("third submit should be dropped (queue full)")
	}
	if p.Dropped() != 1 {
		t.Fatalf("expected 1 dropped, got %d", p.Dropped())
	}
}

func TestGracefulDrainOnCancel(t *testing.T) {
	mock := storage.NewMock()
	cfg := Config{Workers: 3, QueueSize: 1000, BatchSize: 50, FlushPeriod: time.Hour}
	p := New(cfg, mock, testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)

	for i := 0; i < 137; i++ {
		p.Submit(event.Event{Type: event.TypeImpression, CampaignID: "c2", Timestamp: time.Now()})
	}
	cancel()
	p.Stop()

	if got := mock.Count(); got != 137 {
		t.Fatalf("expected all 137 events drained, got %d", got)
	}
}
