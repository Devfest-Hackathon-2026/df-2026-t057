// Package apperr はアプリケーション全体で使う型つきエラーを定義する。
//
// サーバーは表示用の文言を持たない。Code は機械が読む識別子で、クライアントが文言に対応付ける。
// HTTP層は Kind からステータスコードを決め、RFC 9457 の Problem Details に変換する。
package apperr

import (
	"errors"
	"fmt"
	"time"
)

// Kind はエラーの種類。HTTPのステータスコードに対応する。
type Kind int

const (
	KindInternal           Kind = iota // 500
	KindInvalid                        // 400 入力エラー
	KindUnauthenticated                // 401
	KindForbidden                      // 403
	KindNotFound                       // 404
	KindConflict                       // 409 状態と矛盾する操作
	KindPreconditionFailed             // 412 版の不一致
	KindRateLimited                    // 429
	KindUnavailable                    // 503 外部サービスや設定が使えない
)

// FieldError は入力項目ごとのエラー。
type FieldError struct {
	Field string
	Code  string
}

// Error はアプリケーションのエラー。
type Error struct {
	Kind       Kind
	Code       string
	Fields     []FieldError
	RetryAfter time.Duration
	cause      error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.cause)
	}
	return e.Code
}

func (e *Error) Unwrap() error { return e.cause }

// Is は Kind と Code が一致すれば同じエラーとみなす。errors.Is(err, apperr.NotFound("task.not_found")) の形で使える。
func (e *Error) Is(target error) bool {
	var t *Error
	if !errors.As(target, &t) {
		return false
	}
	return e.Kind == t.Kind && e.Code == t.Code
}

func newError(kind Kind, code string) *Error { return &Error{Kind: kind, Code: code} }

func Invalid(code string, fields ...FieldError) *Error {
	e := newError(KindInvalid, code)
	e.Fields = fields
	return e
}
func Field(field, code string) FieldError { return FieldError{Field: field, Code: code} }
func Unauthenticated(code string) *Error  { return newError(KindUnauthenticated, code) }
func Forbidden(code string) *Error        { return newError(KindForbidden, code) }
func NotFound(code string) *Error         { return newError(KindNotFound, code) }
func Conflict(code string) *Error         { return newError(KindConflict, code) }
func PreconditionFailed(code string) *Error {
	return newError(KindPreconditionFailed, code)
}
func Unavailable(code string) *Error { return newError(KindUnavailable, code) }
func RateLimited(code string, retryAfter time.Duration) *Error {
	e := newError(KindRateLimited, code)
	e.RetryAfter = retryAfter
	return e
}

// External は外部サービスの失敗を包む。原因は記録用に残すが、クライアントには返さない。
func External(code string, cause error) *Error {
	e := newError(KindUnavailable, code)
	e.cause = cause
	return e
}

// Internal は想定外の失敗を包む。
func Internal(cause error) *Error {
	e := newError(KindInternal, "internal")
	e.cause = cause
	return e
}

// As は err から *Error を取り出す。見つからなければ Internal として包む。
func As(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return Internal(err)
}
