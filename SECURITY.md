# Security

This document records the security & reliability review of the Ad Event
Ingestion Service and the hardening applied. It also describes how to report
issues and the current threat-model boundaries.

## Reporting a vulnerability

Please open a private security advisory on the GitHub repo, or email the
maintainer listed in `go.mod`. Do **not** file public issues for exploitable
vulnerabilities. Expect an acknowledgement within a few business days.

## Hardening review

The service was reviewed for production reliability and abuse resistance. The
findings and their resolutions:

| # | Issue | Severity | Status | Resolution |
|---|-------|----------|--------|------------|
| 1 | **Rate-limiter memory leak** — the per-IP limiter map grew without bound (one entry per unique client IP), enabling a slow OOM / resource-exhaustion DoS. | High | ✅ Fixed | `IPLimiter` now tracks `lastSeen` per IP and a background sweeper evicts idle entries after `RATE_IDLE_TTL_SEC` (default 600s). Verified by `TestRateLimiterEvictsStaleIPs`. |
| 2 | **Event durability** — events buffered in memory are lost on crash/OOM/power loss. | Medium | ⚠️ Accepted trade-off | Speed-over-durability is intentional for ad-tech volumes. At-risk count is observable via `GET /health` → `queue_depth`; SIGTERM drains the queue. Durable-broker migration path documented in `README.md`. |
| 3 | **Aggregation cost** — on-the-fly `SUM(CASE…)` over the raw `events` table degrades to a full/partial scan at millions of rows, a DoS amplifier on the read path. | Medium | ✅ Fixed | Pre-aggregated `hourly_campaign_metrics` rollup table + `refresh_hourly_metrics()` (migration `002`). Enable with `USE_ROLLUPS=true`. |
| 4 | **Cache stampede (dogpile)** — concurrent cache misses for a hot campaign all hit Postgres simultaneously, risking DB overload. | Medium | ✅ Fixed | `singleflight.Group` collapses concurrent misses for the same key into one DB query. Verified by `TestSingleflightCoalescesConcurrentMisses` (50 reqs → 1 query). |
| 5 | **Input validation gap** — `user_id` (and effectively `campaign_id` length) were unvalidated, allowing malformed/oversized/control-character data into the database. | Low | ✅ Fixed | All string IDs are trimmed and length-bounded (128); `user_id` is charset-sanitized (letters, digits, `-_.:@` only), rejecting control chars and whitespace. Optional `REQUIRE_USER_ID_FOR_CONVERSION`. Verified by `event` package tests. |

## Defense-in-depth controls in place

- **Per-IP rate limiting** (token bucket) with bounded memory.
- **Input validation & sanitization** on all ingested fields.
- **Parameterized queries** throughout (`pgx` placeholders) — no string-built SQL,
  so SQL injection is not possible on the query paths.
- **Non-blocking ingestion with backpressure** — a flooded queue drops and counts
  events rather than exhausting memory or blocking the event loop.
- **Graceful shutdown** drains in-flight work on SIGTERM.
- **Minimal runtime image** — multi-stage build to a `scratch` image running as a
  non-root user (`65534:65534`), no shell or package manager in the final image.

## Threat-model boundaries (not yet implemented)

These are **out of scope** for the current build and should be added before any
public production deployment:

- **AuthN/AuthZ** — `POST /events` and metrics endpoints are unauthenticated.
  Add API keys / mTLS / a gateway in front for production.
- **TLS termination** — expected to be handled by a load balancer / ingress.
- **Request body size limits** — add `http.MaxBytesReader` to cap payload size.
- **Secrets management** — `DATABASE_URL` is passed via env var; use a secrets
  manager (Vault, AWS/GCP secret stores) in production rather than plaintext env.
- **Per-campaign / per-key quotas** — current limiting is per-IP only.

## Dependency hygiene

- Run `go vet ./...` and `go test ./... -race` in CI (all currently pass).
- Run `govulncheck ./...` to scan for known CVEs in dependencies.
- Keep `go.mod` pinned and update deliberately.

## Supported versions

Only the latest `main` is supported. Security fixes are applied to `main` and a
new tagged release is cut.
