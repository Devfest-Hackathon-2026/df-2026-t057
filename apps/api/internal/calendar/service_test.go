package calendar

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
)

func manualEvent(eventID, title string, end time.Time, minutes int) Event {
	return Event{
		ID: eventID, Summary: title, Description: "本人のメモ",
		Start: EventTime{DateTime: end.Add(-time.Duration(minutes) * time.Minute).Format(time.RFC3339)},
		End:   EventTime{DateTime: end.Format(time.RFC3339)},
	}
}

func TestEnqueuerIsNoOpWhenNotConnected(t *testing.T) {
	e := newEnv(t)
	created := e.createTask("レポート", 30, e.clock.Now().Add(3*time.Hour))
	if pending, failed := e.jobCounts(); pending != 0 || failed != 0 {
		t.Fatalf("jobs enqueued without a connection: %d %d", pending, failed)
	}
	if e.list().Linked[created.ID] {
		t.Fatal("unconnected task reported as linked")
	}
	if e.revision(store.ResourceCalendar) != 0 {
		t.Fatal("calendar revision changed without a connection")
	}
	st, err := e.svc.Status(e.ctx, uid)
	if err != nil || !st.Enabled || st.Connected || st.Sync != SyncDisconnected {
		t.Fatalf("status: %+v %v", st, err)
	}
}

func TestDisabledService(t *testing.T) {
	e := newEnv(t)
	svc := NewService(e.runner, e.clock, &fakeIDs{}, e.outbox, Config{AppURL: "https://app.example"}, Deps{})
	st, err := svc.Status(e.ctx, uid)
	if err != nil || st.Enabled || st.Connected || st.Sync != SyncDisconnected {
		t.Fatalf("status: %+v %v", st, err)
	}
	if _, err := svc.Connect(e.ctx, uid, "g", "m"); !errors.Is(err, apperr.Unavailable("calendar.unavailable")) {
		t.Fatalf("connect: %v", err)
	}
	if _, err := svc.SyncNow(e.ctx, uid); !errors.Is(err, apperr.Unavailable("calendar.unavailable")) {
		t.Fatalf("sync: %v", err)
	}
	if got := svc.Callback(e.ctx, strings.Repeat("a", 32), "code", ""); got != "https://app.example/?calendar=error" {
		t.Fatalf("callback: %s", got)
	}
	links, err := Disabled{}.LinkedTasks(e.ctx, nil, uid)
	if err != nil || links == nil || len(links) != 0 {
		t.Fatal("disabled links")
	}
}

type fakeIDs struct{}

func (fakeIDs) New() string { return strings.Repeat("f", 32) }

func TestConnectRequiresGoogleSubject(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.Connect(e.ctx, uid, "", "user@example.com"); !errors.Is(err, apperr.Forbidden("calendar.reauth_required")) {
		t.Fatalf("got %v", err)
	}
	if _, err := e.svc.SyncNow(e.ctx, uid); !errors.Is(err, apperr.Conflict("calendar.not_connected")) {
		t.Fatalf("sync before connect: %v", err)
	}
}

func TestConnectCreatesEventsForTasksRegisteredBeforehand(t *testing.T) {
	e := newEnv(t)
	before := e.createTask("先に登録した課題", 30, e.clock.Now().Add(3*time.Hour))
	url, err := e.svc.Connect(e.ctx, uid, "google-1", "user@example.com")
	if err != nil || !strings.Contains(url, "login_hint=user@example.com") {
		t.Fatalf("connect: %s %v", url, err)
	}
	if got := e.svc.Callback(e.ctx, e.google.lastState, "code", ""); got != "https://app.example/?calendar=connected" {
		t.Fatalf("callback: %s", got)
	}
	c := e.connection()
	if !c.Connected || c.CalendarID != "cal-1" || c.GoogleID != "google-1" || c.ChannelID == "" || c.ChannelResource == "" || string(e.google.saved[uid]) != "token" {
		t.Fatalf("connection: %+v", c)
	}
	if len(e.cal.watches) != 1 || e.cal.watches[0].Address != "https://app.example/api/calendar/webhook" {
		t.Fatalf("watch: %+v", e.cal.watches)
	}
	st, _ := e.svc.Status(e.ctx, uid)
	if st.Sync != SyncPending {
		t.Fatalf("initial jobs should be pending: %+v", st)
	}
	e.mustDrain()
	ev, ok := e.cal.event(EventID(uid, before.ID))
	if !ok || ev.Summary != before.Title || !strings.Contains(ev.Description, blockStart) || ev.Private[PropTaskID] != before.ID {
		t.Fatalf("event for the earlier task: %+v %v", ev, ok)
	}
	if !e.list().Linked[before.ID] {
		t.Fatal("task not linked after connecting")
	}
	st, _ = e.svc.Status(e.ctx, uid)
	if st.Sync != SyncSynced || st.LastSync == nil || !st.Connected {
		t.Fatalf("status after drain: %+v", st)
	}
	if e.rewards.total() != 300 {
		t.Fatalf("own event re-imported as a new task: %v", e.rewards.granted)
	}
}

