package calendar

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/id"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/outbox"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
)

const (
	// stateTTL は同意画面の state の有効期間。
	stateTTL = 10 * time.Minute
	// channelTTL は変更通知のチャンネルの有効期間（Google の上限は7日）。
	channelTTL = 6 * 24 * time.Hour
	// renewWithin は、期限がこの時間以内のチャンネルを更新する。
	renewWithin = 24 * time.Hour
	// redispatchAfter は、登録からこの時間がたっても実行されないジョブを登録し直す。
	redispatchAfter = 10 * time.Minute
	// maxPages は1回の取り込みで読むページの上限（終わらない一覧で止まらないように）。
	maxPages = 200
	// webhookPath は変更通知を受ける API のパス。
	webhookPath = "/api/calendar/webhook"
	// resourceStateSync は、チャンネルを登録した直後の確認の通知。取り込みは要らない。
	resourceStateSync = "sync"
)

// Config はカレンダー連携の設定。
type Config struct {
	// Enabled はこの環境でカレンダー連携を使うか（live かつ OAuth の設定がある）。
	Enabled bool
	// AppURL は管理画面のオリジン。同意画面から戻る先と、変更通知の宛先に使う。
	AppURL string
}

// Deps は Service が使う外部の機能。Enabled が false のときは Google が nil でもよい。
type Deps struct {
	Tasks    Tasks
	Sleeps   Sleeps
	Profiles Profiles
	Planner  task.Planner
	Google   Google
	Log      *slog.Logger
}

// Service はカレンダー連携のユースケースと、Outbox のジョブの処理。
type Service struct {
	runner *store.Runner
	clock  clock.Clock
	ids    id.Generator
	outbox *outbox.Outbox
	cfg    Config
	Deps
}

// NewService は Service を作る。
func NewService(runner *store.Runner, c clock.Clock, ids id.Generator, ob *outbox.Outbox, cfg Config, deps Deps) *Service {
	return &Service{runner: runner, clock: c, ids: ids, outbox: ob, cfg: cfg, Deps: deps}
}

// Status は連携の状態。
type Status struct {
	Enabled    bool
	Connected  bool
	Sync       SyncState
	LastSync   *time.Time
	FailedJobs int
	Conflicts  int
}

// Status は連携の状態を返す。連携を使わない環境では enabled=false、disconnected。
func (s *Service) Status(ctx context.Context, uid string) (Status, error) {
	if !s.cfg.Enabled {
		return Status{Sync: SyncDisconnected}, nil
	}
	var st Status
	err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		c, err := loadConnection(ctx, tx, uid)
		if err != nil {
			return err
		}
		pending, failed, err := outbox.Counts(ctx, tx, uid)
		if err != nil {
			return err
		}
		conflicts, err := store.List[Conflict](ctx, tx, store.Query{Collection: store.UserCollection(uid, "calendarConflicts")})
		if err != nil {
			return err
		}
		st = Status{Conflicts: len(conflicts), Enabled: true, Connected: c.Connected, Sync: DeriveSync(c.Connected, pending, failed), LastSync: c.LastSync, FailedJobs: failed}
		if c.Reauth {
			st.Sync = SyncReauth
		} else if st.Conflicts > 0 {
			st.Sync = SyncConflict
		}
		return nil
	})
	return st, err
}

// Connect は同意画面の URL を返す。googleSubject はログイン中の Google アカウントの ID（ID トークンの sub）。
func (s *Service) Connect(ctx context.Context, uid, googleSubject, email string) (string, error) {
	if !s.cfg.Enabled {
		return "", apperr.Unavailable("calendar.unavailable")
	}
	if googleSubject == "" {
		return "", apperr.Forbidden("calendar.reauth_required")
	}
	state := s.ids.New()
	expires := store.Truncate(s.clock.Now().Add(stateTTL))
	err := s.runner.Update(ctx, func(_ context.Context, tx *store.Tx) error {
		return tx.Set(oauthStateRef(state), OAuthState{UID: uid, GoogleID: googleSubject, ExpiresAt: expires})
	})
	if err != nil {
		return "", err
	}
	return s.Google.AuthURL(state, email), nil
}

// Callback は同意画面から戻ったときの処理。戻る先の URL（成功なら /?calendar=connected、失敗なら /?calendar=error）を返す。
func (s *Service) Callback(ctx context.Context, state, code, errParam string) string {
	if !s.cfg.Enabled {
		return s.cfg.AppURL + "/?calendar=error"
	}
	if err := s.callback(ctx, state, code, errParam); err != nil {
		s.Log.WarnContext(ctx, "calendar connect failed", "code", apperr.As(err).Code)
		return s.cfg.AppURL + "/?calendar=error"
	}
	return s.cfg.AppURL + "/?calendar=connected"
}

