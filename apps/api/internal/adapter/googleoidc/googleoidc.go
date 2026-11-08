// Package googleoidc は Cloud Tasks と Cloud Scheduler が付ける OIDC トークン（Authorization: Bearer）を検証する。
package googleoidc

import (
	"context"
	"strings"

	"google.golang.org/api/idtoken"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
)

// Validator は ID トークンの署名・期限・audience を検証する（本番は idtoken.Validate）。
type Validator func(ctx context.Context, token, audience string) (*idtoken.Payload, error)

// Verifier は OIDC トークンを検証する。
type Verifier struct {
	audience string
	validate Validator
}

// New は Verifier を作る。audience は OIDC_AUDIENCE（既定は APP_URL）。
func New(audience string) *Verifier { return NewWithValidator(audience, idtoken.Validate) }

// NewWithValidator は検証の処理を差し替えた Verifier を作る（テスト用）。
func NewWithValidator(audience string, v Validator) *Verifier {
	return &Verifier{audience: audience, validate: v}
}

// Verify は Authorization ヘッダーのトークンが、audience 宛ての正しいトークンで、expectedEmail のサービスアカウントのものかを確かめる。
// ヘッダーが無い・形式が違う・トークンが正しくなければ auth.required（401）、別のアカウントなら origin.forbidden（403）。
func (v *Verifier) Verify(ctx context.Context, authorization, expectedEmail string) error {
	token, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok || strings.TrimSpace(token) == "" {
		return apperr.Unauthenticated("auth.required")
	}
	payload, err := v.validate(ctx, token, v.audience)
	if err != nil || payload == nil {
		return apperr.Unauthenticated("auth.required")
	}
	email, _ := payload.Claims["email"].(string)
	verified, _ := payload.Claims["email_verified"].(bool)
	if expectedEmail == "" || email != expectedEmail || !verified {
		return apperr.Forbidden("origin.forbidden")
	}
	return nil
}
