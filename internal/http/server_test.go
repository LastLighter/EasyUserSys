package httpapi

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestParseID(t *testing.T) {
	id, err := parseID("123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 123 {
		t.Fatalf("unexpected id: %d", id)
	}
	if _, err := parseID(""); err == nil {
		t.Fatalf("expected error for empty id")
	}
}

func TestParseRangeDefaults(t *testing.T) {
	req := &http.Request{URL: &url.URL{}}
	from, to, err := parseRange(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if to.Before(from) {
		t.Fatalf("expected to >= from")
	}
	if to.Sub(from) < 29*24*time.Hour {
		t.Fatalf("expected default range around 30 days")
	}
}

func TestParseRangeCustom(t *testing.T) {
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)
	q := url.Values{}
	q.Set("from", from.Format(time.RFC3339))
	q.Set("to", to.Format(time.RFC3339))
	req := &http.Request{URL: &url.URL{RawQuery: q.Encode()}}
	parsedFrom, parsedTo, err := parseRange(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !parsedFrom.Equal(from) || !parsedTo.Equal(to) {
		t.Fatalf("range mismatch")
	}
}
