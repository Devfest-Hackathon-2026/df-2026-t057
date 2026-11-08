package googleoidc

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/api/idtoken"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
)

func TestVerify(t *testing.T) {
	const invoker = "invoker@p.iam.gserviceaccount.com"
	claims := map[string]map[string]any{
		"good":       {"email": invoker, "email_verified": true},
		"other":      {"email": "someone@p.iam.gserviceaccount.com", "email_verified": true},
		"unverified": {"email": invoker, "email_verified": false},
		"no-email":   {},
	}
	v := NewWithValidator("https://app.example", func(_ context.Context, token, audience string) (*idtoken.Payload, error) {
		c, ok := claims[token]
		if !ok || audience != "https://app.example" {
			return nil, errors.New("invalid token")
		}
		return &idtoken.Payload{Claims: c}, nil
	})
	unauthenticated := apperr.Unauthenticated("auth.required")
	forbidden := apperr.Forbidden("origin.forbidden")
	cases := []struct {
		name, header, email string
		want                error
	}{
		{"valid", "Bearer good", invoker, nil},
		{"missing header", "", invoker, unauthenticated},
		{"basic scheme", "Basic good", invoker, unauthenticated},
		{"empty token", "Bearer ", invoker, unauthenticated},
		{"invalid token", "Bearer forged", invoker, unauthenticated},
		{"other account", "Bearer other", invoker, forbidden},
		{"unverified email", "Bearer unverified", invoker, forbidden},
		{"no email claim", "Bearer no-email", invoker, forbidden},
		{"expected email not configured", "Bearer good", "", forbidden},
	}
	for _, c := range cases {
		err := v.Verify(context.Background(), c.header, c.email)
		if (c.want == nil && err != nil) || (c.want != nil && !errors.Is(err, c.want)) {
			t.Errorf("%s: got %v want %v", c.name, err, c.want)
		}
	}
}
