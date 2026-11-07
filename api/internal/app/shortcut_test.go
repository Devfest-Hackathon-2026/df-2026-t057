package app

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// open calls the Shortcuts endpoint the way the iPhone automation does: no cookie, only the key.
func open(c testClient, key string) (int, map[string]string) {
	r := httptest.NewRequest("POST", "/api/shortcut/open", nil)
	r.Header.Set("Authorization", "Shortcut "+key)
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	var out map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func issue(t *testing.T, c testClient) string {
	t.Helper()
	code, raw := c.call("POST", "/api/shortcut/key", nil)
	var out struct{ Key string }
	if code != 200 || json.Unmarshal(raw, &out) != nil || out.Key == "" {
		t.Fatalf("issue key: %d %s", code, raw)
	}
	return out.Key
}

func TestShortcutKeyOpensWithTheNextTaskAndStoresOnlyAHash(t *testing.T) {
	c := client(t)
	in := input()
	in.Why = "単位を落とさない"
	c.state("POST", "/api/tasks", in)
	key := issue(t, c)
	state := c.state("GET", "/api/state", nil)
	if state.Shortcut == nil || strings.Contains(state.Shortcut.Hash, strings.SplitN(key, ".", 2)[1]) {
		t.Fatal("the secret itself was stored")
	}
	code, out := open(c, key)
	if code != 200 || !strings.Contains(out["message"], in.Why) || !strings.HasSuffix(out["url"], "?notification=one") {
		t.Fatalf("open: %d %v", code, out)
	}
	if after := c.state("GET", "/api/state", nil); after.Revision != state.Revision {
		t.Fatal("opening from the shortcut wrote the state")
	}
}

func TestShortcutKeyRejectsWrongRevokedAndUnknownKeys(t *testing.T) {
	c := client(t)
	old := issue(t, c)
	key := issue(t, c)
	if code, _ := open(c, old); code != 401 {
		t.Fatal("a replaced key still works")
	}
	if code, _ := open(c, key[:len(key)-1]+"0"); code != 401 {
		t.Fatal("a wrong secret was accepted")
	}
	stranger := base64.RawURLEncoding.EncodeToString([]byte("demo-0123456789abcdef0123456789abcdef")) + "." + strings.Repeat("a", 64)
	if code, _ := open(c, stranger); code != 401 {
		t.Fatal("an unknown account was accepted")
	}
	if _, found, _ := c.store.Find(t.Context(), "demo-0123456789abcdef0123456789abcdef"); found {
		t.Fatal("an unauthenticated request created an account")
	}
	c.state("DELETE", "/api/shortcut/key", nil)
	if code, _ := open(c, key); code != 401 {
		t.Fatal("a revoked key still works")
	}
	r := httptest.NewRequest("POST", "/api/shortcut/open", nil)
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("a request without a key was accepted")
	}
}
