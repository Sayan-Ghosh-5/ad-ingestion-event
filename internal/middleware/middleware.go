package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// statusRecorder captures the response status code for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Logger logs method, path, status and latency for each request.
func Logger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			log.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"latency_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

// limiterEntry pairs a token-bucket limiter with the last time its IP was seen,
// so idle entries can be evicted by the background sweeper.
type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// IPLimiter is a per-IP token-bucket rate limiter with automatic eviction of
// stale entries. Without eviction the map grows unbounded (one entry per unique
// client IP) and eventually OOM-kills the process on a public endpoint.
type IPLimiter struct {
	mu       sync.Mutex
	entries  map[string]*limiterEntry
	r        rate.Limit
	burst    int
	ttl      time.Duration // evict entries idle longer than this
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewIPLimiter builds a limiter allowing rps requests/sec with the given burst.
// idleTTL controls how long an unseen IP is retained before eviction.
// A background goroutine sweeps the map every idleTTL; call Stop to end it.
func NewIPLimiter(rps float64, burst int, idleTTL time.Duration) *IPLimiter {
	if idleTTL <= 0 {
		idleTTL = 10 * time.Minute
	}
	l := &IPLimiter{
		entries: make(map[string]*limiterEntry),
		r:       rate.Limit(rps),
		burst:   burst,
		ttl:     idleTTL,
		stopCh:  make(chan struct{}),
	}
	go l.sweepLoop()
	return l
}

func (l *IPLimiter) get(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[ip]
	if !ok {
		e = &limiterEntry{limiter: rate.NewLimiter(l.r, l.burst)}
		l.entries[ip] = e
	}
	e.lastSeen = time.Now()
	return e.limiter
}

// sweepLoop periodically removes entries that have not been seen within ttl.
func (l *IPLimiter) sweepLoop() {
	ticker := time.NewTicker(l.ttl)
	defer ticker.Stop()
	for {
		select {
		case <-l.stopCh:
			return
		case now := <-ticker.C:
			l.mu.Lock()
			for ip, e := range l.entries {
				if now.Sub(e.lastSeen) > l.ttl {
					delete(l.entries, ip)
				}
			}
			l.mu.Unlock()
		}
	}
}

// Len reports the number of tracked IPs (useful for tests / metrics).
func (l *IPLimiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// Stop terminates the background sweeper. Safe to call multiple times.
func (l *IPLimiter) Stop() {
	l.stopOnce.Do(func() { close(l.stopCh) })
}

// Middleware applies the per-IP limiter to an HTTP handler.
func (l *IPLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		if !l.get(ip).Allow() {
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