func TestReconnectKeepsSyncTokenAndStopsOldChannel(t *testing.T) {
	e := newEnv(t)
	e.connect()
	e.mustDrain()
	first := e.connection()
	if first.SyncToken == "" {
		t.Fatal("no sync token after the initial sync")
	}
	e.connect()
	second := e.connection()
	if second.CalendarID != first.CalendarID || second.SyncToken != first.SyncToken || e.cal.calendars != 1 {
		t.Fatalf("reconnect reset the connection: %+v -> %+v", first, second)
	}
	if second.ChannelID == first.ChannelID || len(e.cal.stopped) != 1 || e.cal.stopped[0] != first.ChannelID {
		t.Fatalf("old channel not replaced: stopped=%v", e.cal.stopped)
	}
	if err := e.svc.Webhook(e.ctx, first.ChannelID, first.ChannelToken, "exists"); !errors.Is(err, apperr.Forbidden("not_found")) {
		t.Fatalf("old channel still accepted: %v", err)
	}
}

func TestCallbackRejectsInvalidRequests(t *testing.T) {
	e := newEnv(t)
	fail := "https://app.example/?calendar=error"
	for name, args := range map[string][3]string{
		"no params":     {"", "", ""},
		"bad state":     {"not-a-state", "code", ""},
		"missing code":  {strings.Repeat("a", 32), "", ""},
		"denied":        {strings.Repeat("a", 32), "code", "access_denied"},
		"unknown state": {strings.Repeat("a", 32), "code", ""},
	} {
		if got := e.svc.Callback(e.ctx, args[0], args[1], args[2]); got != fail {
			t.Errorf("%s: %s", name, got)
		}
	}

	// 別の Google アカウント・継続の許可なし・使用済み・期限切れ。
	if _, err := e.svc.Connect(e.ctx, uid, "google-1", "m"); err != nil {
		t.Fatal(err)
	}
	e.google.grant.Subject = "google-2"
	if got := e.svc.Callback(e.ctx, e.google.lastState, "code", ""); got != fail {
		t.Fatal("different Google account accepted")
	}
	if got := e.svc.Callback(e.ctx, e.google.lastState, "code", ""); got != fail {
		t.Fatal("state reused")
	}
	e.google.grant = Grant{Subject: "google-1", Token: []byte("t")}
	_, _ = e.svc.Connect(e.ctx, uid, "google-1", "m")
	if got := e.svc.Callback(e.ctx, e.google.lastState, "code", ""); got != fail {
		t.Fatal("missing refresh token accepted")
	}
	e.google.grant.HasRefreshToken = true
	_, _ = e.svc.Connect(e.ctx, uid, "google-1", "m")
	e.clock.Advance(11 * time.Minute)
	if got := e.svc.Callback(e.ctx, e.google.lastState, "code", ""); got != fail {
		t.Fatal("expired state accepted")
	}
	if e.connection().Connected {
		t.Fatal("connected after failures")
	}
}

