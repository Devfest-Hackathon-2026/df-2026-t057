package task

import (
	"context"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/activity"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

func (s *Service) Archived(ctx context.Context, uid string) (items []Task, err error) {
	err = s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		items, err = store.List[Task](ctx, tx, store.Query{Collection: store.UserCollection(uid, "archivedTasks")})
		return err
	})
	return
}
func (s *Service) Archive(ctx context.Context, uid, taskID string, restore bool) error {
	return s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		source, target := taskRef(uid, taskID), store.UserItem(uid, "archivedTasks", taskID)
		if restore {
			source, target = target, source
		}
		t, found, err := store.Get[Task](ctx, tx, source)
		if err != nil {
			return err
		}
		if !found {
			return apperr.NotFound("task.not_found")
		}
		if t.Status != StatusCompleted {
			return apperr.Conflict("task.not_completed")
		}
		if restore {
			items, err := s.All(ctx, tx, uid)
			if err != nil {
				return err
			}
			if len(items) >= MaxTasks {
				return apperr.Conflict("task.limit_reached")
			}
		}
		if t.CompletedAt != nil {
			if err := (activity.Journal{}).Complete(ctx, tx, uid, activity.Completion{TaskID: t.ID, Title: t.Title, At: *t.CompletedAt}); err != nil {
				return err
			}
		}
		if err := tx.Set(target, t); err != nil {
			return err
		}
		if err := tx.Delete(source); err != nil {
			return err
		}
		_, err = s.touch(ctx, tx, uid)
		return err
	})
}
