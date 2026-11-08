// Package http は API の HTTP の境界。生成した strict-server の実装、ミドルウェア、管理画面の配信を持つ。
//
// エラーは RFC 9457 の Problem Details（application/problem+json）で返し、code を必ず付ける。表示の文言は返さない。
package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"

	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/status"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/http/oapi"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
)

// statusOf は種類に対応するステータスコード。
func statusOf(k apperr.Kind) int {
	switch k {
	case apperr.KindInvalid:
		return http.StatusBadRequest
	case apperr.KindUnauthenticated:
		return http.StatusUnauthorized
	case apperr.KindForbidden:
		return http.StatusForbidden
	case apperr.KindNotFound:
		return http.StatusNotFound
	case apperr.KindConflict:
		return http.StatusConflict
	case apperr.KindPreconditionFailed:
		return http.StatusPreconditionFailed
	case apperr.KindPreconditionRequired:
		return http.StatusPreconditionRequired
	case apperr.KindTooLarge:
		return http.StatusRequestEntityTooLarge
	case apperr.KindRateLimited:
		return http.StatusTooManyRequests
	case apperr.KindUnavailable:
		return http.StatusServiceUnavailable
	case apperr.KindInternal:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

// classify は任意のエラーを *apperr.Error にする。Google の SDK・通信のエラーは external.failed（詳細は返さない）。
func classify(err error) *apperr.Error {
	var e *apperr.Error
	if errors.As(err, &e) {
		return e
	}
	var gerr *googleapi.Error
	var uerr *url.Error
	if errors.As(err, &gerr) || errors.As(err, &uerr) {
		return apperr.External("external.failed", err)
	}
	if _, ok := status.FromError(err); ok {
		return apperr.External("external.failed", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return apperr.External("external.failed", err)
	}
	return apperr.Internal(err)
}

// problemOf は Problem Details の本文。仕様に無い code は internal にする。
func problemOf(e *apperr.Error) (oapi.Problem, int) {
	st := statusOf(e.Kind)
	code := oapi.ProblemCode(e.Code)
	if !code.Valid() {
		code, st = oapi.Internal, http.StatusInternalServerError
	}
	p := oapi.Problem{Type: "/problems/" + string(code), Status: st, Code: code}
	if len(e.Fields) > 0 {
		fields := make([]oapi.FieldProblem, 0, len(e.Fields))
		for _, f := range e.Fields {
			fc := oapi.FieldCode(f.Code)
			if !fc.Valid() {
				fc = oapi.InvalidFormat
			}
			fields = append(fields, oapi.FieldProblem{Field: f.Field, Code: fc})
		}
		p.Errors = &fields
	}
	return p, st
}

// writeProblem はエラーを Problem Details で書き、5xx はログに残す（入力・トークン・外部の応答の本文は出さない）。
func writeProblem(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	e := classify(err)
	p, st := problemOf(e)
	if st >= 500 {
		attrs := []any{"code", p.Code, "status", st, "error_type", fmt.Sprintf("%T", errors.Unwrap(e))}
		if e.Kind == apperr.KindInternal {
			attrs = append(attrs, "error", e.Error())
		}
		log.ErrorContext(r.Context(), "request failed", attrs...)
	}
	if e.Kind == apperr.KindRateLimited {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(e.RetryAfter.Seconds())))))
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(st)
	_ = json.NewEncoder(w).Encode(p)
}
