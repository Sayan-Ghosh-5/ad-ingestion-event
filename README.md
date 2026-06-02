# Ad Event Ingestion Service

A high-throughput Go backend that ingests ad **click / impression / conversion**
events, buffers them through a goroutine worker pool, batch-writes them to
PostgreSQL, and serves cached aggregation queries from Redis.

Built as a portfolio project to demonstrate Go's real strengths: **concurrency,
channels, graceful shutdown, and high-throughput API design.**

---

## Architecture

```mermaid
flowchart LR
    C[Client / k6] -->|POST /events| R[Chi HTTP router]
    R -->|non-blocking submit| Q[(Buffered channel<br/>queue, cap 10k)]
    Q --> W[Worker pool<br/>N goroutines]
    W -->|batch CopyFrom| PG[(PostgreSQL)]
    C -->|GET /metrics| R
    R --> CA{Redis<br/>cache-aside}
    CA -->|hit| R
    CA -->|miss| PG
```

**Flow:** an event hits `POST /events`, is validated, and pushed onto a buffered
channel — the handler returns **202 Accepted immediately** (never blocks on a DB
write). Worker goroutines drain the channel, accumulate batches of 100, and bulk
insert via `pgx.CopyFrom`. Metrics reads hit Redis first (30s TTL) and only fall
through to Postgres on a miss.

---

## Endpoints

| Method | Path                          | Description                                   |
|--------|-------------------------------|-----------------------------------------------|
| POST   | `/events`                     | Ingest one event or a JSON array. Returns 202.|
| GET    | `/campaigns/{id}/metrics`     | Aggregated clicks/impressions/conversions.    |
| POST   | `/campaigns/{id}/invalidate`  | Drop the cached metrics for a campaign.       |
| GET    | `/health`                     | Status + queue depth + stored/dropped counts. |

### Example

```bash
# single event
curl -XPOST localhost:8080/events -d '{"type":"click","campaign_id":"camp_1","user_id":"u1"}'

# batch
curl -XPOST localhost:8080/events -d '[{"type":"impression","campaign_id":"camp_1"},{"type":"click","campaign_id":"camp_1"}]'

# metrics (note the X-Cache: HIT/MISS response header)
curl -i localhost:8080/campaigns/camp_1/metrics
```

---

## Run it

### Full stack (recommended)

```bash
make up          # docker compose: API + Postgres + Redis, migrations auto-applied
```

The server listens on `:8080`. Tear down with `make down`.

### Locally (bring your own Postgres + Redis)

```bash
export DATABASE_URL="postgres://ads:ads@localhost:5432/ads?sslmode=disable"
export REDIS_ADDR="localhost:6379"
make run
```

### Configuration (env vars)

| Var            | Default                          | Meaning                       |
|----------------|----------------------------------|-------------------------------|
| `DATABASE_URL` | `postgres://ads:ads@localhost…`  | pgx connection string         |
| `REDIS_ADDR`   | `localhost:6379`                 | Redis address                 |
| `HTTP_ADDR`    | `:8080`                          | Listen address                |
| `WORKERS`      | `10`                             | Worker goroutines             |
| `BATCH_SIZE`   | `100`                            | Events per flush              |
| `RATE_RPS`     | `1000`                           | Per-IP rate limit (req/sec)   |
| `RATE_BURST`   | `2000`                           | Token bucket burst            |
| `RATE_IDLE_TTL_SEC` | `600`                       | Evict per-IP limiters idle this long |
| `USE_ROLLUPS`  | `false`                          | Read metrics from hourly rollup table |
| `ROLLUP_REFRESH_SEC` | `60`                       | Background rollup refresh interval |
| `REQUIRE_USER_ID_FOR_CONVERSION` | `false`        | Reject conversions without a user_id |

---

## Tests

```bash
make test    # go test ./... -race
```

Covers worker batch-flush logic, queue-full backpressure, graceful drain on
shutdown, and HTTP endpoints (single + array ingest, validation, metrics, health)
using an in-memory mock `Store` — no database required.

---

## Load test

```bash
make smoke    # quick connectivity + correctness check (low volume)
make bench    # restarts the stack with the rate limit raised, then loads it
make load     # throughput test only (assumes limit already raised)
```

