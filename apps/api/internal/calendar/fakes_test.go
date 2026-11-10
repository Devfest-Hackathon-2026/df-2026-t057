package calendar

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/memory"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/id"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/outbox"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
)

// fakeCalendar は Google カレンダーの偽物。変更の順番を記録し、syncToken はその位置を表す。
type fakeCalendar struct {
	mu        sync.Mutex
	events    map[string]Event
	log       []string
	etag      int
	calendars int
	// expireToken が true なら、次の syncToken つきの一覧で 410 を返す。
	expireToken bool
	// duplicate が true なら、一覧で同じ予定を2回返す（重複した通知）。
	duplicate bool
	patchErr  error
	watchErr  error
	// watchOnce が true なら、watchErr を1回だけ返す。
	watchOnce bool
	watches   []WatchRequest
	stopped   []string
	patches   int
	// primary は主カレンダーの予定。ranges は ListRange に渡された期間。
	primary []Event
	ranges  []string
	// reminders はカレンダーごとの既定の通知（何分前か）。remindersErr を返すと書き換えに失敗する。
	reminders    map[string][]int
	remindersErr error
}

func newFakeCalendar() *fakeCalendar {
	return &fakeCalendar{events: map[string]Event{}, reminders: map[string][]int{}}
}

func (f *fakeCalendar) SetDefaultReminders(_ context.Context, calendarID string, minutes []int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.remindersErr != nil {
		return f.remindersErr
	}
	f.reminders[calendarID] = slices.Clone(minutes)
	return nil
}

func (f *fakeCalendar) touch(e Event) Event {
	f.etag++
	e.ETag = fmt.Sprintf("etag-%d", f.etag)
	f.events[e.ID] = e
	f.log = append(f.log, e.ID)
	return e
}

func (f *fakeCalendar) CreateCalendar(context.Context, string, string, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calendars++
	return fmt.Sprintf("cal-%d", f.calendars), nil
}

func (f *fakeCalendar) InsertEvent(_ context.Context, _ string, e Event) (Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.events[e.ID]; ok {
		return Event{}, ErrEventExists
	}
	if e.Status == "" {
		e.Status = EventConfirmed
	}
	return f.touch(e), nil
}

func (f *fakeCalendar) GetEvent(_ context.Context, _, eventID string) (Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.events[eventID]
	if !ok || e.Status == EventCancelled {
		return Event{}, ErrEventGone
	}
	return e, nil
}

func (f *fakeCalendar) UpdateEvent(_ context.Context, _ string, e Event) (Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.events[e.ID]; !ok {
		return Event{}, ErrEventGone
	}
	return f.touch(e), nil
}

func (f *fakeCalendar) PatchEvent(_ context.Context, _, eventID, etag string, p EventPatch) (Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.patches++
	if err := f.patchErr; err != nil {
		f.patchErr = nil
		return Event{}, err
	}
	e, ok := f.events[eventID]
	if !ok || e.Status == EventCancelled {
		return Event{}, ErrEventGone
	}
	if e.ETag != etag {
		return Event{}, ErrPreconditionFailed
	}
	return f.touch(p.Apply(e)), nil
}

func (f *fakeCalendar) DeleteEvent(_ context.Context, _, eventID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.events[eventID]
	if !ok || e.Status == EventCancelled {
		return ErrEventGone
	}
	e.Status = EventCancelled
	f.touch(e)
	return nil
}

func (f *fakeCalendar) ListEvents(_ context.Context, _, syncToken, _ string) (EventPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []string
	if syncToken == "" {
		for _, eventID := range f.log {
			if !slices.Contains(ids, eventID) {
				ids = append(ids, eventID)
			}
		}
	} else {
		if f.expireToken {
			f.expireToken = false
			return EventPage{}, ErrSyncTokenExpired
		}
		n, err := strconv.Atoi(syncToken)
		if err != nil || n > len(f.log) {
			return EventPage{}, ErrSyncTokenExpired
		}
		ids = f.log[n:]
	}
	page := EventPage{NextSyncToken: strconv.Itoa(len(f.log))}
	for _, eventID := range ids {
		page.Events = append(page.Events, f.events[eventID])
		if f.duplicate {
			page.Events = append(page.Events, f.events[eventID])
		}
	}
	return page, nil
}

func (f *fakeCalendar) Watch(_ context.Context, _ string, req WatchRequest) (Watch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.watchErr; err != nil {
		if f.watchOnce {
			f.watchErr = nil
		}
		return Watch{}, err
	}
	f.watches = append(f.watches, req)
	return Watch{ResourceID: "res-" + req.ChannelID, Expires: req.Expires}, nil
}

func (f *fakeCalendar) StopChannel(_ context.Context, channelID, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, channelID)
	return nil
}

// userAdd と userEdit は、本人が Google カレンダーで予定を作る・編集する操作。
func (f *fakeCalendar) userAdd(e Event) Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e.Status == "" {
		e.Status = EventConfirmed
	}
	return f.touch(e)
}