func TestImportGrantsOnceAndIgnoresDuplicateNotifications(t *testing.T) {
	e := newEnv(t)
	e.connect()
	e.mustDrain()
	end := e.clock.Now().Add(4 * time.Hour)
	e.cal.userAdd(manualEvent("manual-event", "手入力の課題", end, 30))
	e.webhook()
	e.mustDrain()

	taskID := ImportedTaskID("manual-event")
	l := e.list()
	if len(l.Items) != 1 || l.Items[0].ID != taskID || l.Items[0].Title != "手入力の課題" || l.Items[0].Notes != "本人のメモ" || !l.Linked[taskID] {
		t.Fatalf("import: %+v", l)
	}
	if e.rewards.total() != 300 {
		t.Fatalf("rewards: %v", e.rewards.granted)
	}
	// 取り込んだ予定にも計画のブロックを書く。
	if ev, _ := e.cal.event("manual-event"); !strings.Contains(ev.Description, blockStart) || !strings.HasPrefix(ev.Description, "本人のメモ") {
		t.Fatalf("imported event not exported: %q", ev.Description)
	}
	rev := e.revision(store.ResourceTasks)
	details := e.planner.details

	// 同じ通知が重なっても、計画・版・報酬は変わらない。
	e.cal.duplicate = true
	e.cal.userEdit("manual-event", func(*Event) {}) // 版だけが変わる通知
	e.webhook()
	e.webhook()
	e.mustDrain()
	if e.revision(store.ResourceTasks) != rev || e.rewards.total() != 300 || e.planner.details != details {
		t.Fatalf("duplicate notification changed state: rev %d->%d details %d->%d", rev, e.revision(store.ResourceTasks), details, e.planner.details)
	}
}

func TestEditIsAppliedOnceAndCancellationRemovesTask(t *testing.T) {
	e := newEnv(t)
	e.connect()
	e.mustDrain()
	end := e.clock.Now().Add(4 * time.Hour)
	e.cal.userAdd(manualEvent("manual-event", "手入力の課題", end, 30))
	e.webhook()
	e.mustDrain()
	taskID := ImportedTaskID("manual-event")
	rev := e.revision(store.ResourceTasks)

	e.cal.userEdit("manual-event", func(ev *Event) { ev.Summary = "変更した課題" })
	e.webhook()
	e.mustDrain()
	l := e.list()
	if l.Items[0].Title != "変更した課題" || e.rewards.total() != 300 {
		t.Fatalf("edit not applied: %+v", l.Items[0])
	}
	afterEdit := e.revision(store.ResourceTasks)
	if afterEdit <= rev {
		t.Fatal("edit did not bump the plan revision")
	}
	e.webhook()
	e.mustDrain()
	if e.revision(store.ResourceTasks) != afterEdit {
		t.Fatal("edit applied twice")
	}

	e.cal.userEdit("manual-event", func(ev *Event) { ev.Status = EventCancelled })
	e.webhook()
	e.mustDrain()
	if len(e.list().Items) != 0 || len(e.sessions.removed) != 1 || e.sessions.removed[0] != taskID {
		t.Fatalf("cancellation: items=%d removed=%v", len(e.list().Items), e.sessions.removed)
	}
	if _, linked := e.link(taskID); linked {
		t.Fatal("link kept after cancellation")
	}

	e.cal.userEdit("manual-event", func(ev *Event) { ev.Status = EventConfirmed })
	e.webhook()
	e.mustDrain()
	l = e.list()
	if len(l.Items) != 1 || l.Items[0].ID != taskID || e.rewards.total() != 300 {
		t.Fatalf("restore: %+v rewards %v", l.Items, e.rewards.granted)
	}
}

func TestIgnoresAllDayAndRecurringEvents(t *testing.T) {
	e := newEnv(t)
	e.connect()
	e.mustDrain()
	e.cal.userAdd(Event{ID: "all-day", Summary: "終日", Start: EventTime{Date: "2026-11-11"}, End: EventTime{Date: "2026-11-12"}})
	recurring := manualEvent("recurring", "毎日", e.clock.Now().Add(2*time.Hour), 30)
	recurring.Recurrence = []string{"RRULE:FREQ=DAILY"}
	e.cal.userAdd(recurring)
	e.webhook()
	e.mustDrain()
	if len(e.list().Items) != 0 || e.rewards.total() != 0 {
		t.Fatal("unsupported event imported")
	}
}

