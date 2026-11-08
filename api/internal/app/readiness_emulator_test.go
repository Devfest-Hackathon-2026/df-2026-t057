package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
)

func TestReadinessFirestoreTransactions(t *testing.T) {
	if os.Getenv("DOPAGAKI_EMULATOR_TEST") != "1" {
		t.Skip("requires selected local emulator")
	}
	if !strings.HasPrefix(os.Getenv("FIRESTORE_EMULATOR_HOST"), "127.0.0.1:") {
		t.Fatal("only local emulator permitted")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	db, err := firestore.NewClient(ctx, "demo-readiness-"+newID()[:8])
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &FirestoreStore{DB: db}
	now := time.Now().UTC()
	_, err = store.Update(ctx, "owner", func(st *State) error {
		st.Tasks = append(st.Tasks, Task{ID: "done", Title: "架空の課題", Status: "completed", Minutes: 30, CompletedAt: &now})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Read(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Update(ctx, "owner", func(st *State) error { st.Tasks = []Task{}; return nil })
	if err != nil {
		t.Fatal(err)
	}
	h, err := store.History(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Completions) != 1 || h.Completions[0].Title != "架空の課題" {
		t.Fatalf("persisted completion: %#v", h)
	}
	archived, err := store.Archived(ctx, "owner")
	if err != nil || len(archived) != 1 {
		t.Fatalf("archive: %v %#v", err, archived)
	}
	after, err := store.Read(ctx, "owner")
	if err != nil || before.BalanceSeconds != after.BalanceSeconds {
		t.Fatal("archive changed wallet", err)
	}
	_, err = store.Update(ctx, "owner", func(st *State) error {
		st.AccountClosing = true
		hash := sha256.Sum256([]byte("0123456789abcdef0123456789abcdef"))
		st.AccountReceiptHash = hex.EncodeToString(hash[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Update(ctx, "owner", func(*State) error { return nil }); !errors.Is(err, errAccountClosing) {
		t.Fatalf("closing account accepted write: %v", err)
	}
	if err = store.DeleteAccount(ctx, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Read(ctx, "owner"); !errors.Is(err, errAccountClosing) {
		t.Fatalf("account resurrected: %v", err)
	}
	handler := (&Server{Mode: "demo", Store: store}).Handler()
	request := httptest.NewRequest("GET", "/api/account/deletion-status", nil)
	request.Header.Set("Authorization", "Deletion 0123456789abcdef0123456789abcdef")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "closing") || strings.Contains(response.Body.String(), "owner") {
		t.Fatalf("deletion status: %d %s", response.Code, response.Body.String())
	}
	h, err = store.History(ctx, "owner")
	if err != nil || len(h.Completions) != 0 {
		t.Fatal("completion survived account deletion", err)
	}
}
