package activity

import (
	"context"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

type Journal struct{}

func (Journal) Record(ctx context.Context, tx *store.Tx, uid string, e Event) error {
	ref := store.UserItem(uid, "activityEvents", e.ID)
	if _, found, err := store.Get[Event](ctx, tx, ref); err != nil || found {
		return err
	}
	return tx.Set(ref, e)
}
func (Journal) Complete(ctx context.Context, tx *store.Tx, uid string, c Completion) error {
	ref := store.UserItem(uid, "completionRecords", c.TaskID)
	if _, found, err := store.Get[Completion](ctx, tx, ref); err != nil || found {
		return err
	}
	return tx.Set(ref, c)
}
func (Journal) Completions(ctx context.Context, tx *store.Tx, uid string) ([]Completion, error) {
	return store.List[Completion](ctx, tx, store.Query{Collection: store.UserCollection(uid, "completionRecords")})
}
func (Journal) Events(ctx context.Context, tx *store.Tx, uid string) ([]Event, error) {
	return store.List[Event](ctx, tx, store.Query{Collection: store.UserCollection(uid, "activityEvents")})
}
