package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
)

func TestAIUsageAcrossFirestoreClients(t *testing.T) {
	if os.Getenv("DOPAGAKI_EMULATOR_TEST") != "1" {
		t.Skip("requires an explicitly selected local Firestore emulator")
	}
	if !strings.HasPrefix(os.Getenv("FIRESTORE_EMULATOR_HOST"), "127.0.0.1:") {
		t.Fatal("this test only permits a loopback emulator")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	project := "demo-dopagaki-" + newID()[:8]
	stores := []*FirestoreAIUsageStore{}
	for range 2 {
		db, err := firestore.NewClient(ctx, project)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		stores = append(stores, &FirestoreAIUsageStore{DB: db})
	}
	limits := AILimits{2, 2, 3, 3}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i, store := range stores {
		for range 6 {
			wg.Go(func() {
				err := store.Take(ctx, []string{"a", "b"}[i], limits)
				if err == nil {
					accepted.Add(1)
					return
				}
				var limited *AILimitError
				if !errors.As(err, &limited) {
					t.Errorf("transaction failed: %v", err)
				}
			})
		}
	}
	wg.Wait()
	if accepted.Load() != 3 {
		t.Fatalf("global quota admitted %d calls", accepted.Load())
	}
	docs, err := stores[0].DB.Collection("aiUsage").Documents(ctx).GetAll()
	if err != nil {
		t.Fatal(err)
	}
	userCalls := 0
	for _, doc := range docs {
		var usage aiUsage
		if err := doc.DataTo(&usage); err != nil {
			t.Fatal(err)
		}
		if doc.Ref.ID == "global" {
			if usage.DayCalls != 3 {
				t.Fatal("global counter does not match reservations")
			}
		} else {
			if usage.DayCalls > 2 {
				t.Fatal("user quota exceeded")
			}
			userCalls += usage.DayCalls
		}
	}
	if userCalls != 3 {
		t.Fatal("user and global counters were not committed atomically")
	}
	var limited *AILimitError
	if err := (&FirestoreAIUsageStore{DB: stores[1].DB}).Take(ctx, "new-user", limits); !errors.As(err, &limited) {
		t.Fatal("a new limiter instance reset global usage")
	}
}
