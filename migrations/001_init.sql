CREATE TABLE IF NOT EXISTS events (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    type        TEXT        NOT NULL,
    campaign_id TEXT        NOT NULL,
    user_id     TEXT,
    timestamp   TIMESTAMPTZ NOT NULL DEFAULT now(),
    metadata    JSONB
);

-- Composite index supports the metrics aggregation query (filter by campaign).
CREATE INDEX IF NOT EXISTS idx_events_campaign      ON events (campaign_id);
CREATE INDEX IF NOT EXISTS idx_events_campaign_type ON events (campaign_id, type);
CREATE INDEX IF NOT EXISTS idx_events_timestamp     ON events (timestamp);
