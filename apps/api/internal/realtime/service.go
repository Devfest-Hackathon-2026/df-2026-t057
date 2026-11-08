package realtime

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

// firstSnapshotTimeout は最初の版を待つ時間。
const firstSnapshotTimeout = 10 * time.Second

// subscriberBuffer は1つの接続に溜める通知の数。溢れたら最新の版だけを送り直す。
const subscriberBuffer = 32

type feed struct {
	cancel context.CancelFunc
	ready  chan struct{}
	latest map[store.Resource]int64
	subs   map[int]chan []Change
}

// Hub は利用者ごとに1つの監視を、そのインスタンスの接続で共有する。
type Hub struct {
	watcher Watcher
	log     *slog.Logger
	mu      sync.Mutex
	feeds   map[string]*feed
	nextID  int
}

// NewHub は Hub を作る。
func NewHub(w Watcher, log *slog.Logger) *Hub {
	return &Hub{watcher: w, log: log, feeds: map[string]*feed{}}
}

// Subscribe は購読を始め、今の版と、以後の変更を受け取るチャネルを返す。cancel で購読をやめる。
func (h *Hub) Subscribe(ctx context.Context, uid string) (initial []Change, updates <-chan []Change, cancel func(), err error) {
	h.mu.Lock()
	f, ok := h.feeds[uid]
	if !ok {
		watchCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
		f = &feed{cancel: stop, ready: make(chan struct{}), latest: map[store.Resource]int64{}, subs: map[int]chan []Change{}}
		h.feeds[uid] = f
		go h.watch(watchCtx, uid, f)
	}
	h.nextID++
	id := h.nextID
	ch := make(chan []Change, subscriberBuffer)
	f.subs[id] = ch
	h.mu.Unlock()

	cancel = func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(f.subs, id)
		if len(f.subs) == 0 && h.feeds[uid] == f {
			delete(h.feeds, uid)
			f.cancel()
		}
	}
	select {
	case <-f.ready:
	case <-time.After(firstSnapshotTimeout):
	case <-ctx.Done():
		cancel()
		return nil, nil, nil, ctx.Err() //nolint:wrapcheck // 接続が切れたことをそのまま返す。
	}
	h.mu.Lock()
	initial = Initial(maps.Clone(f.latest))
	h.mu.Unlock()
	return initial, ch, cancel, nil
}

func (h *Hub) watch(ctx context.Context, uid string, f *feed) {
	var once sync.Once
	first := true
	err := h.watcher.WatchMeta(ctx, uid, func(m store.Meta) {
		h.mu.Lock()
		changes := Diff(f.latest, m)
		for _, c := range changes {
			f.latest[c.Resource] = c.Revision
		}
		if first {
			// 最初の値は、購読を始めたときに Initial として送る。
			first, changes = false, nil
		}
		for _, ch := range f.subs {
			if len(changes) == 0 {
				break
			}
			select {
			case ch <- changes:
			default:
				// 遅い接続には、溜まった通知を捨てて最新の版をまとめて送る。
				for len(ch) > 0 {
					<-ch
				}
				ch <- Initial(maps.Clone(f.latest))
			}
		}
		h.mu.Unlock()
		once.Do(func() { close(f.ready) })
	})
	once.Do(func() { close(f.ready) })
	if ctx.Err() != nil {
		return
	}
	// 監視が止まったら接続を閉じる。クライアントはつなぎ直して版を比べ、取り直す。
	h.log.ErrorContext(ctx, "realtime watch stopped", "error_type", fmt.Sprintf("%T", err))
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.feeds[uid] == f {
		delete(h.feeds, uid)
	}
	for id, ch := range f.subs {
		close(ch)
		delete(f.subs, id)
	}
	f.cancel()
}

// Close はすべての購読を閉じる（サーバーの終了時）。
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for uid, f := range h.feeds {
		for id, ch := range f.subs {
			close(ch)
			delete(f.subs, id)
		}
		f.cancel()
		delete(h.feeds, uid)
	}
}
