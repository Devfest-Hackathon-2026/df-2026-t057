package gcal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/idtoken"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/calendar"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
)

type memoryTokens struct {
	mu     sync.Mutex
	tokens map[string][]byte
}

func (m *memoryTokens) Save(_ context.Context, uid string, token []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[uid] = token
	return nil
}

func (m *memoryTokens) Delete(_ context.Context, uid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tokens, uid)
	return nil
}

func (m *memoryTokens) Load(_ context.Context, uid string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[uid]
	if !ok {
		return nil, apperr.Forbidden("calendar.reauth_required")
	}
	return t, nil
}

type recorded struct {
	method, path, ifMatch, auth string
	query                       url.Values
	body                        map[string]any
}

// fakeAPI は Calendar API と OAuth のトークンのエンドポイントの偽物。
type fakeAPI struct {
	mu       sync.Mutex
	requests []recorded
	handle   func(w http.ResponseWriter, r recorded)
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	rec := recorded{method: r.Method, path: r.URL.Path, ifMatch: r.Header.Get("If-Match"), auth: r.Header.Get("Authorization"), query: r.URL.Query()}
	if r.URL.Path == "/token" {
		form, _ := url.ParseQuery(string(raw))
		rec.query = form
	} else if len(raw) > 0 {
		_ = json.Unmarshal(raw, &rec.body)
	}
	f.mu.Lock()
	f.requests = append(f.requests, rec)
	f.mu.Unlock()
	f.handle(w, rec)
}

func (f *fakeAPI) last() recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func googleError(w http.ResponseWriter, status int) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": status, "message": "secret upstream detail"}})
}

func setup(t *testing.T, handle func(w http.ResponseWriter, r recorded)) (*Google, *fakeAPI, *memoryTokens) {
	t.Helper()
	api := &fakeAPI{handle: handle}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	tokens := &memoryTokens{tokens: map[string][]byte{}}
	validate := func(_ context.Context, raw, audience string) (*idtoken.Payload, error) {
		if raw != "valid-id-token" || audience != "client-id" {
			return nil, errors.New("invalid")
		}
		return &idtoken.Payload{Subject: "google-1"}, nil
	}
	g := New(Config{
		ClientID: "client-id", ClientSecret: "secret", AppURL: "https://app.example",
		OAuthEndpoint:    oauth2.Endpoint{AuthURL: srv.URL + "/auth", TokenURL: srv.URL + "/token", AuthStyle: oauth2.AuthStyleInParams},
		CalendarEndpoint: srv.URL + "/calendar/v3/", HTTPClient: srv.Client(),
	}, tokens, validate)
	return g, api, tokens
}

