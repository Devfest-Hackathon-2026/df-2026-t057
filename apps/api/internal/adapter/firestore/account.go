package firestore

import (
	"context"
	"errors"
	"fmt"

	"cloud.google.com/go/firestore"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/account"
	"google.golang.org/api/iterator"
)

func (s *Store) Export(ctx context.Context, uid string) (map[string][]map[string]any, error) {
	out := map[string][]map[string]any{}
	for _, c := range account.ExportCollections {
		out[c] = []map[string]any{}
		docs, err := s.client.Collection("users/" + uid + "/" + c).Documents(ctx).GetAll()
		if err != nil {
			return nil, fmt.Errorf("export collection: %w", err)
		}
		for _, d := range docs {
			if account.Exportable(c, d.Ref.ID) {
				out[c] = append(out[c], account.ExportRow(c, d.Ref.ID, d.Data()))
			}
		}
	}
	return out, nil
}
func (s *Store) Erase(ctx context.Context, uid string) error {
	root := s.client.Doc("users/" + uid)
	if err := s.eraseDoc(ctx, root); err != nil {
		return err
	}
	for _, collection := range []string{"oauthStates", "calendarChannels"} {
		docs, err := s.client.Collection(collection).Where("uid", "==", uid).Documents(ctx).GetAll()
		if err != nil {
			return fmt.Errorf("find account references: %w", err)
		}
		for _, doc := range docs {
			if _, err := doc.Ref.Delete(ctx); err != nil {
				return fmt.Errorf("delete account reference: %w", err)
			}
		}
	}
	return nil
}
func (s *Store) eraseDoc(ctx context.Context, ref *firestore.DocumentRef) error {
	collections := ref.Collections(ctx)
	for {
		c, err := collections.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return fmt.Errorf("list account collections: %w", err)
		}
		for {
			docs, err := c.Limit(100).Documents(ctx).GetAll()
			if err != nil {
				return fmt.Errorf("read cleanup batch: %w", err)
			}
			if len(docs) == 0 {
				break
			}
			for _, d := range docs {
				if err := s.eraseDoc(ctx, d.Ref); err != nil {
					return err
				}
			}
		}
	}
	if _, err := ref.Delete(ctx); err != nil {
		return fmt.Errorf("delete account document: %w", err)
	}
	return nil
}
