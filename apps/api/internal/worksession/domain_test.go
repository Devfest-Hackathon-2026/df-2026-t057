package worksession

import (
	"testing"
	"time"
)

func TestLimitFor(t *testing.T) {
	for planned, want := range map[int]int{30: 60, 600: 600, 3600: 1500} {
		if got := LimitFor(planned); got != want {
			t.Fatalf("%d: %d", planned, got)
		}
	}
}

func TestPauseResumeAndElapsed(t *testing.T) {
	t0 := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
	s := Start("s", "t", t0, 600)
	if !s.Pause(t0.Add(3*time.Minute)) || s.Pause(t0.Add(4*time.Minute)) {
		t.Fatal("pause should be idempotent")
	}
	if !s.Resume(t0.Add(43*time.Minute)) || s.Resume(t0.Add(44*time.Minute)) {
		t.Fatal("resume should be idempotent")
	}
	if got := s.ElapsedSeconds(t0.Add(45 * time.Minute)); got != 300 {
		t.Fatalf("elapsed = %d", got)
	}
	if got := s.ElapsedSeconds(t0.Add(5 * time.Hour)); got != 600 {
		t.Fatalf("capped elapsed = %d", got)
	}
	s.Close(t0.Add(5 * time.Hour))
	if s.Running() || s.ElapsedSeconds(t0.Add(6*time.Hour)) != 600 {
		t.Fatalf("closed %+v", s)
	}
}
