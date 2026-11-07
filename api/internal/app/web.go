package app

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const noncePlaceholder = "__DOPAGAKI_CSP_NONCE__"

func WebHandler(api http.Handler, static, authDomain string) (http.Handler, error) {
	index, err := os.ReadFile(filepath.Join(static, "index.html"))
	if err != nil {
		return nil, err
	}
	if !bytes.Contains(index, []byte(noncePlaceholder)) {
		return nil, fmt.Errorf("web assets are missing the CSP nonce placeholder; rebuild the web app")
	}
	authOrigin := ""
	if authDomain != "" {
		u, err := url.Parse("https://" + authDomain)
		if err != nil || u.Host != authDomain || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("invalid Firebase auth domain")
		}
		authOrigin = " https://" + authDomain
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	files := http.FileServer(http.Dir(static))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if _, err := os.Stat(filepath.Join(static, filepath.Clean(r.URL.Path))); err == nil && r.URL.Path != "/" && r.URL.Path != "/index.html" {
			files.ServeHTTP(w, r)
			return
		}
		nonce := newID()
		policy := "default-src 'self'; base-uri 'self'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; " +
			"script-src 'nonce-" + nonce + "' 'strict-dynamic'; style-src 'self' 'nonce-" + nonce + "'; style-src-attr 'unsafe-inline'; " +
			"img-src 'self' data: https://*.googleusercontent.com; font-src 'self'; " +
			"connect-src 'self' https://identitytoolkit.googleapis.com https://securetoken.googleapis.com https://apis.google.com" + authOrigin + "; " +
			"frame-src" + authOrigin
		if authOrigin == "" {
			policy += " 'none'"
		}
		w.Header().Set("Content-Security-Policy", policy)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(bytes.ReplaceAll(index, []byte(noncePlaceholder), []byte(nonce))))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'none'; style-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		}
		mux.ServeHTTP(w, r)
	}), nil
}
