package app

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Shortcut is the user's key for the iPhone Shortcuts automation. Only a hash of the secret is stored.
type Shortcut struct {
	Hash      string    `json:"keyHash"`
	CreatedAt time.Time `json:"createdAt"`
}

// shortcutKey is "<base64url(uid)>.<secret>"; the uid part lets the server find the account without an index.
func shortcutKey(id string) (key, hash string) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	secret := hex.EncodeToString(b[:])
	sum := sha256.Sum256([]byte(secret))
	return base64.RawURLEncoding.EncodeToString([]byte(id)) + "." + secret, hex.EncodeToString(sum[:])
}

func parseShortcutKey(key string) (id, hash string, ok bool) {
	owner, secret, found := strings.Cut(key, ".")
	if len(owner) > 200 {
		return "", "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(owner)
	if !found || err != nil || len(raw) == 0 || len(secret) != 64 {
		return "", "", false
	}
	sum := sha256.Sum256([]byte(secret))
	return string(raw), hex.EncodeToString(sum[:]), true
}

func (s *Server) issueShortcut(w http.ResponseWriter, r *http.Request) {
	key, hash := shortcutKey(uid(r))
	_, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		st.Shortcut = &Shortcut{Hash: hash, CreatedAt: s.now()}
		return nil
	})
	respond(w, map[string]string{"key": key}, err)
}

func (s *Server) revokeShortcut(w http.ResponseWriter, r *http.Request) {
	st, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		st.Shortcut = nil
		return nil
	})
	respond(w, st, err)
}

// shortcutOpen answers the Shortcuts automation that runs when the YouTube app opens:
// the companion's remark about the next task, for a notification. It reads only; nothing is saved.
func (s *Server) shortcutOpen(w http.ResponseWriter, r *http.Request) {
	key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Shortcut ")
	id, hash, valid := parseShortcutKey(key)
	if !ok || !valid {
		writeError(w, 401, errors.New("ショートカットの接続キーを確認してください"))
		return
	}
	state, found, err := s.Store.Find(r.Context(), id)
	if err != nil || !found || state.Shortcut == nil || subtle.ConstantTimeCompare([]byte(state.Shortcut.Hash), []byte(hash)) != 1 {
		writeError(w, 401, errors.New("ショートカットの接続キーが無効です。管理画面で発行し直してください"))
		return
	}
	var next *Task
	if pending := state.pending(); len(pending) > 0 {
		next = &pending[0]
	}
	line, _ := s.Planner.Line(r.Context(), id, "app_open", state, next)
	open := s.AppURL + "/"
	if next != nil {
		open += "?notification=" + url.QueryEscape(next.ID)
	}
	writeJSON(w, 200, map[string]string{"title": "DOPAGAKI · " + CompanionName, "message": line, "url": open})
}
