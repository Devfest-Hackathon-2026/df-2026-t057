package app

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/a-sumikawa/devfest-2026/api/internal/records"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var errAccountClosing = errors.New("アカウントの削除を受け付けています")

type Store interface {
	Read(context.Context, string) (State, error)
	Update(context.Context, string, func(*State) error) (State, error)
}
type MemoryStore struct {
	mu        sync.Mutex
	states    map[string]State
	deleted   map[string]bool
	histories map[string]records.History
	archived  map[string]map[string]Task
	now       func() time.Time
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{states: map[string]State{}, now: time.Now} }
func clone(s State) State {
	b, _ := json.Marshal(s)
	var out State
	_ = json.Unmarshal(b, &out)
	return out
}
func (m *MemoryStore) Read(_ context.Context, uid string) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleted[uid] {
		return State{}, errAccountClosing
	}
	s, ok := m.states[uid]
	if !ok {
		s = NewState(m.now())
	}
	if s.AccountClosing {
		return State{}, errAccountClosing
	}
	s = clone(s)
	if s.advanceTimePolicy(m.now()) {
		s.Revision++
	}
	m.states[uid] = s
	return clone(s), nil
}
func (m *MemoryStore) Update(_ context.Context, uid string, fn func(*State) error) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleted[uid] {
		return State{}, errAccountClosing
	}
	s, ok := m.states[uid]
	if !ok {
		s = NewState(m.now())
	}
	if s.AccountClosing {
		return State{}, errAccountClosing
	}
	s = clone(s)
	before := clone(s)
	s.advanceTimePolicy(m.now())
	if err := fn(&s); err != nil {
		return State{}, err
	}
	m.saveHistory(uid, before, s, m.now())
	s.Revision++
	m.states[uid] = s
	return clone(s), nil
}

type FirestoreStore struct{ DB *firestore.Client }

func stateFrom(doc *firestore.DocumentSnapshot) (State, error) {
	var s State
	raw, err := doc.DataAt("state")
	if err != nil {
		return s, err
	}
	err = json.Unmarshal([]byte(raw.(string)), &s)
	return s, err
}
func (f *FirestoreStore) Read(ctx context.Context, uid string) (State, error) {
	job, jobErr := f.DB.Collection("accountJobs").Doc(uid).Get(ctx)
	if closingDocument(job, jobErr) {
		return State{}, errAccountClosing
	}
	if err := missingOrNil(jobErr); err != nil {
		return State{}, err
	}
	doc, err := f.DB.Collection("users").Doc(uid).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return f.Update(ctx, uid, func(*State) error { return nil })
	}
	if err != nil {
		return State{}, err
	}
	s, err := stateFrom(doc)
	if err == nil && (s.TimePolicy == nil || s.TimePolicy.Version < timePolicyVersion || !time.Now().Before(s.TimePolicy.ResetAt)) {
		return f.Update(ctx, uid, func(*State) error { return nil })
	}
	return s, err
}
func (f *FirestoreStore) Update(ctx context.Context, uid string, fn func(*State) error) (State, error) {
	ref := f.DB.Collection("users").Doc(uid)
	var result State
	now := time.Now()
	err := f.DB.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		job, jobErr := tx.Get(f.DB.Collection("accountJobs").Doc(uid))
		if closingDocument(job, jobErr) {
			return errAccountClosing
		}
		if err := missingOrNil(jobErr); err != nil {
			return err
		}
		doc, err := tx.Get(ref)
		s := NewState(now)
		if err == nil {
			s, err = stateFrom(doc)
		}
		if err != nil && status.Code(err) != codes.NotFound {
			return err
		}
		if s.AccountClosing {
			return errAccountClosing
		}
		before := clone(s)
		s.advanceTimePolicy(now)
		if err = fn(&s); err != nil {
			return err
		}
		if err = f.saveHistory(tx, uid, before, s, now); err != nil {
			return err
		}
		if s.AccountClosing {
			if err = tx.Set(f.DB.Collection("accountJobs").Doc(uid), map[string]any{"status": "closing", "deleteCalendar": s.DeleteCalendar, "notBefore": now.Add(90 * time.Second), "receiptHash": s.AccountReceiptHash}); err != nil {
				return err
			}
		}
		s.Revision++
		raw, err := json.Marshal(s)
		if err != nil {
			return err
		}
		if err = tx.Set(ref, map[string]any{"state": string(raw), "updatedAt": firestore.ServerTimestamp}); err != nil {
			return err
		}
		result = s
		return nil
	})
	return result, err
}