func (s *Service) callback(ctx context.Context, state, code, errParam string) error {
	// ストレージを読む前に形式を確かめる。
	if errParam != "" || !ValidToken(state) || code == "" {
		return apperr.Invalid("request.invalid")
	}
	now := s.clock.Now()
	var pending OAuthState
	err := s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		var found bool
		var err error
		if pending, found, err = store.Get[OAuthState](ctx, tx, oauthStateRef(state)); err != nil {
			return err
		}
		if !found || !pending.ExpiresAt.After(now) {
			return apperr.Forbidden("not_found")
		}
		return tx.Delete(oauthStateRef(state))
	})
	if err != nil {
		return err
	}
	grant, err := s.Google.Exchange(ctx, code)
	if err != nil {
		return err
	}
	if pending.GoogleID == "" || grant.Subject != pending.GoogleID || !grant.HasRefreshToken {
		return apperr.Forbidden("calendar.reauth_required")
	}
	uid := pending.UID
	if err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error { return store.EnsureAccountOpen(ctx, tx, uid) }); err != nil {
		return err
	}
	if err := s.Google.SaveToken(ctx, uid, grant.Token); err != nil {
		return err
	}
	client, err := s.Google.ClientFor(ctx, grant.Token)
	if err != nil {
		return err
	}
	var current Connection
	var zone string
	err = s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		if current, err = loadConnection(ctx, tx, uid); err != nil {
			return err
		}
		loc, err := s.Profiles.Location(ctx, tx, uid)
		zone = loc.String()
		return err
	})
	if err != nil {
		return err
	}
	calendarID, replaced := current.CalendarID, false
	if calendarID == "" {
		if calendarID, err = client.CreateCalendar(ctx, calendarSummary, calendarDescription, zone); err != nil {
			return err
		}
		replaced = true
	}
	ch, watch, err := s.startWatch(ctx, client, uid, calendarID, grant.Subject, replaced, now)
	if errors.Is(err, ErrEventGone) && !replaced {
		// 本人が DOPAGAKI のカレンダーを消していた。作り直して、予定の結び付きもやり直す。
		if calendarID, err = client.CreateCalendar(ctx, calendarSummary, calendarDescription, zone); err != nil {
			return err
		}
		replaced = true
		ch, watch, err = s.startWatch(ctx, client, uid, calendarID, grant.Subject, replaced, now)
	}
	if err != nil {
		return err
	}
	old, err := s.finishConnect(ctx, uid, ch, watch, replaced)
	if err != nil {
		return err
	}
	if old.ChannelID != "" && old.ChannelID != ch.id {
		s.stopChannel(ctx, client, old.ChannelID, old.ChannelResource)
	}
	return nil
}

type newChannel struct {
	id, token string
	expires   time.Time
}

// startWatch は変更通知のチャンネルを登録する。チャンネルの記録を先に書いてから Google に登録する
// （登録の直後に届く通知を受け付けられるように）。カレンダーの ID も先に保存し、失敗しても作り直さないようにする。
func (s *Service) startWatch(ctx context.Context, client Client, uid, calendarID, googleID string, replaced bool, now time.Time) (newChannel, Watch, error) {
	ch := newChannel{id: s.ids.New(), token: s.ids.New(), expires: store.Truncate(now.Add(channelTTL))}
	err := s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		c, err := loadConnection(ctx, tx, uid)
		if err != nil {
			return err
		}
		if err := tx.Set(channelRef(ch.id), Channel{UID: uid, Token: ch.token, Expires: ch.expires}); err != nil {
			return err
		}
		if replaced || c.CalendarID != calendarID {
			c.SyncToken = ""
		}
		c.CalendarID, c.GoogleID = calendarID, googleID
		return saveConnection(ctx, tx, uid, c)
	})
	if err != nil {
		return newChannel{}, Watch{}, err
	}
	w, err := client.Watch(ctx, calendarID, WatchRequest{ChannelID: ch.id, Token: ch.token, Address: s.cfg.AppURL + webhookPath, Expires: ch.expires})
	if err != nil {
		s.deleteChannel(ctx, ch.id)
		return newChannel{}, Watch{}, err
	}
	if w.Expires.IsZero() {
		w.Expires = ch.expires
	}
	return ch, w, nil
}

