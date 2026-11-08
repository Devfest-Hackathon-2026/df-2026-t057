// Package store は、機能のサービスが使うトランザクションの抽象。
//
// 機能ごとのドキュメントを、1つのトランザクションの中で読み書きできるようにする（課題の登録と報酬の付与、
// Idempotency-Key の記録を同じトランザクションで確定する、など）。実装（Firestore・メモリ）は adapter にある。
//
// トランザクションの約束:
//   - 書き込みはコミットまで溜めておき、最後にまとめて反映する（Firestore は「読み取りをすべて書き込みより前に行う」必要があるため）。
//   - 同じトランザクションの中で書いたドキュメントを読むと、書いた内容が返る。
//   - 書き込みを溜めているコレクションを Query すると、実装の結果に溜めた書き込みを重ねて返す。
//   - fn は競合で何度か呼ばれることがある。fn の中では外部に副作用を起こさず、確定後の処理は AfterCommit に登録する。
//   - 状態を変えたリソースは Touch で版を上げる。版は users/{uid}/state/meta にまとめて持ち、変更通知（SSE）の元になる。
//
// 要求ごとのトランザクション（Request）:
//   - HTTP の状態を変える要求は Runner.Request の中で処理する。その間の Update はすべて同じトランザクションに加わり
//     （書き込みは溜まり、失敗した Update の分だけ取り消す）、要求が終わったときにまとめて確定する。
//     Idempotency-Key の応答を、状態の変更と同じトランザクションに記録するため（platform/idempotency）。
//   - View は、溜めた書き込みがあるときだけ同じトランザクションで読む（書いた内容が見えるように）。無ければ別の読み取り専用の
//     トランザクションで読み、AI の呼び出しなど時間のかかる処理の間にドキュメントをロックしない。
//   - 溜めた書き込みのあるコレクションの Query は、実装の結果に溜めた書き込みを重ねて返す。
//   - 要求と関係なく、その場で確定したい書き込み（AI の利用量など）は Detach した context で Update する。
package store

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
)

// Resource は版と変更通知の単位。仕様の Resource と同じ値。
type Resource string

const (
	ResourceProfile     Resource = "profile"
	ResourceSettings    Resource = "settings"
	ResourceTasks       Resource = "tasks"
	ResourceWorkSession Resource = "workSession"
	ResourceWallet      Resource = "wallet"
	ResourceViewing     Resource = "viewing"
	ResourceSleep       Resource = "sleep"
	ResourceShortcut    Resource = "shortcut"
	ResourceAssistant   Resource = "assistant"
	ResourceCalendar    Resource = "calendar"
)

// Resources はすべてのリソース。SSE の接続直後に、この順で現在の版を送る。
var Resources = []Resource{
	ResourceProfile, ResourceSettings, ResourceTasks, ResourceWorkSession, ResourceWallet,
	ResourceViewing, ResourceSleep, ResourceShortcut, ResourceAssistant, ResourceCalendar,
}

// Ref はドキュメントの場所。Collection はコレクションの完全なパス（例 users/u1/ledger）。
type Ref struct {
	Collection string
	ID         string
}

// Path はドキュメントの完全なパス。
func (r Ref) Path() string { return r.Collection + "/" + r.ID }

// UserCollection は利用者ごとのコレクション。
func UserCollection(uid, name string) string { return "users/" + uid + "/" + name }

// UserDoc は利用者ごとの単独のドキュメント（profile、wallet など）。
// Firestore のパスはコレクションとドキュメントが交互に並ぶため、users/{uid}/state/{name} に置く。
func UserDoc(uid, name string) Ref { return Ref{Collection: UserCollection(uid, "state"), ID: name} }

// UserItem は利用者ごとのコレクションの中のドキュメント（tasks/{id}、ledger/{id} など）。
func UserItem(uid, collection, id string) Ref {
	return Ref{Collection: UserCollection(uid, collection), ID: id}
}

// GlobalDoc は利用者に属さないドキュメント（AI の利用量、カレンダーのチャンネルなど）。
func GlobalDoc(collection, id string) Ref { return Ref{Collection: collection, ID: id} }

// MetaRef は版をまとめたドキュメント。
func MetaRef(uid string) Ref { return UserDoc(uid, "meta") }

// Meta はリソースごとの版。
type Meta struct {
	Revisions map[string]int64 `json:"revisions" firestore:"revisions"`
}

