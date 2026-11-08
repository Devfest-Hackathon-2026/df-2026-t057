// Package firestore は store.Backend と realtime.Watcher の Firestore による実装。
package firestore

import (
	"context"
	"errors"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

// Store は Firestore のドキュメントストア。
type Store struct {
	client *firestore.Client
}

// New は Store を作る。
func New(client *firestore.Client) *Store { return &Store{client: client} }

// Run はトランザクションを実行する。競合したときは Firestore が fn を呼び直す。
func (s *Store) Run(ctx context.Context, readOnly bool, fn func(ctx context.Context, tx store.BackendTx) error) error {
	var opts []firestore.TransactionOption
	if readOnly {
		opts = append(opts, firestore.ReadOnly)
	}
	return s.client.RunTransaction(ctx, func(ctx context.Context, t *firestore.Transaction) error { //nolint:wrapcheck // fn のエラー（apperr）をそのまま返す。
		return fn(ctx, &tx{client: s.client, t: t})
	}, opts...)
}

type tx struct {
	client *firestore.Client
	t      *firestore.Transaction
}

func (x *tx) Get(_ context.Context, ref store.Ref, dst any) (bool, error) {
	snap, err := x.t.Get(x.client.Doc(ref.Path()))
	if status.Code(err) == codes.NotFound {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("firestore get: %w", err)
	}
	if err := snap.DataTo(dst); err != nil {
		return false, fmt.Errorf("firestore decode: %w", err)
	}
	return true, nil
}

func (x *tx) Query(_ context.Context, q store.Query, each func(ref store.Ref, decode func(dst any) error) error) error {
	query := x.client.Collection(q.Collection).Query
	for _, f := range q.Filters {
		query = query.Where(f.Field, string(f.Op), f.Value)
	}
	if q.OrderBy != "" {
		dir := firestore.Asc
		if q.Desc {
			dir = firestore.Desc
		}
		query = query.OrderBy(q.OrderBy, dir)
	}
	if q.Limit > 0 {
		query = query.Limit(q.Limit)
	}
	docs, err := x.t.Documents(query).GetAll()
	if err != nil {
		return fmt.Errorf("firestore query: %w", err)
	}
	for _, d := range docs {
		snap := d
		err := each(store.Ref{Collection: q.Collection, ID: snap.Ref.ID}, func(dst any) error {
			if err := snap.DataTo(dst); err != nil {
				return fmt.Errorf("firestore decode: %w", err)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (x *tx) Set(ref store.Ref, value any) error {
	if err := x.t.Set(x.client.Doc(ref.Path()), value); err != nil {
		return fmt.Errorf("firestore set: %w", err)
	}
	return nil
}

func (x *tx) Delete(ref store.Ref) error {
	if err := x.t.Delete(x.client.Doc(ref.Path())); err != nil {
		return fmt.Errorf("firestore delete: %w", err)
	}
	return nil
}

// WatchMeta は利用者の版のドキュメントを snapshot listener で監視する。
func (s *Store) WatchMeta(ctx context.Context, uid string, fn func(store.Meta)) error {
	it := s.client.Doc(store.MetaRef(uid).Path()).Snapshots(ctx)
	defer it.Stop()
	for {
		snap, err := it.Next()
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return fmt.Errorf("firestore watch: %w", err)
		}
		var m store.Meta
		if snap.Exists() {
			if err := snap.DataTo(&m); err != nil {
				return fmt.Errorf("firestore decode: %w", err)
			}
		}
		fn(m)
	}
}

// ErrNoEmulator は、エミュレーターのテストを飛ばすときの理由。
var ErrNoEmulator = errors.New("FIRESTORE_EMULATOR_HOST is not set")
