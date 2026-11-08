package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	apihttp "github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/http"
)

func TestWebServesSPAWithFreshNonce(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<app-root ngCspNonce="__DOPAGAKI_CSP_NONCE__"></app-root><script nonce="__DOPAGAKI_CSP_NONCE__"></script>`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "fullcalendar.css"), []byte(`a{}`), 0o600)
	h, err := apihttp.WithWeb(http.NotFoundHandler(), dir, "p.firebaseapp.com")
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), "GET", path, nil))
		return rec
	}
	nonce := regexp.MustCompile(`'nonce-([a-f0-9]{32})'`)
	first, second := get("/calendar?date=2026-11-08"), get("/")
	m1, m2 := nonce.FindStringSubmatch(first.Header().Get("Content-Security-Policy")), nonce.FindStringSubmatch(second.Header().Get("Content-Security-Policy"))
	if m1 == nil || m2 == nil || m1[1] == m2[1] || strings.Count(first.Body.String(), m1[1]) != 2 {
		t.Fatalf("nonce %v %v %s", m1, m2, first.Body.String())
	}
	csp := first.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "'strict-dynamic'") || strings.Contains(csp, "unsafe-eval") || !strings.Contains(csp, "frame-src https://p.firebaseapp.com") {
		t.Fatalf("csp %s", csp)
	}
	css := get("/fullcalendar.css")
	if css.Body.String() != "a{}" || !strings.Contains(css.Header().Get("Content-Security-Policy"), "script-src 'none'") || css.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("static %v", css.Header())
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), "POST", "/x", nil))
	if rec.Code != 405 {
		t.Fatalf("post %d", rec.Code)
	}
	if _, err := apihttp.WithWeb(http.NotFoundHandler(), t.TempDir(), ""); err == nil {
		t.Fatal("missing index accepted")
	}
}

func TestWebIssuesDemoCookieOnPageOnlyWhenMissing(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<app-root ngCspNonce="__DOPAGAKI_CSP_NONCE__"></app-root>`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "app.js"), []byte(`1`), 0o600)
	h, err := apihttp.WithWeb(http.NotFoundHandler(), dir, "", apihttp.WithDemoSession())
	if err != nil {
		t.Fatal(err)
	}
	serve := func(path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(context.Background(), "GET", path, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	page := serve("/calendar", nil)
	cookies := page.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "dopagaki_demo" || !cookies[0].HttpOnly {
		t.Fatalf("ページで Cookie を発行していない: %v", cookies)
	}
	if again := serve("/", cookies[0]).Result().Cookies(); len(again) != 0 {
		t.Fatalf("Cookie があるのに発行し直した: %v", again)
	}
	if asset := serve("/app.js", nil).Result().Cookies(); len(asset) != 0 {
		t.Fatalf("静的ファイルでは発行しない: %v", asset)
	}
}
