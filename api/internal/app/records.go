package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/a-sumikawa/devfest-2026/api/internal/records"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type historyStore interface {
	History(context.Context, string) (records.History, error)
	HistorySince(context.Context, string, time.Time) (records.History, error)
	Archived(context.Context, string) ([]Task, error)
}

func historyDelta(before, after State, now time.Time) ([]records.Completion, []records.Event, []Task) {
	completions := []records.Completion{}
	events := []records.Event{}
	archived := []Task{}
	for _, t := range after.Tasks {
		previous, previousErr := before.task(t.ID)
		if t.Status == "completed" && t.CompletedAt != nil && (previousErr != nil || previous.Status != "completed") {
			c := records.Completion{TaskID: t.ID, Title: t.Title, At: *t.CompletedAt, EstimateSeconds: t.Minutes * 60, StartedAt: t.StartedAt, ScheduledStartAt: t.ScheduledStartAt}
			if t.StartedAt != nil {
				seconds := 0
				c.WorkSeconds = &seconds
			}
			for _, e := range after.Ledger {
				if e.TaskID == t.ID && (e.Kind == "work" || e.Kind == "completion") {
					c.RewardSeconds += e.Seconds
					if c.WorkSeconds != nil {
						*c.WorkSeconds += e.WorkSeconds
					}
				}
			}
			completions = append(completions, c)
		}
		previous, err := before.task(t.ID)
		if err == nil && previous.StartedAt == nil && t.StartedAt != nil {
			events = append(events, records.Event{ID: "start:" + t.ID, TaskID: t.ID, Kind: records.Started, At: *t.StartedAt, ScheduledAt: t.ScheduledStartAt})
		}
	}
	for _, t := range before.Tasks {
		if t.Status == "completed" {
			if _, err := after.task(t.ID); err != nil {
				archived = append(archived, t)
				historical, _, _ := historyDelta(State{}, State{Tasks: []Task{t}, Ledger: before.Ledger}, now)
				completions = append(completions, historical...)
			}
		}
	}
	if before.Session != nil && after.Session != nil && before.Session.ID == after.Session.ID {
		a, b := before.Session.PausedAt, after.Session.PausedAt
		if (a == nil) != (b == nil) {
			kind := records.Resumed
			if b != nil {
				kind = records.Paused
			}
			events = append(events, records.Event{ID: after.Session.ID + ":" + string(kind) + ":" + strconv.Itoa(after.Revision+1), TaskID: after.Session.TaskID, Kind: kind, At: now})
		}
	}
	if after.LastSnoozeID != "" && after.LastSnoozeID != before.LastSnoozeID {
		events = append(events, records.Event{ID: "snooze:" + after.LastSnoozeID, TaskID: after.LastSnoozeTask, Kind: records.Snoozed, At: now})
	}
	applied := map[string]bool{}
	for _, e := range before.Ledger {
		applied[e.ID] = true
	}
	for _, e := range after.Ledger {
		if e.Kind == "work" && !applied[e.ID] {
			events = append(events, records.Event{ID: e.ID, TaskID: e.TaskID, Kind: records.WorkRecorded, At: e.At, Seconds: e.WorkSeconds})
		}
	}
	return completions, events, archived
}
func (m *MemoryStore) History(_ context.Context, id string) (records.History, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.histories[id]
	raw, err := json.Marshal(h)
	if err != nil {
		return h, err
	}
	err = json.Unmarshal(raw, &h)
	if h.Completions == nil {
		h.Completions = []records.Completion{}
	}
	if h.Events == nil {
		h.Events = []records.Event{}
	}
	return h, err
}
func (m *MemoryStore) Archived(_ context.Context, id string) ([]Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Task{}
	for _, t := range m.archived[id] {
		out = append(out, t)
	}
	return out, nil
}
func (m *MemoryStore) saveHistory(id string, before, after State, now time.Time) {
	if m.histories == nil {
		m.histories = map[string]records.History{}
	}
	if m.archived == nil {
		m.archived = map[string]map[string]Task{}
	}
	h := m.histories[id]
	cs, es, as := historyDelta(before, after, now)
	for _, c := range cs {
		found := false
		for _, old := range h.Completions {
			if old.TaskID == c.TaskID {
				found = true
				break
			}
		}
		if !found {
			h.Completions = append(h.Completions, c)
		}
	}
	for _, event := range es {
		found := false
		for _, old := range h.Events {
			if old.ID == event.ID {
				found = true
				break
			}
		}
		if !found {
			h.Events = append(h.Events, event)
		}
	}
	m.histories[id] = h
	if m.archived[id] == nil {
		m.archived[id] = map[string]Task{}
	}
	for _, t := range as {
		m.archived[id][t.ID] = t
	}
}
func (f *FirestoreStore) saveHistory(tx *firestore.Transaction, uid string, before, after State, now time.Time) error {
	cs, es, as := historyDelta(before, after, now)
	root := f.DB.Collection("users").Doc(uid)
	// Complete records are immutable; read all existence checks before any writes.
	missing := []records.Completion{}
	for _, c := range cs {
		_, err := tx.Get(root.Collection("completions").Doc(c.TaskID))
		if status.Code(err) == codes.NotFound {
			missing = append(missing, c)
		} else if err != nil {
			return err
		}
	}
	missingEvents := []records.Event{}
	for _, event := range es {
		_, err := tx.Get(root.Collection("activityEvents").Doc(event.ID))
		if status.Code(err) == codes.NotFound {
			missingEvents = append(missingEvents, event)
		} else if err != nil {
			return err
		}
	}
	for _, c := range missing {
		if err := tx.Create(root.Collection("completions").Doc(c.TaskID), map[string]any{"record": c, "at": c.At}); err != nil {
			return err
		}
	}
	for _, e := range missingEvents {
		if err := tx.Create(root.Collection("activityEvents").Doc(e.ID), map[string]any{"record": e, "at": e.At}); err != nil {
			return err
		}
	}
	for _, t := range as {
		raw, err := json.Marshal(t)
		if err != nil {
			return err
		}
		if err = tx.Set(root.Collection("archivedTasks").Doc(t.ID), map[string]any{"task": string(raw)}); err != nil {
			return err
		}
	}
	return nil
}
func (f *FirestoreStore) History(ctx context.Context, id string) (records.History, error) {
	return f.HistorySince(ctx, id, time.Time{})
}
func (f *FirestoreStore) HistorySince(ctx context.Context, id string, since time.Time) (records.History, error) {
	h := records.History{Completions: []records.Completion{}, Events: []records.Event{}}
	root := f.DB.Collection("users").Doc(id)
	for _, name := range []string{"completions", "activityEvents"} {
		query := root.Collection(name).Query
		if !since.IsZero() {
			query = query.Where("at", ">=", since)
		}
		docs, err := query.Documents(ctx).GetAll()
		if err != nil {
			return h, err
		}
		for _, doc := range docs {
			var wrapper struct {
				Record map[string]any `firestore:"record"`
			}
			if err = doc.DataTo(&wrapper); err != nil {
				return h, err
			}
			raw, err := json.Marshal(wrapper.Record)
			if err != nil {
				return h, err
			}
			if name == "completions" {
				var c records.Completion
				if err = json.Unmarshal(raw, &c); err != nil {
					return h, err
				}
				h.Completions = append(h.Completions, c)
			} else {
				var e records.Event
				if err = json.Unmarshal(raw, &e); err != nil {
					return h, err
				}
				h.Events = append(h.Events, e)
			}
		}
	}
	return h, nil
}
func (f *FirestoreStore) Archived(ctx context.Context, id string) ([]Task, error) {
	docs, err := f.DB.Collection("users").Doc(id).Collection("archivedTasks").Documents(ctx).GetAll()
	out := []Task{}
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		raw, err := doc.DataAt("task")
		if err != nil {
			return nil, err
		}
		var t Task
		if err = json.Unmarshal([]byte(raw.(string)), &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

type recordCursor struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func (s *Server) records(w http.ResponseWriter, r *http.Request) {
	store, ok := s.Store.(historyStore)
	if !ok {
		respond(w, nil, errors.New("記録の保存先がありません"))
		return
	}
	st, err := s.Store.Read(r.Context(), uid(r))
	if err != nil {
		respond(w, nil, err)
		return
	}
	loc, err := time.LoadLocation(st.Profile.TimeZone)
	if err != nil {
		respond(w, nil, err)
		return
	}
	local := s.now().In(loc)
	since := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -83)
	h, err := store.HistorySince(r.Context(), uid(r), since)
	if err != nil {
		respond(w, nil, err)
		return
	}
	existing := map[string]bool{}
	for _, c := range h.Completions {
		existing[c.TaskID] = true
	}
	legacy, _, _ := historyDelta(State{}, st, s.now())
	for _, c := range legacy {
		if !existing[c.TaskID] && !c.At.Before(since) {
			h.Completions = append(h.Completions, c)
		}
	}
	observed := map[string]bool{}
	for _, event := range h.Events {
		observed[event.ID] = true
	}
	addStart := func(id string, at, scheduled *time.Time) {
		key := "start:" + id
		if at != nil && !at.Before(since) && !observed[key] {
			h.Events = append(h.Events, records.Event{ID: key, TaskID: id, Kind: records.Started, At: *at, ScheduledAt: scheduled})
			observed[key] = true
		}
	}
	for _, task := range st.Tasks {
		addStart(task.ID, task.StartedAt, task.ScheduledStartAt)
	}
	for _, completion := range h.Completions {
		addStart(completion.TaskID, completion.StartedAt, completion.ScheduledStartAt)
	}
	for _, entry := range st.Ledger {
		if entry.Kind == "work" && !entry.At.Before(since) && !observed[entry.ID] {
			h.Events = append(h.Events, records.Event{ID: entry.ID, TaskID: entry.TaskID, Kind: records.WorkRecorded, At: entry.At, Seconds: entry.WorkSeconds})
			observed[entry.ID] = true
		}
	}
	sort.Slice(h.Completions, func(i, j int) bool {
		a, b := h.Completions[i], h.Completions[j]
		if a.At.Equal(b.At) {
			return a.TaskID > b.TaskID
		}
		return a.At.After(b.At)
	})
	summary, err := records.Summarize(h, s.now(), st.Profile.TimeZone)
	if err != nil {
		respond(w, nil, err)
		return
	}
	activity, err := records.BuildActivity(h, s.now(), st.Profile.TimeZone)
	if err != nil {
		respond(w, nil, err)
		return
	}
	page := h.Completions
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		var position recordCursor
		raw, decodeErr := base64.RawURLEncoding.DecodeString(cursor)
		if decodeErr != nil || json.Unmarshal(raw, &position) != nil || position.At.IsZero() || position.ID == "" {
			writeError(w, 400, errors.New("記録の読み込み位置を確認してください"))
			return
		}
		page = []records.Completion{}
		for _, c := range h.Completions {
			if c.At.Before(position.At) || c.At.Equal(position.At) && c.TaskID < position.ID {
				page = append(page, c)
			}
		}
	}
	next := ""
	if len(page) > 100 {
		last := page[99]
		raw, encodeErr := json.Marshal(recordCursor{At: last.At, ID: last.TaskID})
		if encodeErr != nil {
			respond(w, nil, encodeErr)
			return
		}
		next = base64.RawURLEncoding.EncodeToString(raw)
		page = page[:100]
	}
	respond(w, map[string]any{"completions": page, "week": summary, "activity": activity, "nextCursor": next}, nil)
}
func (m *MemoryStore) HistorySince(ctx context.Context, id string, since time.Time) (records.History, error) {
	h, err := m.History(ctx, id)
	if err != nil {
		return h, err
	}
	filtered := records.History{Completions: []records.Completion{}, Events: []records.Event{}}
	for _, c := range h.Completions {
		if !c.At.Before(since) {
			filtered.Completions = append(filtered.Completions, c)
		}
	}
	for _, e := range h.Events {
		if !e.At.Before(since) {
			filtered.Events = append(filtered.Events, e)
		}
	}
	return filtered, nil
}
func (s *Server) archiveTask(w http.ResponseWriter, r *http.Request) {
	state, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		for i, t := range st.Tasks {
			if t.ID == r.PathValue("id") {
				if t.Status != "completed" {
					return errors.New("完了した課題だけを整理できます")
				}
				st.Tasks = append(st.Tasks[:i], st.Tasks[i+1:]...)
				return nil
			}
		}
		return nil
	})
	respond(w, state, err)
}
func (s *Server) archivedTasks(w http.ResponseWriter, r *http.Request) {
	store, ok := s.Store.(historyStore)
	if !ok {
		respond(w, nil, errors.New("記録の保存先がありません"))
		return
	}
	tasks, err := store.Archived(r.Context(), uid(r))
	respond(w, tasks, err)
}
