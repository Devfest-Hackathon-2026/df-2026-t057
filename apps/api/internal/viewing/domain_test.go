package viewing

import (
	"errors"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
)

func TestCharge(t *testing.T) {
	t0 := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
	cases := []struct {
		name                         string
		planned, consumed, available int
		elapsed                      time.Duration
		reported                     int
		want                         int
		ended                        bool
	}{
		{"normal", 300, 0, 600, 20 * time.Second, 10, 10, false},
		{"wall clock bound", 300, 0, 600, 3 * time.Second, 10, 3, false},
		{"session remaining", 300, 295, 600, time.Minute, 10, 5, true},
		{"daily remaining", 300, 0, 5, time.Minute, 10, 5, true},
	}
	for _, c := range cases {
		s := Session{PlannedSeconds: c.planned, ConsumedSeconds: c.consumed, LastReportAt: t0}
		got := Charge(&s, c.reported, c.available, t0.Add(c.elapsed))
		if got != c.want || (s.EndedAt != nil) != c.ended {
			t.Fatalf("%s: got %d ended=%v", c.name, got, s.EndedAt != nil)
		}
	}
}

func TestCheckStart(t *testing.T) {
	ended := time.Now()
	active := &Session{ID: "a"}
	done := &Session{ID: "a", EndedAt: &ended}
	cases := []struct {
		current    *Session
		extendFrom string
		available  int
		want       error
	}{
		{nil, "", 600, nil},
		{active, "", 600, apperr.Conflict("viewing.session_active")},
		{done, "b", 600, apperr.Conflict("viewing.extend_mismatch")},
		{done, "a", 0, apperr.Conflict("viewing.exhausted")},
		{done, "a", 10, nil},
	}
	for i, c := range cases {
		if err := CheckStart(c.current, c.extendFrom, c.available); !errors.Is(err, c.want) && (err != nil || c.want != nil) {
			t.Fatalf("%d: %v", i, err)
		}
	}
}
