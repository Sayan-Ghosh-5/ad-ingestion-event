package storage

import (
	"context"
	"fmt"

	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/event"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres implements Store using a pgx connection pool.
type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }
func (p *Postgres) Close()                         { p.pool.Close() }

// BatchInsert uses pgx.CopyFrom for high-throughput bulk insertion.
func (p *Postgres) BatchInsert(ctx context.Context, events []event.Event) error {
	if len(events) == 0 {
		return nil
	}
	rows := make([][]any, 0, len(events))
	for _, e := range events {
		var meta []byte
		if len(e.Metadata) > 0 {
			meta = e.Metadata
		}
		rows = append(rows, []any{string(e.Type), e.CampaignID, e.UserID, e.Timestamp, meta})
	}
	_, err := p.pool.CopyFrom(
		ctx,
		pgx.Identifier{"events"},
		[]string{"type", "campaign_id", "user_id", "timestamp", "metadata"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("copyfrom events: %w", err)
	}
	return nil
}

// CampaignMetricsRollup reads from the pre-aggregated hourly rollup table,
// which stays fast no matter how many raw events exist.
func (p *Postgres) CampaignMetricsRollup(ctx context.Context, campaignID string) (event.Metrics, error) {
	const q = `
		SELECT
			COALESCE(SUM(clicks), 0),
			COALESCE(SUM(impressions), 0),
			COALESCE(SUM(conversions), 0)
		FROM hourly_campaign_metrics
		WHERE campaign_id = $1`
	m := event.Metrics{CampaignID: campaignID}
	err := p.pool.QueryRow(ctx, q, campaignID).Scan(&m.Clicks, &m.Impressions, &m.Conversions)
	if err != nil {
		return event.Metrics{}, fmt.Errorf("query rollup metrics: %w", err)
	}
	return m, nil
}

// RefreshRollups recomputes recent hourly buckets from the raw events table.
func (p *Postgres) RefreshRollups(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, "SELECT refresh_hourly_metrics()")
	if err != nil {
		return fmt.Errorf("refresh rollups: %w", err)
	}
	return nil
}

func (p *Postgres) CampaignMetrics(ctx context.Context, campaignID string) (event.Metrics, error) {
	const q = `
		SELECT
			COALESCE(SUM(CASE WHEN type = 'click'      THEN 1 ELSE 0 END), 0) AS clicks,
			COALESCE(SUM(CASE WHEN type = 'impression' THEN 1 ELSE 0 END), 0) AS impressions,
			COALESCE(SUM(CASE WHEN type = 'conversion' THEN 1 ELSE 0 END), 0) AS conversions
		FROM events
		WHERE campaign_id = $1`
	m := event.Metrics{CampaignID: campaignID}
	err := p.pool.QueryRow(ctx, q, campaignID).Scan(&m.Clicks, &m.Impressions, &m.Conversions)
	if err != nil {
		return event.Metrics{}, fmt.Errorf("query metrics: %w", err)
	}
	return m, nil
}
