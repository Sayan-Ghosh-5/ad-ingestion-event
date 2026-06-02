package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/event"
)

// countingStore records how many times CampaignMetrics is called and adds a
// small delay so concurrent requests overlap (simulating a heavy query).
type countingStore struct {
	calls atomic.Int64
	delay time.Duration
}

func (c *countingStore) BatchInsert(context.Context, []event.Event) error { return nil }
func (c *countingStore) Ping(context.Context) error                       { return nil }
func (c *countingStore) Close()                                           {}
func (c *countingStore) RefreshRollups(context.Context) error             { return nil }

func (c *countingStore) CampaignMetrics(_ context.Context, id string) (event.Metrics, error) {
	c.calls.Add(1)
	time.Sleep(c.delay)
	return event.Metrics{CampaignID: id, Clicks: 7}, nil
}

func (c *countingStore) CampaignMetricsRollup(ctx context.Context, id string) (event.Metrics, error) {
	return c.CampaignMetrics(ctx, id)
}

// TestSingleflightCoalescesConcurrentMisses proves the cache-stampede fix: when
// many requests miss the cache for the same campaign at once, only ONE DB query
// runs. Without singleflight this count would equal the number of requests.
func TestSingleflightCoalescesConcurrentMisses(t *testing.T) {
	store := &countingStore{delay: 80 * time.Millisecond}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := &API{Store: store, Cache: nil, Log: log} // Cache nil => always "miss"

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/campaigns/hot/metrics", nil)
			rec := httptest.NewRecorder()
			router(api).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", rec.Code)
			}
		}()
	}
	wg.Wait()

	if got := store.calls.Load(); got > 5 {
		t.Fatalf("singleflight not coalescing: %d DB calls for %d concurrent requests (want <=5)", got, n)
	}
	t.Logf("coalesced %d concurrent requests into %d DB call(s)", n, store.calls.Load())
}
