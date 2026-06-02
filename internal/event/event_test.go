package event

import (
	"errors"
	"testing"
	"time"
)

func TestValidateBasics(t *testing.T) {
	e := &Event{Type: TypeClick, CampaignID: "c1"}
	if err := e.Validate(nil); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
	if e.Timestamp.IsZero() {
		t.Fatal("timestamp should be defaulted")
	}
}

func TestValidateRejectsBadType(t *testing.T) {
	e := &Event{Type: "banana", CampaignID: "c1"}
	if err := e.Validate(nil); !errors.Is(err, ErrBadType) {
		t.Fatalf("expected ErrBadType, got %v", err)
	}
}

func TestValidateRequiresCampaign(t *testing.T) {
	e := &Event{Type: TypeClick, CampaignID: "   "} // whitespace only
	if err := e.Validate(nil); !errors.Is(err, ErrNoCampaign) {
		t.Fatalf("expected ErrNoCampaign, got %v", err)
	}
}

func TestValidateSanitizesUserID(t *testing.T) {
	// Valid id with allowed separators is trimmed and accepted.
	e := &Event{Type: TypeClick, CampaignID: "c1", UserID: "  user_42.a:b@x  "}
	if err := e.Validate(nil); err != nil {
		t.Fatalf("expected valid user id, got %v", err)
	}
	if e.UserID != "user_42.a:b@x" {
		t.Fatalf("user id not trimmed: %q", e.UserID)
	}

	// Control/whitespace chars rejected.
	bad := &Event{Type: TypeClick, CampaignID: "c1", UserID: "user 42\n"}
	if err := bad.Validate(nil); !errors.Is(err, ErrUserIDChars) {
		t.Fatalf("expected ErrUserIDChars, got %v", err)
	}

	// Over-length rejected.
	long := &Event{Type: TypeClick, CampaignID: "c1", UserID: makeString(200)}
	if err := long.Validate(nil); !errors.Is(err, ErrUserIDTooLong) {
		t.Fatalf("expected ErrUserIDTooLong, got %v", err)
	}
}

func TestValidateRequireUserIDForConversion(t *testing.T) {
	opts := &ValidationOptions{RequireUserIDForConversion: true}

	// Conversion without user id -> rejected.
	conv := &Event{Type: TypeConversion, CampaignID: "c1"}
	if err := conv.Validate(opts); !errors.Is(err, ErrUserIDRequired) {
		t.Fatalf("expected ErrUserIDRequired, got %v", err)
	}

	// Click without user id -> still fine.
	click := &Event{Type: TypeClick, CampaignID: "c1"}
	if err := click.Validate(opts); err != nil {
		t.Fatalf("click should not require user id, got %v", err)
	}

	// Conversion with user id -> fine.
	ok := &Event{Type: TypeConversion, CampaignID: "c1", UserID: "u1", Timestamp: time.Now()}
	if err := ok.Validate(opts); err != nil {
		t.Fatalf("expected valid conversion, got %v", err)
	}
}

func makeString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
