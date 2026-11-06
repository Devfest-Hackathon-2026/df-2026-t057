package app

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Store interface {
	Read(context.Context, string) (State, error)
	Update(context.Context, string, func(*State) error) (State, error)
}
type MemoryStore struct {
	mu     sync.Mutex
	states map[string]State
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{states: map[string]State{}} }
func clone(s State) State {
	b, _ := json.Marshal(s)
	var out State
	_ = json.Unmarshal(b, &out)
	return out
}
func (m *MemoryStore) Read(_ context.Context, uid string) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.states[uid]
	if !ok {
		s = NewState(time.Now())
		m.states[uid] = s
	}
	return clone(s), nil
}
func (m *MemoryStore) Update(_ context.Context, uid string, fn func(*State) error) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.states[uid]
	if !ok {
		s = NewState(time.Now())
	}
	s = clone(s)
	if err := fn(&s); err != nil {
		return State{}, err
	}
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
	doc, err := f.DB.Collection("users").Doc(uid).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return f.Update(ctx, uid, func(*State) error { return nil })
	}
	if err != nil {
		return State{}, err
	}
	return stateFrom(doc)
}
func (f *FirestoreStore) Update(ctx context.Context, uid string, fn func(*State) error) (State, error) {
	ref := f.DB.Collection("users").Doc(uid)
	var result State
	err := f.DB.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(ref)
		s := NewState(time.Now())
		if err == nil {
			s, err = stateFrom(doc)
		}
		if err != nil && status.Code(err) != codes.NotFound {
			return err
		}
		if err = fn(&s); err != nil {
			return err
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