// Revision はリソースの版（無ければ0）。
func (m Meta) Revision(r Resource) int64 { return m.Revisions[string(r)] }

// Op は Query の比較演算子。
type Op string

const (
	Eq Op = "=="
	Lt Op = "<"
	Le Op = "<="
	Gt Op = ">"
	Ge Op = ">="
)

// Filter は1つの項目の条件。Value は string、bool、int、int64、time.Time のどれか。
type Filter struct {
	Field string
	Op    Op
	Value any
}

// Query はコレクションの検索。Firestore の複合インデックスを使わないよう、並べ替えと範囲の条件は同じ項目にする。
type Query struct {
	Collection string
	Filters    []Filter
	OrderBy    string
	Desc       bool
	Limit      int
}

// RunOptions はトランザクションの実行方法。
type RunOptions struct {
	ReadOnly bool
	// Scope は要求ごとのトランザクション（Runner.Request）の範囲（利用者の ID）。空なら1つの Update・View。
	// 実装は fn の間ロックを持たず、競合したら fn を呼び直す。メモリ実装は同じ Scope の要求を直列に実行する。
	Scope string
}

// Backend はトランザクションを実行する実装（Firestore・メモリ）。
type Backend interface {
	Run(ctx context.Context, opts RunOptions, fn func(ctx context.Context, tx BackendTx) error) error
}

// BackendTx は実装側のトランザクション。Set と Delete はコミットの直前にだけ呼ばれる。
type BackendTx interface {
	Get(ctx context.Context, ref Ref, dst any) (bool, error)
	Query(ctx context.Context, q Query, each func(ref Ref, decode func(dst any) error) error) error
	Set(ref Ref, value any) error
	Delete(ref Ref) error
}

// ErrReadOnly は、読み取り専用のトランザクションで書こうとしたときのエラー。
var ErrReadOnly = errors.New("store: write in a read-only transaction")

// ErrConflict は、要求ごとのトランザクションが読んだ内容が確定までに変わり、やり直しても解決しなかったときのエラー。
var ErrConflict = errors.New("store: transaction conflict")

type cached struct {
	found bool
	data  []byte
}

type write struct {
	ref     Ref
	value   any
	data    []byte
	deleted bool
}

// Tx はサービスが使うトランザクション。
type Tx struct {
	backend  BackendTx
	readOnly bool
	reads    map[string]cached
	writes   map[string]*write
	order    []string
	touched  map[string]map[Resource]bool
	after    []func()
}

func newTx(b BackendTx, readOnly bool) *Tx {
	return &Tx{backend: b, readOnly: readOnly, reads: map[string]cached{}, writes: map[string]*write{}, touched: map[string]map[Resource]bool{}}
}

// Get はドキュメントを dst に読み込む。無ければ false を返し、dst は変えない。
func (t *Tx) Get(ctx context.Context, ref Ref, dst any) (bool, error) {
	path := ref.Path()
	if w, ok := t.writes[path]; ok {
		if w.deleted {
			return false, nil
		}
		return true, decodeJSON(w.data, dst)
	}
	if c, ok := t.reads[path]; ok {
		if !c.found {
			return false, nil
		}
		return true, decodeJSON(c.data, dst)
	}
	found, err := t.backend.Get(ctx, ref, dst)
	if err != nil {
		return false, fmt.Errorf("store: get %s: %w", ref.Collection, err)
	}
	c := cached{found: found}
	if found {
		if c.data, err = json.Marshal(dst); err != nil {
			return false, fmt.Errorf("store: cache %s: %w", ref.Collection, err)
		}
	}
	t.reads[path] = c
	return found, nil
}

// Set はドキュメントを丸ごと置き換える（コミットで反映する）。
func (t *Tx) Set(ref Ref, value any) error {
	if t.readOnly {
		return ErrReadOnly
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("store: encode %s: %w", ref.Collection, err)
	}
	t.put(&write{ref: ref, value: value, data: data})
	return nil
}

// Delete はドキュメントを消す（コミットで反映する）。
func (t *Tx) Delete(ref Ref) error {
	if t.readOnly {
		return ErrReadOnly
	}
	t.put(&write{ref: ref, deleted: true})
	return nil
}

func (t *Tx) put(w *write) {
	path := w.ref.Path()
	if _, ok := t.writes[path]; !ok {
		t.order = append(t.order, path)
	}
	t.writes[path] = w
}

