# Anchor paths to the Makefile's own directory so targets work from anywhere
# (and from Git Bash / mingw32-make on Windows).
MAKEFILE_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

.PHONY: run test vet build docker up down load smoke bench fmt tidy

run:        ## Run the server locally (needs Postgres + Redis)
	go run ./cmd/server

test:       ## Run unit + integration tests
	go test ./... -race -count=1

vet:        ## Static analysis
	go vet ./...

fmt:        ## Format code
	gofmt -w .

tidy:       ## Tidy modules
	go mod tidy

build:      ## Build the binary
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/server ./cmd/server

docker:     ## Build the production image
	docker build -t ad-event-ingestion:latest .

up:         ## Start the full stack (API + Postgres + Redis)
	docker compose up --build

down:       ## Tear down the stack and volumes
	docker compose down -v

load:       ## Run the k6 throughput test (override with TARGET_RPS / DURATION)
	cd "$(MAKEFILE_DIR)" && k6 run loadtest/load.js

smoke:      ## Quick k6 connectivity + correctness check
	cd "$(MAKEFILE_DIR)" && k6 run loadtest/smoke.js

bench:      ## Restart stack with rate limit raised (via .env.bench), verify, then load test
	docker compose --env-file .env.bench up -d --build --force-recreate
	@echo "Waiting for API to become healthy..."
	@sleep 6
	@echo "---- Verifying rate limit took effect (must show a large rps_per_ip) ----"
	@docker compose logs api | grep "rate limiter configured" | tail -1 || true
	@echo "---- Health check ----"
	@curl -fsS http://localhost:8080/health || echo "API not reachable on :8080 — is the container up?"
	@echo
	cd "$(MAKEFILE_DIR)" && k6 run loadtest/load.js