// finishConnect は連携を確定し、最初の取り込みと書き直し、まだ予定の無い課題の予定づくりを積む。
// 同じカレンダーにつなぎ直したときは syncToken を残す。前のチャンネルを返す（呼び出し側が止める）。
func (s *Service) finishConnect(ctx context.Context, uid string, ch newChannel, w Watch, replaced bool) (Connection, error) {
	var old Connection
	err := s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		c, err := loadConnection(ctx, tx, uid)
		if err != nil {
			return err
		}
		old = c
		tasks, err := s.Tasks.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		linked, err := linkedTasks(ctx, tx, uid)
		if err != nil {
			return err
		}
		if replaced {
			// 前のカレンダーの予定との結び付きは使えない。
			for taskID := range linked {
				if err := tx.Delete(linkRef(uid, taskID)); err != nil {
					return err
				}
			}
			linked = map[string]bool{}
		}
		expires := store.Truncate(w.Expires)
		c.Connected = true
		c.Reauth = false
		c.ChannelID, c.ChannelToken, c.ChannelResource, c.ChannelExpires = ch.id, ch.token, w.ResourceID, &expires
		if _, err := enqueueOnce(ctx, tx, s.outbox, uid, outbox.KindSync, &c.SyncJob); err != nil {
			return err
		}
		if _, err := enqueueOnce(ctx, tx, s.outbox, uid, outbox.KindExport, &c.ExportJob); err != nil {
			return err
		}
		// 連携の前に登録した課題にも予定を作る。
		for _, t := range tasks {
			if t.Status != task.StatusCompleted && !linked[t.ID] {
				if _, err := s.outbox.Enqueue(ctx, tx, uid, outbox.KindTaskUpsert, t.ID); err != nil {
					return err
				}
			}
		}
		if old.ChannelID != "" && old.ChannelID != ch.id {
			if err := tx.Delete(channelRef(old.ChannelID)); err != nil {
				return err
			}
		}
		if _, err := tx.Touch(ctx, uid, store.ResourceTasks); err != nil {
			return err
		}
		return saveConnection(ctx, tx, uid, c)
	})
	return old, err
}

func (s *Service) deleteChannel(ctx context.Context, channelID string) {
	err := s.runner.Update(ctx, func(_ context.Context, tx *store.Tx) error { return tx.Delete(channelRef(channelID)) })
	if err != nil {
		s.Log.WarnContext(ctx, "calendar channel cleanup failed", "code", apperr.As(err).Code)
	}
}

// stopChannel は使わなくなったチャンネルを止める。失敗しても期限で止まるので、記録だけ残す。
func (s *Service) stopChannel(ctx context.Context, client Client, channelID, resourceID string) {
	if resourceID == "" {
		return
	}
	if err := client.StopChannel(ctx, channelID, resourceID); err != nil && !errors.Is(err, ErrEventGone) {
		s.Log.WarnContext(ctx, "calendar channel stop failed", "code", apperr.As(err).Code)
	}
}

// SyncNow は取り込みのジョブを積み、連携の状態を返す（その場では取り込まない）。
func (s *Service) SyncNow(ctx context.Context, uid string) (Status, error) {
	if !s.cfg.Enabled {
		return Status{}, apperr.Unavailable("calendar.unavailable")
	}
	err := s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		c, err := loadConnection(ctx, tx, uid)
		if err != nil {
			return err
		}
		if !c.Connected {
			return apperr.Conflict("calendar.not_connected")
		}
		enqueued, err := enqueueOnce(ctx, tx, s.outbox, uid, outbox.KindSync, &c.SyncJob)
		if err != nil || !enqueued {
			return err
		}
		return saveConnection(ctx, tx, uid, c)
	})
	if err != nil {
		return Status{}, err
	}
	return s.Status(ctx, uid)
}

// Disconnect は連携をやめる。連携の状態・チャンネルの記録・課題と予定の結び付きを消し、反映待ちと失敗のジョブを取り消し、
// 課題の一覧の calendarLinked が変わるので課題の版も上げる。確定した後に、チャンネルを止めてトークンを破棄する
// （外部の失敗で解除を止めない。チャンネルは期限で止まり、トークンはつなぎ直すと置き換わる）。
// 専用のカレンダーは消さず、ID を残してつなぎ直したときに使う。連携していなければ何もせず、今の状態を返す。
func (s *Service) Disconnect(ctx context.Context, uid string) (Status, error) {
	if !s.cfg.Enabled {
		return Status{}, apperr.Unavailable("calendar.unavailable")
	}
	err := s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		c, err := loadConnection(ctx, tx, uid)
		if err != nil || !c.Connected {
			return err
		}
		links, err := store.List[Link](ctx, tx, store.Query{Collection: linksCollection(uid)})
		if err != nil {
			return err
		}
		if _, err := s.outbox.Cancel(ctx, tx, uid); err != nil {
			return err
		}
		for _, l := range links {
			if err := tx.Delete(linkRef(uid, l.TaskID)); err != nil {
				return err
			}
		}
		if c.ChannelID != "" {
			if err := tx.Delete(channelRef(c.ChannelID)); err != nil {
				return err
			}
		}
		if _, err := tx.Touch(ctx, uid, store.ResourceTasks); err != nil {
			return err
		}
		old := c
		tx.AfterCommit(func() { s.teardown(context.WithoutCancel(ctx), uid, old) })
		// つなぎ直したときに予定を結び付け直せるよう、カレンダーの ID と Google アカウントだけ残す。
		return saveConnection(ctx, tx, uid, Connection{CalendarID: c.CalendarID, GoogleID: c.GoogleID})
	})
	if err != nil {
		return Status{}, err
	}
	return s.Status(ctx, uid)
}