func token(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(oauth2.Token{AccessToken: "access", RefreshToken: "refresh", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAuthURL(t *testing.T) {
	g, _, _ := setup(t, nil)
	u, err := url.Parse(g.AuthURL("state123", "user@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("state") != "state123" || q.Get("access_type") != "offline" || q.Get("prompt") != "consent select_account" ||
		q.Get("login_hint") != "user@example.com" || q.Get("redirect_uri") != "https://app.example/api/calendar/callback" {
		t.Fatalf("auth url: %v", q)
	}
	for _, scope := range []string{"openid", "email", "https://www.googleapis.com/auth/calendar.app.created", "https://www.googleapis.com/auth/calendar.events.owned.readonly"} {
		if !strings.Contains(q.Get("scope"), scope) {
			t.Errorf("missing scope %s", scope)
		}
	}
}

func TestExchange(t *testing.T) {
	idToken := "valid-id-token"
	refresh := "refresh"
	scope := "openid https://www.googleapis.com/auth/calendar.app.created https://www.googleapis.com/auth/calendar.events.owned.readonly"
	g, _, _ := setup(t, func(w http.ResponseWriter, r recorded) {
		if r.query.Get("code") == "bad" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "secret upstream detail"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"access_token": "access", "token_type": "Bearer", "expires_in": 3600, "refresh_token": refresh, "id_token": idToken, "scope": scope})
	})
	grant, err := g.Exchange(context.Background(), "code")
	if err != nil || grant.Subject != "google-1" || !grant.HasRefreshToken || !grant.PrimaryEvents {
		t.Fatalf("grant: %+v %v", grant, err)
	}
	// 同意画面で主カレンダーの読み取りを外された。
	scope = "openid https://www.googleapis.com/auth/calendar.app.created"
	if grant, err := g.Exchange(context.Background(), "code"); err != nil || grant.PrimaryEvents {
		t.Fatalf("without the read scope: %+v %v", grant, err)
	}
	if _, err := g.ClientFor(context.Background(), grant.Token); err != nil {
		t.Fatalf("token not reusable: %v", err)
	}

	refresh = ""
	if grant, err := g.Exchange(context.Background(), "code"); err != nil || grant.HasRefreshToken {
		t.Fatalf("missing refresh token: %+v %v", grant, err)
	}
	idToken = "forged"
	if _, err := g.Exchange(context.Background(), "code"); !errors.Is(err, apperr.Forbidden("calendar.reauth_required")) {
		t.Fatalf("invalid id token: %v", err)
	}
	_, err = g.Exchange(context.Background(), "bad")
	if apperr.As(err).Code != "external.failed" || strings.Contains(err.Error(), "secret upstream detail") {
		t.Fatalf("exchange error leaked details: %v", err)
	}
}

func eventJSON(id, etag string) map[string]any {
	return map[string]any{
		"id": id, "etag": etag, "status": "confirmed", "summary": "課題", "description": "メモ",
		"start":              map[string]any{"dateTime": "2026-12-01T09:00:00+09:00"},
		"end":                map[string]any{"dateTime": "2026-12-01T10:00:00+09:00"},
		"extendedProperties": map[string]any{"private": map[string]any{"dopagakiTaskId": "t1"}},
	}
}

func newClient(t *testing.T, handle func(w http.ResponseWriter, r recorded)) (calendar.Client, *fakeAPI) {
	t.Helper()
	g, api, tokens := setup(t, handle)
	_ = tokens.Save(context.Background(), "u1", token(t))
	c, err := g.Client(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	return c, api
}

func TestInsertAndConflict(t *testing.T) {
	exists := false
	c, api := newClient(t, func(w http.ResponseWriter, r recorded) {
		if exists {
			googleError(w, http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusOK, eventJSON("ev1", `"1"`))
	})
	ev, err := c.InsertEvent(context.Background(), "cal", calendar.Event{ID: "ev1", Summary: "課題", Start: calendar.EventTime{DateTime: "2026-12-01T00:00:00Z"}, End: calendar.EventTime{DateTime: "2026-12-01T01:00:00Z"}, Private: map[string]string{"dopagakiTaskId": "t1"}})
	if err != nil || ev.ETag != `"1"` || ev.Private["dopagakiTaskId"] != "t1" || ev.Start.DateTime != "2026-12-01T09:00:00+09:00" {
		t.Fatalf("insert: %+v %v", ev, err)
	}
	r := api.last()
	if r.method != http.MethodPost || r.path != "/calendar/v3/calendars/cal/events" || r.auth != "Bearer access" {
		t.Fatalf("request: %+v", r)
	}
	if props := r.body["extendedProperties"].(map[string]any)["private"].(map[string]any); props["dopagakiTaskId"] != "t1" {
		t.Fatalf("private property not sent: %v", r.body)
	}
	exists = true
	if _, err := c.InsertEvent(context.Background(), "cal", calendar.Event{ID: "ev1"}); !errors.Is(err, calendar.ErrEventExists) {
		t.Fatalf("409: %v", err)
	}
}

func TestPatchSendsIfMatch(t *testing.T) {
	status := http.StatusOK
	c, api := newClient(t, func(w http.ResponseWriter, r recorded) {
		if status != http.StatusOK {
			googleError(w, status)
			return
		}
		writeJSON(w, http.StatusOK, eventJSON("ev1", `"2"`))
	})
	summary := "新しい題名"
	start := calendar.EventTime{DateTime: "2026-12-01T00:00:00Z"}
	if _, err := c.PatchEvent(context.Background(), "cal", "ev1", `"1"`, calendar.EventPatch{Summary: &summary, Description: "説明", Start: &start}); err != nil {
		t.Fatal(err)
	}
	r := api.last()
	if r.method != http.MethodPatch || r.ifMatch != `"1"` || r.body["summary"] != summary || r.body["description"] != "説明" || r.body["end"] != nil {
		t.Fatalf("patch request: %+v", r)
	}
	if _, err := c.PatchEvent(context.Background(), "cal", "ev1", `"1"`, calendar.EventPatch{Description: "説明"}); err != nil {
		t.Fatal(err)
	}
	if r := api.last(); r.body["summary"] != nil {
		t.Fatalf("plan-only patch sent the summary: %v", r.body)
	}
	for code, want := range map[int]error{http.StatusPreconditionFailed: calendar.ErrPreconditionFailed, http.StatusNotFound: calendar.ErrEventGone, http.StatusGone: calendar.ErrEventGone} {
		status = code
		if _, err := c.PatchEvent(context.Background(), "cal", "ev1", `"1"`, calendar.EventPatch{Description: "x"}); !errors.Is(err, want) {
			t.Errorf("%d: %v", code, err)
		}
	}
	status = http.StatusInternalServerError
	_, err := c.PatchEvent(context.Background(), "cal", "ev1", `"1"`, calendar.EventPatch{Description: "x"})
	if apperr.As(err).Code != "external.failed" || strings.Contains(err.Error(), "secret upstream detail") {
		t.Fatalf("500: %v", err)
	}
}

func TestGetAndDelete(t *testing.T) {
	cancelled := false
	c, api := newClient(t, func(w http.ResponseWriter, r recorded) {
		switch {
		case r.method == http.MethodDelete:
			googleError(w, http.StatusGone)
		case cancelled:
			e := eventJSON("ev1", `"3"`)
			e["status"] = "cancelled"
			writeJSON(w, http.StatusOK, e)
		default:
			writeJSON(w, http.StatusOK, eventJSON("ev1", `"3"`))
		}
	})
	if ev, err := c.GetEvent(context.Background(), "cal", "ev1"); err != nil || ev.Summary != "課題" {
		t.Fatalf("get: %+v %v", ev, err)
	}
	cancelled = true
	if _, err := c.GetEvent(context.Background(), "cal", "ev1"); !errors.Is(err, calendar.ErrEventGone) {
		t.Fatalf("cancelled get: %v", err)
	}
	if err := c.DeleteEvent(context.Background(), "cal", "ev1"); !errors.Is(err, calendar.ErrEventGone) {
		t.Fatalf("delete gone: %v", err)
	}
	if api.last().method != http.MethodDelete {
		t.Fatal("delete not sent")
	}
}

func TestListPagingAndExpiredToken(t *testing.T) {
	c, api := newClient(t, func(w http.ResponseWriter, r recorded) {
		switch {
		case r.query.Get("syncToken") == "expired":
			googleError(w, http.StatusGone)
		case r.query.Get("pageToken") == "":
			writeJSON(w, http.StatusOK, map[string]any{"items": []any{eventJSON("a", "1")}, "nextPageToken": "p2"})
		default:
			cancelled := map[string]any{"id": "b", "status": "cancelled"}
			writeJSON(w, http.StatusOK, map[string]any{"items": []any{cancelled}, "nextSyncToken": "next"})
		}
	})
	page, err := c.ListEvents(context.Background(), "cal", "tok", "")
	if err != nil || len(page.Events) != 1 || page.NextPageToken != "p2" {
		t.Fatalf("page1: %+v %v", page, err)
	}
	r := api.last()
	if r.query.Get("syncToken") != "tok" || r.query.Get("showDeleted") != "true" || r.query.Get("maxResults") != "250" {
		t.Fatalf("list query: %v", r.query)
	}
	page, err = c.ListEvents(context.Background(), "cal", "tok", "p2")
	if err != nil || page.NextSyncToken != "next" || page.Events[0].Status != calendar.EventCancelled {
		t.Fatalf("page2: %+v %v", page, err)
	}
	if _, err := c.ListEvents(context.Background(), "cal", "expired", ""); !errors.Is(err, calendar.ErrSyncTokenExpired) {
		t.Fatalf("410: %v", err)
	}
}

func TestListRange(t *testing.T) {
	c, api := newClient(t, func(w http.ResponseWriter, r recorded) {
		if r.query.Get("pageToken") == "" {
			writeJSON(w, http.StatusOK, map[string]any{"items": []any{eventJSON("a", "1")}, "nextPageToken": "p2"})
			return
		}
		allDay := map[string]any{"id": "b", "status": "confirmed", "start": map[string]any{"date": "2026-12-02"}, "end": map[string]any{"date": "2026-12-03"}}
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{allDay}})
	})
	from := time.Date(2026, 11, 30, 15, 0, 0, 0, time.UTC)
	events, err := c.ListRange(context.Background(), "primary", from, from.Add(7*24*time.Hour))
	if err != nil || len(events) != 2 || events[0].Start.DateTime != "2026-12-01T09:00:00+09:00" || events[1].Start.Date != "2026-12-02" {
		t.Fatalf("events: %+v %v", events, err)
	}
	r := api.last()
	if r.path != "/calendar/v3/calendars/primary/events" || r.query.Get("timeMin") != "2026-11-30T15:00:00Z" || r.query.Get("timeMax") != "2026-12-07T15:00:00Z" ||
		r.query.Get("singleEvents") != "true" || r.query.Get("orderBy") != "startTime" || r.query.Get("pageToken") != "p2" {
		t.Fatalf("list query: %s %v", r.path, r.query)
	}
}

func TestWatchAndStop(t *testing.T) {
	expires := time.Date(2026, 12, 7, 0, 0, 0, 0, time.UTC)
	c, api := newClient(t, func(w http.ResponseWriter, r recorded) {
		if strings.HasSuffix(r.path, "/channels/stop") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": r.body["id"], "resourceId": "res-1", "expiration": "1796601600000"})
	})
	w, err := c.Watch(context.Background(), "cal", calendar.WatchRequest{ChannelID: "ch1", Token: "tok", Address: "https://app.example/api/calendar/webhook", Expires: expires})
	if err != nil || w.ResourceID != "res-1" || !w.Expires.Equal(time.UnixMilli(1796601600000)) {
		t.Fatalf("watch: %+v %v", w, err)
	}
	r := api.last()
	if r.body["type"] != "web_hook" || r.body["token"] != "tok" || r.body["address"] != "https://app.example/api/calendar/webhook" {
		t.Fatalf("watch body: %v", r.body)
	}
	if err := c.StopChannel(context.Background(), "ch1", "res-1"); err != nil {
		t.Fatal(err)
	}
	if r := api.last(); r.body["id"] != "ch1" || r.body["resourceId"] != "res-1" {
		t.Fatalf("stop body: %v", r.body)
	}
}

func TestSleepEventFields(t *testing.T) {
	c, api := newClient(t, func(w http.ResponseWriter, r recorded) {
		writeJSON(w, http.StatusOK, eventJSON("sleep", "1"))
	})
	ev := calendar.Event{ID: "sleep", Status: "confirmed", Summary: "就寝", Recurrence: []string{"RRULE:FREQ=DAILY"}, Transparent: true,
		Start: calendar.EventTime{DateTime: "2026-12-01T23:00:00+09:00", TimeZone: "Asia/Tokyo"}, End: calendar.EventTime{DateTime: "2026-12-02T07:00:00+09:00", TimeZone: "Asia/Tokyo"}}
	if _, err := c.UpdateEvent(context.Background(), "cal", ev); err != nil {
		t.Fatal(err)
	}
	r := api.last()
	if r.method != http.MethodPut || r.path != "/calendar/v3/calendars/cal/events/sleep" || r.body["transparency"] != "transparent" ||
		r.body["start"].(map[string]any)["timeZone"] != "Asia/Tokyo" || len(r.body["recurrence"].([]any)) != 1 {
		t.Fatalf("update: %+v", r)
	}
}

func TestClientWithoutTokenRequiresReauth(t *testing.T) {
	g, _, _ := setup(t, nil)
	if _, err := g.Client(context.Background(), "nobody"); !errors.Is(err, apperr.Forbidden("calendar.reauth_required")) {
		t.Fatalf("got %v", err)
	}
	if _, err := g.ClientFor(context.Background(), []byte("not json")); !errors.Is(err, apperr.Forbidden("calendar.reauth_required")) {
		t.Fatalf("got %v", err)
	}
}

func TestSecretID(t *testing.T) {
	id := SecretID("u1")
	if !strings.HasPrefix(id, "dopagaki-calendar-") || len(id) != len("dopagaki-calendar-")+32 || strings.Contains(id, "u1") || SecretID("u2") == id {
		t.Fatalf("secret id: %s", id)
	}
}
