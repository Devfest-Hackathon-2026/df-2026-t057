// Package memory は、デモとテストで使うメモリ上の実装。外部サービスに接続しない。
package memory

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

type document struct {
	ref  store.Ref
	data []byte
}

// Store はメモリ上のドキュメントストア。1つの Update・View は1つずつ順に実行する（直列化で整合性を保つ）。
// 要求ごとのトランザクション（Scope つき）は fn の間ロックを持たず、同じ Scope の要求を直列に実行し、
// 確定するときに読んだ内容が変わっていないことを確かめる。
type Store struct {
	mu       sync.RWMutex
	docs     map[string]document
	scopes   sync.Map // Scope → *sync.Mutex
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
	// seen は読んだドキュメントの内容（無ければ nil）。要求ごとのトランザクションの確定で、変わっていないことを確かめる。
	seen map[string][]byte
}

// maxAttempts は要求ごとのトランザクションを競合でやり直す回数の上限。
const maxAttempts = 5

// FailNextCommit は、次の書き込みトランザクションのコミットを err で失敗させる（ロールバックのテスト用）。
func (s *Store) FailNextCommit(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNext = err
}

// Run はトランザクションを実行する。
func (s *Store) Run(ctx context.Context, opts store.RunOptions, fn func(ctx context.Context, tx store.BackendTx) error) error {
	if opts.ReadOnly {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return fn(ctx, &tx{s: s})
	}
	if opts.Scope != "" {
		return s.runRequest(ctx, opts.Scope, fn)
	}
	s.mu.Lock()
	t := &tx{s: s}
	if err := fn(ctx, t); err != nil {
		s.mu.Unlock()
		return err
	}
	changed, err := s.commit(t)
	s.mu.Unlock()
	s.notifyAll(changed)
	return err
}

// runRequest は要求ごとのトランザクション。fn の間は全体のロックを持たず（fn の中の View や Detach した Update が
// 動けるように）、同じ Scope の要求どうしだけを直列にする。確定で読んだ内容が変わっていれば fn をやり直す。
func (s *Store) runRequest(ctx context.Context, scope string, fn func(ctx context.Context, tx store.BackendTx) error) error {
	lock, _ := s.scopes.LoadOrStore(scope, &sync.Mutex{})
	mu := lock.(*sync.Mutex) // LoadOrStore には *sync.Mutex しか入れない。
	mu.Lock()
	defer mu.Unlock()
	for range maxAttempts {
		t := &tx{s: s, seen: map[string][]byte{}}
		if err := fn(ctx, t); err != nil {
			return err
		}
		s.mu.Lock()
		if !t.unchanged() {
			s.mu.Unlock()
			continue
		}
		changed, err := s.commit(t)
		s.mu.Unlock()
		s.notifyAll(changed)
		return err
	}
	return store.ErrConflict
}

// unchanged は読んだドキュメントが今も同じ内容か（s.mu を持って呼ぶ）。
func (t *tx) unchanged() bool {
	for path, data := range t.seen {
		d, ok := t.s.docs[path]
		if ok != (data != nil) || (ok && !bytes.Equal(d.data, data)) {
			return false
		}
	}
	return true
}

func (t *tx) note(path string, data []byte) {
	if t.seen == nil {
		return
	}
	if _, already := t.seen[path]; !already {
		t.seen[path] = data
	}
}

// commit は溜めた書き込みを反映し、変わった版のドキュメントを返す（s.mu を持って呼ぶ）。
func (s *Store) commit(t *tx) (map[string]store.Meta, error) {
	if err := s.failNext; err != nil {
		s.failNext = nil
		return nil, err
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
	return changed, nil
}

// notifyAll はロックを外した後に版の変更を通知する。
func (s *Store) notifyAll(changed map[string]store.Meta) {
	for path, m := range changed {
		s.notify(path, m)
	}
}

func (t *tx) Get(_ context.Context, ref store.Ref, dst any) (bool, error) {
	if t.seen != nil {
		// 要求ごとのトランザクションは全体のロックを持たない。
		t.s.mu.RLock()
		defer t.s.mu.RUnlock()
	}
	d, ok := t.s.docs[ref.Path()]
	if !ok {
		t.note(ref.Path(), nil)
		return false, nil
	}
	t.note(ref.Path(), d.data)
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
	rows, err := t.collect(q)
	if err != nil {
		return err
	}
	slices.SortFunc(rows, func(a, b row) int {
		if q.OrderBy != "" {
			if c := store.Compare(a.fields[q.OrderBy], b.fields[q.OrderBy]); c != 0 {
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
		t.note(r.doc.ref.Path(), r.doc.data)
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

// collect は条件に合うドキュメントを集める。Firestore と同じく、並べ替えの項目が無いドキュメントは返さない。
func (t *tx) collect(q store.Query) ([]row, error) {
	if t.seen != nil {
		t.s.mu.RLock()
		defer t.s.mu.RUnlock()
	}
	var rows []row
	for _, d := range t.s.docs {
		if d.ref.Collection != q.Collection {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal(d.data, &fields); err != nil {
			return nil, fmt.Errorf("memory: decode %s: %w", d.ref.Path(), err)
		}
		if _, ordered := fields[q.OrderBy]; (q.OrderBy != "" && !ordered) || !store.Matches(fields, q.Filters) {
			continue
		}
		rows = append(rows, row{doc: d, fields: fields})
	}
	return rows, nil
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