func (f *fakeCalendar) userEdit(eventID string, edit func(e *Event)) Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.events[eventID]
	edit(&e)
	return f.touch(e)
}

func (f *fakeCalendar) ListRange(_ context.Context, calendarID string, from, to time.Time) ([]Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ranges = append(f.ranges, calendarID+" "+from.Format(time.RFC3339)+" "+to.Format(time.RFC3339))
	return f.primary, nil
}

func (f *fakeCalendar) event(eventID string) (Event, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.events[eventID]
	return e, ok
}

type fakeGoogle struct {
	cal       *fakeCalendar
	grant     Grant
	lastState string
	saved     map[string][]byte
	deleted   map[string]int
	deleteErr error
	clientErr map[string]error
}

func (g *fakeGoogle) AuthURL(state, loginHint string) string {
	g.lastState = state
	return "https://accounts.example/auth?state=" + state + "&login_hint=" + loginHint
}

func (g *fakeGoogle) Exchange(context.Context, string) (Grant, error) { return g.grant, nil }

func (g *fakeGoogle) SaveToken(_ context.Context, uid string, token []byte) error {
	g.saved[uid] = token
	return nil
}

func (g *fakeGoogle) DeleteToken(_ context.Context, uid string) error {
	if g.deleteErr != nil {
		return g.deleteErr
	}
	g.deleted[uid]++
	delete(g.saved, uid)
	return nil
}

func (g *fakeGoogle) Client(_ context.Context, uid string) (Client, error) {
	if err := g.clientErr[uid]; err != nil {
		return nil, err
	}
	if _, ok := g.saved[uid]; !ok {
		return nil, apperr.Forbidden("calendar.reauth_required")
	}
	return g.cal, nil
}

func (g *fakeGoogle) ClientFor(context.Context, []byte) (Client, error) { return g.cal, nil }

type fakePlanner struct {
	mu      sync.Mutex
	err     error
	details int
}

func (p *fakePlanner) Detail(_ context.Context, _ string, req task.DetailRequest) (task.DetailResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.details++
	if p.err != nil {
		return task.DetailResult{}, p.err
	}
	return task.DetailResult{Steps: []task.StepSpec{{Title: "準備", Minutes: 10}, {Title: req.Title, Minutes: req.Minutes - 10}}}, nil
}

func (p *fakePlanner) Extract(context.Context, string, string, time.Time, *time.Location) ([]task.Input, error) {
	return nil, apperr.Unavailable("ai.unavailable")
}

func (p *fakePlanner) Adjust(context.Context, string, task.AdjustRequest) ([]task.ChangeInput, error) {
	return nil, apperr.Unavailable("ai.unavailable")
}

type fakeSessions struct{ removed []string }

func (s *fakeSessions) TaskRemoved(_ context.Context, _ *store.Tx, _, taskID string) error {
	s.removed = append(s.removed, taskID)
	return nil
}

type fakeProfiles struct{ loc *time.Location }

func (p fakeProfiles) Context(context.Context, *store.Tx, string) (string, *time.Time, error) {
	return "", nil, nil
}
func (p fakeProfiles) Location(context.Context, *store.Tx, string) (*time.Location, error) {
	return p.loc, nil
}
func (p fakeProfiles) Locale(context.Context, *store.Tx, string) (locale.Locale, error) {
	return locale.Ja, nil
}
func (p fakeProfiles) SnoozeMinutes(context.Context, *store.Tx, string) (int, error) { return 10, nil }

// fakeRewards は登録の報酬を1課題に1回だけ付与する（wallet と同じべき等性）。
type fakeRewards struct{ granted map[string]int }

func (r *fakeRewards) GrantRegistration(_ context.Context, _ *store.Tx, _, taskID, _ string, _ time.Time) (int, error) {
	if _, ok := r.granted[taskID]; ok {
		return 0, nil
	}
	r.granted[taskID] = 300
	return 300, nil
}

func (r *fakeRewards) total() int {
	sum := 0
	for _, v := range r.granted {
		sum += v
	}
	return sum
}

type fakeSleeps struct{ schedule *sleep.Schedule }

func (s *fakeSleeps) GetTx(context.Context, *store.Tx, string, time.Time) (sleep.State, error) {
	if s.schedule == nil {
		return sleep.State{}, nil
	}
	return sleep.State{Sleep: &sleep.Sleep{Schedule: *s.schedule}}, nil
}

type dispatched struct{ uid, jobID string }

type fakeDispatcher struct {
	mu   sync.Mutex
	jobs []dispatched
}

func (d *fakeDispatcher) Dispatch(_ context.Context, uid, jobID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.jobs = append(d.jobs, dispatched{uid, jobID})
	return nil
}

func (d *fakeDispatcher) take() []dispatched {
	d.mu.Lock()
	defer d.mu.Unlock()
	jobs := d.jobs
	d.jobs = nil
	return jobs
}

