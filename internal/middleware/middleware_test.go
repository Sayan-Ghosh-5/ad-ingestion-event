package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterBlocksOverBurst(t *testing.T) {
	l := NewIPLimiter(1, 2, time.Minute) // 1 rps, burst 2
	defer l.Stop()

	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	codes := make([]int, 4)
	for i := 0; i < 4; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		codes[i] = rec.Code
	}

	// First two within burst -> 200, then rate-limited -> 429.
	if codes[0] != 200 || codes[1] != 200 {
		t.Fatalf("expected first two 200, got %v", codes)
	}
	if codes[2] != 429 || codes[3] != 429 {
		t.Fatalf("expected later requests 429, got %v", codes)
	}
}

func TestRateLimiterEvictsStaleIPs(t *testing.T) {
	// Short TTL so the sweeper runs quickly within the test.
	l := NewIPLimiter(100, 100, 50*time.Millisecond)
	defer l.Stop()

	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	// Hit from 5 distinct IPs.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0." + string(rune('1'+i)) + ":9999"
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	if got := l.Len(); got != 5 {
		t.Fatalf("expected 5 tracked IPs, got %d", got)
	}

	// Wait for entries to go stale and be swept (TTL + a sweep tick + margin).
	time.Sleep(150 * time.Millisecond)

	if got := l.Len(); got != 0 {
		t.Fatalf("expected stale IPs evicted (0), got %d — memory leak not fixed", got)
	}
}

func TestRateLimiterStopIdempotent(t *testing.T) {
	l := NewIPLimiter(10, 10, time.Minute)
	l.Stop()
	l.Stop() // must not panic
}