func TestOwnTaskEventLifecycle(t *testing.T) {
	e := newEnv(t)
	e.connect()
	e.mustDrain()
	deadline := e.clock.Now().Add(5 * time.Hour)
	created := e.createTask("課題A", 60, deadline)
	e.mustDrain()
	eventID := EventID(uid, created.ID)
	ev, ok := e.cal.event(eventID)
	if !ok || ev.Summary != "課題A" || ev.End.DateTime != deadline.UTC().Truncate(time.Second).Format(time.RFC3339) {
		t.Fatalf("event: %+v", ev)
	}
	if e.rewards.total() != 300 {
		t.Fatal("own event imported as a new task")
	}

	// 内容の変更は題名・時刻も書き直す。
	rev := e.list().Revision
	if _, err := e.tasks.Update(e.ctx, uid, created.ID, task.Input{Title: "課題A改", Notes: "メモ", Deadline: deadline, Minutes: 45}, rev); err != nil {
		t.Fatal(err)
	}
	e.mustDrain()
	ev, _ = e.cal.event(eventID)
	if ev.Summary != "課題A改" || StripBlock(ev.Description) != "メモ" || ev.Start.DateTime != deadline.Add(-45*time.Minute).UTC().Truncate(time.Second).Format(time.RFC3339) {
		t.Fatalf("content not written: %+v", ev)
	}
	if got := e.list().Items[0]; got.Title != "課題A改" || got.Plan.Reason != task.ReasonBackward {
		t.Fatalf("own write re-imported: %+v", got)
	}
	l, _ := e.link(created.ID)
	if c := e.connection(); c.ContentPending(l) {
		t.Fatal("content still pending")
	}

	// 予定が既に消されていても、課題の削除は成功する。
	e.cal.userEdit(eventID, func(ev *Event) { ev.Status = EventCancelled })
	if err := e.tasks.Delete(e.ctx, uid, created.ID); err != nil {
		t.Fatal(err)
	}
	if errs := e.drain(); len(errs) != 0 {
		t.Fatalf("delete failed on a missing event: %v", errs)
	}
	if _, linked := e.link(created.ID); linked {
		t.Fatal("link kept after delete")
	}
	if _, failed := e.jobCounts(); failed != 0 {
		t.Fatal("failed jobs")
	}
}

func TestDeleteRemovesEvent(t *testing.T) {
	e := newEnv(t)
	e.connect()
	e.mustDrain()
	created := e.createTask("消す課題", 30, e.clock.Now().Add(3*time.Hour))
	e.mustDrain()
	if err := e.tasks.Delete(e.ctx, uid, created.ID); err != nil {
		t.Fatal(err)
	}
	e.mustDrain()
	if ev, _ := e.cal.event(EventID(uid, created.ID)); ev.Status != EventCancelled {
		t.Fatalf("event not deleted: %+v", ev)
	}
	if len(e.list().Items) != 0 {
		t.Fatal("deleted task came back from the cancellation notification")
	}
}

func TestExternalEditIsImportedInsteadOfOverwritten(t *testing.T) {
	e := newEnv(t)
	e.connect()
	e.mustDrain()
	deadline := e.clock.Now().Add(5 * time.Hour)
	a := e.createTask("課題A", 60, deadline)
	e.mustDrain()
	eventID := EventID(uid, a.ID)

	// 本人が Google で題名を変えたが、通知はまだ届いていない。その間に計画が変わる。
	e.cal.userEdit(eventID, func(ev *Event) { ev.Summary = "Googleで変えた題名" })
	e.createTask("課題B", 60, deadline)
	e.mustDrain()

	ev, _ := e.cal.event(eventID)
	if ev.Summary != "Googleで変えた題名" {
		t.Fatalf("external edit overwritten: %q", ev.Summary)
	}
	var got task.Task
	for _, item := range e.list().Items {
		if item.ID == a.ID {
			got = item
		}
	}
	if got.Title != "Googleで変えた題名" {
		t.Fatalf("external edit not imported: %+v", got)
	}
	// 取り込んだ後は、新しい計画のブロックが書かれている。
	if !strings.Contains(ev.Description, blockStart) {
		t.Fatal("block missing")
	}
	if _, failed := e.jobCounts(); failed != 0 {
		t.Fatal("external edit left failed jobs")
	}
}

func TestPreconditionFailedEnqueuesSync(t *testing.T) {
	e := newEnv(t)
	e.connect()
	e.mustDrain()
	deadline := e.clock.Now().Add(5 * time.Hour)
	e.createTask("課題A", 60, deadline)
	e.mustDrain()
	e.cal.patchErr = ErrPreconditionFailed
	e.createTask("課題B", 60, deadline)
	if errs := e.drain(); len(errs) != 0 {
		t.Fatalf("412 should not fail the job: %v", errs)
	}
	if pending, failed := e.jobCounts(); pending != 0 || failed != 0 {
		t.Fatalf("jobs: %d %d", pending, failed)
	}
}