// teardown は解除を確定した後に、チャンネルを止めてトークンを破棄する。トークンを読めなければチャンネルは期限で止まる。
func (s *Service) teardown(ctx context.Context, uid string, old Connection) {
	if client, err := s.Google.Client(ctx, uid); err == nil {
		s.stopChannel(ctx, client, old.ChannelID, old.ChannelResource)
	}
	if err := s.Google.DeleteToken(ctx, uid); err != nil {
		s.Log.WarnContext(ctx, "calendar token delete failed", "code", apperr.As(err).Code)
	}
}

// Webhook は Google Calendar の変更通知を受け付け、取り込みのジョブを積む。照合できなければ Forbidden。
func (s *Service) Webhook(ctx context.Context, channelID, token, resourceState string) error {
	if !s.cfg.Enabled {
		return apperr.NotFound("not_found")
	}
	// ストレージを読む前に形式を確かめる。
	if !ValidToken(channelID) || !ValidToken(token) {
		return apperr.Forbidden("not_found")
	}
	now := s.clock.Now()
	var ch Channel
	err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		var found bool
		var err error
		if ch, found, err = store.Get[Channel](ctx, tx, channelRef(channelID)); err != nil {
			return err
		}
		if !found || subtle.ConstantTimeCompare([]byte(ch.Token), []byte(token)) != 1 || !now.Before(ch.Expires) {
			return apperr.Forbidden("not_found")
		}
		return nil
	})
	if err != nil || resourceState == resourceStateSync {
		return err
	}
	return s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		c, err := loadConnection(ctx, tx, ch.UID)
		if err != nil || !c.Connected {
			return err
		}
		enqueued, err := enqueueOnce(ctx, tx, s.outbox, ch.UID, outbox.KindSync, &c.SyncJob)
		if err != nil || !enqueued {
			return err
		}
		return saveConnection(ctx, tx, ch.UID, c)
	})
}

type channelEntry struct {
	id string
	Channel
}

// Renew は期限が24時間以内のチャンネルを登録し直し、前のチャンネルを止める。1人の失敗で止めずに、ほかの利用者を続ける。
// あわせて、実行されないまま残ったジョブを登録し直す。
func (s *Service) Renew(ctx context.Context) error {
	if !s.cfg.Enabled {
		return apperr.Unavailable("calendar.unavailable")
	}
	now := s.clock.Now()
	var channels []channelEntry
	err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		return tx.Query(ctx, store.Query{Collection: channelsCollection}, func(ref store.Ref, decode func(dst any) error) error {
			var ch Channel
			if err := decode(&ch); err != nil {
				return err
			}
			channels = append(channels, channelEntry{id: ref.ID, Channel: ch})
			return nil
		})
	})
	if err != nil {
		return err
	}
	users := map[string]bool{}
	for _, ch := range channels {
		users[ch.UID] = true
		if ch.Expires.After(now.Add(renewWithin)) {
			continue
		}
		if err := s.renewChannel(ctx, ch, now); err != nil {
			s.Log.WarnContext(ctx, "calendar channel renew failed", "code", apperr.As(err).Code)
		}
	}
	for uid := range users {
		if err := s.outbox.Redispatch(ctx, uid, redispatchAfter); err != nil {
			s.Log.WarnContext(ctx, "outbox redispatch failed", "code", apperr.As(err).Code)
		}
	}
	return nil
}

func (s *Service) renewChannel(ctx context.Context, old channelEntry, now time.Time) error {
	var c Connection
	err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		c, err = loadConnection(ctx, tx, old.UID)
		return err
	})
	if err != nil {
		return err
	}
	if !c.Connected || c.ChannelID != old.id {
		// 今のチャンネルではない（つなぎ直した後に残ったもの）。記録を消せば通知を受け付けなくなる。
		s.deleteChannel(ctx, old.id)
		return nil
	}
	client, err := s.Google.Client(ctx, old.UID)
	if err != nil {
		return err
	}
	ch, w, err := s.startWatch(ctx, client, old.UID, c.CalendarID, c.GoogleID, false, now)
	if err != nil {
		return err
	}
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		c, err := loadConnection(ctx, tx, old.UID)
		if err != nil {
			return err
		}
		expires := store.Truncate(w.Expires)
		c.ChannelID, c.ChannelToken, c.ChannelResource, c.ChannelExpires = ch.id, ch.token, w.ResourceID, &expires
		if err := tx.Delete(channelRef(old.id)); err != nil {
			return err
		}
		return saveConnection(ctx, tx, old.UID, c)
	})
	if err != nil {
		return err
	}
	s.stopChannel(ctx, client, old.id, c.ChannelResource)
	return nil
}

// Register はカレンダーのジョブの処理を Outbox に登録する。連携を使わない環境では何もしない。
func (s *Service) Register(o *outbox.Outbox) {
	s.handle(o, kindResolve, s.resolveJob)
	if !s.cfg.Enabled {
		return
	}
	s.handle(o, outbox.KindTaskUpsert, s.upsertTask)
	s.handle(o, outbox.KindTaskDelete, s.deleteTask)
	s.handle(o, outbox.KindSleepUpsert, s.reconcileSleep)
	s.handle(o, outbox.KindSleepDelete, s.reconcileSleep)
	s.handle(o, outbox.KindExport, s.export)
	s.handle(o, outbox.KindSync, s.sync)
}

