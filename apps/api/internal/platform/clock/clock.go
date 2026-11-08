// Package clock は現在時刻の取得を抽象化する。ルールとテストが時刻に依存しないようにするため。
package clock

import (
	"sync"
	"time"
)

// Clock は現在時刻を返す。
type Clock interface {
	Now() time.Time
}

// System は実際の時刻を返す。
type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }

// Fake はテスト用に、任意に進められる時計。
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

func NewFake(now time.Time) *Fake { return &Fake{now: now.UTC()} }

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance は時計を d だけ進める。
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// Set は時計を t に合わせる。
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t.UTC()
}
