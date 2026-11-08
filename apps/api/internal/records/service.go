package records

import (
	"context"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/activity"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/profile"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/viewing"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

// Service は記録の集計のユースケース。
type Service struct {
	clock    clock.Clock
	profiles *profile.Service
	tasks    *task.Service
	wallet   *wallet.Service
	viewing  *viewing.Service
}

// New は Service を作る。
func New(c clock.Clock, p *profile.Service, t *task.Service, w *wallet.Service, v *viewing.Service) *Service {
	return &Service{clock: c, profiles: p, tasks: t, wallet: w, viewing: v}
}

// Get は記録を計算する。
func (s *Service) Get(ctx context.Context, uid string) (r Records, err error) {
	now := s.clock.Now()
	err = s.wallet.Read(ctx, uid, func(ctx context.Context, tx *store.Tx, a *wallet.Account) error {
		in := Input{Now: now, Account: *a}
		var err error
		if in.Location, err = s.profiles.Location(ctx, tx, uid); err != nil {
			return err
		}
		if in.Tasks, err = s.tasks.All(ctx, tx, uid); err != nil {
			return err
		}
		if in.Work, err = s.wallet.Entries(ctx, tx, uid); err != nil {
			return err
		}
		if in.Sessions, err = s.viewing.SessionsSince(ctx, tx, uid, now.Add(-PracticeWindow)); err != nil {
			return err
		}
		journal := activity.Journal{}
		if in.Completions, err = journal.Completions(ctx, tx, uid); err != nil {
			return err
		}
		if in.Events, err = journal.Events(ctx, tx, uid); err != nil {
			return err
		}
		r = Compute(in)
		return nil
	})
	return r, err
}