// claim は、まだ始まっていないジョブの記録（slot）がこのジョブなら消す。始まった後の変更は新しいジョブで反映する。
func (s *Service) claim(ctx context.Context, job outbox.Job, slot func(c *Connection) *string) error {
	return s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		c, err := loadConnection(ctx, tx, job.UID)
		if err != nil {
			return err
		}
		p := slot(&c)
		if *p != job.ID {
			return nil
		}
		*p = ""
		return saveConnection(ctx, tx, job.UID, c)
	})
}

// enqueueSync は取り込みのジョブを積む（外部での編集を見つけたとき）。
func (s *Service) enqueueSync(ctx context.Context, tx *store.Tx, uid string) error {
	c, err := loadConnection(ctx, tx, uid)
	if err != nil || !c.Connected {
		return err
	}
	enqueued, err := enqueueOnce(ctx, tx, s.outbox, uid, outbox.KindSync, &c.SyncJob)
	if err != nil || !enqueued {
		return err
	}
	return saveConnection(ctx, tx, uid, c)
}

func findTask(tasks []task.Task, taskID string) (task.Task, bool) {
	t, ok := task.Find(tasks, taskID)
	if !ok {
		return task.Task{}, false
	}
	return *t, true
}

// upsertTask は課題の予定を作る・書き直す。内容の変更が未反映なら題名・メモ・時刻も書き直す（DOPAGAKI での変更を優先する）。
// 計画だけの書き直しで外部での編集を見つけたら、上書きせずに取り込みのジョブを積む。
func (s *Service) upsertTask(ctx context.Context, job outbox.Job) error {
	uid, taskID := job.UID, job.Subject
	var c Connection
	var t task.Task
	var exists, linked bool
	var link Link
	var loc *time.Location
	err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		if c, err = loadConnection(ctx, tx, uid); err != nil || !c.Connected {
			return err
		}
		tasks, err := s.Tasks.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		t, exists = findTask(tasks, taskID)
		if link, linked, err = store.Get[Link](ctx, tx, linkRef(uid, taskID)); err != nil {
			return err
		}
		loc, err = s.Profiles.Location(ctx, tx, uid)
		return err
	})
	if err != nil || !c.Connected || !exists || t.Status == task.StatusCompleted {
		return err
	}
	client, err := s.Google.Client(ctx, uid)
	if err != nil {
		return err
	}
	seq := c.ContentSeq[taskID]
	applied := link.ContentSeq
	needSync := false
	var ev Event
	if !linked || link.EventID == "" {
		ev, err = client.InsertEvent(ctx, c.CalendarID, NewTaskEvent(uid, t, loc))
		switch {
		case err == nil:
			applied = seq
		case errors.Is(err, ErrEventExists):
			// 前の試行で作った予定が残っている。DOPAGAKI の内容で書き直す。
			if ev, err = client.GetEvent(ctx, c.CalendarID, EventID(uid, taskID)); err != nil {
				return err
			}
			if ev, err = s.patchContent(ctx, client, c.CalendarID, ev, t, loc); err != nil {
				return err
			}
			applied = seq
		default:
			return err
		}
		link = Link{TaskID: taskID, EventID: ev.ID}
	} else {
		ev, err = client.GetEvent(ctx, c.CalendarID, link.EventID)
		if errors.Is(err, ErrEventGone) {
			// 本人がカレンダーで予定を消した。取り込みで課題を消す。
			return nil
		}
		if err != nil {
			return err
		}
		switch {
		case c.ContentPending(link) && ExternallyEdited(link, ev):
			return s.captureConflict(ctx, uid, t, ev, false)
		case c.ContentPending(link):
			if ev, err = s.patchContent(ctx, client, c.CalendarID, ev, t, loc); err != nil {
				return err
			}
			applied = seq
		case ExternallyEdited(link, ev):
			needSync = true
		default:
			if patch := PlanPatch(t, ev, loc); patch.Changes(ev) {
				updated, err := client.PatchEvent(ctx, c.CalendarID, ev.ID, ev.ETag, patch)
				switch {
				case errors.Is(err, ErrPreconditionFailed):
					needSync = true
				case errors.Is(err, ErrEventGone):
					return nil
				case err != nil:
					return err
				default:
					ev = updated
				}
			}
		}
	}
	return s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		current, found, err := store.Get[Link](ctx, tx, linkRef(uid, taskID))
		if err != nil {
			return err
		}
		if needSync {
			return s.enqueueSync(ctx, tx, uid)
		}
		next := Link{TaskID: taskID, EventID: ev.ID, ETag: ev.ETag, InputHash: Hash(ev), ContentSeq: max(applied, current.ContentSeq)}
		if err := tx.Set(linkRef(uid, taskID), next); err != nil {
			return err
		}
		if !found || current.EventID != ev.ID {
			// 課題の一覧の calendarLinked が変わる。
			if _, err := tx.Touch(ctx, uid, store.ResourceTasks); err != nil {
				return err
			}
		}
		_, err = tx.Touch(ctx, uid, store.ResourceCalendar)
		return err
	})
}

