package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestServiceFailureDetailsAreNotReturned(t *testing.T) {
	for _, err := range []error{
		status.Error(codes.PermissionDenied, "private-service-detail"),
		&googleapi.Error{Code: 500, Message: "private-service-detail", Body: "private-body"},
		&oauth2.RetrieveError{Response: &http.Response{StatusCode: 400}, Body: []byte("private-service-detail")},
		&url.Error{Op: "POST", URL: "https://example.com/private-service-detail", Err: errors.New("failed")},
	} {
		w := httptest.NewRecorder()
		respond(w, nil, err)
		if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "private-") {
			t.Fatalf("unsafe service failure: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestJSONBodyMustBeOneBoundedObject(t *testing.T) {
	raw, _ := json.Marshal(input())
	for _, body := range []string{
		"null", string(raw) + "{}", string(raw) + " garbage",
		string(raw) + strings.Repeat(" ", 64<<10),
	} {
		t.Run(body[:min(len(body), 32)], func(t *testing.T) {
			c := client(t)
			r := httptest.NewRequest("POST", "/api/tasks", strings.NewReader(body))
			r.AddCookie(c.cookie)
			w := httptest.NewRecorder()
			c.handler.ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("invalid body accepted: %d", w.Code)
			}
			if len(c.state("GET", "/api/state", nil).Tasks) != 0 {
				t.Fatal("invalid body changed saved tasks")
			}
		})
	}
	c := client(t)
	r := httptest.NewRequest("POST", "/api/tasks", bytes.NewReader(append(raw, '\n', ' ')))
	r.AddCookie(c.cookie)
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("ordinary trailing whitespace rejected: %d", w.Code)
	}
}

func TestNullProfileDoesNotClearSavedInformation(t *testing.T) {
	c := client(t)
	c.state("PUT", "/api/profile", Profile{Name: "saved", Context: "keep this", TimeZone: "Asia/Tokyo"})
	r := httptest.NewRequest("PUT", "/api/profile", strings.NewReader("null"))
	r.AddCookie(c.cookie)
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest || c.state("GET", "/api/state", nil).Profile.Context != "keep this" {
		t.Fatal("null profile was accepted or changed saved information")
	}
}

func TestDemoAuthorizationRequiresItsScheme(t *testing.T) {
	for _, authorization := range []string{
		"abcdef0123456789abcdef0123456789",
		"Bearer abcdef0123456789abcdef0123456789",
		"Demo invalid",
	} {
		r := httptest.NewRequest("GET", "/api/state", nil)
		r.Header.Set("Authorization", authorization)
		w := httptest.NewRecorder()
		client(t).handler.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("invalid authorization %q accepted: %d", authorization, w.Code)
		}
	}
}

func TestMalformedCalendarRequestsDoNotAccessStorage(t *testing.T) {
	// No DB is configured: malformed public requests must stop before accessing it.
	handler := (&Server{Calendar: &Calendar{}, AppURL: "https://app.example"}).Handler()
	for _, target := range []string{
		"/api/calendar/callback",
		"/api/calendar/callback?state=bad&code=code",
		"/api/calendar/callback?state=" + strings.Repeat("a", 32),
	} {
		t.Run(target, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", target, nil))
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "https://app.example/?calendar=error" {
				t.Fatalf("malformed callback: %d %s", w.Code, w.Header().Get("Location"))
			}
		})
	}
	for _, headers := range [][2]string{
		{"", ""}, {"bad", strings.Repeat("b", 32)},
		{strings.Repeat("a", 32), ""},
		{"a/b/c", strings.Repeat("b", 32)},
	} {
		r := httptest.NewRequest("POST", "/api/calendar/webhook", nil)
		r.Header.Set("X-Goog-Channel-ID", headers[0])
		r.Header.Set("X-Goog-Channel-Token", headers[1])
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("malformed webhook: %d", w.Code)
		}
	}
}
