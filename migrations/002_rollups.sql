-- Hourly rollup table for fast campaign metrics at scale.
--
-- Aggregating raw events with SUM(CASE WHEN ...) gets slow once a campaign has
-- millions of rows. Instead we maintain pre-aggregated hourly counters and read
-- from them. A background job (or a trigger) keeps this table fresh; campaign
-- metrics then become a cheap SUM over a handful of hourly buckets.

CREATE TABLE IF NOT EXISTS hourly_campaign_metrics (
    campaign_id  TEXT        NOT NULL,
    bucket_hour  TIMESTAMPTZ NOT NULL,           -- truncated to the hour
    clicks       BIGINT      NOT NULL DEFAULT 0,
    impressions  BIGINT      NOT NULL DEFAULT 0,
    conversions  BIGINT      NOT NULL DEFAULT 0,
    PRIMARY KEY (campaign_id, bucket_hour)
);

CREATE INDEX IF NOT EXISTS idx_hourly_campaign ON hourly_campaign_metrics (campaign_id);

-- Refresh function: recomputes rollups for the last N hours from raw events.
-- Call periodically (e.g. every minute via cron / pg_cron / app scheduler).
-- ON CONFLICT makes it idempotent so re-running a window is safe.
CREATE OR REPLACE FUNCTION refresh_hourly_metrics(lookback INTERVAL DEFAULT INTERVAL '3 hours')
RETURNS void AS $$
BEGIN
    INSERT INTO hourly_campaign_metrics AS h (campaign_id, bucket_hour, clicks, impressions, conversions)
    SELECT
        campaign_id,
        date_trunc('hour', timestamp) AS bucket_hour,
        COUNT(*) FILTER (WHERE type = 'click')      AS clicks,
        COUNT(*) FILTER (WHERE type = 'impression') AS impressions,
        COUNT(*) FILTER (WHERE type = 'conversion') AS conversions
    FROM events
    WHERE timestamp >= date_trunc('hour', now() - lookback)
    GROUP BY campaign_id, date_trunc('hour', timestamp)
    ON CONFLICT (campaign_id, bucket_hour) DO UPDATE
        SET clicks      = EXCLUDED.clicks,
            impressions = EXCLUDED.impressions,
            conversions = EXCLUDED.conversions;
END;
$$ LANGUAGE plpgsql;
