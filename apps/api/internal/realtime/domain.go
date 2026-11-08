// Package realtime は変更の通知（SSE）の元になる、リソースの版の購読を扱う。データ本体は送らない。
package realtime

import "github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"

// Change は1つのリソースの新しい版。
type Change struct {
	Resource store.Resource
	Revision int64
}

// Initial は接続した直後に送る、すべてのリソースの今の版（決まった順）。
func Initial(revisions map[store.Resource]int64) []Change {
	out := make([]Change, 0, len(store.Resources))
	for _, r := range store.Resources {
		out = append(out, Change{Resource: r, Revision: revisions[r]})
	}
	return out
}

// Diff は前の版から上がったリソースだけを返す。古い通知（版が下がる）は無視する。
func Diff(before map[store.Resource]int64, after store.Meta) []Change {
	var out []Change
	for _, r := range store.Resources {
		if v := after.Revision(r); v > before[r] {
			out = append(out, Change{Resource: r, Revision: v})
		}
	}
	return out
}
