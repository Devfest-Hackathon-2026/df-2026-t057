package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestWebNonceAndSecurityHeaders(t *testing.T) {
	static := t.TempDir()
	if err := os.WriteFile(filepath.Join(static, "index.html"), []byte(`<app-root ngCspNonce="__DOPAGAKI_CSP_NONCE__"></app-root><script nonce="__DOPAGAKI_CSP_NONCE__" src="/main.js"></script>`), 0600); err != nil {
		t.Fatal(err)
	}
	handler, err := WebHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), static, "app.firebaseapp.com")
	if err != nil {
		t.Fatal(err)
	}
	var previous string
	for _, path := range []string{"/", "/calendar", "/index.html"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		match := regexp.MustCompile(`'nonce-([a-f0-9]{32})'`).FindStringSubmatch(w.Header().Get("Content-Security-Policy"))
		if len(match) != 2 || match[1] == previous || !strings.Contains(w.Body.String(), `nonce="`+match[1]+`"`) || !strings.Contains(w.Body.String(), `ngCspNonce="`+match[1]+`"`) || strings.Contains(w.Body.String(), noncePlaceholder) {
			t.Fatal("nonce was missing, reused, or inconsistent")
		}
		if w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-src https://app.firebaseapp.com") || w.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatal("document security headers missing")
		}
		previous = match[1]
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/health", nil))
	if w.Code != 204 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatal("API was served as a page")
	}
	if _, err := WebHandler(nil, static, "app.firebaseapp.com; script-src *"); err == nil {
		t.Fatal("unsafe auth origin accepted")
	}
}
