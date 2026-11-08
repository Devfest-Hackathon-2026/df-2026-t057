// Package firebaseauth は Firebase の ID トークンを確かめる。
package firebaseauth

import (
	"context"
	"fmt"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"

	apihttp "github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/http"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
)

// Verifier は Firebase Admin SDK で ID トークンを確かめる。
type Verifier struct {
	client *auth.Client
}

// New は Verifier を作る。
func New(ctx context.Context, projectID string) (*Verifier, error) {
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("firebase app: %w", err)
	}
	client, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("firebase auth: %w", err)
	}
	return &Verifier{client: client}, nil
}

// Verify は ID トークンを確かめ、利用者を返す。Google のアカウントの ID は、google.com のプロバイダーが1つだけのときに使う。
func (v *Verifier) Verify(ctx context.Context, idToken string) (apihttp.Principal, error) {
	token, err := v.client.VerifyIDToken(ctx, idToken)
	if err != nil {
		return apihttp.Principal{}, apperr.Unauthenticated("auth.expired").Wrap(err)
	}
	return principal(token), nil
}

func principal(token *auth.Token) apihttp.Principal {
	p := apihttp.Principal{UID: token.UID}
	if email, ok := token.Claims["email"].(string); ok {
		p.Email = email
	}
	if ids, ok := token.Firebase.Identities["google.com"].([]any); ok && len(ids) == 1 {
		if subject, ok := ids[0].(string); ok {
			p.GoogleSubject = subject
		}
	}
	return p
}
