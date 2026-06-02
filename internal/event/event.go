package event

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
)

// EventType enumerates the kinds of ad events we ingest.
type EventType string

const (
	TypeClick      EventType = "click"
	TypeImpression EventType = "impression"
	TypeConversion EventType = "conversion"
)

func (t EventType) Valid() bool {
	switch t {
	case TypeClick, TypeImpression, TypeConversion:
		return true
	default:
		return false
	}
}

// Field length bounds to keep malformed/abusive payloads out of the database.
const (
	maxCampaignIDLen = 128
	maxUserIDLen     = 128
)

// Event is a single ad event posted to the ingestion API.
type Event struct {
	Type       EventType       `json:"type"`
	CampaignID string          `json:"campaign_id"`
	UserID     string          `json:"user_id"`
	Timestamp  time.Time       `json:"timestamp"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
}

var (
	ErrBadType         = errors.New("invalid event type")
	ErrNoCampaign      = errors.New("campaign_id is required")
	ErrCampaignTooLong = errors.New("campaign_id exceeds max length")
	ErrUserIDTooLong   = errors.New("user_id exceeds max length")
	ErrUserIDChars     = errors.New("user_id contains invalid characters")
	ErrUserIDRequired  = errors.New("user_id is required for conversion events")
)

// ValidationOptions controls optional, deployment-specific validation rules.
type ValidationOptions struct {
	// RequireUserIDForConversion enforces a non-empty UserID on conversion
	// events (needed when you track unique-user conversions).
	RequireUserIDForConversion bool
}

// safeUserIDRune reports whether r is allowed in a sanitized user_id. We allow
// letters, digits, and a small set of ID-friendly separators. This blocks
// control characters, whitespace, and injection-prone punctuation from
// polluting the database.
func safeUserIDRune(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return true
	}
	switch r {
	case '-', '_', '.', ':', '@':
		return true
	}
	return false
}

// Validate checks required fields, sanitizes user_id, and normalizes the
// timestamp. Pass nil opts for default behaviour.
func (e *Event) Validate(opts *ValidationOptions) error {
	if !e.Type.Valid() {
		return ErrBadType
	}

	e.CampaignID = strings.TrimSpace(e.CampaignID)
	if e.CampaignID == "" {
		return ErrNoCampaign
	}
	if len(e.CampaignID) > maxCampaignIDLen {
		return ErrCampaignTooLong
	}

	// UserID is optional in general, but always sanitized when present.
	e.UserID = strings.TrimSpace(e.UserID)
	if e.UserID != "" {
		if len(e.UserID) > maxUserIDLen {
			return ErrUserIDTooLong
		}
		for _, r := range e.UserID {
			if !safeUserIDRune(r) {
				return ErrUserIDChars
			}
		}
	}

	if opts != nil && opts.RequireUserIDForConversion &&
		e.Type == TypeConversion && e.UserID == "" {
		return ErrUserIDRequired
	}

	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	return nil
}

// Metrics is the aggregated view returned by the metrics endpoint.
type Metrics struct {
	CampaignID  string `json:"campaign_id"`
	Clicks      int64  `json:"clicks"`
	Impressions int64  `json:"impressions"`
	Conversions int64  `json:"conversions"`
}
