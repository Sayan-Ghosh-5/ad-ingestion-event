package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/storage"
	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/worker"
	"github.com/go-chi/chi/v5"
)

func newTestAPI(t *testing.T) (*API, *storage.Mock, func()) {
	t.Helper()
	mock := storage.NewMock()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := worker.Config{Workers: 2, QueueSize: 1000, BatchSize: 10, FlushPeriod: 20 * time.Millisecond}
	pool := worker.New(cfg, mock, log)
	ctx, cancel := context.WithCancel(context.Background())
	pool.Start(ctx)
	api := &API{Pool: pool, Store: mock, Cache: nil, Log: log}
	cleanup := func() { cancel(); pool.Stop() }
	return api, mock, cleanup
}

func router(api *API) http.Handler {
	r := chi.NewRouter()
	api.Routes(r)
	return r
}

func TestPostEventsSingle(t *testing.T) {
	api, mock, cleanup := newTestAPI(t)
	defer cleanup()

	body := `{"type":"click","campaign_id":"c1","user_id":"u1"}`
	req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(body))
	rec := httptest.NewRecorder()
	router(api).ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	time.Sleep(80 * time.Millisecond)
	if mock.Count() != 1 {
		t.Fatalf("expected 1 stored, got %d", mock.Count())
	}
}

func TestPostEventsArray(t *testing.T) {
	api, mock, cleanup := newTestAPI(t)
	defer cleanup()

	body := `[
		{"type":"impression","campaign_id":"c1"},
		{"type":"click","campaign_id":"c1"},
		{"type":"conversion","campaign_id":"c1"}
	]`
	req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(body))
	rec := httptest.NewRecorder()
	router(api).ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}
	time.Sleep(80 * time.Millisecond)
	if mock.Count() != 3 {
		t.Fatalf("expected 3 stored, got %d", mock.Count())
	}
}

func TestPostEventsValidationError(t *testing.T) {
	api, _, cleanup := newTestAPI(t)
	defer cleanup()

	body := `{"type":"banana","campaign_id":"c1"}` // invalid type
	req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(body))
	rec := httptest.NewRecorder()
	router(api).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGetMetrics(t *testing.T) {
	api, _, cleanup := newTestAPI(t)
	defer cleanup()

	// Ingest some events first.
	body := `[
		{"type":"click","campaign_id":"c9"},
		{"type":"click","campaign_id":"c9"},
		{"type":"impression","campaign_id":"c9"}
	]`
	post := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(body))
	router(api).ServeHTTP(httptest.NewRecorder(), post)
	time.Sleep(80 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/campaigns/c9/metrics", nil)
	rec := httptest.NewRecorder()
	router(api).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"clicks":2`) {
		t.Fatalf("expected 2 clicks in body, got %s", rec.Body.String())
	}
}

func TestHealth(t *testing.T) {
	api, _, cleanup := newTestAPI(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router(api).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("unexpected health body: %s", rec.Body.String())
	}
}
