package storage

import (
	"context"

	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/event"
)

// Store is the persistence interface. It is intentionally small so it can be
// mocked in unit tests and swapped for any backing database.
type Store interface {
	// BatchInsert persists a batch of events in a single round trip.
	BatchInsert(ctx context.Context, events []event.Event) error
	// CampaignMetrics returns aggregated counters for a campaign by scanning
	// raw events. Accurate but O(rows) — fine for small/medium campaigns.
	CampaignMetrics(ctx context.Context, campaignID string) (event.Metrics, error)
	// CampaignMetricsRollup returns counters from the pre-aggregated hourly
	// rollup table. O(hours) regardless of raw event volume — use at scale.
	CampaignMetricsRollup(ctx context.Context, campaignID string) (event.Metrics, error)
	// RefreshRollups recomputes the hourly rollup table for a recent window.
	RefreshRollups(ctx context.Context) error
	// Ping verifies connectivity.
	Ping(ctx context.Context) error
	// Close releases resources.
	Close()
}