type env struct {
	t        *testing.T
	ctx      context.Context
	runner   *store.Runner
	clock    *clock.Fake
	outbox   *outbox.Outbox
	disp     *fakeDispatcher
	cal      *fakeCalendar
	google   *fakeGoogle
	planner  *fakePlanner
	sessions *fakeSessions
	rewards  *fakeRewards
	sleeps   *fakeSleeps
	tasks    *task.Service
	enq      *Enqueuer
	svc      *Service
}

const uid = "u1"

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, ctx: context.Background()}
	e.runner = store.NewRunner(memory.NewStore())
	e.clock = clock.NewFake(time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC))
	ids := &id.Sequence{}
	log := slog.New(slog.DiscardHandler)
	e.disp = &fakeDispatcher{}
	e.outbox = outbox.New(e.runner, e.disp, e.clock, ids, log)
	e.cal = newFakeCalendar()
	e.google = &fakeGoogle{cal: e.cal, grant: Grant{Subject: "google-1", HasRefreshToken: true, PrimaryEvents: true, Token: []byte("token")}, saved: map[string][]byte{}, deleted: map[string]int{}, clientErr: map[string]error{}}
	e.planner = &fakePlanner{err: apperr.Unavailable("ai.unavailable")}
	e.sessions = &fakeSessions{}
	e.rewards = &fakeRewards{granted: map[string]int{}}
	e.sleeps = &fakeSleeps{}
	e.enq = NewEnqueuer(e.outbox)
	profiles := fakeProfiles{loc: time.UTC}
	e.tasks = task.New(e.runner, e.clock, ids, task.Deps{Planner: e.planner, Calendar: e.enq, Sessions: e.sessions, Profiles: profiles, Rewards: e.rewards})
	e.svc = NewService(e.runner, e.clock, ids, e.outbox, Config{Enabled: true, AppURL: "https://app.example"}, Deps{
		Tasks: e.tasks, Sleeps: e.sleeps, Profiles: profiles, Planner: e.planner, Google: e.google, Log: log,
	})
	e.svc.Register(e.outbox)
	return e
}

// drain は登録されたジョブを、新しいジョブが無くなるまで実行する。失敗したジョブのエラーを返す。
func (e *env) drain() []error {
	e.t.Helper()
	var errs []error
	for range 50 {
		jobs := e.disp.take()
		if len(jobs) == 0 {
			return errs
		}
		for _, j := range jobs {
			if err := e.outbox.Run(e.ctx, j.uid, j.jobID); err != nil {
				errs = append(errs, err)
			}
		}
	}
	e.t.Fatal("jobs never settled")
	return nil
}

func (e *env) mustDrain() {
	e.t.Helper()
	if errs := e.drain(); len(errs) > 0 {
		e.t.Fatalf("jobs failed: %v", errors.Join(errs...))
	}
}

func (e *env) connect() {
	e.t.Helper()
	if _, err := e.svc.Connect(e.ctx, uid, "google-1", "user@example.com"); err != nil {
		e.t.Fatal(err)
	}
	if got := e.svc.Callback(e.ctx, e.google.lastState, "code", ""); got != "https://app.example/?calendar=connected" {
		e.t.Fatalf("callback: %s", got)
	}
}

func (e *env) connection() Connection {
	e.t.Helper()
	var c Connection
	err := e.runner.View(e.ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		c, err = loadConnection(ctx, tx, uid)
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return c
}

func (e *env) link(taskID string) (Link, bool) {
	e.t.Helper()
	var l Link
	var found bool
	err := e.runner.View(e.ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		l, found, err = store.Get[Link](ctx, tx, linkRef(uid, taskID))
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return l, found
}

func (e *env) revision(r store.Resource) int64 {
	e.t.Helper()
	var rev int64
	err := e.runner.View(e.ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		rev, err = tx.Revision(ctx, uid, r)
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return rev
}

func (e *env) jobCounts() (pending, failed int) {
	e.t.Helper()
	err := e.runner.View(e.ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		pending, failed, err = outbox.Counts(ctx, tx, uid)
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return pending, failed
}

func (e *env) list() task.List {
	e.t.Helper()
	l, err := e.tasks.List(e.ctx, uid)
	if err != nil {
		e.t.Fatal(err)
	}
	return l
}

func (e *env) createTask(title string, minutes int, deadline time.Time) task.Task {
	e.t.Helper()
	created, err := e.tasks.Create(e.ctx, uid, task.CreateRequest{Input: task.Input{Title: title, Deadline: deadline, Minutes: minutes}})
	if err != nil {
		e.t.Fatal(err)
	}
	return created.Task
}

func (e *env) webhook() {
	e.t.Helper()
	c := e.connection()
	if err := e.svc.Webhook(e.ctx, c.ChannelID, c.ChannelToken, "exists"); err != nil {
		e.t.Fatal(err)
	}
}
