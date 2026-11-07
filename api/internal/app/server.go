package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	firebaseauth "firebase.google.com/go/v4/auth"
)

type Server struct {
	Now       func() time.Time
	Store     Store
	Planner   *Planner
	Calendar  *Calendar
	Auth      *firebaseauth.Client
	Mode      string
	Project   string
	AppURL    string
	Firebase  map[string]string
	Assistant AssistantStore
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

var requestIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)
var tokenIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", s.config)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /api/extension/token", s.protected(s.extensionToken))
	mux.HandleFunc("GET /api/state", s.protected(s.state))
	mux.HandleFunc("GET /api/assistant", s.protected(s.assistantHistory))
	mux.HandleFunc("POST /api/assistant/messages", s.protected(s.assistantMessage))
	mux.HandleFunc("POST /api/assistant/memory", s.protected(s.remember))
	mux.HandleFunc("POST /api/assistant/preview", s.protected(s.assistantPreview))
	mux.HandleFunc("POST /api/companion/line", s.protected(s.companionLine))
	mux.HandleFunc("PUT /api/sleep", s.protected(s.setSleep))
	mux.HandleFunc("POST /api/sleep/morning", s.protected(s.morning))
	mux.HandleFunc("POST /api/tasks", s.protected(s.addTask))
	mux.HandleFunc("POST /api/tasks/{id}/preview", s.protected(s.previewTaskEdit))
	mux.HandleFunc("PUT /api/tasks/{id}", s.protected(s.updateTask))
	mux.HandleFunc("DELETE /api/tasks/{id}", s.protected(s.deleteTask))
	mux.HandleFunc("POST /api/parse", s.protected(s.parse))
	mux.HandleFunc("PUT /api/settings", s.protected(s.settings))
	mux.HandleFunc("PUT /api/profile", s.protected(s.profile))
	mux.HandleFunc("POST /api/tasks/{id}/start", s.protected(s.start))
	mux.HandleFunc("POST /api/tasks/{id}/complete", s.protected(s.complete))
	mux.HandleFunc("POST /api/session/pause", s.protected(s.pause))
	mux.HandleFunc("POST /api/session/confirm", s.protected(s.confirmSession))
	mux.HandleFunc("PUT /api/time-policy", s.protected(s.timePolicy))
	mux.HandleFunc("POST /api/viewing/start", s.protected(s.beginViewing))
	mux.HandleFunc("POST /api/viewing/end", s.protected(s.finishViewing))
	mux.HandleFunc("POST /api/viewing", s.protected(s.viewing))
	mux.HandleFunc("GET /api/tasks/{id}/draft", s.protected(s.draft))
	mux.HandleFunc("POST /api/draft/adjust", s.protected(s.adjustDraft))
	mux.HandleFunc("POST /api/draft/commit", s.protected(s.commitDraft))
	mux.HandleFunc("POST /api/demo/sample", s.protected(s.sample))
	mux.HandleFunc("POST /api/calendar/connect", s.protected(s.connectCalendar))
	mux.HandleFunc("GET /api/calendar/callback", s.calendarCallback)
	mux.HandleFunc("POST /api/calendar/sync", s.protected(s.syncCalendar))
	mux.HandleFunc("POST /api/calendar/webhook", s.calendarWebhook)
	mux.HandleFunc("POST /api/calendar/renew", s.renewCalendar)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != "GET" && r.Header.Get("Origin") != "" {
			origin := r.Header.Get("Origin")
			if origin != s.AppURL && !strings.HasPrefix(origin, "chrome-extension://") {
				writeError(w, 403, errors.New("このページからの操作は許可されていません"))
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func (s *Server) config(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"mode": s.Mode, "projectId": s.Project, "firebase": s.Firebase, "aiEnabled": s.Planner != nil && s.Planner.Client != nil, "calendarEnabled": s.Calendar != nil})
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	state, err := s.Store.Read(r.Context(), uid(r))
	if err == nil {
		prepareDraft(&state, s.now())
	}
	respond(w, state, err)
}
func validateInput(t TaskInput, now time.Time) error {
	if len(strings.TrimSpace(t.Title)) < 1 || utf8.RuneCountInString(t.Title) > 200 {
		return errors.New("課題名を1〜200文字で入力してください")
	}
	if t.Minutes < 1 || t.Minutes > 720 {
		return errors.New("必要時間は1〜720分で入力してください")
	}
	if t.Deadline.Before(now) {
		return errors.New("締切は現在より後にしてください")
	}
	if utf8.RuneCountInString(t.Notes) > 4000 {
		return errors.New("メモは4000文字以内にしてください")
	}
	if utf8.RuneCountInString(t.Why) > 200 {
		return errors.New("やる理由は200文字以内にしてください")
	}
	if t.URL != "" {
		u, err := url.Parse(t.URL)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			return errors.New("作業リンクはhttpまたはhttpsのURLにしてください")
		}
	}
	return nil
}
func (s *Server) addTask(w http.ResponseWriter, r *http.Request) {
	var input TaskInput
	if !decode(w, r, &input) {
		return
	}
	if err := validateInput(input, s.now()); err != nil {
		respond(w, nil, err)
		return
	}
	if input.ID == "" {
		input.ID = newID()
	}
	if !requestIDPattern.MatchString(input.ID) {
		respond(w, nil, errors.New("課題IDの形式を確認してください"))
		return
	}
	state, err := s.createTask(r.Context(), uid(r), input)
	respond(w, state, err)
}
func (s *Server) createTask(ctx context.Context, id string, input TaskInput) (State, error) {
	current, err := s.Store.Read(ctx, id)
	if err != nil {
		return State{}, err
	}
	if _, err := current.task(input.ID); err == nil {
		if s.Calendar != nil && current.CalendarSync == "pending" {
			return s.Calendar.Export(ctx, id, current)
		}
		return current, nil
	}
	if input.BaseRevision != nil && *input.BaseRevision != current.PlanRevision {
		return State{}, ErrConflict
	}
	if len(current.Tasks) >= 200 {
		return State{}, errors.New("課題は200件までです。完了済みの課題を整理してください")
	}
	if s.Calendar != nil && !current.CalendarConnected {
		return State{}, errors.New("先にGoogleカレンダーを連携してください")
	}
	task := Task{ID: input.ID, Title: strings.TrimSpace(input.Title), Notes: input.Notes, Why: strings.TrimSpace(input.Why), Deadline: input.Deadline, Minutes: input.Minutes, URL: input.URL, Status: "pending", CreatedAt: s.now()}
	task.Plan, err = s.Planner.Detail(ctx, id, task, current.Profile)
	if err != nil {
		return State{}, err
	}
	eventID := ""
	if s.Calendar != nil {
		event, err := s.Calendar.CreateEvent(ctx, id, input)
		if err != nil {
			return State{}, err
		}
		eventID = event.Id
		task.CalendarETag = event.Etag
		task.InputHash = eventHash(event)
	}
	state, err := s.Store.Update(ctx, id, func(st *State) error {
		if _, err := st.task(input.ID); err == nil {
			return nil
		}
		if input.BaseRevision != nil && *input.BaseRevision != st.PlanRevision {
			return ErrConflict
		}
		if len(st.Tasks) >= 200 {
			return errors.New("課題は200件までです。完了済みの課題を整理してください")
		}
		task.CalendarEventID = eventID
		st.Tasks = append(st.Tasks, task)
		st.grant("registration", input.ID, 300, s.now())
		st.replan()
		prepareDraft(st, s.now())
		if eventID != "" {
			st.CalendarSync = "pending"
		}
		return nil
	})
	if err == nil && s.Calendar != nil {
		state, err = s.Calendar.Export(ctx, id, state)
	}
	return state, err
}
func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.Calendar != nil {
		st, err := s.Store.Read(r.Context(), uid(r))
		if err != nil {
			respond(w, nil, err)
			return
		}
		t, err := st.task(id)
		if err != nil {
			respond(w, nil, err)
			return
		}
		if t.CalendarEventID != "" {
			if err = s.Calendar.DeleteEvent(r.Context(), uid(r), t.CalendarEventID); err != nil {
				respond(w, nil, err)
				return
			}
		}
	}
	st, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		_, err := st.task(id)
		if err != nil {
			return err
		}
		for i, t := range st.Tasks {
			if t.ID == id {
				st.Tasks = append(st.Tasks[:i], st.Tasks[i+1:]...)
				break
			}
		}
		if st.Session != nil && st.Session.TaskID == id {
			st.Session = nil
		}
		st.replan()
		prepareDraft(st, s.now())
		return nil
	})
	respond(w, st, err)
}
func (s *Server) parse(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &in) {
		return
	}
	st, err := s.Store.Read(r.Context(), uid(r))
	if err != nil {
		respond(w, nil, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	tasks, err := s.Planner.Parse(ctx, uid(r), in.Text, st.Profile)
	respond(w, map[string]any{"tasks": tasks}, err)
}
func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	var in Settings
	if !decode(w, r, &in) {
		return
	}
	if in.SnoozeMinutes < 1 || in.SnoozeMinutes > 120 || (in.Mode != "pachinko" && in.Mode != "mahjong" && in.Mode != "simple") {
		respond(w, nil, errors.New("設定値を確認してください"))
		return
	}
	st, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		if st.Settings != in {
			st.PlanRevision++
		}
		st.Settings = in
		prepareDraft(st, s.now())
		return nil
	})
	respond(w, st, err)
}
func (s *Server) profile(w http.ResponseWriter, r *http.Request) {
	var in Profile
	if !decode(w, r, &in) {
		return
	}
	if utf8.RuneCountInString(in.Name) > 100 || utf8.RuneCountInString(in.Context) > 2000 {
		respond(w, nil, errors.New("入力が長すぎます"))
		return
	}
	if _, err := time.LoadLocation(in.TimeZone); err != nil {
		respond(w, nil, errors.New("タイムゾーンを確認してください"))
		return
	}
	zoneChanged := false
	st, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		if st.Profile.Context != in.Context {
			now := s.now()
			st.AssistantContextSince = &now
		}
		zoneChanged = st.Profile.TimeZone != in.TimeZone
		st.Profile = in
		return nil
	})
	if err == nil && zoneChanged {
		st, err = s.moveSleepEvent(r.Context(), uid(r), st)
	}
	respond(w, st, err)
}
func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	st, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		t, err := st.task(r.PathValue("id"))
		if err != nil {
			return err
		}
		if t.Status == "completed" {
			return errors.New("この課題は完了済みです")
		}
		if st.Session != nil && st.Session.TaskID != t.ID {
			return errors.New("別の課題を進行中です。完了してから開始してください")
		}
		if st.Session == nil {
			now := s.now()
			if t.StartedAt == nil {
				t.StartedAt, t.ScheduledStartAt = &now, &t.Plan.Start
			}
			limit := max(60, int(t.Plan.End.Sub(t.Plan.Start).Seconds()))
			if st.TimePolicy != nil {
				limit = min(limit, 25*60)
			}
			st.Session = &Session{ID: newID(), TaskID: t.ID, StartedAt: now, LimitSeconds: limit, RunningSince: &now}
			st.PlanRevision++
		}
		t.Status = "active"
		prepareDraft(st, s.now())
		return nil
	})
	respond(w, st, err)
}
func (s *Server) pause(w http.ResponseWriter, r *http.Request) {
	st, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		if st.Session == nil {
			return errors.New("進行中の課題がありません")
		}
		now := s.now()
		if st.Session.PausedAt == nil {
			st.closeWorkInterval(now)
			st.Session.PausedAt = &now
		} else {
			st.Session.PausedSeconds += int(now.Sub(*st.Session.PausedAt).Seconds())
			st.Session.PausedAt = nil
			st.Session.RunningSince = &now
		}
		return nil
	})
	respond(w, st, err)
}
func (s *Server) complete(w http.ResponseWriter, r *http.Request) {
	st, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		t, err := st.task(r.PathValue("id"))
		if err != nil {
			return err
		}
		if t.Status == "completed" {
			return nil
		}
		now := s.now()
		t.Status = "completed"
		t.CompletedAt = &now
		if st.Session != nil && st.Session.TaskID == t.ID {
			st.confirmWork(now)
			st.Session = nil
		}
		st.PlanRevision++
		prepareDraft(st, now)
		return nil
	})
	respond(w, st, err)
}
func (s *Server) viewing(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID        string `json:"id"`
		Seconds   int    `json:"seconds"`
		SessionID string `json:"sessionId"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.ID) < 8 || len(in.ID) > 80 || in.Seconds < 1 || in.Seconds > 65 {
		respond(w, nil, errors.New("視聴時間を確認してください"))
		return
	}
	st, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		if st.ViewingReceipts == nil {
			st.ViewingReceipts = map[string]bool{}
		}
		if st.ViewingReceipts[in.ID] {
			return nil
		}
		now := s.now()
		seconds := in.Seconds
		if st.TimePolicy != nil && st.TimePolicy.Mode != "self" {
			x := st.ViewingSession
			if x == nil || x.ID != in.SessionID || x.EndedAt != nil {
				return errors.New("視聴する長さを選んでから再生してください")
			}
			seconds = min(seconds, max(0, int(now.Sub(x.LastReportAt).Seconds())), x.PlannedSeconds-x.ConsumedSeconds, st.viewingAvailable())
			x.ConsumedSeconds += seconds
			x.LastReportAt = now
			if x.ConsumedSeconds >= x.PlannedSeconds || st.viewingAvailable() <= seconds {
				x.EndedAt, x.Reason = &now, "limit"
			}
		} else if st.LastViewingAt != nil {
			delta := int(now.Sub(*st.LastViewingAt).Seconds())
			if delta < seconds {
				seconds = delta
			}
		}
		if seconds < 0 {
			seconds = 0
		}
		if st.TimePolicy != nil {
			st.TimePolicy.ViewedSeconds += seconds
		}
		limited := st.TimePolicy == nil || st.TimePolicy.Mode == "rewards" || st.TimePolicy.Mode == "fixed"
		if limited && seconds > st.BalanceSeconds {
			seconds = st.BalanceSeconds
		}
		if limited {
			st.BalanceSeconds -= seconds
		}
		st.LastViewingAt = &now
		if night, ok := st.nightAt(now); ok && seconds > 0 {
			st.Sleep.Watched = night
		}
		if len(st.ViewingReceipts) > 2000 {
			st.ViewingReceipts = map[string]bool{}
		}
		st.ViewingReceipts[in.ID] = true
		return nil
	})
	respond(w, st, err)
}
func prepareDraft(st *State, now time.Time) {
	tasks := st.pending()
	st.NextDraft = nil
	if len(tasks) == 0 {
		return
	}
	start := tasks[0].Plan.Start.Add(time.Duration(st.Settings.SnoozeMinutes) * time.Minute)
	if start.Before(now) {
		start = now.Add(time.Duration(st.Settings.SnoozeMinutes) * time.Minute)
	}
	draft, err := MakeDraft(*st, tasks[0].ID, start, now)
	if err == nil {
		st.NextDraft = &draft
	}
}
func (s *Server) draft(w http.ResponseWriter, r *http.Request) {
	st, err := s.Store.Read(r.Context(), uid(r))
	if err != nil {
		respond(w, nil, err)
		return
	}
	t, err := st.task(r.PathValue("id"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	start := s.now().Add(time.Duration(st.Settings.SnoozeMinutes) * time.Minute)
	if r.URL.Query().Get("adjust") == "true" {
		start = t.Plan.Start
		if start.Before(s.now()) {
			start = s.now().Add(time.Minute)
		}
	}
	if requested := r.URL.Query().Get("start"); requested != "" {
		start, err = time.Parse(time.RFC3339, requested)
		if err != nil {
			respond(w, nil, errors.New("開始時刻を確認してください"))
			return
		}
		revision, err := strconv.Atoi(r.URL.Query().Get("revision"))
		if err != nil || revision != st.PlanRevision {
			respond(w, nil, ErrConflict)
			return
		}
	}
	draft, err := MakeDraft(st, t.ID, start, s.now())
	respond(w, draft, err)
}
func (s *Server) adjustDraft(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Draft       Draft  `json:"draft"`
		Instruction string `json:"instruction"`
	}
	if !decode(w, r, &in) {
		return
	}
	st, err := s.Store.Read(r.Context(), uid(r))
	if err != nil {
		respond(w, nil, err)
		return
	}
	if st.PlanRevision != in.Draft.BaseRevision {
		respond(w, nil, ErrConflict)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	draft, err := s.Planner.Adjust(ctx, uid(r), in.Draft, in.Instruction, st.Profile)
	if err == nil {
		allowed := map[string]bool{}
		for _, change := range in.Draft.Changes {
			allowed[change.TaskID] = true
		}
		if len(draft.Changes) != len(in.Draft.Changes) {
			respond(w, nil, errors.New("課題の数が変わっています。調整をやり直してください"))
			return
		}
		for i := range draft.Changes {
			change := &draft.Changes[i]
			if !allowed[change.TaskID] {
				respond(w, nil, errors.New("調整対象の課題が変わっています。やり直してください"))
				return
			}
			task, taskErr := st.task(change.TaskID)
			if taskErr != nil {
				respond(w, nil, taskErr)
				return
			}
			change.Title = task.Title
			change.Before = task.Plan
			change.OriginalMinutes = task.Minutes
			change.PlannedMinutes = change.After.End.Sub(change.After.Start).Minutes()
		}
		err = ValidateChanges(draft.Changes, st, s.now())
	}
	respond(w, draft, err)
}
func (s *Server) commitDraft(w http.ResponseWriter, r *http.Request) {
	var draft Draft
	if !decode(w, r, &draft) {
		return
	}
	if s.Calendar != nil {
		if _, err := s.Calendar.Sync(r.Context(), uid(r)); err != nil {
			respond(w, nil, err)
			return
		}
	}
	state, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		if draft.BaseRevision != st.PlanRevision {
			return ErrConflict
		}
		if err := ValidateChanges(draft.Changes, *st, s.now()); err != nil {
			return err
		}
		for _, c := range draft.Changes {
			t, _ := st.task(c.TaskID)
			t.Plan = c.After
		}
		st.PlanRevision++
		prepareDraft(st, s.now())
		if st.CalendarConnected {
			st.CalendarSync = "pending"
		}
		return nil
	})
	if err == nil && s.Calendar != nil {
		state, err = s.Calendar.Export(r.Context(), uid(r), state)
	}
	respond(w, state, err)
}
func (s *Server) sample(w http.ResponseWriter, r *http.Request) {
	if s.Mode != "demo" {
		writeError(w, 403, errors.New("サンプルはデモ環境でのみ作成できます"))
		return
	}
	now := s.now()
	st, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		if len(st.Tasks) > 0 {
			return errors.New("サンプルは課題がないときだけ追加できます")
		}
		for i, title := range []string{"レポートの構成をまとめる", "演習問題を解く", "見直して提出する"} {
			id := newID()
			st.Tasks = append(st.Tasks, Task{ID: id, Title: title, Minutes: []int{30, 30, 15}[i], Deadline: now.Add(90 * time.Minute), Status: "pending", CreatedAt: now})
			st.grant("registration", id, 300, now)
		}
		st.replan()
		prepareDraft(st, now)
		return nil
	})
	respond(w, st, err)
}
func (s *Server) extensionToken(w http.ResponseWriter, r *http.Request) {
	if s.Mode != "demo" {
		writeError(w, 400, errors.New("FirebaseのIDトークンをご利用ください"))
		return
	}
	writeJSON(w, 200, map[string]string{"token": "Demo " + strings.TrimPrefix(uid(r), "demo-")})
}
func sourceError(operation string, err error) error {
	var limit *AILimitError
	if errors.As(err, &limit) || errors.Is(err, ErrAIUsageUnavailable) {
		return err
	}
	LogFailure(operation, err)
	return fmt.Errorf("%sに失敗しました。入力は残っています。もう一度お試しください", operation)
}
