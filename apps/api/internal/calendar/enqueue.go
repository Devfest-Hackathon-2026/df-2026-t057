package calendar

import (
	"context"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/outbox"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/profile"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
)

// Enqueuer と Disabled は、課題・睡眠・本人の情報の機能が宣言したポートを実装する。
var (
	_ task.Calendar        = (*Enqueuer)(nil)
	_ sleep.CalendarSync   = (*Enqueuer)(nil)
	_ profile.CalendarSync = (*Enqueuer)(nil)
	_ task.Calendar        = Disabled{}
	_ sleep.CalendarSync   = Disabled{}
	_ profile.CalendarSync = Disabled{}
)

const (
	linksCollectionName = "calendarLinks"
	channelsCollection  = "calendarChannels"
	oauthStates         = "oauthStates"
)

func connectionRef(uid string) store.Ref    { return store.UserDoc(uid, "calendar") }
func linksCollection(uid string) string     { return store.UserCollection(uid, linksCollectionName) }
func linkRef(uid, taskID string) store.Ref  { return store.UserItem(uid, linksCollectionName, taskID) }
func channelRef(channelID string) store.Ref { return store.GlobalDoc(channelsCollection, channelID) }
func oauthStateRef(state string) store.Ref  { return store.GlobalDoc(oauthStates, state) }
func jobRef(uid, jobID string) store.Ref    { return store.UserItem(uid, outbox.Collection, jobID) }
func loadConnection(ctx context.Context, tx *store.Tx, uid string) (Connection, error) {
	c, _, err := store.Get[Connection](ctx, tx, connectionRef(uid))
	return c, err
}

// saveConnection は連携の状態を書き、版を上げる。
func saveConnection(ctx context.Context, tx *store.Tx, uid string, c Connection) error {
	if err := tx.Set(connectionRef(uid), c); err != nil {
		return err
	}
	_, err := tx.Touch(ctx, uid, store.ResourceCalendar)
	return err
}

// jobPending はジョブがまだ実行されていないか。Outbox のコレクションを検索せずに1件だけ読む
// （同じトランザクションでジョブを書いた後でも読めるように）。
func jobPending(ctx context.Context, tx *store.Tx, uid, jobID string) (bool, error) {
	if jobID == "" {
		return false, nil
	}
	job, found, err := store.Get[outbox.Job](ctx, tx, jobRef(uid, jobID))
	return found && job.Status == outbox.StatusPending, err
}

// enqueueOnce は、まだ始まっていない同じ種類のジョブが無ければジョブを積み、その ID を slot に入れる。
// 積んだら true（呼び出し側が連携の状態を保存する）。
func enqueueOnce(ctx context.Context, tx *store.Tx, ob *outbox.Outbox, uid string, kind outbox.Kind, slot *string) (bool, error) {
	pending, err := jobPending(ctx, tx, uid, *slot)
	if err != nil || pending {
		return false, err
	}
	job, err := ob.Enqueue(ctx, tx, uid, kind, "")
	if err != nil {
		return false, err
	}
	*slot = job.ID
	return true, nil
}

func linkedTasks(ctx context.Context, tx *store.Tx, uid string) (map[string]bool, error) {
	links, err := store.List[Link](ctx, tx, store.Query{Collection: linksCollection(uid)})
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(links))
	for _, l := range links {
		if l.EventID != "" {
			out[l.TaskID] = true
		}
	}
	return out, nil
}

// Enqueuer は課題・睡眠・本人の情報の変更を、呼び出し側のトランザクションの中で Outbox のジョブとして積む。
// 連携していなければ何もしない。task.Calendar・sleep.CalendarSync・profile.CalendarSync を実装する。
//
// calendarLinks には書かない（課題の機能が同じトランザクションで LinkedTasks を検索するため）。
type Enqueuer struct {
	outbox *outbox.Outbox
}

// NewEnqueuer は Enqueuer を作る。
func NewEnqueuer(ob *outbox.Outbox) *Enqueuer { return &Enqueuer{outbox: ob} }

// TaskContentChanged は課題の内容の連番を上げ、予定を作る・書き直すジョブを積む。
func (e *Enqueuer) TaskContentChanged(ctx context.Context, tx *store.Tx, uid, taskID string) error {
	c, err := loadConnection(ctx, tx, uid)
	if err != nil || !c.Connected {
		return err
	}
	if c.ContentSeq == nil {
		c.ContentSeq = map[string]int64{}
	}
	c.ContentSeq[taskID]++
	if _, err := e.outbox.Enqueue(ctx, tx, uid, outbox.KindTaskUpsert, taskID); err != nil {
		return err
	}
	return saveConnection(ctx, tx, uid, c)
}

// PlansChanged は計画の書き直しのジョブを積む。まだ始まっていない書き直しがあれば、それにまとめる。
func (e *Enqueuer) PlansChanged(ctx context.Context, tx *store.Tx, uid string) error {
	c, err := loadConnection(ctx, tx, uid)
	if err != nil || !c.Connected {
		return err
	}
	enqueued, err := enqueueOnce(ctx, tx, e.outbox, uid, outbox.KindExport, &c.ExportJob)
	if err != nil || !enqueued {
		return err
	}
	return saveConnection(ctx, tx, uid, c)
}

