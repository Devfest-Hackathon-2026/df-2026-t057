package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"golang.org/x/oauth2"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CalendarConflict struct {
	TaskID string    `json:"taskId"`
	Remote TaskInput `json:"remote"`
	ETag   string    `json:"etag"`
}

func (c *Calendar) Disconnect(ctx context.Context, id string, removeCalendar bool) error {
	ref := c.DB.Collection("calendarConnections").Doc(id)
	doc, err := ref.Get(ctx)
	if status.Code(err) == codes.NotFound {
		err = c.Secrets.DeleteSecret(ctx, &secretmanagerpb.DeleteSecretRequest{Name: c.secretName(id)})
		if status.Code(err) == codes.NotFound {
			return nil
		}
		return err
	}
	if err != nil {
		return err
	}
	var conn Connection
	if err = doc.DataTo(&conn); err != nil {
		return err
	}
	// Persist the stop before any remote cleanup, and keep the calendar ID for reconnect.
	svc, serviceErr := c.serviceForCleanup(ctx, id)
	if _, err = ref.Set(ctx, map[string]any{"disabled": true}, firestore.MergeAll); err != nil {
		return err
	}
	if svc != nil {
		if conn.ResourceID != "" {
			if err = svc.Channels.Stop(&calendar.Channel{Id: conn.ChannelID, ResourceId: conn.ResourceID}).Context(ctx).Do(); err != nil && !alreadyRemoved(err) && !(calendarAuthorizationLost(err) && !removeCalendar) {
				return err
			}
		}
		if removeCalendar && !conn.CalendarDeleted {
			if err = svc.Calendars.Delete(conn.CalendarID).Context(ctx).Do(); err != nil && !alreadyRemoved(err) {
				return err
			}
		}
	} else if removeCalendar && !conn.CalendarDeleted {
		return serviceErr
	}
	if removeCalendar && !conn.CalendarDeleted {
		if _, err = ref.Set(ctx, map[string]any{"calendarDeleted": true}, firestore.MergeAll); err != nil {
			return err
		}
	}
	err = c.Secrets.DeleteSecret(ctx, &secretmanagerpb.DeleteSecretRequest{Name: c.secretName(id)})
	if status.Code(err) == codes.NotFound {
		return nil
	}
	return err
}
func alreadyRemoved(err error) bool {
	var apiErr *googleapi.Error
	return errors.As(err, &apiErr) && (apiErr.Code == 404 || apiErr.Code == 410)
}
func (c *Calendar) saveConflict(ctx context.Context, id string, task Task, event *calendar.Event) (State, error) {
	deadline, err := time.Parse(time.RFC3339, event.End.DateTime)
	if err != nil {
		return State{}, err
	}
	start, err := time.Parse(time.RFC3339, event.Start.DateTime)
	if err != nil {
		return State{}, err
	}
	conflict := CalendarConflict{TaskID: task.ID, ETag: event.Etag, Remote: TaskInput{ID: task.ID, Title: event.Summary, Notes: stripPlan(event.Description), Deadline: deadline, Minutes: int(deadline.Sub(start).Minutes()), Why: task.Why, URL: task.URL}}
	return c.Store.Update(ctx, id, func(st *State) error {
		for i, old := range st.CalendarConflicts {
			if old.TaskID == task.ID {
				st.CalendarConflicts[i] = conflict
				st.CalendarSync = "conflict"
				return nil
			}
		}
		st.CalendarConflicts = append(st.CalendarConflicts, conflict)
		st.CalendarSync = "conflict"
		return nil
	})
}
func (s *Server) resolveCalendar(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID       string     `json:"id"`
		Choice   string     `json:"choice"`
		ETag     string     `json:"etag"`
		Revision int        `json:"revision"`
		Input    *TaskInput `json:"input,omitempty"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !requestIDPattern.MatchString(in.ID) {
		writeError(w, 400, errors.New("操作IDを確認してください"))
		return
	}
	if s.Calendar == nil {
		respond(w, nil, errors.New("Google連携がありません"))
		return
	}
	current, err := s.Store.Read(r.Context(), uid(r))
	if err != nil {
		respond(w, nil, err)
		return
	}
	task, err := current.task(r.PathValue("id"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	if task.LastCalendarResolutionID == in.ID {
		respond(w, current, nil)
		return
	}
	svc, err := s.Calendar.service(r.Context(), uid(r))
	if err != nil {
		respond(w, nil, err)
		return
	}
	conn, err := s.Calendar.connection(r.Context(), uid(r))
	if err != nil {
		respond(w, nil, err)
		return
	}
	event, err := svc.Events.Get(conn.CalendarID, task.CalendarEventID).Context(r.Context()).Do()
	if err != nil {
		respond(w, nil, err)
		return
	}
	if event.Etag != in.ETag || current.PlanRevision != in.Revision {
		if event.Etag != in.ETag {
			if _, saveErr := s.Calendar.saveConflict(r.Context(), uid(r), *task, event); saveErr != nil {
				respond(w, nil, saveErr)
				return
			}
		}
		respond(w, nil, ErrConflict)
		return
	}
	selected := TaskInput{ID: task.ID, Title: task.Title, Notes: task.Notes, Why: task.Why, URL: task.URL, Minutes: task.Minutes, Deadline: task.Deadline}
	if in.Choice == "google" {
		found := false
		for _, c := range current.CalendarConflicts {
			if c.TaskID == task.ID && c.ETag == in.ETag {
				selected = c.Remote
				found = true
			}
		}
		if !found {
			respond(w, nil, ErrConflict)
			return
		}
	} else if in.Choice == "edit" && in.Input != nil {
		selected = *in.Input
		selected.ID = task.ID
	} else if in.Choice != "app" {
		respond(w, nil, errors.New("採用する内容を選んでください"))
		return
	}
	preview, err := previewTaskEdit(current, selected)
	if err != nil {
		respond(w, nil, err)
		return
	}
	if r.URL.Query().Get("preview") == "true" {
		respond(w, preview, nil)
		return
	}
	state, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		if st.PlanRevision != in.Revision {
			return ErrConflict
		}
		t, err := st.task(task.ID)
		if err != nil {
			return err
		}
		setTaskInput(t, selected)
		t.LastCalendarResolutionID = in.ID
		t.CalendarInputPending = true
		t.CalendarETag = event.Etag
		t.InputHash = eventHash(event)
		st.replan()
		if err = ValidateChanges(preview.Draft.Changes, *st, s.now()); err != nil {
			return err
		}
		conflicts := []CalendarConflict{}
		for _, c := range st.CalendarConflicts {
			if c.TaskID != task.ID {
				conflicts = append(conflicts, c)
			}
		}
		st.CalendarConflicts = conflicts
		st.CalendarSync = "pending"
		prepareDraft(st, s.now())
		return nil
	})
	if err == nil {
		state, err = s.Calendar.Export(r.Context(), uid(r), state)
	}
	respond(w, state, err)
}
func (s *Server) retryCalendar(w http.ResponseWriter, r *http.Request) {
	if s.Calendar == nil || !s.authorizeMaintenance(r) {
		w.WriteHeader(403)
		return
	}
	docs, err := s.Calendar.DB.Collection("calendarConnections").Documents(r.Context()).GetAll()
	if err != nil {
		respond(w, nil, err)
		return
	}
	failed := 0
	for _, doc := range docs {
		var conn Connection
		if err = doc.DataTo(&conn); err != nil {
			failed++
			continue
		}
		if conn.Disabled {
			continue
		}
		st, readErr := s.Store.Read(r.Context(), conn.UID)
		if readErr != nil {
			continue
		}
		if st.CalendarSync != "pending" {
			continue
		}
		if _, err = s.Calendar.Sync(r.Context(), conn.UID); err != nil {
			failed++
			LogFailure("calendar retry", err)
		}
	}
	if failed > 0 {
		writeJSON(w, 503, map[string]int{"failed": failed})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (c *Calendar) markSyncFailure(ctx context.Context, id string, cause error) {
	var tokenError *oauth2.RetrieveError
	var apiError *googleapi.Error
	state := "pending"
	if errors.As(cause, &tokenError) && tokenError.ErrorCode == "invalid_grant" || errors.As(cause, &apiError) && apiError.Code == 401 {
		state = "reauth_required"
	}
	if _, err := c.Store.Update(ctx, id, func(st *State) error {
		if st.CalendarSync != "conflict" && st.CalendarSync != "disconnected" {
			st.CalendarSync = state
		}
		return nil
	}); err != nil {
		LogFailure("record calendar failure", err)
	}
}

func calendarAuthorizationLost(err error) bool {
	var tokenError *oauth2.RetrieveError
	var apiError *googleapi.Error
	return errors.As(err, &tokenError) && tokenError.ErrorCode == "invalid_grant" || errors.As(err, &apiError) && apiError.Code == 401
}
