package realtime

import (
	"context"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

// Watcher は利用者の版のドキュメントを監視する。最初に今の値で fn を呼び、変わるたびに呼ぶ。ctx が終わるまで戻らない。
// live は Firestore の snapshot listener、demo はメモリ上の通知で実装する。
type Watcher interface {
	WatchMeta(ctx context.Context, uid string, fn func(store.Meta)) error
}
