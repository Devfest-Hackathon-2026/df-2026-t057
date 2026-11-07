package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
	"google.golang.org/genai"
)

func TestAIUsageMinuteAndDayBoundaries(t *testing.T) {
	now := time.Date(2026, 11, 7, 23, 59, 0, 0, time.UTC)
	limits := AILimits{1, 2, 10, 10}
	u, g, err := reserveAIUsage(aiUsage{}, aiUsage{}, limits, now)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = reserveAIUsage(u, g, limits, now.Add(59*time.Second))
	var limited *AILimitError
	if !errors.As(err, &limited) || !limited.RetryAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("minute limit: %v", err)
	}
	u, g, err = reserveAIUsage(u, g, limits, now.Add(time.Minute))
	if err != nil || u.DayCalls != 1 || g.DayCalls != 1 {
		t.Fatalf("UTC day did not reset: %v", err)
	}
	u, g, err = reserveAIUsage(u, g, limits, now.Add(2*time.Minute))
	if err != nil || u.DayCalls != 2 {
		t.Fatal("minute reset lost daily usage")
	}
	_, _, err = reserveAIUsage(u, g, limits, now.Add(3*time.Minute))
	if !errors.As(err, &limited) || !limited.RetryAt.Equal(now.Add(24*time.Hour+time.Minute)) {
		t.Fatalf("daily limit: %v", err)
	}
}

func TestAIUsageGlobalRejectionDoesNotSpendAnotherUsersQuota(t *testing.T) {
	now := time.Now()
	limits := AILimits{5, 30, 1, 300}
	_, global, err := reserveAIUsage(aiUsage{}, aiUsage{}, limits, now)
	if err != nil {
		t.Fatal(err)
	}
	user, nextGlobal, err := reserveAIUsage(aiUsage{}, global, limits, now)
	if err == nil || user.MinuteCalls != 0 || user.DayCalls != 0 || nextGlobal.DayCalls != 1 {
		t.Fatal("global rejection spent quota")
	}
	if _, _, err := reserveAIUsage(user, global, limits, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func TestAIUsageConcurrentUsersShareGlobalLimit(t *testing.T) {
	store := NewMemoryAIUsageStore()
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := range 30 {
		wg.Go(func() {
			err := store.Take(t.Context(), strconv.Itoa(i), AILimits{5, 30, 7, 7})
			if err == nil {
				accepted.Add(1)
				return
			}
			var limited *AILimitError
			if !errors.As(err, &limited) {
				t.Errorf("unexpected failure: %v", err)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 7 {
		t.Fatalf("global limit admitted %d calls", accepted.Load())
	}
}

type unavailableAIUsage struct{}

func (unavailableAIUsage) Take(context.Context, string, AILimits) error {
	return errors.New("private-store-detail")
}

func TestAllGeminiOperationsShareQuotaAndFailedAttemptsAreCounted(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeJSON(w, 503, map[string]any{"error": map[string]any{"code": 503, "message": "private-service-detail"}})
	}))
	defer upstream.Close()
	client, err := genai.NewClient(t.Context(), &genai.ClientConfig{Backend: genai.BackendGeminiAPI, APIKey: "test-only", HTTPOptions: genai.HTTPOptions{BaseURL: upstream.URL}})
	if err != nil {
		t.Fatal(err)
	}
	p := &Planner{Client: client, Model: "test", Usage: NewMemoryAIUsageStore(), Limits: AILimits{4, 4, 10, 10}}
	_, _ = p.Parse(t.Context(), "a", "課題", Profile{})
	_, _ = p.Detail(t.Context(), "a", Task{Minutes: 30, Deadline: time.Now().Add(time.Hour)}, Profile{})
	_, _ = p.Adjust(t.Context(), "a", Draft{}, "調整して", Profile{})
	_, _, _ = p.Consult(t.Context(), "a", "相談", nil, NewState(time.Now()), "")
	_, err = p.Parse(t.Context(), "a", "課題", Profile{})
	var limited *AILimitError
	if calls.Load() != 4 || !errors.As(err, &limited) {
		t.Fatalf("quota or SDK retry bypass: calls=%d err=%v", calls.Load(), err)
	}
	response := httptest.NewRecorder()
	respond(response, nil, err)
	if response.Code != 429 || response.Header().Get("Retry-After") == "" {
		t.Fatal("limit was not returned as retryable HTTP 429")
	}
	plan, err := p.Detail(t.Context(), "a", Task{Title: "手入力の課題", Minutes: 30, Deadline: time.Now().Add(time.Hour)}, Profile{})
	if err != nil || !strings.Contains(plan.Reason, "AIの利用上限") || calls.Load() != 4 || plan.End.Sub(plan.Start) != 30*time.Minute {
		t.Fatal("quota exhaustion blocked manual planning or made an unreserved request")
	}
	_, _ = p.Parse(t.Context(), "b", "課題", Profile{})
	if calls.Load() != 5 {
		t.Fatal("one user's limit blocked another user")
	}
	for _, usage := range []AIUsageStore{nil, unavailableAIUsage{}} {
		p.Usage = usage
		_, err = p.Parse(t.Context(), "b", "課題", Profile{})
		if !errors.Is(err, ErrAIUsageUnavailable) || calls.Load() != 5 {
			t.Fatal("unavailable usage store allowed a paid request")
		}
	}
}

func TestFailureLogsDoNotContainUpstreamBodies(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)
	err := &googleapi.Error{Code: 500, Message: "private-message", Body: "private-token-and-body"}
	LogFailure("fixture operation", err)
	sourceError("fixture operation", err)
	respond(httptest.NewRecorder(), nil, err)
	if strings.Contains(output.String(), "private-") || !strings.Contains(output.String(), "http_status") {
		t.Fatal("sensitive upstream content was logged")
	}
}
