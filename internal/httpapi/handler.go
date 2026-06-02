package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"golang.org/x/sync/singleflight"

	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/cache"
	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/event"
	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/storage"
	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/worker"
)

// API bundles the dependencies the HTTP handlers need.
type API struct {
	Pool  *worker.Pool
	Store storage.Store
	Cache *cache.Cache
	Log   *slog.Logger

	// Validation holds optional, deployment-specific validation rules applied
	// to every ingested event.
	Validation event.ValidationOptions

	// UseRollups reads campaign metrics from the pre-aggregated hourly rollup
	// table instead of scanning raw events (recommended at scale).
	UseRollups bool

	// sf collapses concurrent cache-miss DB loads for the same campaign into a
	// single query (cache-stampede / dogpile protection).
	sf singleflight.Group
}

func (a *API) Routes(r chi.Router) {
	r.Post("/events", a.postEvents)
	r.Get("/health", a.health)
	r.Get("/campaigns/{id}/metrics", a.getMetrics)
	r.Post("/campaigns/{id}/invalidate", a.invalidate)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// postEvents accepts one event or a JSON array of events and queues them.
func (a *API) postEvents(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)

	// Peek to allow either a single object or an array.
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	var events []event.Event
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &events); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON array"})
			return
		}
	} else {
		var single event.Event
		if err := json.Unmarshal(raw, &single); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON object"})
			return
		}
		events = []event.Event{single}
	}

	accepted, dropped := 0, 0
	for i := range events {
		if err := events[i].Validate(&a.Validation); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if a.Pool.Submit(events[i]) {
			accepted++
		} else {
			dropped++
		}
	}

	// 202 Accepted: we never block the caller on a DB write.
	writeJSON(w, http.StatusAccepted, map[string]int{"accepted": accepted, "dropped": dropped})
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"queue_depth": a.Pool.QueueDepth(),
		"stored":      a.Pool.Stored(),
		"dropped":     a.Pool.Dropped(),
	})
}

func (a *API) getMetrics(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	// 1. Cache-aside: try Redis first.
	if a.Cache != nil {
		if m, err := a.Cache.GetMetrics(ctx, id); err == nil {
			w.Header().Set("X-Cache", "HIT")
			writeJSON(w, http.StatusOK, m)
			return
		} else if !errors.Is(err, cache.ErrMiss) {
			a.Log.Warn("cache get failed", "err", err)
		}
	}

	// 2. Cache miss. Use singleflight so that N concurrent misses for the same
	//    campaign trigger only ONE database aggregation; the rest wait and share
	//    the result. This prevents a cache stampede from overwhelming Postgres.
	//    Note: we deliberately use context.Background() for the DB load (not the
	//    request ctx) so that an early client cancellation doesn't poison the
	//    shared result for everyone else waiting on the same flight.
	v, err, shared := a.sf.Do(id, func() (any, error) {
		var m event.Metrics
		var err error
		if a.UseRollups {
			m, err = a.Store.CampaignMetricsRollup(context.Background(), id)
		} else {
			m, err = a.Store.CampaignMetrics(context.Background(), id)
		}
		if err != nil {
			return nil, err
		}
		if a.Cache != nil {
			if err := a.Cache.SetMetrics(context.Background(), m); err != nil {
				a.Log.Warn("cache set failed", "err", err)
			}
		}
		return m, nil
	})
	if err != nil {
		a.Log.Error("metrics query failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
		return
	}

	m := v.(event.Metrics)
	if shared {
		w.Header().Set("X-Cache", "MISS-COALESCED")
	} else {
		w.Header().Set("X-Cache", "MISS")
	}
	writeJSON(w, http.StatusOK, m)
}

func (a *API) invalidate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if a.Cache != nil {
		if err := a.Cache.Invalidate(r.Context(), id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalidate failed"})
			return
		}
	}
	// Also forget any in-flight singleflight result so the next read reloads.
	a.sf.Forget(id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "invalidated", "campaign_id": id})
}
