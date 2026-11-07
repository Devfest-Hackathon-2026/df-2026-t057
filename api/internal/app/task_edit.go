package app

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
)

// TaskEdit keeps the proposed content and its complete timetable together.
type TaskEdit struct {
	Input TaskInput `json:"input"`
	Draft Draft     `json:"draft"`
}

func setTaskInput(task *Task, in TaskInput) {
	task.Title = strings.TrimSpace(in.Title)
	task.Notes, task.Why = in.Notes, strings.TrimSpace(in.Why)
	task.Deadline, task.Minutes, task.URL = in.Deadline, in.Minutes, in.URL
}

func previewTaskEdit(state State, in TaskInput) (TaskEdit, error) {
	if err := validateInput(in, time.Now()); err != nil {
		return TaskEdit{}, err
	}
	preview := clone(state)
	task, err := preview.task(in.ID)
	if err != nil {
		return TaskEdit{}, err
	}
	if task.Status != "pending" {
		return TaskEdit{}, errors.New("内容の編集は開始前の課題で行ってください")
	}
	setTaskInput(task, in)
	// Old steps may describe different work. The confirmed content is detailed on save.
	task.Plan = Plan{}
	preview.replan()
	draft := Draft{ID: newID(), TaskID: in.ID, BaseRevision: state.PlanRevision, CreatedAt: time.Now(), Reason: "課題の内容を変更し、未開始の課題を締切から組み直します。"}
	for _, after := range preview.Tasks {
		if after.Status != "pending" {
			continue
		}
		before, _ := state.task(after.ID)
		draft.Changes = append(draft.Changes, PlanChange{TaskID: after.ID, Title: after.Title, Before: before.Plan, After: after.Plan, OriginalMinutes: before.Minutes, PlannedMinutes: float64(after.Minutes)})
	}
	sort.SliceStable(draft.Changes, func(i, j int) bool { return draft.Changes[i].After.Start.Before(draft.Changes[j].After.Start) })
	if err := ValidateChanges(draft.Changes, preview, time.Now()); err != nil {
		return TaskEdit{}, err
	}
	in.BaseRevision = nil
	return TaskEdit{Input: in, Draft: draft}, nil
}

func (s *Server) previewTaskEdit(w http.ResponseWriter, r *http.Request) {
	var in TaskInput
	if !decode(w, r, &in) {
		return
	}
	in.ID = r.PathValue("id")
	st, err := s.Store.Read(r.Context(), uid(r))
	if err != nil {
		respond(w, nil, err)
		return
	}
	change, err := previewTaskEdit(st, in)
	respond(w, change, err)
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	var change struct {
		Input        TaskInput `json:"input"`
		BaseRevision int       `json:"baseRevision"`
	}
	if !decode(w, r, &change) {
		return
	}
	change.Input.ID = r.PathValue("id")
	current, err := s.Store.Read(r.Context(), uid(r))
	if err != nil {
		respond(w, nil, err)
		return
	}
	// Recompute the preview on the server; clients cannot change unrelated task fields.
	expected, err := previewTaskEdit(current, change.Input)
	if err != nil {
		respond(w, nil, err)
		return
	}
	if change.BaseRevision != current.PlanRevision {
		respond(w, nil, ErrConflict)
		return
	}
	task, _ := current.task(change.Input.ID)
	updated := *task
	setTaskInput(&updated, change.Input)
	updated.Plan = Plan{}
	updated.Plan, err = s.Planner.Detail(r.Context(), uid(r), updated, current.Profile)
	if err != nil {
		respond(w, nil, err)
		return
	}
	st, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		if st.PlanRevision != expected.Draft.BaseRevision || st.Profile != current.Profile {
			return ErrConflict
		}
		t, err := st.task(updated.ID)
		if err != nil {
			return err
		}
		if t.Status != "pending" {
			return ErrConflict
		}
		setTaskInput(t, change.Input)
		t.Plan = updated.Plan
		t.CalendarInputPending = t.CalendarEventID != ""
		st.replan()
		if err := ValidateChanges(expected.Draft.Changes, *st, time.Now()); err != nil {
			return err
		}
		prepareDraft(st, time.Now())
		if t.CalendarInputPending {
			st.CalendarSync = "pending"
		}
		return nil
	})
	if err == nil && s.Calendar != nil {
		st, err = s.Calendar.Export(r.Context(), uid(r), st)
	}
	respond(w, st, err)
}
