package account

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/id"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

type Service struct {
	Runner   *store.Runner
	Clock    clock.Clock
	IDs      id.Generator
	Data     Data
	Identity Identity
	Calendar Calendar
	Enabled  bool
	Usage    Usage
}

func hash(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func (s *Service) Export(ctx context.Context, uid string) (map[string][]map[string]any, error) {
	return s.Data.Export(ctx, uid)
}
func (s *Service) Begin(ctx context.Context, uid string, authTime time.Time) (string, error) {
	now := s.Clock.Now()
	if !s.Enabled {
		return "", apperr.Unavailable("account.cleanup_disabled")
	}
	if authTime.IsZero() || now.Sub(authTime) > 5*time.Minute || authTime.After(now.Add(time.Minute)) {
		return "", apperr.Forbidden("account.reauth_required")
	}
	receipt := s.IDs.New() + s.IDs.New()
	err := s.Runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		if err := store.EnsureAccountOpen(ctx, tx, uid); err != nil {
			return err
		}
		job := Job{UID: uid, Status: Pending, ReceiptHash: hash(receipt), ReadyAt: now.Add(90 * time.Second), UpdatedAt: now}
		return tx.Set(store.GlobalDoc("accountJobs", uid), job)
	})
	return receipt, err
}
func (s *Service) Status(ctx context.Context, receipt string) (Status, error) {
	if len(receipt) != 64 {
		return "", apperr.NotFound("not_found")
	}
	var result Status
	err := s.Runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		jobs, err := store.List[Job](ctx, tx, store.Query{Collection: "accountJobs", Filters: []store.Filter{{Field: "receiptHash", Op: store.Eq, Value: hash(receipt)}}})
		if err != nil {
			return err
		}
		if len(jobs) != 1 {
			return apperr.NotFound("not_found")
		}
		result = jobs[0].Status
		return nil
	})
	return result, err
}
func (s *Service) Cleanup(ctx context.Context) error {
	if !s.Enabled {
		return apperr.Unavailable("account.cleanup_disabled")
	}
	var jobs []Job
	if err := s.Runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		jobs, err = store.List[Job](ctx, tx, store.Query{Collection: "accountJobs", Filters: []store.Filter{{Field: "status", Op: store.Eq, Value: string(Pending)}}})
		return err
	}); err != nil {
		return err
	}
	slices.SortFunc(jobs, func(a, b Job) int { return a.UpdatedAt.Compare(b.UpdatedAt) })
	var first error
	count := 0
	for _, j := range jobs {
		if s.Clock.Now().Before(j.ReadyAt) {
			continue
		}
		count++
		if count > 10 {
			break
		}
		if err := s.clean(ctx, j); err != nil && first == nil {
			first = err
		}
	}
	return first
}
func (s *Service) clean(ctx context.Context, j Job) error {
	if err := s.Runner.Update(ctx, func(_ context.Context, tx *store.Tx) error {
		j.UpdatedAt = s.Clock.Now()
		return tx.Set(store.GlobalDoc("accountJobs", j.UID), j)
	}); err != nil {
		return err
	}
	if s.Calendar != nil {
		if err := s.Calendar.Cleanup(ctx, j.UID); err != nil {
			return err
		}
	}
	if s.Usage != nil {
		if err := s.Usage.Forget(ctx, j.UID); err != nil {
			return err
		}
	}
	if err := s.Data.Erase(ctx, j.UID); err != nil {
		return err
	}
	if s.Identity != nil {
		if err := s.Identity.Delete(ctx, j.UID); err != nil {
			return err
		}
	}
	return s.Runner.Update(ctx, func(_ context.Context, tx *store.Tx) error {
		now := s.Clock.Now()
		expires := now.Add(7 * 24 * time.Hour)
		j.Status, j.UpdatedAt, j.ExpiresAt = Complete, now, &expires
		return tx.Set(store.GlobalDoc("accountJobs", j.UID), j)
	})
}

func (s *Service) CheckOpen(ctx context.Context, uid string) error {
	return s.Runner.View(ctx, func(ctx context.Context, tx *store.Tx) error { return store.EnsureAccountOpen(ctx, tx, uid) })
}