// patchContent は課題の内容で予定を書き直す。版が合わなければ（書き直しの間に編集された）、再試行させる。
func (s *Service) patchContent(ctx context.Context, client Client, calendarID string, ev Event, t task.Task, loc *time.Location) (Event, error) {
	patch := ContentPatch(t, loc)
	if !patch.Changes(ev) {
		return ev, nil
	}
	updated, err := client.PatchEvent(ctx, calendarID, ev.ID, ev.ETag, patch)
	if errors.Is(err, ErrPreconditionFailed) {
		return Event{}, apperr.External("external.failed", err)
	}
	return updated, err
}

// deleteTask は消した課題の予定を消す。予定が既に無ければ成功とみなす。同じ ID の課題が登録し直されていれば何もしない。
func (s *Service) deleteTask(ctx context.Context, job outbox.Job) error {
	uid, taskID := job.UID, job.Subject
	var c Connection
	var exists bool
	var link Link
	err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		if c, err = loadConnection(ctx, tx, uid); err != nil || !c.Connected {
			return err
		}
		tasks, err := s.Tasks.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		_, exists = findTask(tasks, taskID)
		link, _, err = store.Get[Link](ctx, tx, linkRef(uid, taskID))
		return err
	})
	if err != nil || !c.Connected || exists {
		return err
	}
	eventID := link.EventID
	if eventID == "" {
		// 予定を作った後、結び付きを書く前に失敗した場合にも消せるよう、決まった ID を使う。
		eventID = EventID(uid, taskID)
	}
	client, err := s.Google.Client(ctx, uid)
	if err != nil {
		return err
	}
	if err := client.DeleteEvent(ctx, c.CalendarID, eventID); err != nil && !errors.Is(err, ErrEventGone) {
		return err
	}
	return s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		c, err := loadConnection(ctx, tx, uid)
		if err != nil {
			return err
		}
		if err := tx.Delete(linkRef(uid, taskID)); err != nil {
			return err
		}
		delete(c.ContentSeq, taskID)
		return saveConnection(ctx, tx, uid, c)
	})
}

// reconcileSleep は就寝の予定を今の設定に合わせる（設定があれば書き直し、なければ消す）。
func (s *Service) reconcileSleep(ctx context.Context, job outbox.Job) error {
	uid := job.UID
	now := s.clock.Now()
	var c Connection
	var desired *Event
	err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		if c, err = loadConnection(ctx, tx, uid); err != nil || !c.Connected {
			return err
		}
		st, err := s.Sleeps.GetTx(ctx, tx, uid, now)
		if err != nil || st.Sleep == nil {
			return err
		}
		loc, err := s.Profiles.Location(ctx, tx, uid)
		if err != nil {
			return err
		}
		ev := SleepEvent(uid, st.Sleep.Schedule, now, loc)
		desired = &ev
		return nil
	})
	if err != nil || !c.Connected {
		return err
	}
	client, err := s.Google.Client(ctx, uid)
	if err != nil {
		return err
	}
	eventID := ""
	if desired == nil {
		if err := client.DeleteEvent(ctx, c.CalendarID, SleepEventID(uid)); err != nil && !errors.Is(err, ErrEventGone) {
			return err
		}
	} else {
		saved, err := client.UpdateEvent(ctx, c.CalendarID, *desired)
		if errors.Is(err, ErrEventGone) {
			saved, err = client.InsertEvent(ctx, c.CalendarID, *desired)
			if errors.Is(err, ErrEventExists) {
				saved, err = client.UpdateEvent(ctx, c.CalendarID, *desired)
			}
		}
		if err != nil {
			return err
		}
		eventID = saved.ID
	}
	return s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		c, err := loadConnection(ctx, tx, uid)
		if err != nil || c.SleepEventID == eventID {
			return err
		}
		c.SleepEventID = eventID
		return saveConnection(ctx, tx, uid, c)
	})
}