// Query はコレクションを検索し、各ドキュメントを each に渡す。溜めた書き込みのあるコレクションでは、
// 実装の結果に溜めた書き込み（置き換え・削除）を重ね、並べ替えと件数の上限を改めて適用する。
func (t *Tx) Query(ctx context.Context, q Query, each func(ref Ref, decode func(dst any) error) error) error {
	pending := map[string]*write{}
	for path, w := range t.writes {
		if w.ref.Collection == q.Collection {
			pending[path] = w
		}
	}
	if len(pending) == 0 {
		return t.queryBackend(ctx, q, each)
	}
	return t.queryMerged(ctx, q, pending, each)
}

func (t *Tx) queryBackend(ctx context.Context, q Query, each func(ref Ref, decode func(dst any) error) error) error {
	err := t.backend.Query(ctx, q, func(ref Ref, decode func(dst any) error) error {
		return each(ref, func(dst any) error {
			if err := decode(dst); err != nil {
				return err
			}
			data, err := json.Marshal(dst)
			if err != nil {
				return fmt.Errorf("store: cache %s: %w", ref.Collection, err)
			}
			t.reads[ref.Path()] = cached{found: true, data: data}
			return nil
		})
	})
	if err != nil {
		return fmt.Errorf("store: query %s: %w", q.Collection, err)
	}
	return nil
}

type queryRow struct {
	ref    Ref
	data   []byte
	fields map[string]any
}

