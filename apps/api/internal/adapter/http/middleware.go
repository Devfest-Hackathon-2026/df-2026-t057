package http

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/idempotency"
)

// MaxBodyBytes は本文の上限（空白を含む）。
const MaxBodyBytes = 64 << 10

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,80}$`)

type requestIDKey struct{}

// statusRecorder は応答のステータスを記録する（ログ用）。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	return n, err //nolint:wrapcheck // ResponseWriter の結果をそのまま返す。
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Flush は SSE のために書き込みを送り出す。
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// observe は要求ID・ログ・パニックからの回復を受け持つ。ログには経路の型・ステータス・時間だけを出す（本文やトークンは出さない）。
func observe(log *slog.Logger, rs routes, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now() //nolint:forbidigo // 所要時間の計測だけに使う。
		id := r.Header.Get("X-Cloud-Trace-Context")
		if i := strings.IndexByte(id, '/'); i > 0 {
			id = id[:i]
		}
		if id == "" {
			var b [8]byte
			_, _ = rand.Read(b[:])
			id = hex.EncodeToString(b[:])
		}
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		rec := &statusRecorder{ResponseWriter: w}
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler { //nolint:errorlint // パニックの値をそのまま比べる。
					panic(v)
				}
				log.ErrorContext(ctx, "panic", "request_id", id, "panic_type", fmt.Sprintf("%T", v))
				if rec.status == 0 {
					writeProblem(rec, r, log, apperr.Internal(errors.New("panic")))
				}
			}
			template := "static"
			if strings.HasPrefix(r.URL.Path, "/api/") {
				template = "unknown"
				if rt := rs.match(r.Method, strings.TrimPrefix(r.URL.Path, "/api")); rt != nil {
					template = rt.template
				}
			}
			log.InfoContext(ctx, "request", "request_id", id, "method", r.Method, "route", template, "status", rec.status, "duration_ms", time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(rec, r.WithContext(ctx))
	})
}

// origins は状態を変える要求の Origin を確かめる。
type origins struct {
	appURL       string
	extensionIDs []string
	anyExtension bool
}

func (o origins) allowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// curl・iPhone のショートカット・サーバー間の呼び出しは Origin を付けない。ブラウザーの別サイトからの要求は必ず付く。
		return true
	}
	if origin == o.appURL {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Scheme == "chrome-extension" {
		return o.anyExtension || slices.Contains(o.extensionIDs, u.Host)
	}
	// 同じオリジン（管理画面を同じサーバーから配信しているとき）は許可する。
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host == r.Host
}

// capture は応答を記録する（Idempotency-Key の応答を保存するため）。
type capture struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newCapture() *capture { return &capture{header: http.Header{}} }

func (c *capture) Header() http.Header { return c.header }
func (c *capture) WriteHeader(code int) {
	if c.status == 0 {
		c.status = code
	}
}

func (c *capture) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	n, err := c.body.Write(b)
	return n, err //nolint:wrapcheck // bytes.Buffer は失敗しない。
}

// replayHeaders は記録して再送時に返すヘッダー。
var replayHeaders = []string{"Content-Type", "Etag", "Location", "Retry-After"}

// response は記録する応答（ヘッダーは replayHeaders だけ）。
func (c *capture) response() idempotency.Response {
	headers := map[string]string{}
	for _, k := range replayHeaders {
		if v := c.header.Get(k); v != "" {
			headers[k] = v
		}
	}
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return idempotency.Response{Status: c.status, Headers: headers, Body: c.body.Bytes()}
}

func (c *capture) flushTo(w http.ResponseWriter) {
	for k, v := range c.header {
		w.Header()[k] = v
	}
	if c.status == 0 {
		c.status = http.StatusOK
	}
	w.WriteHeader(c.status)
	_, _ = w.Write(c.body.Bytes())
}

func requestHash(r *http.Request, body []byte) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\n%s\n%s\n", r.Method, r.URL.RequestURI(), r.Header.Get("If-Match"))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// apiPipeline は /api/ の要求を、本文の上限・Origin・認証・厳密な JSON・Idempotency-Key の順に確かめてから next に渡す。
type apiPipeline struct {
	log         *slog.Logger
	routes      routes
	origins     origins
	auth        authenticator
	idempotency *idempotency.Service
	next        http.Handler
}

func (p apiPipeline) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	rt := p.routes.match(r.Method, strings.TrimPrefix(r.URL.Path, "/api"))
	if rt == nil {
		writeProblem(w, r, p.log, apperr.NotFound("not_found"))
		return
	}
	mutating := r.Method != http.MethodGet && r.Method != http.MethodHead
	var body []byte
	if mutating {
		var err error
		body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeProblem(w, r, p.log, apperr.TooLarge("request.too_large"))
				return
			}
			writeProblem(w, r, p.log, apperr.Invalid("request.malformed"))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		if rt.auth != authCustom && !p.origins.allowed(r) {
			writeProblem(w, r, p.log, apperr.Forbidden("origin.forbidden"))
			return
		}
	}
	ctx := r.Context()
	if rt.auth == authUser {
		principal, err := p.auth.authenticate(w, r)
		if err != nil {
			writeProblem(w, r, p.log, err)
			return
		}
		ctx = withPrincipal(ctx, principal)
	}
	if rt.bodySchema != nil {
		if err := checkBody(body, rt.bodySchema); err != nil {
			writeProblem(w, r, p.log, err)
			return
		}
	}
	r = r.WithContext(ctx)
	if !rt.idempotent {
		p.next.ServeHTTP(w, r)
		return
	}
	p.serveIdempotent(w, r, rt, body)
}

// legacyIdempotency は、要求ごとのトランザクションを使わない操作（platform/idempotency の Complete 方式）。
// 相談は AI とのやり取りの合間に状態を保存するので、要求の間トランザクションを開いたままにしない。
// 相談は Idempotency-Key を相談の ID にするので、やり直しても返答済みなら同じ結果を返す。
var legacyIdempotency = map[string]bool{"sendAssistantMessage": true}

func (p apiPipeline) serveIdempotent(w http.ResponseWriter, r *http.Request, rt *route, body []byte) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeProblem(w, r, p.log, apperr.Invalid("idempotency.key_required"))
		return
	}
	if !idempotencyKeyPattern.MatchString(key) {
		writeProblem(w, r, p.log, apperr.Invalid("request.invalid", apperr.Field("Idempotency-Key", "invalid_format")))
		return
	}
	uid := uidFrom(r.Context())
	attempt, replay, err := p.idempotency.Begin(r.Context(), uid, key, requestHash(r, body))
	if err != nil {
		writeProblem(w, r, p.log, err)
		return
	}
	if replay != nil {
		for k, v := range replay.Headers {
			w.Header().Set(k, v)
		}
		w.Header().Set("Idempotent-Replayed", "true")
		w.WriteHeader(replay.Status)
		_, _ = w.Write(replay.Body) //nolint:gosec // 自分が記録した応答の本文を、記録した Content-Type のまま返す。
		return
	}
	handle := func(ctx context.Context) (*capture, idempotency.Response) {
		c := newCapture()
		// 要求ごとのトランザクションがやり直されても本文を読み直せるようにする。
		r.Body = io.NopCloser(bytes.NewReader(body))
		p.next.ServeHTTP(c, r.WithContext(ctx))
		return c, c.response()
	}
	if legacyIdempotency[rt.operationID] {
		c, res := handle(idempotency.WithAttempt(r.Context(), attempt))
		// 応答を記録できなくても、変更は確定している。ログに残して応答は返す。
		if err := p.idempotency.Complete(context.WithoutCancel(r.Context()), attempt, res); err != nil {
			p.log.ErrorContext(r.Context(), "idempotency record failed", "code", apperr.As(err).Code)
		}
		c.flushTo(w)
		return
	}
	var c *capture
	if _, err := p.idempotency.Run(r.Context(), attempt, func(ctx context.Context) idempotency.Response {
		var res idempotency.Response
		c, res = handle(ctx)
		return res
	}); err != nil {
		writeProblem(w, r, p.log, err)
		return
	}
	c.flushTo(w)
}

// etag は版を ETag の形にする。
func etag(revision int64) *string {
	s := `"` + strconv.FormatInt(revision, 10) + `"`
	return &s
}

// parseIfMatch は If-Match を版にする。無ければ 428、読めなければ 412。
func parseIfMatch(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, apperr.PreconditionRequired("precondition.required")
	}
	value = strings.TrimPrefix(value, "W/")
	n, err := strconv.ParseInt(strings.Trim(value, `"`), 10, 64)
	if err != nil {
		return 0, apperr.PreconditionFailed("precondition.failed")
	}
	return n, nil
}

func parseOptionalIfMatch(value *string) (*int64, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	n, err := parseIfMatch(*value)
	return &n, err
}