func TestSyncRecoversFromExpiredToken(t *testing.T) {
	e := newEnv(t)
	e.connect()
	e.mustDrain()
	e.cal.userAdd(manualEvent("manual-event", "手入力の課題", e.clock.Now().Add(4*time.Hour), 30))
	e.cal.expireToken = true
	if _, err := e.svc.SyncNow(e.ctx, uid); err != nil {
		t.Fatal(err)
	}
	e.mustDrain()
	if len(e.list().Items) != 1 || e.rewards.total() != 300 {
		t.Fatal("full resync after 410 did not import")
	}
}

func TestSyncFallsBackWhenAIFails(t *testing.T) {
	for _, code := range []string{"ai.rate_limited", "ai.unavailable", "ai.failed"} {
		e := newEnv(t)
		e.planner.err = apperr.Unavailable(code)
		e.connect()
		e.mustDrain()
		e.cal.userAdd(manualEvent("manual-event", "手入力の課題", e.clock.Now().Add(4*time.Hour), 30))
		e.webhook()
		e.mustDrain()
		l := e.list()
		if len(l.Items) != 1 || len(l.Items[0].Plan.Steps) != 1 {
			t.Fatalf("%s: import failed: %+v", code, l.Items)
		}
	}
	e := newEnv(t)
	e.planner.err = nil
	e.connect()
	e.mustDrain()
	e.cal.userAdd(manualEvent("manual-event", "手入力の課題", e.clock.Now().Add(4*time.Hour), 30))
	e.webhook()
	e.mustDrain()
	if l := e.list(); len(l.Items[0].Plan.Steps) != 2 || l.Items[0].Plan.Reason != task.ReasonAIDetailed {
		t.Fatalf("AI detail not used: %+v", l.Items[0].Plan)
	}
}

