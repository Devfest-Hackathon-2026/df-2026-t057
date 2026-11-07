package app

import (
	"context"
	"errors"
	"net/http"
	"strings"

	firebaseauth "firebase.google.com/go/v4/auth"
)

type userKey struct{}
type googleAccountKey struct{}
type googleAccount struct{ Subject, Email string }

func tokenGoogleAccount(token *firebaseauth.Token) googleAccount {
	identities, _ := token.Firebase.Identities["google.com"].([]interface{})
	var subject string
	if len(identities) == 1 {
		subject, _ = identities[0].(string)
	}
	email, _ := token.Claims["email"].(string)
	return googleAccount{Subject: subject, Email: email}
}

func uid(r *http.Request) string { return r.Context().Value(userKey{}).(string) }
func (s *Server) protected(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var id string
		if s.Mode == "demo" {
			authorization := r.Header.Get("Authorization")
			token, demo := strings.CutPrefix(authorization, "Demo ")
			cookie, err := r.Cookie("dopagaki_demo")
			if authorization != "" {
				if !demo || !tokenIDPattern.MatchString(token) {
					writeError(w, 401, errors.New("デモの接続情報を確認してください"))
					return
				}
				id = "demo-" + token
			} else if err == nil && tokenIDPattern.MatchString(cookie.Value) {
				id = "demo-" + cookie.Value
			} else {
				id = "demo-" + newID()
				http.SetCookie(w, &http.Cookie{Name: "dopagaki_demo", Value: strings.TrimPrefix(id, "demo-"), Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil, MaxAge: 7 * 24 * 60 * 60})
			}
		} else {
			token, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !bearer || token == "" || s.Auth == nil {
				writeError(w, 401, errors.New("Googleアカウントでログインしてください"))
				return
			}
			verified, err := s.Auth.VerifyIDToken(r.Context(), token)
			if err != nil {
				writeError(w, 401, errors.New("ログインの有効期限が切れました。再度ログインしてください"))
				return
			}
			id = verified.UID
			r = r.WithContext(context.WithValue(r.Context(), googleAccountKey{}, tokenGoogleAccount(verified)))
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey{}, id)))
	}
}
