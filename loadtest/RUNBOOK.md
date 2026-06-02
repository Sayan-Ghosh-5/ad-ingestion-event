# Load-test runbook (do this in order)

Your last runs failed for THREE reasons, none of them a code bug:
1. The API container wasn't up / not reachable (`connection refused`).
2. The rate limit was still 100 (override never reached the container) → 95% `429`.
3. k6 on the same machine ran out of VUs at 5000/s (`Insufficient VUs`,
   `dropped_iterations`, 22s latencies) — that's k6/OS saturation, not the server.

Follow these steps exactly.

## Step 1 — Start the stack with the raised limit

```bash
# from the project root (the folder with docker-compose.yml)
docker compose --env-file .env.bench up -d --build --force-recreate
```

## Step 2 — VERIFY before running k6 (this is the step that was skipped)

```bash
# (a) the API container must be running:
docker compose ps

# (b) the limiter must show a LARGE number, not 100:
docker compose logs api | grep "rate limiter configured"
#   expect: ... "rate limiter configured" rps_per_ip=1000000 burst=2000000

# (c) the API must answer on :8080:
curl http://localhost:8080/health
#   expect: {"status":"ok", ...}
```

If (a) shows the api container `Exited` / restarting → run `docker compose logs api`
and read the error (usually it can't reach Postgres yet; wait and retry).
If (b) still says `rps_per_ip=100` → you're on old code or wrong dir. Make sure
this repo (with `${RATE_RPS:-100}` in docker-compose.yml) is what you `up`'d.
If (c) refuses the connection → the container isn't ready; wait 5s and retry.

## Step 3 — Run a SMOKE test first (1 VU, proves correctness)

```bash
k6 run loadtest/smoke.js
```
All checks should pass (202 ingest, 200 health, 200 metrics).

## Step 4 — Baseline load (modest, clean)

```bash
k6 run loadtest/load.js        # defaults to 1000 rps now
```
You want: `status is 202` ≈ 100%, `events_throttled_429` rate ≈ 0%, p99 sane.

## Step 5 — Push higher ONLY if step 4 is clean

```bash
k6 run -e TARGET_RPS=3000 -e DURATION=60s loadtest/load.js
```
If you see `Insufficient VUs` or big `dropped_iterations`, your machine (k6 +
server sharing CPU) is the bottleneck, not the service. For real high numbers,
run k6 from a SEPARATE machine pointed at the server's IP:

```bash
k6 run -e BASE_URL=http://<server-ip>:8080 -e TARGET_RPS=5000 loadtest/load.js
```

## Step 6 — Check the server's own view

```bash
curl http://localhost:8080/health
#   stored = events persisted, dropped = queue overflow (bump WORKERS if >0)
```

`make bench` automates steps 1, 2, and 4.
