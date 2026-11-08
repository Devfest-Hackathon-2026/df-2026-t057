package http

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// NoncePlaceholder は管理画面の index.html に置く CSP の nonce の目印（<app-root ngCspNonce="__DOPAGAKI_CSP_NONCE__">）。
const NoncePlaceholder = "__DOPAGAKI_CSP_NONCE__"

const (
	staticCSP = "default-src 'self'; script-src 'none'; style-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'"
	apiCSP    = "default-src 'none'; base-uri 'none'; frame-ancestors 'none'"
)

func securityHeaders(w http.ResponseWriter, apiPath bool) {
	h := w.Header()
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
	if apiPath {
		h.Set("Content-Security-Policy", apiCSP)
	} else {
		h.Set("Content-Security-Policy", staticCSP)
	}
}

// pageCSP は管理画面のページの CSP。スクリプトは nonce と strict-dynamic だけを許可する。
// 変更通知（SSE）と API は同じオリジン（connect-src 'self'）。Firebase Auth の通信先とログインの画面を加える。
func pageCSP(nonce, authDomain string) string {
	authOrigin := ""
	if authDomain != "" {
		authOrigin = " https://" + authDomain
	}
	frame := "frame-src 'none'"
	if authOrigin != "" {
		frame = "frame-src" + authOrigin
	}
	return "default-src 'self'; base-uri 'self'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; " +
		"script-src 'nonce-" + nonce + "' 'strict-dynamic'; style-src 'self' 'nonce-" + nonce + "'; style-src-attr 'unsafe-inline'; " +
		"img-src 'self' data: https://*.googleusercontent.com; font-src 'self'; " +
		"connect-src 'self' https://identitytoolkit.googleapis.com https://securetoken.googleapis.com https://apis.google.com" + authOrigin + "; " +
		frame
}

// APIOnly は API だけを配信する（管理画面を別のサーバーで開発するとき）。
func APIOnly(api http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		securityHeaders(w, true)
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		api.ServeHTTP(w, r)
	})
}

// WithWeb は API と、管理画面の静的ファイル（SPA）を同じオリジンから配信する。
func WithWeb(api http.Handler, staticDir, authDomain string) (http.Handler, error) {
	index, err := os.ReadFile(filepath.Join(staticDir, "index.html")) //nolint:gosec // STATIC_DIR は起動時の設定で、利用者の入力ではない。
	if err != nil {
		return nil, fmt.Errorf("管理画面のファイルを読めません（STATIC_DIR=%s）: %w", staticDir, err)
	}
	if !bytes.Contains(index, []byte(NoncePlaceholder)) {
		return nil, errors.New("管理画面の index.html に CSP の nonce の目印（" + NoncePlaceholder + "）がありません。管理画面をビルドし直してください")
	}
	root, err := os.OpenRoot(staticDir)
	if err != nil {
		return nil, fmt.Errorf("open static dir: %w", err)
	}
	files := http.FileServerFS(root.FS())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			securityHeaders(w, true)
			api.ServeHTTP(w, r)
			return
		}
		securityHeaders(w, false)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		clean := path.Clean("/" + r.URL.Path)
		if clean != "/" && clean != "/index.html" {
			if info, err := root.Stat(strings.TrimPrefix(clean, "/")); err == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		var b [16]byte
		_, _ = rand.Read(b[:])
		nonce := hex.EncodeToString(b[:])
		w.Header().Set("Content-Security-Policy", pageCSP(nonce, authDomain))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(bytes.ReplaceAll(index, []byte(NoncePlaceholder), []byte(nonce))))
	}), nil
}
