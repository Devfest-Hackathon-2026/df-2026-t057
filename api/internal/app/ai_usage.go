package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AILimits struct {
	UserPerMinute, UserPerDay, GlobalPerMinute, GlobalPerDay int
}

func DefaultAILimits() AILimits { return AILimits{5, 30, 30, 300} }

type AILimitError struct{ RetryAt time.Time }

func (*AILimitError) Error() string {
	return "AIの利用上限に達しました。時間をおいて再試行するか、手入力をご利用ください"
}

var ErrAIUsageUnavailable = errors.New("AIの利用状況を確認できません。少し待って再試行してください")

type AIUsageStore interface {
	Take(context.Context, string, AILimits) error
}

type aiUsage struct {
	Minute      time.Time
	MinuteCalls int
	Day         time.Time
	DayCalls    int
}

// Check both counters before incrementing either; rejected calls spend no quota.
func reserveAIUsage(user, global aiUsage, limits AILimits, now time.Time) (aiUsage, aiUsage, error) {
	minute, day := now.UTC().Truncate(time.Minute), now.UTC().Truncate(24*time.Hour)
	var retryAt time.Time
	for i, usage := range []*aiUsage{&user, &global} {
		if !usage.Minute.Equal(minute) {
			usage.Minute, usage.MinuteCalls = minute, 0
		}
		if !usage.Day.Equal(day) {
			usage.Day, usage.DayCalls = day, 0
		}
		perMinute, perDay := limits.UserPerMinute, limits.UserPerDay
		if i == 1 {
			perMinute, perDay = limits.GlobalPerMinute, limits.GlobalPerDay
		}
		if usage.MinuteCalls >= perMinute && minute.Add(time.Minute).After(retryAt) {
			retryAt = minute.Add(time.Minute)
		}
		if usage.DayCalls >= perDay && day.Add(24*time.Hour).After(retryAt) {
			retryAt = day.Add(24 * time.Hour)
		}
	}
	if !retryAt.IsZero() {
		return user, global, &AILimitError{RetryAt: retryAt}
	}
	user.MinuteCalls++
	user.DayCalls++
	global.MinuteCalls++
	global.DayCalls++
	return user, global, nil
}

type MemoryAIUsageStore struct {
	mu     sync.Mutex
	users  map[string]aiUsage
	global aiUsage
}

func NewMemoryAIUsageStore() *MemoryAIUsageStore {
	return &MemoryAIUsageStore{users: map[string]aiUsage{}}
}

func (m *MemoryAIUsageStore) Take(_ context.Context, owner string, limits AILimits) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, global, err := reserveAIUsage(m.users[owner], m.global, limits, time.Now())
	if err == nil {
		m.users[owner], m.global = user, global
	}
	return err
}

type FirestoreAIUsageStore struct{ DB *firestore.Client }

func (f *FirestoreAIUsageStore) Take(ctx context.Context, owner string, limits AILimits) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	hash := sha256.Sum256([]byte(owner))
	users := f.DB.Collection("aiUsage")
	userRef, globalRef := users.Doc("user-"+hex.EncodeToString(hash[:])), users.Doc("global")
	return f.DB.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		usage := [2]aiUsage{}
		// Acquire the shared document first so concurrent users use the same lock order.
		for i, ref := range []*firestore.DocumentRef{globalRef, userRef} {
			doc, err := tx.Get(ref)
			if status.Code(err) == codes.NotFound {
				continue
			}
			if err != nil {
				return err
			}
			if err := doc.DataTo(&usage[i]); err != nil {
				return err
			}
		}
		user, global, err := reserveAIUsage(usage[1], usage[0], limits, time.Now())
		if err != nil {
			return err
		}
		if err := tx.Set(userRef, user); err != nil {
			return err
		}
		return tx.Set(globalRef, global)
	})
}
