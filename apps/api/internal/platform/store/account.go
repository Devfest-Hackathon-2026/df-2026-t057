package store

import (
	"context"
	"strings"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
)

func EnsureAccountOpen(ctx context.Context, tx *Tx, uid string) error {
	var job struct {
		Status string `json:"status" firestore:"status"`
	}
	found, err := tx.Get(ctx, GlobalDoc("accountJobs", uid), &job)
	if err != nil {
		return err
	}
	if found {
		return apperr.Forbidden("account.closed")
	}
	return nil
}
func (t *Tx) checkAccounts(ctx context.Context) error {
	checked := map[string]bool{}
	for _, w := range t.writes {
		parts := strings.Split(w.ref.Path(), "/")
		if len(parts) < 2 || parts[0] != "users" || (len(parts) > 2 && parts[2] == "idempotency") || checked[parts[1]] {
			continue
		}
		checked[parts[1]] = true
		if err := EnsureAccountOpen(ctx, t, parts[1]); err != nil {
			return err
		}
	}
	return nil
}