// export は予定と結び付いた未完了の課題すべてについて、予定の説明の DOPAGAKI のブロックを書き直す。
// 外部での編集を見つけた予定は上書きせず、取り込みのジョブを積む。
func (s *Service) export(ctx context.Context, job outbox.Job) error {
	uid := job.UID
	if err := s.claim(ctx, job, func(c *Connection) *string { return &c.ExportJob }); err != nil {
		return err
	}
	var c Connection
	var tasks []task.Task
	var links []Link
	var loc *time.Location
	err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		if c, err = loadConnection(ctx, tx, uid); err != nil || !c.Connected {
			return err
		}
		if tasks, err = s.Tasks.All(ctx, tx, uid); err != nil {
			return err
		}
		if links, err = store.List[Link](ctx, tx, store.Query{Collection: linksCollection(uid)}); err != nil {
			return err
		}
		loc, err = s.Profiles.Location(ctx, tx, uid)
		return err
	})
	if err != nil || !c.Connected {
		return err
	}
	client, err := s.Google.Client(ctx, uid)
	if err != nil {
		return err
	}
	var updates []Link
	var failures []error
	needSync := false
	for _, l := range links {
		t, ok := findTask(tasks, l.TaskID)
		if l.EventID == "" || !ok || t.Status == task.StatusCompleted || c.ContentPending(l) {
			continue
		}
		ev, err := client.GetEvent(ctx, c.CalendarID, l.EventID)
		if errors.Is(err, ErrEventGone) {
			continue
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if ExternallyEdited(l, ev) {
			needSync = true
			continue
		}
		if patch := PlanPatch(t, ev, loc); patch.Changes(ev) {
			updated, err := client.PatchEvent(ctx, c.CalendarID, ev.ID, ev.ETag, patch)
			switch {
			case errors.Is(err, ErrPreconditionFailed):
				needSync = true
				continue
			case errors.Is(err, ErrEventGone):
				continue
			case err != nil:
				failures = append(failures, err)
				continue
			}
			ev = updated
		}
		if ev.ETag != l.ETag || Hash(ev) != l.InputHash {
			l.ETag, l.InputHash = ev.ETag, Hash(ev)
			updates = append(updates, l)
		}
	}
	if len(updates) > 0 || needSync {
		err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
			for _, u := range updates {
				current, found, err := store.Get[Link](ctx, tx, linkRef(uid, u.TaskID))
				if err != nil {
					return err
				}
				if !found || current.EventID != u.EventID {
					continue
				}
				current.ETag, current.InputHash = u.ETag, u.InputHash
				if err := tx.Set(linkRef(uid, u.TaskID), current); err != nil {
					return err
				}
			}
			if _, err := tx.Touch(ctx, uid, store.ResourceCalendar); err != nil {
				return err
			}
			if needSync {
				return s.enqueueSync(ctx, tx, uid)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	if len(failures) > 0 {
		return apperr.External("external.failed", errors.Join(failures...))
	}
	return nil
}

// listAll は変更された予定をすべて読む。syncToken が使えなくなっていたら、最初から1回だけ読み直す。
func listAll(ctx context.Context, client Client, calendarID, syncToken string) ([]Event, string, error) {
	events, next, err := listPages(ctx, client, calendarID, syncToken)
	if errors.Is(err, ErrSyncTokenExpired) && syncToken != "" {
		return listPages(ctx, client, calendarID, "")
	}
	return events, next, err
}

func listPages(ctx context.Context, client Client, calendarID, syncToken string) ([]Event, string, error) {
	var events []Event
	page := ""
	for range maxPages {
		p, err := client.ListEvents(ctx, calendarID, syncToken, page)
		if err != nil {
			return nil, "", err
		}
		events = append(events, p.Events...)
		if p.NextPageToken == "" {
			return events, p.NextSyncToken, nil
		}
		page = p.NextPageToken
	}
	return nil, "", apperr.External("external.failed", errors.New("calendar: too many pages"))
}

// syncView は取り込みの判定に使う、保存している状態。
type syncView struct {
	conn      Connection
	tasks     map[string]task.Task
	links     map[string]Link // 課題の ID → 結び付き
	eventTask map[string]string
	taskEvent map[string]string
	count     int
}

func newSyncView(c Connection, tasks []task.Task, links []Link) syncView {
	v := syncView{conn: c, tasks: map[string]task.Task{}, links: map[string]Link{}, eventTask: map[string]string{}, taskEvent: map[string]string{}, count: len(tasks)}
	for _, t := range tasks {
		v.tasks[t.ID] = t
	}
	for _, l := range links {
		v.links[l.TaskID] = l
		if l.EventID != "" {
			v.eventTask[l.EventID] = l.TaskID
			v.taskEvent[l.TaskID] = l.EventID
		}
	}
	return v
}

// needsImport は予定の内容を課題に取り込むか。取り込み済み（ハッシュが同じ）、完了した課題、内容が同じ課題、
// DOPAGAKI での内容の変更がまだ予定に反映されていない課題は取り込まない。
func (v syncView) needsImport(taskID string, c EventChange) bool {
	l, linked := v.links[taskID]
	if linked && l.EventID == c.EventID && l.InputHash == c.Hash {
		return false
	}
	t, exists := v.tasks[taskID]
	if !exists {
		return true
	}
	return t.Status != task.StatusCompleted && !SameContent(t, c) && (!linked || !v.conn.ContentPending(l))
}

// sync はカレンダーの変更を取り込む。新しい予定は課題として登録し（登録の報酬は1課題に1回）、編集された予定は課題に反映し、
// 消された予定の課題は消す。手順の具体化（AI）は変わった予定だけに使い、使えなければ逆算だけの計画にする。
func (s *Service) sync(ctx context.Context, job outbox.Job) error {
	uid := job.UID
	if err := s.claim(ctx, job, func(c *Connection) *string { return &c.SyncJob }); err != nil {
		return err
	}
	var view syncView
	var profileContext string
	err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		c, err := loadConnection(ctx, tx, uid)
		if err != nil || !c.Connected {
			view.conn = c
			return err
		}
		tasks, err := s.Tasks.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		links, err := store.List[Link](ctx, tx, store.Query{Collection: linksCollection(uid)})
		if err != nil {
			return err
		}
		view = newSyncView(c, tasks, links)
		profileContext, _, err = s.Profiles.Context(ctx, tx, uid)
		return err
	})
	if err != nil || !view.conn.Connected {
		return err
	}
	client, err := s.Google.Client(ctx, uid)
	if err != nil {
		return err
	}
	events, nextToken, err := listAll(ctx, client, view.conn.CalendarID, view.conn.SyncToken)
	if err != nil {
		return err
	}
	events = LatestByID(events)
	now := s.clock.Now()
	details := map[string]*task.DetailResult{}
	for _, e := range events {
		c, ok := Classify(e, now)
		if !ok || c.Cancelled || !view.needsImport(TaskIDFor(c, view.eventTask, view.taskEvent), c) {
			continue
		}
		details[c.EventID] = s.detail(ctx, uid, c, profileContext)
	}
	return s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		c, err := loadConnection(ctx, tx, uid)
		if err != nil || !c.Connected {
			return err
		}
		tasks, err := s.Tasks.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		links, err := store.List[Link](ctx, tx, store.Query{Collection: linksCollection(uid)})
		if err != nil {
			return err
		}
		v := newSyncView(c, tasks, links)
		var items []task.Imported
		var writes []Link
		var removed []string
		linkSetChanged := false
		for _, e := range events {
			ch, ok := Classify(e, now)
			if !ok {
				continue
			}
			taskID := TaskIDFor(ch, v.eventTask, v.taskEvent)
			l, linked := v.links[taskID]
			if t, ok := v.tasks[taskID]; ok && linked && (c.ContentPending(l)) && (ch.Cancelled || ExternallyEdited(l, e)) {
				if err := captureTx(ctx, tx, uid, t, e, ch.Cancelled); err != nil {
					return err
				}
				continue
			}
			_, exists := v.tasks[taskID]
			if ch.Cancelled {
				if exists {
					items = append(items, ch.Import(taskID, nil))
					v.count--
				}
				if linked && l.EventID == ch.EventID {
					removed = append(removed, taskID)
				}
				continue
			}
			if v.needsImport(taskID, ch) {
				if !exists {
					if v.count >= task.MaxTasks {
						continue
					}
					v.count++
				}
				items = append(items, ch.Import(taskID, details[ch.EventID]))
			} else if linked && v.conn.ContentPending(l) {
				// DOPAGAKI での変更を予定に書き込むジョブが、結び付きを更新する。
				continue
			}
			next := Link{TaskID: taskID, EventID: ch.EventID, ETag: ch.ETag, InputHash: ch.Hash, ContentSeq: l.ContentSeq}
			if !linked || l != next {
				writes = append(writes, next)
				linkSetChanged = linkSetChanged || !linked || l.EventID != next.EventID
			}
		}
		changed, err := s.Tasks.ApplyImport(ctx, tx, uid, items, now)
		if err != nil {
			return err
		}
		for _, l := range writes {
			if err := tx.Set(linkRef(uid, l.TaskID), l); err != nil {
				return err
			}
		}
		for _, taskID := range removed {
			if err := tx.Delete(linkRef(uid, taskID)); err != nil {
				return err
			}
			delete(c.ContentSeq, taskID)
		}
		if linkSetChanged || len(removed) > 0 {
			// 課題の一覧の calendarLinked が変わる。
			if _, err := tx.Touch(ctx, uid, store.ResourceTasks); err != nil {
				return err
			}
		}
		synced := store.Truncate(now)
		c.SyncToken, c.LastSync = nextToken, &synced
		if changed {
			// 取り込みで計画が組み直されたので、予定の説明を書き直す。
			if _, err := enqueueOnce(ctx, tx, s.outbox, uid, outbox.KindExport, &c.ExportJob); err != nil {
				return err
			}
		}
		return saveConnection(ctx, tx, uid, c)
	})
}

// detail は取り込む予定の手順を AI で具体化する。AI が使えない・上限に達した・失敗したときは nil（逆算だけの計画）にし、
// 取り込み全体は止めない。
func (s *Service) detail(ctx context.Context, uid string, c EventChange, profileContext string) *task.DetailResult {
	res, err := s.Planner.Detail(ctx, uid, task.DetailRequest{Title: c.Title, Notes: c.Notes, Minutes: c.Minutes, Context: profileContext})
	if err != nil {
		s.Log.InfoContext(ctx, "calendar import without ai detail", "code", apperr.As(err).Code)
		return nil
	}
	return &res
}
