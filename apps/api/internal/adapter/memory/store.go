// Package memory は、デモとテストで使うメモリ上の実装。外部サービスに接続しない。
package memory

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

type document struct {
	ref  store.Ref
	data []byte
}

// Store はメモリ上のドキュメントストア。トランザクションは1つずつ順に実行する（直列化で整合性を保つ）。
type Store struct {
	mu       sync.RWMutex
	docs     map[string]document
	watchMu  sync.Mutex
	watchers map[string]map[int]func(store.Meta)
	nextID   int
	// failNext はテスト用に、次のコミットを失敗させる。
	failNext error
}

// NewStore は空のストアを作る。
func NewStore() *Store {
	return &Store{docs: map[string]document{}, watchers: map[string]map[int]func(store.Meta){}}
}

type op struct {
	ref     store.Ref
	data    []byte
	deleted bool
}

type tx struct {
	s   *Store
	ops []op
}

// FailNextCommit は、次の書き込みトランザクションのコミットを err で失敗させる（ロールバックのテスト用）。
func (s *Store) FailNextCommit(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNext = err
}

// Run はトランザクションを実行する。
func (s *Store) Run(ctx context.Context, readOnly bool, fn func(ctx context.Context, tx store.BackendTx) error) error {
	if readOnly {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return fn(ctx, &tx{s: s})
	}
	s.mu.Lock()
	t := &tx{s: s}
	if err := fn(ctx, t); err != nil {
		s.mu.Unlock()
		return err
	}
	if err := s.failNext; err != nil {
		s.failNext = nil
		s.mu.Unlock()
		return err
	}
	changed := map[string]store.Meta{}
	for _, o := range t.ops {
		path := o.ref.Path()
		if o.deleted {
			delete(s.docs, path)
		} else {
			s.docs[path] = document{ref: o.ref, data: o.data}
		}
		if o.ref.ID == "meta" && strings.HasSuffix(o.ref.Collection, "/state") {
			var m store.Meta
			if !o.deleted {
				_ = json.Unmarshal(o.data, &m)
			}
			changed[path] = m
		}
	}
	s.mu.Unlock()
	for path, m := range changed {
		s.notify(path, m)
	}
	return nil
}

func (t *tx) Get(_ context.Context, ref store.Ref, dst any) (bool, error) {
	d, ok := t.s.docs[ref.Path()]
	if !ok {
		return false, nil
	}
	if err := json.Unmarshal(d.data, dst); err != nil {
		return false, fmt.Errorf("memory: decode %s: %w", ref.Path(), err)
	}
	return true, nil
}

func (t *tx) Set(ref store.Ref, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("memory: encode %s: %w", ref.Path(), err)
	}
	t.ops = append(t.ops, op{ref: ref, data: data})
	return nil
}

func (t *tx) Delete(ref store.Ref) error {
	t.ops = append(t.ops, op{ref: ref, deleted: true})
	return nil
}

type row struct {
	doc    document
	fields map[string]any
}

func (t *tx) Query(_ context.Context, q store.Query, each func(ref store.Ref, decode func(dst any) error) error) error {
	var rows []row
	for _, d := range t.s.docs {
		if d.ref.Collection != q.Collection {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal(d.data, &fields); err != nil {
			return fmt.Errorf("memory: decode %s: %w", d.ref.Path(), err)
		}
		match := true
		for _, f := range q.Filters {
			if !matches(fields[f.Field], f) {
				match = false
				break
			}
		}
		if match {
			rows = append(rows, row{doc: d, fields: fields})
		}
	}
	slices.SortFunc(rows, func(a, b row) int {
		if q.OrderBy != "" {
			if c := compare(a.fields[q.OrderBy], b.fields[q.OrderBy]); c != 0 {
				if q.Desc {
					return -c
				}
				return c
			}
		}
		return cmp.Compare(a.doc.ref.ID, b.doc.ref.ID)
	})
	if q.Limit > 0 && len(rows) > q.Limit {
		rows = rows[:q.Limit]
	}
	for _, r := range rows {
		data := r.doc.data
		err := each(r.doc.ref, func(dst any) error {
			if err := json.Unmarshal(data, dst); err != nil {
				return fmt.Errorf("memory: decode: %w", err)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// matches は JSON で保存した値が条件に合うか。
func matches(value any, f store.Filter) bool {
	c, ok := compareFilter(value, f.Value)
	if !ok {
		return false
	}
	switch f.Op {
	case store.Eq:
		return c == 0
	case store.Lt:
		return c < 0
	case store.Le:
		return c <= 0
	case store.Gt:
		return c > 0
	case store.Ge:
		return c >= 0
	default:
		return false
	}
}

// compareFilter は JSON の値と、Go の条件の値を比べる。型が合わなければ false。
func compareFilter(value, want any) (int, bool) {
	switch w := want.(type) {
	case string:
		v, ok := value.(string)
		return cmp.Compare(v, w), ok
	case bool:
		v, ok := value.(bool)
		if !ok {
			return 0, false
		}
		if v == w {
			return 0, true
		}
		return 1, true
	case int:
		v, ok := value.(float64)
		return cmp.Compare(v, float64(w)), ok
	case int64:
		v, ok := value.(float64)
		return cmp.Compare(v, float64(w)), ok
	case time.Time:
		s, ok := value.(string)
		if !ok {
			return 0, false
		}
		v, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return 0, false
		}
		return v.Compare(w), true
	default:
		return 0, false
	}
}

// compare は JSON の値どうしを比べる（並べ替え用）。
func compare(a, b any) int {
	switch x := a.(type) {
	case float64:
		y, _ := b.(float64)
		return cmp.Compare(x, y)
	case string:
		y, _ := b.(string)
		tx, errX := time.Parse(time.RFC3339Nano, x)
		ty, errY := time.Parse(time.RFC3339Nano, y)
		if errX == nil && errY == nil {
			return tx.Compare(ty)
		}
		return cmp.Compare(x, y)
	default:
		return 0
	}
}

// WatchMeta は利用者の版のドキュメントを監視し、変わるたびに fn を呼ぶ。最初に今の値で1回呼ぶ。ctx が終わるまで戻らない。
func (s *Store) WatchMeta(ctx context.Context, uid string, fn func(store.Meta)) error {
	path := store.MetaRef(uid).Path()
	s.watchMu.Lock()
	s.nextID++
	id := s.nextID
	if s.watchers[path] == nil {
		s.watchers[path] = map[int]func(store.Meta){}
	}
	s.watchers[path][id] = fn
	s.watchMu.Unlock()
	defer func() {
		s.watchMu.Lock()
		delete(s.watchers[path], id)
		if len(s.watchers[path]) == 0 {
			delete(s.watchers, path)
		}
		s.watchMu.Unlock()
	}()

	var current store.Meta
	s.mu.RLock()
	if d, ok := s.docs[path]; ok {
		_ = json.Unmarshal(d.data, &current)
	}
	s.mu.RUnlock()
	fn(current)
	<-ctx.Done()
	return nil
}

func (s *Store) notify(path string, m store.Meta) {
	s.watchMu.Lock()
	fns := make([]func(store.Meta), 0, len(s.watchers[path]))
	for _, fn := range s.watchers[path] {
		fns = append(fns, fn)
	}
	s.watchMu.Unlock()
	for _, fn := range fns {
		fn(m)
	}
}
