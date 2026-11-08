package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/account"
)

func (s *Store) Export(_ context.Context, uid string) (map[string][]map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string][]map[string]any{}
	for _, c := range account.ExportCollections {
		out[c] = []map[string]any{}
		prefix := "users/" + uid + "/" + c + "/"
		for path, doc := range s.docs {
			if !strings.HasPrefix(path, prefix) {
				continue
			}
			id := strings.TrimPrefix(path, prefix)
			if !account.Exportable(c, id) {
				continue
			}
			var row map[string]any
			if err := json.Unmarshal(doc.data, &row); err != nil {
				return nil, fmt.Errorf("export: %w", err)
			}
			out[c] = append(out[c], account.ExportRow(c, id, row))
		}
	}
	return out, nil
}
func (s *Store) Erase(_ context.Context, uid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for path, doc := range s.docs {
		remove := strings.HasPrefix(path, "users/"+uid+"/")
		if strings.HasPrefix(path, "oauthStates/") || strings.HasPrefix(path, "calendarChannels/") {
			var row struct {
				UID string `json:"uid"`
			}
			if err := json.Unmarshal(doc.data, &row); err != nil {
				return fmt.Errorf("erase: %w", err)
			}
			remove = remove || row.UID == uid
		}
		if remove {
			delete(s.docs, path)
		}
	}
	return nil
}
