package storage

import (
	"context"
	"sync"

	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/event"
)

// Mock is an in-memory Store for unit and integration tests.
type Mock struct {
	mu       sync.Mutex
	Events   []event.Event
	Batches  int
	FailNext bool
}

func NewMock() *Mock { return &Mock{} }

func (m *Mock) BatchInsert(_ context.Context, events []event.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailNext {
		m.FailNext = false
		return context.DeadlineExceeded
	}
	m.Events = append(m.Events, events...)
	m.Batches++
	return nil
}

func (m *Mock) CampaignMetrics(_ context.Context, campaignID string) (event.Metrics, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := event.Metrics{CampaignID: campaignID}
	for _, e := range m.Events {
		if e.CampaignID != campaignID {
			continue
		}
		switch e.Type {
		case event.TypeClick:
			out.Clicks++
		case event.TypeImpression:
			out.Impressions++
		case event.TypeConversion:
			out.Conversions++
		}
	}
	return out, nil
}

// CampaignMetricsRollup mirrors CampaignMetrics for the in-memory mock.
func (m *Mock) CampaignMetricsRollup(ctx context.Context, campaignID string) (event.Metrics, error) {
	return m.CampaignMetrics(ctx, campaignID)
}

// RefreshRollups is a no-op for the mock.
func (m *Mock) RefreshRollups(context.Context) error { return nil }

func (m *Mock) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Events)
}

func (m *Mock) Ping(context.Context) error { return nil }
func (m *Mock) Close()                     {}
