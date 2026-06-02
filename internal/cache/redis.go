package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/event"
	"github.com/redis/go-redis/v9"
)

// ErrMiss is returned when a key is not present in the cache.
var ErrMiss = errors.New("cache miss")

// Cache wraps a Redis client for caching campaign metrics (cache-aside pattern).
type Cache struct {
	rdb *redis.Client
	ttl time.Duration
}

func New(addr string, ttl time.Duration) *Cache {
	return &Cache{
		rdb: redis.NewClient(&redis.Options{Addr: addr}),
		ttl: ttl,
	}
}

func (c *Cache) Ping(ctx context.Context) error { return c.rdb.Ping(ctx).Err() }
func (c *Cache) Close() error                   { return c.rdb.Close() }

func key(campaignID string) string { return "metrics:" + campaignID }

func (c *Cache) GetMetrics(ctx context.Context, campaignID string) (event.Metrics, error) {
	val, err := c.rdb.Get(ctx, key(campaignID)).Result()
	if errors.Is(err, redis.Nil) {
		return event.Metrics{}, ErrMiss
	}
	if err != nil {
		return event.Metrics{}, fmt.Errorf("redis get: %w", err)
	}
	var m event.Metrics
	if err := json.Unmarshal([]byte(val), &m); err != nil {
		return event.Metrics{}, fmt.Errorf("unmarshal: %w", err)
	}
	return m, nil
}

func (c *Cache) SetMetrics(ctx context.Context, m event.Metrics) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, key(m.CampaignID), b, c.ttl).Err()
}

func (c *Cache) Invalidate(ctx context.Context, campaignID string) error {
	return c.rdb.Del(ctx, key(campaignID)).Err()
}