func (t *Tx) queryMerged(ctx context.Context, q Query, pending map[string]*write, each func(ref Ref, decode func(dst any) error) error) error {
	var rows []queryRow
	unlimited := q
	unlimited.Limit = 0
	err := t.backend.Query(ctx, unlimited, func(ref Ref, decode func(dst any) error) error {
		if _, overridden := pending[ref.Path()]; overridden {
			return nil
		}
		var fields map[string]any
		if err := decode(&fields); err != nil {
			return err
		}
		data, err := json.Marshal(fields)
		if err != nil {
			return fmt.Errorf("store: cache %s: %w", ref.Collection, err)
		}
		if err := json.Unmarshal(data, &fields); err != nil {
			return fmt.Errorf("store: decode %s: %w", ref.Collection, err)
		}
		t.reads[ref.Path()] = cached{found: true, data: data}
		rows = append(rows, queryRow{ref: ref, data: data, fields: fields})
		return nil
	})
	if err != nil {
		return fmt.Errorf("store: query %s: %w", q.Collection, err)
	}
	for _, w := range pending {
		if w.deleted {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal(w.data, &fields); err != nil {
			return fmt.Errorf("store: decode %s: %w", w.ref.Collection, err)
		}
		if _, ordered := fields[q.OrderBy]; (q.OrderBy != "" && !ordered) || !Matches(fields, q.Filters) {
			continue
		}
		rows = append(rows, queryRow{ref: w.ref, data: w.data, fields: fields})
	}
	slices.SortFunc(rows, func(a, b queryRow) int {
		if q.OrderBy != "" {
			if c := Compare(a.fields[q.OrderBy], b.fields[q.OrderBy]); c != 0 {
				if q.Desc {
					return -c
				}
				return c
			}
		}
		return cmp.Compare(a.ref.ID, b.ref.ID)
	})
	if q.Limit > 0 && len(rows) > q.Limit {
		rows = rows[:q.Limit]
	}
	for _, r := range rows {
		if err := each(r.ref, func(dst any) error { return decodeJSON(r.data, dst) }); err != nil {
			return err
		}
	}
	return nil
}

// Matches は JSON に変換したドキュメントが条件に合うか（項目が無ければ合わない）。メモリ実装と溜めた書き込みの検索で使う。
func Matches(fields map[string]any, filters []Filter) bool {
	for _, f := range filters {
		c, ok := compareFilter(fields[f.Field], f.Value)
		if !ok {
			return false
		}
		switch f.Op {
		case Eq:
			if c != 0 {
				return false
			}
		case Lt:
			if c >= 0 {
				return false
			}
		case Le:
			if c > 0 {
				return false
			}
		case Gt:
			if c <= 0 {
				return false
			}
		case Ge:
			if c < 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
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

// Compare は JSON の値どうしを比べる（並べ替え用）。日時の文字列は時刻として比べる。
func Compare(a, b any) int {
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

// HasWrites は書き込みを溜めているか。
func (t *Tx) HasWrites() bool { return len(t.writes) > 0 }

// Touched は、このトランザクションで利用者のリソースの版を上げたか。
func (t *Tx) Touched(uid string) bool { return len(t.touched[uid]) > 0 }

// Touch はリソースの版を上げ、新しい版を返す。同じトランザクションで何度呼んでも、上がるのは1回だけ。
func (t *Tx) Touch(ctx context.Context, uid string, r Resource) (int64, error) {
	meta, err := t.meta(ctx, uid)
	if err != nil {
		return 0, err
	}
	if t.touched[uid] == nil {
		t.touched[uid] = map[Resource]bool{}
	}
	if !t.touched[uid][r] {
		t.touched[uid][r] = true
		meta.Revisions[string(r)]++
		if err := t.Set(MetaRef(uid), meta); err != nil {
			return 0, err
		}
	}
	return meta.Revisions[string(r)], nil
}

// Revision はリソースの今の版（このトランザクションで上げた分を含む）。
func (t *Tx) Revision(ctx context.Context, uid string, r Resource) (int64, error) {
	meta, err := t.meta(ctx, uid)
	if err != nil {
		return 0, err
	}
	return meta.Revision(r), nil
}

// Meta はすべてのリソースの版。
func (t *Tx) Meta(ctx context.Context, uid string) (Meta, error) { return t.meta(ctx, uid) }

func (t *Tx) meta(ctx context.Context, uid string) (Meta, error) {
	var meta Meta
	if _, err := t.Get(ctx, MetaRef(uid), &meta); err != nil {
		return Meta{}, err
	}
	if meta.Revisions == nil {
		meta.Revisions = map[string]int64{}
	}
	return meta, nil
}

// AfterCommit は確定した後に実行する処理を登録する（外部への通知など）。失敗してもトランザクションには影響しない。
func (t *Tx) AfterCommit(fn func()) { t.after = append(t.after, fn) }

// savepoint は要求ごとのトランザクションで、1つの Update の分だけ取り消すための控え。
type savepoint struct {
	writes  map[string]*write
	order   []string
	touched map[string]map[Resource]bool
	after   int
}

func (t *Tx) save() savepoint {
	touched := make(map[string]map[Resource]bool, len(t.touched))
	for uid, rs := range t.touched {
		touched[uid] = maps.Clone(rs)
	}
	return savepoint{writes: maps.Clone(t.writes), order: slices.Clone(t.order), touched: touched, after: len(t.after)}
}

func (t *Tx) restore(sp savepoint) {
	t.writes, t.order, t.touched, t.after = sp.writes, sp.order, sp.touched, t.after[:sp.after]
}

// view は同じトランザクションを読み取り専用として使う。
func (t *Tx) view() *Tx {
	return &Tx{backend: t.backend, readOnly: true, reads: t.reads, writes: t.writes, touched: t.touched}
}

func (t *Tx) flush() error {
	for _, path := range t.order {
		w := t.writes[path]
		var err error
		if w.deleted {
			err = t.backend.Delete(w.ref)
		} else {
			err = t.backend.Set(w.ref, w.value)
		}
		if err != nil {
			return fmt.Errorf("store: write %s: %w", w.ref.Collection, err)
		}
	}
	return nil
}

// Hook はコミットの直前に、書き込みがあるトランザクションで実行する処理（Idempotency-Key の記録など）。
type Hook func(ctx context.Context, tx *Tx) error

// Runner はトランザクションを実行する。
type Runner struct {
	backend Backend
	hooks   []Hook
}

// NewRunner は Runner を作る。
func NewRunner(b Backend) *Runner { return &Runner{backend: b} }

// AddHook はコミット直前の処理を加える。起動時にだけ呼ぶ。
func (r *Runner) AddHook(h Hook) { r.hooks = append(r.hooks, h) }

type ambientKey struct{}

// ambient は context が属する要求ごとのトランザクション（無ければ nil）。
func ambient(ctx context.Context) *Tx {
	tx, _ := ctx.Value(ambientKey{}).(*Tx)
	return tx
}

// Detach は要求ごとのトランザクションから切り離した context を返す。その context の Update はその場で確定する。
func Detach(ctx context.Context) context.Context {
	return context.WithValue(ctx, ambientKey{}, (*Tx)(nil))
}

// Request は要求ごとのトランザクションを実行する。fn の中（fn が渡す ctx から派生した context）で呼んだ Update は
// すべてこのトランザクションに加わり、fn が nil を返したときにまとめて確定する。fn がエラーを返すと何も書き込まない。
// scope は実装が同じ要求を直列に実行するための鍵（利用者の ID）。
func (r *Runner) Request(ctx context.Context, scope string, fn func(ctx context.Context, tx *Tx) error) error {
	return r.run(ctx, RunOptions{Scope: scope}, func(ctx context.Context, tx *Tx) error {
		return fn(context.WithValue(ctx, ambientKey{}, tx), tx)
	})
}

// Update は読み書きするトランザクションを実行する。fn がエラーを返すと何も書き込まない。
// 要求ごとのトランザクションの中では、それに加わる（fn の書き込みは要求の終わりに確定する）。
func (r *Runner) Update(ctx context.Context, fn func(ctx context.Context, tx *Tx) error) error {
	if amb := ambient(ctx); amb != nil {
		sp := amb.save()
		if err := fn(ctx, amb); err != nil {
			amb.restore(sp)
			return err //nolint:wrapcheck // fn のエラー（apperr）をそのまま返す。
		}
		return nil
	}
	return r.run(ctx, RunOptions{}, fn)
}

func (r *Runner) run(ctx context.Context, opts RunOptions, fn func(ctx context.Context, tx *Tx) error) error {
	var committed *Tx
	err := r.backend.Run(ctx, opts, func(ctx context.Context, b BackendTx) error {
		tx := newTx(b, false)
		if err := fn(ctx, tx); err != nil {
			return err
		}
		if tx.HasWrites() {
			for _, h := range r.hooks {
				if err := h(ctx, tx); err != nil {
					return err
				}
			}
		}
		if err := tx.checkAccounts(ctx); err != nil {
			return err
		}
		if err := tx.flush(); err != nil {
			return err
		}
		committed = tx
		return nil
	})
	if err != nil {
		return err //nolint:wrapcheck // fn のエラー（apperr）をそのまま返す。
	}
	for _, f := range committed.after {
		f()
	}
	return nil
}

// View は読み取り専用のトランザクションを実行する。要求ごとのトランザクションに溜めた書き込みがあれば、それを通して読む。
func (r *Runner) View(ctx context.Context, fn func(ctx context.Context, tx *Tx) error) error {
	if amb := ambient(ctx); amb != nil && amb.HasWrites() {
		return fn(ctx, amb.view()) //nolint:wrapcheck // fn のエラーをそのまま返す。
	}
	return r.backend.Run(ctx, RunOptions{ReadOnly: true}, func(ctx context.Context, b BackendTx) error { //nolint:wrapcheck // fn のエラーをそのまま返す。
		return fn(ctx, newTx(b, true))
	})
}

// Get はドキュメントを型つきで読む。
func Get[T any](ctx context.Context, tx *Tx, ref Ref) (T, bool, error) {
	var v T
	ok, err := tx.Get(ctx, ref, &v)
	return v, ok, err
}

// List は検索結果を型つきで返す。
func List[T any](ctx context.Context, tx *Tx, q Query) ([]T, error) {
	var out []T
	err := tx.Query(ctx, q, func(_ Ref, decode func(dst any) error) error {
		var v T
		if err := decode(&v); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	})
	return out, err
}

func decodeJSON(data []byte, dst any) error {
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("store: decode: %w", err)
	}
	return nil
}

// ValidUID は利用者IDとして Firestore のパスに使えるか（"/" を含まず、"." や "__…__" でない）。
func ValidUID(uid string) bool {
	if uid == "" || len(uid) > 128 || strings.Contains(uid, "/") || uid == "." || uid == ".." {
		return false
	}
	return !strings.HasPrefix(uid, "__") || !strings.HasSuffix(uid, "__")
}

// ExpiresAt は TTL で消すドキュメントの期限を表す項目名。Firestore の TTL ポリシーをこの項目に設定する。
const ExpiresAt = "expiresAt"

// Truncate は Firestore に保存できる精度（マイクロ秒）に時刻を丸める。メモリ実装と結果を揃えるため。
func Truncate(t time.Time) time.Time { return t.UTC().Truncate(time.Microsecond) }

// Expect は、リソースの今の版が want と同じか確かめる。違えば 412 precondition.failed。
func (t *Tx) Expect(ctx context.Context, uid string, r Resource, want int64) error {
	got, err := t.Revision(ctx, uid, r)
	if err != nil {
		return err
	}
	if got != want {
		return apperr.PreconditionFailed("precondition.failed")
	}
	return nil
}
