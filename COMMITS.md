# Suggested commits (Conventional Commits)

Group the changes into focused commits so the history reads as a story. Run from
the project root.

## First time only — initialise the repo

```bash
git init
git add .gitignore .dockerignore go.mod go.sum Makefile Dockerfile docker-compose.yml \
        README.md SECURITY.md cmd internal migrations loadtest
git commit -m "feat: ad event ingestion service (Go + chi + pgx + redis)

High-throughput ingestion API: chi router, buffered-channel worker pool with
batch pgx.CopyFrom inserts, Redis cache-aside metrics, per-IP rate limiting,
structured logging, graceful shutdown, Docker Compose stack, and k6 load test."
```

## If the repo already exists — commit the hardening as separate features

```bash
# 1. Rate-limiter memory leak
git add internal/middleware/middleware.go internal/middleware/middleware_test.go cmd/server/main.go
git commit -m "fix(middleware): evict idle per-IP rate limiters to prevent OOM

The per-IP limiter map grew unbounded (one entry per unique client IP),
leaking memory until OOM on public traffic. IPLimiter now tracks lastSeen
and a background sweeper evicts entries idle longer than RATE_IDLE_TTL_SEC.
Added Stop() (idempotent), wired into graceful shutdown.

Tested: TestRateLimiterEvictsStaleIPs, TestRateLimiterBlocksOverBurst."

# 2. Cache stampede / dogpile
git add internal/httpapi/handler.go internal/httpapi/stampede_test.go go.mod go.sum
git commit -m "fix(httpapi): collapse concurrent cache misses with singleflight

Concurrent metric reads for a hot campaign all missed the cache and hit
Postgres at once (dogpile). Wrapped the miss->DB->cache path in a
singleflight.Group so N concurrent misses for the same campaign trigger a
single query; invalidate() calls sf.Forget. Adds X-Cache: MISS-COALESCED.

Tested: TestSingleflightCoalescesConcurrentMisses (50 reqs -> 1 DB call)."

# 3. Rollup table for scalable aggregation
git add migrations/002_rollups.sql internal/storage/storage.go internal/storage/postgres.go \
        internal/storage/mock.go internal/httpapi/handler.go cmd/server/main.go docker-compose.yml
git commit -m "feat(metrics): add hourly rollup table for O(hours) aggregation

On-the-fly SUM(CASE...) over raw events degrades at millions of rows.
Adds hourly_campaign_metrics + idempotent refresh_hourly_metrics(), a
rollup-backed read path (USE_ROLLUPS), and a background refresher goroutine
(ROLLUP_REFRESH_SEC)."

# 4. Stronger input validation
git add internal/event/event.go internal/event/event_test.go internal/httpapi/handler.go cmd/server/main.go
git commit -m "feat(event): validate and sanitize user_id and campaign_id

Trim and length-bound IDs (max 128); charset-sanitize user_id (letters,
digits, -_.:@ only), rejecting control chars and whitespace. Optional
REQUIRE_USER_ID_FOR_CONVERSION enforces user_id on conversion events.

Tested: event package validation tests."

# 5. Docs
git add SECURITY.md README.md docker-compose.yml
git commit -m "docs: add SECURITY.md and document hardening + durability trade-off

Records the reliability/abuse review (rate-limiter leak, cache stampede,
aggregation scaling, validation) and the deliberate speed-over-durability
trade-off with the durable-broker migration path. New env vars documented."
```

# 6. Load-test tooling + module path
git add loadtest/ Makefile README.md go.mod
git commit -m "test(loadtest): add smoke + bench scripts and document rate-limit gotcha

The throughput test runs from a single IP, so the per-IP rate limiter
throttles it unless raised. Adds loadtest/smoke.js (connectivity check),
loadtest/load.js throttle metric + thresholds, and make smoke/bench/load
targets. bench restarts the stack with RATE_RPS/RATE_BURST raised."

## Notes

- `go.mod`/`go.sum` change because `golang.org/x/sync` (singleflight) is now a
  direct dependency — include them with commit #2.
- The module path is `github.com/Sayan-Ghosh-5/ad-ingestion-event`. If you fork
  or rename the repo, update the `module` line in `go.mod` and re-run
  `goimports`/`go build`.
- Verify before pushing:

  ```bash
  go vet ./... && go test ./... -race -count=1 && gofmt -l .
  ```