> **Benchmarking gotcha #1 — rate limiter:** the per-IP rate limiter will
> throttle k6 (it runs from a single IP). You must raise the limit **on the
> server container** for the test, then **recreate** the container so it picks
> up the new value:
>
> ```bash
> RATE_RPS=100000 RATE_BURST=200000 docker compose up -d --force-recreate api
> # confirm it took effect (look for rps_per_ip=100000 in the logs):
> docker compose logs api | grep "rate limiter configured"
> k6 run -e TARGET_RPS=5000 -e DURATION=60s loadtest/load.js
> ```
>
> `make bench` does all of this (raise + recreate + verify + run). If you still
> see `429`s, the env override didn't reach the container — check the log line
> above. Setting `RATE_RPS=...` only on the **k6** process does nothing; it must
> be set when starting the **api** container.
>
> **Benchmarking gotcha #2 — co-located load:** running k6 on the same machine
> as the API/Postgres/Redis steals CPU and inflates latency. For honest numbers,
> run k6 from a separate host (or at least close other apps).

> Fill in your own numbers after running — don't quote numbers you haven't measured.

| Metric            | Result        |
|-------------------|---------------|
| Throughput        | _TODO req/s_  |
| p99 latency       | _TODO ms_     |
| Error rate        | _TODO %_      |
| Events persisted  | _TODO_        |

---

## Go concepts demonstrated

- **Goroutines + channels** — worker pool draining a buffered event queue
- **Backpressure** — non-blocking submit; drop + count when the queue is full
- **`context` cancellation** — graceful shutdown drains the queue on SIGTERM
- **Interfaces** — small `Store` interface, real Postgres impl + mock for tests
- **Composable middleware** — token-bucket rate limiter + structured request log
- **Batch I/O** — `pgx.CopyFrom` instead of row-by-row inserts

---

## Production hardening & known trade-offs

This started as a portfolio project and was then reviewed for production
concerns. Here's what was addressed and what remains a deliberate trade-off.

| # | Concern | Status | How |
|---|---------|--------|-----|
| 1 | **Rate-limiter memory leak** — per-IP map grew unbounded → OOM | ✅ Fixed | `IPLimiter` tracks `lastSeen` and a background sweeper evicts idle IPs after `RATE_IDLE_TTL_SEC`. Verified by `TestRateLimiterEvictsStaleIPs`. |
| 2 | **Durability vs throughput** — in-flight events lost on crash | ⚠️ Documented trade-off | See below. `/health` exposes `queue_depth` (events currently at risk). |
| 3 | **Aggregation slow at scale** — `SUM(CASE…)` over millions of rows | ✅ Fixed | `hourly_campaign_metrics` rollup table + `refresh_hourly_metrics()` (migration `002`). Enable with `USE_ROLLUPS=true`; a background goroutine refreshes it. |
| 4 | **Cache stampede / dogpile** — many concurrent misses hammer Postgres | ✅ Fixed | `singleflight.Group` collapses concurrent misses for the same campaign into one DB query. Verified by `TestSingleflightCoalescesConcurrentMisses` (50 reqs → 1 query). |
| 5 | **Validation gap** — `user_id` unchecked | ✅ Fixed | `user_id` is trimmed, length-bounded, and charset-sanitized; optional `REQUIRE_USER_ID_FOR_CONVERSION`. Verified by `event` package tests. |

### On durability (#2)

`POST /events` returns **202 Accepted** the moment an event is queued — it never
blocks on the DB. The cost: events sitting in the in-memory channel or in a
worker's pending batch are **lost if the process is killed**. This is a common
and acceptable trade-off in ad tech, where approximate, high-throughput counting
beats perfect durability.

The at-risk count is observable via `GET /health` → `queue_depth`. Graceful
shutdown (SIGTERM) drains the queue before exit, so the loss window is only
hard crashes / OOM / power loss.

**If you cannot drop events**, replace the in-memory channel with a durable
broker (Kafka / Redpanda / RabbitMQ): the handler produces to the broker
(durable ack), and workers consume from it. The worker-pool and batch-insert
code stays largely the same — only the queue source changes.

## Project layout

```
cmd/server/         main: wiring, config, graceful shutdown
internal/event/     domain types + validation
internal/worker/    buffered-channel worker pool (+ tests)
internal/storage/   Store interface, Postgres impl, mock
internal/cache/     Redis cache-aside helper
internal/httpapi/   HTTP handlers (+ tests)
internal/middleware/ rate limiter + request logger
migrations/         schema + indexes + hourly rollup table/function
loadtest/           k6 script
```