func TestSleepEventFollowsSchedule(t *testing.T) {
	e := newEnv(t)
	e.connect()
	e.mustDrain()
	e.sleeps.schedule = &sleep.Schedule{Bedtime: "23:00", Wake: "07:00"}
	err := e.runner.Update(e.ctx, func(ctx context.Context, tx *store.Tx) error {
		return e.enq.SleepChanged(ctx, tx, uid)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !e.sleepPending() {
		t.Fatal("sleep job should be pending")
	}
	e.mustDrain()
	if e.sleepPending() {
		t.Fatal("sleep job still pending")
	}
	ev, ok := e.cal.event(SleepEventID(uid))
	if !ok || len(ev.Recurrence) != 1 || !ev.Transparent || ev.Status != EventConfirmed || e.connection().SleepEventID != ev.ID {
		t.Fatalf("sleep event: %+v", ev)
	}
	// 本人が消した就寝の予定も、時刻を変えると戻る。
	e.cal.userEdit(ev.ID, func(ev *Event) { ev.Status = EventCancelled })
	e.sleeps.schedule = &sleep.Schedule{Bedtime: "22:30", Wake: "06:30"}
	_ = e.runner.Update(e.ctx, func(ctx context.Context, tx *store.Tx) error { return e.enq.SleepChanged(ctx, tx, uid) })
	e.mustDrain()
	if ev, _ = e.cal.event(SleepEventID(uid)); ev.Status != EventConfirmed || !strings.HasPrefix(ev.Start.DateTime, "2026-11-10T22:30") {
		t.Fatalf("sleep event not restored: %+v", ev)
	}
	// 設定をやめると予定を消す。
	e.sleeps.schedule = nil
	_ = e.runner.Update(e.ctx, func(ctx context.Context, tx *store.Tx) error { return e.enq.SleepCleared(ctx, tx, uid) })
	e.mustDrain()
	if ev, _ = e.cal.event(SleepEventID(uid)); ev.Status != EventCancelled {
		t.Fatal("sleep event not deleted")
	}
	if len(e.list().Items) != 0 {
		t.Fatal("sleep event imported as a task")
	}
}

func (e *env) sleepPending() bool {
	e.t.Helper()
	var pending bool
	err := e.runner.View(e.ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		pending, err = e.enq.SleepPending(ctx, tx, uid)
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return pending
}

func TestPlansChangedCoalescesExportJobs(t *testing.T) {
	e := newEnv(t)
	e.connect()
	e.mustDrain()
	for range 3 {
		err := e.runner.Update(e.ctx, func(ctx context.Context, tx *store.Tx) error { return e.enq.PlansChanged(ctx, tx, uid) })
		if err != nil {
			t.Fatal(err)
		}
	}
	if pending, _ := e.jobCounts(); pending != 1 {
		t.Fatalf("export jobs not coalesced: %d", pending)
	}
}

func TestWebhookValidation(t *testing.T) {
	e := newEnv(t)
	e.connect()
	e.mustDrain()
	c := e.connection()
	forbidden := apperr.Forbidden("not_found")
	cases := map[string][2]string{
		"bad id format":    {"x", c.ChannelToken},
		"bad token format": {c.ChannelID, "x"},
		"unknown channel":  {strings.Repeat("b", 32), c.ChannelToken},
		"wrong token":      {c.ChannelID, strings.Repeat("c", 32)},
	}
	for name, args := range cases {
		if err := e.svc.Webhook(e.ctx, args[0], args[1], "exists"); !errors.Is(err, forbidden) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := e.svc.Webhook(e.ctx, c.ChannelID, c.ChannelToken, "sync"); err != nil {
		t.Fatal(err)
	}
	if pending, _ := e.jobCounts(); pending != 0 {
		t.Fatal("sync notification enqueued a job")
	}
	e.webhook()
	if pending, _ := e.jobCounts(); pending != 1 {
		t.Fatal("notification did not enqueue a sync")
	}
	e.clock.Advance(7 * 24 * time.Hour)
	if err := e.svc.Webhook(e.ctx, c.ChannelID, c.ChannelToken, "exists"); !errors.Is(err, forbidden) {
		t.Fatalf("expired channel accepted: %v", err)
	}
}

func TestRenewContinuesPastFailingUsers(t *testing.T) {
	e := newEnv(t)
	e.connect()
	e.mustDrain()
	// 2人目の利用者（トークンが壊れている）。
	broken := Channel{UID: "u2", Token: strings.Repeat("d", 32), Expires: e.clock.Now().Add(time.Hour)}
	err := e.runner.Update(e.ctx, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.Set(channelRef(strings.Repeat("0", 31)+"1"), broken); err != nil {
			return err
		}
		return tx.Set(connectionRef("u2"), Connection{Connected: true, CalendarID: "cal-x", ChannelID: strings.Repeat("0", 31) + "1"})
	})
	if err != nil {
		t.Fatal(err)
	}
	e.google.clientErr["u2"] = apperr.External("external.failed", errors.New("revoked"))
	old := e.connection()
	e.clock.Advance(5*24*time.Hour + time.Hour)
	if err := e.svc.Renew(e.ctx); err != nil {
		t.Fatal(err)
	}
	renewed := e.connection()
	if renewed.ChannelID == old.ChannelID || len(e.cal.stopped) != 1 || e.cal.stopped[0] != old.ChannelID {
		t.Fatalf("channel not renewed: %+v stopped=%v", renewed, e.cal.stopped)
	}
	if err := e.svc.Webhook(e.ctx, renewed.ChannelID, renewed.ChannelToken, "exists"); err != nil {
		t.Fatalf("new channel rejected: %v", err)
	}
}

func TestRenewRedispatchesStaleJobs(t *testing.T) {
	e := newEnv(t)
	e.connect()
	e.disp.take() // 登録が失われたとみなす
	e.clock.Advance(time.Hour)
	if err := e.svc.Renew(e.ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.disp.take()) == 0 {
		t.Fatal("stale jobs not redispatched")
	}
}

func TestDisconnectClearsLinkageAndCancelsJobs(t *testing.T) {
	e := newEnv(t)
	e.connect()
	e.mustDrain()
	created := e.createTask("課題A", 60, e.clock.Now().Add(5*time.Hour))
	e.mustDrain()
	if !e.list().Linked[created.ID] {
		t.Fatal("task should be linked before disconnecting")
	}
	before := e.connection()
	// まだ実行していないジョブと、失敗したジョブを残しておく。
	e.createTask("課題B", 30, e.clock.Now().Add(6*time.Hour))
	e.disp.take()
	if pending, _ := e.jobCounts(); pending == 0 {
		t.Fatal("expected pending jobs before disconnecting")
	}
	tasksRev, calRev := e.revision(store.ResourceTasks), e.revision(store.ResourceCalendar)

	st, err := e.svc.Disconnect(e.ctx, uid)
	if err != nil || st.Connected || st.Sync != SyncDisconnected || !st.Enabled || st.FailedJobs != 0 {
		t.Fatalf("status after disconnect: %+v %v", st, err)
	}
	c := e.connection()
	if c.Connected || c.ChannelID != "" || c.ChannelToken != "" || c.ChannelResource != "" || c.SyncToken != "" || c.LastSync != nil || c.ContentSeq != nil {
		t.Fatalf("connection not cleared: %+v", c)
	}
	if c.CalendarID != before.CalendarID || c.GoogleID != before.GoogleID {
		t.Fatalf("calendar id should be kept for reconnecting: %+v", c)
	}
	if e.google.deleted[uid] != 1 || e.google.saved[uid] != nil {
		t.Fatalf("token not deleted: %v", e.google.deleted)
	}
	if len(e.cal.stopped) != 1 || e.cal.stopped[0] != before.ChannelID {
		t.Fatalf("channel not stopped: %v", e.cal.stopped)
	}
	if err := e.svc.Webhook(e.ctx, before.ChannelID, before.ChannelToken, "exists"); !errors.Is(err, apperr.Forbidden("not_found")) {
		t.Fatalf("old channel still accepted: %v", err)
	}
	if pending, failed := e.jobCounts(); pending != 0 || failed != 0 {
		t.Fatalf("jobs not cancelled: pending=%d failed=%d", pending, failed)
	}
	l := e.list()
	if len(l.Items) != 2 || len(l.Linked) != 0 {
		t.Fatalf("tasks must remain but unlinked: items=%d linked=%v", len(l.Items), l.Linked)
	}
	if _, linked := e.link(created.ID); linked {
		t.Fatal("link document kept")
	}
	if e.revision(store.ResourceTasks) != tasksRev+1 || e.revision(store.ResourceCalendar) <= calRev {
		t.Fatalf("revisions not bumped: tasks %d->%d calendar %d->%d", tasksRev, e.revision(store.ResourceTasks), calRev, e.revision(store.ResourceCalendar))
	}
	// 取り消したジョブは Cloud Tasks から呼ばれても何もしない。
	if errs := e.drain(); len(errs) != 0 {
		t.Fatalf("cancelled jobs ran: %v", errs)
	}
	// 課題の変更はジョブを積まない。
	e.createTask("課題C", 30, e.clock.Now().Add(7*time.Hour))
	if pending, _ := e.jobCounts(); pending != 0 {
		t.Fatal("jobs enqueued after disconnecting")
	}

	// 連携していないときに呼んでも 200 で、何も変えない。
	tasksRev, calRev = e.revision(store.ResourceTasks), e.revision(store.ResourceCalendar)
	if st, err := e.svc.Disconnect(e.ctx, uid); err != nil || st.Connected {
		t.Fatalf("second disconnect: %+v %v", st, err)
	}
	if e.revision(store.ResourceTasks) != tasksRev || e.revision(store.ResourceCalendar) != calRev || e.google.deleted[uid] != 1 {
		t.Fatal("disconnect while disconnected changed state")
	}

	// つなぎ直すと、同じカレンダーに予定を結び付け直す（報酬は二重に付与しない）。
	e.connect()
	e.mustDrain()
	if e.cal.calendars != 1 {
		t.Fatalf("calendar recreated: %d", e.cal.calendars)
	}
	l = e.list()
	if len(l.Items) != 3 || !l.Linked[created.ID] || e.rewards.total() != 900 {
		t.Fatalf("reconnect: items=%d linked=%v rewards=%d", len(l.Items), l.Linked, e.rewards.total())
	}
	if ev, ok := e.cal.event(EventID(uid, created.ID)); !ok || ev.Status != EventConfirmed {
		t.Fatalf("event lost on reconnect: %+v %v", ev, ok)
	}
}

func TestDisconnectSucceedsWhenTokenDeleteFails(t *testing.T) {
	e := newEnv(t)
	e.connect()
	e.mustDrain()
	e.google.deleteErr = apperr.External("external.failed", errors.New("secret manager down"))
	st, err := e.svc.Disconnect(e.ctx, uid)
	if err != nil || st.Connected {
		t.Fatalf("disconnect should not depend on the token store: %+v %v", st, err)
	}
	if e.connection().Connected {
		t.Fatal("still connected")
	}
}
