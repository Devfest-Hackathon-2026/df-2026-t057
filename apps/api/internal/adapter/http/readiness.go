package http

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/http/oapi"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/calendar"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/config"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
)

func (s *Server) ListArchivedTasks(ctx context.Context, req oapi.ListArchivedTasksRequestObject) (oapi.ListArchivedTasksResponseObject, error) {
	items, err := s.Tasks.Archived(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	slices.SortFunc(items, func(a, b task.Task) int { return cmp.Compare(a.ID, b.ID) })
	cursor := ""
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	out := oapi.ListArchivedTasks200JSONResponse{Items: []oapi.Task{}}
	for _, t := range items {
		if t.ID <= cursor {
			continue
		}
		if len(out.Items) == 100 {
			last := out.Items[len(out.Items)-1].Id
			out.Cursor = &last
			break
		}
		out.Items = append(out.Items, taskOut(t, false))
	}
	return out, nil
}
func (s *Server) ArchiveTask(ctx context.Context, req oapi.ArchiveTaskRequestObject) (oapi.ArchiveTaskResponseObject, error) {
	if err := checkID("taskId", req.TaskId); err != nil {
		return nil, err
	}
	if err := s.Tasks.Archive(ctx, uidFrom(ctx), req.TaskId, false); err != nil {
		return nil, err
	}
	return oapi.ArchiveTask204Response{}, nil
}
func (s *Server) RestoreTask(ctx context.Context, req oapi.RestoreTaskRequestObject) (oapi.RestoreTaskResponseObject, error) {
	if err := checkID("taskId", req.TaskId); err != nil {
		return nil, err
	}
	if err := s.Tasks.Archive(ctx, uidFrom(ctx), req.TaskId, true); err != nil {
		return nil, err
	}
	return oapi.RestoreTask204Response{}, nil
}
func (s *Server) ExportAccount(ctx context.Context, _ oapi.ExportAccountRequestObject) (oapi.ExportAccountResponseObject, error) {
	if s.Accounts == nil {
		return nil, apperr.Unavailable("account.cleanup_disabled")
	}
	data, err := s.Accounts.Export(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.ExportAccount200JSONResponse(data), nil
}
func (s *Server) DeleteAccount(ctx context.Context, req oapi.DeleteAccountRequestObject) (oapi.DeleteAccountResponseObject, error) {
	if s.Accounts == nil {
		return nil, apperr.Unavailable("account.cleanup_disabled")
	}
	if req.Body.Confirmation != "DELETE" {
		return nil, apperr.Invalid("request.invalid")
	}
	authTime := principalFrom(ctx).AuthTime
	if s.Config.Mode == config.ModeDemo {
		authTime = s.Clock.Now()
	}
	receipt, err := s.Accounts.Begin(ctx, uidFrom(ctx), authTime)
	if err != nil {
		return nil, err
	}
	return oapi.DeleteAccount202JSONResponse{Receipt: receipt}, nil
}
func (s *Server) GetDeletionStatus(ctx context.Context, req oapi.GetDeletionStatusRequestObject) (oapi.GetDeletionStatusResponseObject, error) {
	if s.Accounts == nil {
		return nil, apperr.NotFound("not_found")
	}
	status, err := s.Accounts.Status(ctx, req.Body.Receipt)
	if err != nil {
		return nil, err
	}
	return oapi.GetDeletionStatus200JSONResponse{Status: oapi.GetDeletionStatus200JSONResponseBodyStatus(status)}, nil
}
func (s *Server) CleanupAccounts(ctx context.Context, _ oapi.CleanupAccountsRequestObject) (oapi.CleanupAccountsResponseObject, error) {
	if s.Internal == nil || s.Accounts == nil {
		return nil, apperr.NotFound("not_found")
	}
	if err := s.Internal.Verify(ctx, rawAuthorization(ctx), s.Config.Calendar.SchedulerInvoker); err != nil {
		return nil, err
	}
	if err := s.Accounts.Cleanup(ctx); err != nil {
		return nil, err
	}
	return oapi.CleanupAccounts204Response{}, nil
}

type historyDeleter interface {
	DeleteHistory(context.Context, string) error
}

func (s *Server) DeleteAssistantTurns(ctx context.Context, req oapi.DeleteAssistantTurnsRequestObject) (oapi.DeleteAssistantTurnsResponseObject, error) {
	deleter, ok := s.Assistant.(historyDeleter)
	if !ok {
		return nil, apperr.Unavailable("ai.unavailable")
	}
	if err := deleter.DeleteHistory(ctx, uidFrom(ctx)); err != nil {
		return nil, err
	}
	if req.Params.ForgetMemory != nil && *req.Params.ForgetMemory {
		if err := s.Profiles.Forget(ctx, uidFrom(ctx)); err != nil {
			return nil, err
		}
	}
	return oapi.DeleteAssistantTurns204Response{}, nil
}

type calendarRecovery interface {
	Conflicts(context.Context, string) ([]calendar.Conflict, error)
	Resolve(context.Context, string, string, string, string, int64) error
	Retry(context.Context, string) (oapi.CalendarStatus, error)
}

func (s *Server) ListCalendarConflicts(ctx context.Context, _ oapi.ListCalendarConflictsRequestObject) (oapi.ListCalendarConflictsResponseObject, error) {
	out := oapi.ListCalendarConflicts200JSONResponse{}
	recovery, ok := s.Calendar.(calendarRecovery)
	if !ok {
		return out, nil
	}
	items, err := recovery.Conflicts(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	for _, c := range items {
		v := oapi.CalendarConflict{Task: taskOut(c.Task, true), GoogleTitle: c.Remote.Summary, GoogleNotes: StripNotes(c.Remote.Description), Etag: c.Remote.ETag, Revision: int(c.Revision), Deleted: c.Deleted, Pending: c.Resolution != ""}
		if end, err := time.Parse(time.RFC3339, c.Remote.End.DateTime); err == nil {
			v.GoogleDeadline = &end
		}
		out = append(out, v)
	}
	return out, nil
}
func StripNotes(s string) string { return calendar.StripBlock(s) }
func (s *Server) ResolveCalendarConflict(ctx context.Context, req oapi.ResolveCalendarConflictRequestObject) (oapi.ResolveCalendarConflictResponseObject, error) {
	recovery, ok := s.Calendar.(calendarRecovery)
	if !ok {
		return nil, apperr.Unavailable("calendar.unavailable")
	}
	rev, err := parseIfMatch(req.Params.IfMatch)
	if err != nil {
		return nil, err
	}
	if err := checkID("taskId", req.TaskId); err != nil {
		return nil, err
	}
	if err := recovery.Resolve(ctx, uidFrom(ctx), req.TaskId, string(req.Body.Choice), req.Body.Etag, rev); err != nil {
		return nil, err
	}
	return oapi.ResolveCalendarConflict202Response{}, nil
}
func (s *Server) RetryCalendar(ctx context.Context, _ oapi.RetryCalendarRequestObject) (oapi.RetryCalendarResponseObject, error) {
	r, ok := s.Calendar.(calendarRecovery)
	if !ok {
		return nil, apperr.Unavailable("calendar.unavailable")
	}
	st, err := r.Retry(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.RetryCalendar200JSONResponse(st), nil
}