// TaskRemoved は予定を消すジョブを積む。
func (e *Enqueuer) TaskRemoved(ctx context.Context, tx *store.Tx, uid, taskID string) error {
	c, err := loadConnection(ctx, tx, uid)
	if err != nil || !c.Connected {
		return err
	}
	_, err = e.outbox.Enqueue(ctx, tx, uid, outbox.KindTaskDelete, taskID)
	return err
}

// LinkedTasks は予定と結び付いている課題の ID。
func (e *Enqueuer) LinkedTasks(ctx context.Context, tx *store.Tx, uid string) (map[string]bool, error) {
	c, err := loadConnection(ctx, tx, uid)
	if err != nil {
		return nil, err
	}
	if !c.Connected {
		return map[string]bool{}, nil
	}
	return linkedTasks(ctx, tx, uid)
}

func (e *Enqueuer) enqueueSleep(ctx context.Context, tx *store.Tx, uid string, kind outbox.Kind) error {
	c, err := loadConnection(ctx, tx, uid)
	if err != nil || !c.Connected {
		return err
	}
	job, err := e.outbox.Enqueue(ctx, tx, uid, kind, "")
	if err != nil {
		return err
	}
	c.SleepJob = job.ID
	return saveConnection(ctx, tx, uid, c)
}

// SleepChanged は就寝の予定を書き直すジョブを積む。
func (e *Enqueuer) SleepChanged(ctx context.Context, tx *store.Tx, uid string) error {
	return e.enqueueSleep(ctx, tx, uid, outbox.KindSleepUpsert)
}

// SleepCleared は就寝の予定を消すジョブを積む。
func (e *Enqueuer) SleepCleared(ctx context.Context, tx *store.Tx, uid string) error {
	return e.enqueueSleep(ctx, tx, uid, outbox.KindSleepDelete)
}

// SleepPending は、最後に積んだ就寝の予定のジョブがまだ実行されていないか。
func (e *Enqueuer) SleepPending(ctx context.Context, tx *store.Tx, uid string) (bool, error) {
	c, err := loadConnection(ctx, tx, uid)
	if err != nil || !c.Connected {
		return false, err
	}
	return jobPending(ctx, tx, uid, c.SleepJob)
}

// ProfileFormatChanged は、タイムゾーンか言語が変わったときに、就寝の予定と、予定の説明の時刻・見出しを書き直すジョブを積む。
// 就寝の予定のジョブは、睡眠を設定していなければ予定を消す（無ければ何もしない）。
func (e *Enqueuer) ProfileFormatChanged(ctx context.Context, tx *store.Tx, uid string) error {
	c, err := loadConnection(ctx, tx, uid)
	if err != nil || !c.Connected {
		return err
	}
	job, err := e.outbox.Enqueue(ctx, tx, uid, outbox.KindSleepUpsert, "")
	if err != nil {
		return err
	}
	c.SleepJob = job.ID
	if _, err := enqueueOnce(ctx, tx, e.outbox, uid, outbox.KindExport, &c.ExportJob); err != nil {
		return err
	}
	return saveConnection(ctx, tx, uid, c)
}

// Disabled はカレンダー連携を使わない環境（demo）の実装。何もしない。
type Disabled struct{}

func (Disabled) TaskContentChanged(context.Context, *store.Tx, string, string) error { return nil }
func (Disabled) PlansChanged(context.Context, *store.Tx, string) error               { return nil }
func (Disabled) TaskRemoved(context.Context, *store.Tx, string, string) error        { return nil }
func (Disabled) LinkedTasks(context.Context, *store.Tx, string) (map[string]bool, error) {
	return map[string]bool{}, nil
}
func (Disabled) SleepChanged(context.Context, *store.Tx, string) error { return nil }
func (Disabled) SleepCleared(context.Context, *store.Tx, string) error { return nil }
func (Disabled) SleepPending(context.Context, *store.Tx, string) (bool, error) {
	return false, nil
}
func (Disabled) ProfileFormatChanged(context.Context, *store.Tx, string) error { return nil }

// Info はトランザクションの中で連携の状態を返す（相談の get_calendar 用）。カレンダーを使わない環境では Disabled が答える。
func (e *Enqueuer) Info(ctx context.Context, tx *store.Tx, uid string) (connected bool, lastSync *time.Time, linked map[string]bool, err error) {
	c, err := loadConnection(ctx, tx, uid)
	if err != nil {
		return false, nil, nil, err
	}
	linked, err = e.LinkedTasks(ctx, tx, uid)
	return c.Connected, c.LastSync, linked, err
}

// Info は連携していない状態を返す。
func (Disabled) Info(context.Context, *store.Tx, string) (bool, *time.Time, map[string]bool, error) {
	return false, nil, map[string]bool{}, nil
}
