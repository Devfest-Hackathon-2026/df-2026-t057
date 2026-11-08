package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

// Principal は認証した利用者。
type Principal struct {
	UID string
	// Email と GoogleSubject は live のときだけ入る（カレンダーの連携で使う）。
	Email         string
	GoogleSubject string
}

// TokenVerifier は Firebase の ID トークンを確かめる（adapter/firebaseauth が実装する）。
// トークンが無効・期限切れなら apperr.Unauthenticated("auth.expired") を返す。
type TokenVerifier interface {
	Verify(ctx context.Context, idToken string) (Principal, error)
}

type principalKey struct{}

func withPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// principalFrom は認証した利用者を返す。認証が必要な操作でだけ呼ぶ。
func principalFrom(ctx context.Context) Principal {
	p, _ := ctx.Value(principalKey{}).(Principal)
	return p
}

func uidFrom(ctx context.Context) string { return principalFrom(ctx).UID }

const (
	demoCookie     = "dopagaki_demo"
	demoCookieAge  = 7 * 24 * 60 * 60
	demoUIDPrefix  = "demo-"
	demoAuthScheme = "Demo "
)

var demoTokenPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

// authenticator は要求の利用者を決める。
type authenticator struct {
	demo     bool
	verifier TokenVerifier
}

// authenticate は利用者を決める。デモで接続情報が無ければ、新しいデモの利用者の Cookie を発行する。
func (a authenticator) authenticate(w http.ResponseWriter, r *http.Request) (Principal, error) {
	header := r.Header.Get("Authorization")
	query := r.URL.Query().Get("token")
	if a.demo {
		return a.demoPrincipal(w, r, header, query)
	}
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || strings.TrimSpace(token) == "" {
		// EventSource はヘッダーを付けられないので、変更通知だけ token クエリを受け付ける。
		if query == "" || r.URL.Path != "/api/events" {
			return Principal{}, apperr.Unauthenticated("auth.required")
		}
		token = query
	}
	p, err := a.verifier.Verify(r.Context(), strings.TrimSpace(token))
	if err != nil {
		return Principal{}, err
	}
	if !store.ValidUID(p.UID) {
		return Principal{}, apperr.Unauthenticated("auth.expired")
	}
	return p, nil
}

func (a authenticator) demoPrincipal(w http.ResponseWriter, r *http.Request, header, query string) (Principal, error) {
	if header == "" && query != "" && r.URL.Path == "/api/events" {
		header = query
		if !strings.HasPrefix(header, demoAuthScheme) {
			header = demoAuthScheme + header
		}
	}
	if header != "" {
		token, ok := strings.CutPrefix(header, demoAuthScheme)
		if !ok || !demoTokenPattern.MatchString(token) {
			return Principal{}, apperr.Unauthenticated("auth.required")
		}
		return Principal{UID: demoUIDPrefix + token}, nil
	}
	if c, err := r.Cookie(demoCookie); err == nil && demoTokenPattern.MatchString(c.Value) {
		return Principal{UID: demoUIDPrefix + c.Value}, nil
	}
	var b [16]byte
	_, _ = rand.Read(b[:])
	token := hex.EncodeToString(b[:])
	//nolint:gosec // Secure は TLS のときだけ付ける（ローカルのデモは http）。HttpOnly と SameSite=Lax は常に付ける。
	http.SetCookie(w, &http.Cookie{Name: demoCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: demoCookieAge, Secure: secureRequest(r)})
	return Principal{UID: demoUIDPrefix + token}, nil
}

func secureRequest(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// demoToken はデモの利用者の拡張用の Authorization の値。
func demoToken(uid string) (string, bool) {
	token, ok := strings.CutPrefix(uid, demoUIDPrefix)
	return demoAuthScheme + token, ok
}

func cutScheme(header, scheme string) (string, bool) {
	v, ok := strings.CutPrefix(header, scheme)
	v = strings.TrimSpace(v)
	return v, ok && v != ""
}

func validUID(uid string) bool { return store.ValidUID(uid) }
