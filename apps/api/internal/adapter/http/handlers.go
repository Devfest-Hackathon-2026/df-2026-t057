package http

import (
	"context"
	"regexp"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/http/oapi"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/config"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/profile"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/viewing"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

// checkID はパスや本文の ID が仕様の Id の形か確かめる（../ などを Firestore のパスに使わない）。
func checkID(field, value string) error {
	if !idPattern.MatchString(value) {
		return apperr.Invalid("request.invalid", apperr.Field(field, "invalid_format"))
	}
	return nil
}

// ───────── system ─────────

func (s *Server) GetHealth(context.Context, oapi.GetHealthRequestObject) (oapi.GetHealthResponseObject, error) {
	return oapi.GetHealth200JSONResponse{Status: oapi.Ok}, nil
}

func (s *Server) GetConfig(context.Context, oapi.GetConfigRequestObject) (oapi.GetConfigResponseObject, error) {
	out := oapi.GetConfig200JSONResponse{Mode: oapi.ConfigMode(s.Config.Mode)}
	out.Features.Ai = s.Config.AIEnabled()
	out.Features.Calendar = s.Config.CalendarEnabled()
	if s.Config.Mode == config.ModeLive && len(s.Config.Firebase) > 0 {
		fb := s.Config.Firebase
		out.Firebase = &fb
	}
	return out, nil
}

func (s *Server) IssueExtensionToken(ctx context.Context, _ oapi.IssueExtensionTokenRequestObject) (oapi.IssueExtensionTokenResponseObject, error) {
	token, ok := demoToken(uidFrom(ctx))
	if s.Config.Mode != config.ModeDemo || !ok {
		return nil, apperr.Forbidden("demo.only")
	}
	return oapi.IssueExtensionToken200JSONResponse{Authorization: token}, nil
}

func (s *Server) AddSampleTasks(ctx context.Context, _ oapi.AddSampleTasksRequestObject) (oapi.AddSampleTasksResponseObject, error) {
	if s.Config.Mode != config.ModeDemo {
		return nil, apperr.Forbidden("demo.only")
	}
	l, err := s.Tasks.AddSamples(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.AddSampleTasks200JSONResponse{Body: taskListOut(l), Headers: oapi.AddSampleTasks200ResponseHeaders{ETag: etag(l.Revision)}}, nil
}

// ───────── profile / settings ─────────

func (s *Server) GetProfile(ctx context.Context, _ oapi.GetProfileRequestObject) (oapi.GetProfileResponseObject, error) {
	p, rev, err := s.Profiles.Get(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.GetProfile200JSONResponse{Body: profileOut(p, rev), Headers: oapi.GetProfile200ResponseHeaders{ETag: etag(rev)}}, nil
}

func (s *Server) UpdateProfile(ctx context.Context, req oapi.UpdateProfileRequestObject) (oapi.UpdateProfileResponseObject, error) {
	ifMatch, err := parseIfMatch(req.Params.IfMatch)
	if err != nil {
		return nil, err
	}
	p, rev, err := s.Profiles.Update(ctx, uidFrom(ctx), profile.Input{Name: req.Body.Name, Context: req.Body.Context, TimeZone: req.Body.TimeZone, Locale: locale.Locale(req.Body.Locale)}, ifMatch)
	if err != nil {
		return nil, err
	}
	return oapi.UpdateProfile200JSONResponse{Body: profileOut(p, rev), Headers: oapi.UpdateProfile200ResponseHeaders{ETag: etag(rev)}}, nil
}

func (s *Server) GetSettings(ctx context.Context, _ oapi.GetSettingsRequestObject) (oapi.GetSettingsResponseObject, error) {
	v, rev, err := s.Profiles.GetSettings(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.GetSettings200JSONResponse{Body: settingsOut(v, rev), Headers: oapi.GetSettings200ResponseHeaders{ETag: etag(rev)}}, nil
}

func (s *Server) UpdateSettings(ctx context.Context, req oapi.UpdateSettingsRequestObject) (oapi.UpdateSettingsResponseObject, error) {
	ifMatch, err := parseIfMatch(req.Params.IfMatch)
	if err != nil {
		return nil, err
	}
	b := req.Body
	v, rev, err := s.Profiles.UpdateSettings(ctx, uidFrom(ctx), profile.Settings{SnoozeMinutes: b.SnoozeMinutes, Mode: profile.NoticeMode(b.Mode), Sound: b.Sound, ReducedMotion: b.ReducedMotion, Grime: b.Grime}, ifMatch)
	if err != nil {
		return nil, err
	}
	return oapi.UpdateSettings200JSONResponse{Body: settingsOut(v, rev), Headers: oapi.UpdateSettings200ResponseHeaders{ETag: etag(rev)}}, nil
}

func (s *Server) RememberContext(ctx context.Context, req oapi.RememberContextRequestObject) (oapi.RememberContextResponseObject, error) {
	p, rev, err := s.Profiles.Remember(ctx, uidFrom(ctx), req.Body.Text)
	if err != nil {
		return nil, err
	}
	return oapi.RememberContext200JSONResponse{Body: profileOut(p, rev), Headers: oapi.RememberContext200ResponseHeaders{ETag: etag(rev)}}, nil
}

// ───────── tasks / plans ─────────

func (s *Server) ListTasks(ctx context.Context, _ oapi.ListTasksRequestObject) (oapi.ListTasksResponseObject, error) {
	l, err := s.Tasks.List(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.ListTasks200JSONResponse{Body: taskListOut(l), Headers: oapi.ListTasks200ResponseHeaders{ETag: etag(l.Revision)}}, nil
}

func (s *Server) CreateTask(ctx context.Context, req oapi.CreateTaskRequestObject) (oapi.CreateTaskResponseObject, error) {
	ifMatch, err := parseOptionalIfMatch(req.Params.IfMatch)
	if err != nil {
		return nil, err
	}
	b := req.Body
	id := ""
	if b.Id != nil {
		if err := checkID("id", *b.Id); err != nil {
			return nil, err
		}
		id = *b.Id
	}
	in := task.Input{Title: b.Title, Notes: b.Notes, Why: b.Why, Deadline: b.Deadline, Minutes: b.Minutes, URL: b.Url}
	c, err := s.Tasks.Create(ctx, uidFrom(ctx), task.CreateRequest{ID: id, Input: in, IfMatch: ifMatch})
	if err != nil {
		return nil, err
	}
	return oapi.CreateTask201JSONResponse{Body: taskOut(c.Task, c.Linked), Headers: oapi.CreateTask201ResponseHeaders{ETag: etag(c.Revision)}}, nil
}

func (s *Server) ExtractTasks(ctx context.Context, req oapi.ExtractTasksRequestObject) (oapi.ExtractTasksResponseObject, error) {
	inputs, err := s.Tasks.Extract(ctx, uidFrom(ctx), req.Body.Text)
	if err != nil {
		return nil, err
	}
	out := make([]oapi.TaskInput, len(inputs))
	for i, in := range inputs {
		out[i] = taskInputOut(in)
	}
	return oapi.ExtractTasks200JSONResponse{Tasks: out}, nil
}

func (s *Server) PreviewNewTask(ctx context.Context, req oapi.PreviewNewTaskRequestObject) (oapi.PreviewNewTaskResponseObject, error) {
	plan, rev, err := s.Tasks.PreviewNew(ctx, uidFrom(ctx), taskInputIn(*req.Body))
	if err != nil {
		return nil, err
	}
	return oapi.PreviewNewTask200JSONResponse{Plan: planOut(plan), Revision: int(rev)}, nil
}

func (s *Server) GetTask(ctx context.Context, req oapi.GetTaskRequestObject) (oapi.GetTaskResponseObject, error) {
	if err := checkID("taskId", req.TaskId); err != nil {
		return nil, err
	}
	t, linked, err := s.Tasks.Get(ctx, uidFrom(ctx), req.TaskId)
	if err != nil {
		return nil, err
	}
	return oapi.GetTask200JSONResponse(taskOut(t, linked)), nil
}

func (s *Server) UpdateTask(ctx context.Context, req oapi.UpdateTaskRequestObject) (oapi.UpdateTaskResponseObject, error) {
	if err := checkID("taskId", req.TaskId); err != nil {
		return nil, err
	}
	ifMatch, err := parseIfMatch(req.Params.IfMatch)
	if err != nil {
		return nil, err
	}
	c, err := s.Tasks.Update(ctx, uidFrom(ctx), req.TaskId, taskInputIn(*req.Body), ifMatch)
	if err != nil {
		return nil, err
	}
	return oapi.UpdateTask200JSONResponse{Body: taskOut(c.Task, c.Linked), Headers: oapi.UpdateTask200ResponseHeaders{ETag: etag(c.Revision)}}, nil
}

func (s *Server) DeleteTask(ctx context.Context, req oapi.DeleteTaskRequestObject) (oapi.DeleteTaskResponseObject, error) {
	if err := checkID("taskId", req.TaskId); err != nil {
		return nil, err
	}
	if err := s.Tasks.Delete(ctx, uidFrom(ctx), req.TaskId); err != nil {
		return nil, err
	}
	return oapi.DeleteTask204Response{}, nil
}

func (s *Server) PreviewTaskEdit(ctx context.Context, req oapi.PreviewTaskEditRequestObject) (oapi.PreviewTaskEditResponseObject, error) {
	if err := checkID("taskId", req.TaskId); err != nil {
		return nil, err
	}
	d, err := s.Tasks.PreviewEdit(ctx, uidFrom(ctx), req.TaskId, taskInputIn(*req.Body))
	if err != nil {
		return nil, err
	}
	return oapi.PreviewTaskEdit200JSONResponse(draftOut(d)), nil
}

func (s *Server) GetRescheduleDraft(ctx context.Context, req oapi.GetRescheduleDraftRequestObject) (oapi.GetRescheduleDraftResponseObject, error) {
	if err := checkID("taskId", req.TaskId); err != nil {
		return nil, err
	}
	r := task.RescheduleRequest{Kind: task.DraftSnooze, Start: req.Params.Start}
	if req.Params.Mode != nil {
		r.Kind = task.DraftKind(*req.Params.Mode)
	}
	if req.Params.Revision != nil {
		rev := int64(*req.Params.Revision)
		r.Revision = &rev
	}
	d, err := s.Tasks.RescheduleDraft(ctx, uidFrom(ctx), req.TaskId, r)
	if err != nil {
		return nil, err
	}
	return oapi.GetRescheduleDraft200JSONResponse(draftOut(d)), nil
}

func (s *Server) AdjustDraft(ctx context.Context, req oapi.AdjustDraftRequestObject) (oapi.AdjustDraftResponseObject, error) {
	d, err := s.Tasks.Adjust(ctx, uidFrom(ctx), draftIn(req.Body.Draft), req.Body.Instruction)
	if err != nil {
		return nil, err
	}
	return oapi.AdjustDraft200JSONResponse(draftOut(d)), nil
}

func (s *Server) CommitDraft(ctx context.Context, req oapi.CommitDraftRequestObject) (oapi.CommitDraftResponseObject, error) {
	ifMatch, err := parseIfMatch(req.Params.IfMatch)
	if err != nil {
		return nil, err
	}
	changes := make([]task.ChangeInput, len(req.Body.Changes))
	for i, c := range req.Body.Changes {
		changes[i] = task.ChangeInput{TaskID: c.TaskId, After: planIn(c.After)}
	}
	l, err := s.Tasks.Commit(ctx, uidFrom(ctx), changes, ifMatch)
	if err != nil {
		return nil, err
	}
	return oapi.CommitDraft200JSONResponse{Body: taskListOut(l), Headers: oapi.CommitDraft200ResponseHeaders{ETag: etag(l.Revision)}}, nil
}

// ───────── work ─────────

func (s *Server) StartTask(ctx context.Context, req oapi.StartTaskRequestObject) (oapi.StartTaskResponseObject, error) {
	if err := checkID("taskId", req.TaskId); err != nil {
		return nil, err
	}
	snap, err := s.Work.Start(ctx, uidFrom(ctx), req.TaskId)
	if err != nil {
		return nil, err
	}
	return oapi.StartTask200JSONResponse(workSessionOut(snap)), nil
}

func (s *Server) CompleteTask(ctx context.Context, req oapi.CompleteTaskRequestObject) (oapi.CompleteTaskResponseObject, error) {
	if err := checkID("taskId", req.TaskId); err != nil {
		return nil, err
	}
	uid := uidFrom(ctx)
	c, err := s.Work.Complete(ctx, uid, req.TaskId)
	if err != nil {
		return nil, err
	}
	_, linked, err := s.Tasks.Get(ctx, uid, req.TaskId)
	if err != nil {
		return nil, err
	}
	out := oapi.CompleteTask200JSONResponse{Task: taskOut(c.Task, linked)}
	if c.Work != nil {
		out.Work = ptr(workRecordOut(*c.Work))
	}
	return out, nil
}

func (s *Server) GetWorkSession(ctx context.Context, _ oapi.GetWorkSessionRequestObject) (oapi.GetWorkSessionResponseObject, error) {
	snap, err := s.Work.Get(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	out := oapi.GetWorkSession200JSONResponse{}
	if snap != nil {
		out.Session = ptr(workSessionOut(*snap))
	}
	return out, nil
}

func (s *Server) PauseWork(ctx context.Context, _ oapi.PauseWorkRequestObject) (oapi.PauseWorkResponseObject, error) {
	snap, err := s.Work.Pause(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.PauseWork200JSONResponse(workSessionOut(snap)), nil
}

func (s *Server) ResumeWork(ctx context.Context, _ oapi.ResumeWorkRequestObject) (oapi.ResumeWorkResponseObject, error) {
	snap, err := s.Work.Resume(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.ResumeWork200JSONResponse(workSessionOut(snap)), nil
}

func (s *Server) ConfirmWork(ctx context.Context, req oapi.ConfirmWorkRequestObject) (oapi.ConfirmWorkResponseObject, error) {
	if err := checkID("sessionId", req.Body.SessionId); err != nil {
		return nil, err
	}
	r, err := s.Work.Confirm(ctx, uidFrom(ctx), req.Body.SessionId)
	if err != nil {
		return nil, err
	}
	return oapi.ConfirmWork200JSONResponse(workRecordOut(r)), nil
}

// ───────── wallet ─────────

func (s *Server) GetWallet(ctx context.Context, _ oapi.GetWalletRequestObject) (oapi.GetWalletResponseObject, error) {
	a, rev, err := s.Wallet.Get(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.GetWallet200JSONResponse{Body: walletOut(a, rev), Headers: oapi.GetWallet200ResponseHeaders{ETag: etag(rev)}}, nil
}

func (s *Server) UpdateTimePolicy(ctx context.Context, req oapi.UpdateTimePolicyRequestObject) (oapi.UpdateTimePolicyResponseObject, error) {
	ifMatch, err := parseIfMatch(req.Params.IfMatch)
	if err != nil {
		return nil, err
	}
	a, rev, err := s.Wallet.UpdatePolicy(ctx, uidFrom(ctx), wallet.Mode(req.Body.Mode), int(req.Body.DailyLimitSeconds), ifMatch)
	if err != nil {
		return nil, err
	}
	return oapi.UpdateTimePolicy200JSONResponse{Body: walletOut(a, rev), Headers: oapi.UpdateTimePolicy200ResponseHeaders{ETag: etag(rev)}}, nil
}

func (s *Server) ListLedger(ctx context.Context, req oapi.ListLedgerRequestObject) (oapi.ListLedgerResponseObject, error) {
	limit, before := 100, ""
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if req.Params.Before != nil {
		if err := checkID("before", *req.Params.Before); err != nil {
			return nil, err
		}
		before = *req.Params.Before
	}
	entries, next, err := s.Wallet.Ledger(ctx, uidFrom(ctx), limit, before)
	if err != nil {
		return nil, err
	}
	out := oapi.ListLedger200JSONResponse{Entries: make([]oapi.LedgerEntry, len(entries)), Next: optString(next)}
	for i, e := range entries {
		out.Entries[i] = ledgerOut(e)
	}
	return out, nil
}

// ───────── viewing ─────────

func (s *Server) GetViewing(ctx context.Context, _ oapi.GetViewingRequestObject) (oapi.GetViewingResponseObject, error) {
	v, err := s.Viewing.Get(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.GetViewing200JSONResponse(viewingOut(v)), nil
}

func (s *Server) StartViewing(ctx context.Context, req oapi.StartViewingRequestObject) (oapi.StartViewingResponseObject, error) {
	extendFrom := ""
	if req.Body.ExtendFrom != nil {
		if err := checkID("extendFrom", *req.Body.ExtendFrom); err != nil {
			return nil, err
		}
		extendFrom = *req.Body.ExtendFrom
	}
	v, err := s.Viewing.Start(ctx, uidFrom(ctx), req.Params.IdempotencyKey, int(req.Body.Seconds), extendFrom)
	if err != nil {
		return nil, err
	}
	return oapi.StartViewing201JSONResponse(viewingOut(v)), nil
}

func (s *Server) EndViewing(ctx context.Context, req oapi.EndViewingRequestObject) (oapi.EndViewingResponseObject, error) {
	if err := checkID("sessionId", req.SessionId); err != nil {
		return nil, err
	}
	v, err := s.Viewing.End(ctx, uidFrom(ctx), req.SessionId, viewing.EndReason(req.Body.Reason))
	if err != nil {
		return nil, err
	}
	return oapi.EndViewing200JSONResponse(viewingOut(v)), nil
}

func (s *Server) ReportViewing(ctx context.Context, req oapi.ReportViewingRequestObject) (oapi.ReportViewingResponseObject, error) {
	sessionID := ""
	if req.Body.SessionId != nil {
		if err := checkID("sessionId", *req.Body.SessionId); err != nil {
			return nil, err
		}
		sessionID = *req.Body.SessionId
	}
	v, err := s.Viewing.Report(ctx, uidFrom(ctx), req.Params.IdempotencyKey, req.Body.Seconds, sessionID)
	if err != nil {
		return nil, err
	}
	return oapi.ReportViewing200JSONResponse(viewingOut(v)), nil
}

// ───────── records ─────────

func (s *Server) GetRecords(ctx context.Context, _ oapi.GetRecordsRequestObject) (oapi.GetRecordsResponseObject, error) {
	r, err := s.Records.Get(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.GetRecords200JSONResponse(recordsOut(r)), nil
}

// ───────── sleep ─────────

func (s *Server) GetSleep(ctx context.Context, _ oapi.GetSleepRequestObject) (oapi.GetSleepResponseObject, error) {
	st, err := s.Sleep.Get(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.GetSleep200JSONResponse{Body: sleepStateOut(st), Headers: oapi.GetSleep200ResponseHeaders{ETag: etag(st.Revision)}}, nil
}

func (s *Server) UpdateSleep(ctx context.Context, req oapi.UpdateSleepRequestObject) (oapi.UpdateSleepResponseObject, error) {
	ifMatch, err := parseIfMatch(req.Params.IfMatch)
	if err != nil {
		return nil, err
	}
	st, err := s.Sleep.Update(ctx, uidFrom(ctx), sleep.Schedule{Bedtime: req.Body.Bedtime, Wake: req.Body.Wake}, ifMatch)
	if err != nil {
		return nil, err
	}
	return oapi.UpdateSleep200JSONResponse{Body: sleepStateOut(st), Headers: oapi.UpdateSleep200ResponseHeaders{ETag: etag(st.Revision)}}, nil
}

func (s *Server) ClearSleep(ctx context.Context, req oapi.ClearSleepRequestObject) (oapi.ClearSleepResponseObject, error) {
	ifMatch, err := parseIfMatch(req.Params.IfMatch)
	if err != nil {
		return nil, err
	}
	st, err := s.Sleep.Clear(ctx, uidFrom(ctx), ifMatch)
	if err != nil {
		return nil, err
	}
	return oapi.ClearSleep200JSONResponse{Body: sleepStateOut(st), Headers: oapi.ClearSleep200ResponseHeaders{ETag: etag(st.Revision)}}, nil
}

func (s *Server) SettleMorning(ctx context.Context, _ oapi.SettleMorningRequestObject) (oapi.SettleMorningResponseObject, error) {
	m, err := s.Sleep.SettleMorning(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.SettleMorning200JSONResponse{Morning: morningOut(m)}, nil
}

// ───────── shortcut ─────────

func (s *Server) GetShortcut(ctx context.Context, _ oapi.GetShortcutRequestObject) (oapi.GetShortcutResponseObject, error) {
	k, err := s.Shortcut.Status(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	out := oapi.GetShortcut200JSONResponse{Configured: k != nil}
	if k != nil {
		out.CreatedAt = &k.CreatedAt
	}
	return out, nil
}

func (s *Server) IssueShortcutKey(ctx context.Context, _ oapi.IssueShortcutKeyRequestObject) (oapi.IssueShortcutKeyResponseObject, error) {
	key, err := s.Shortcut.Issue(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.IssueShortcutKey201JSONResponse{Key: key}, nil
}

func (s *Server) RevokeShortcutKey(ctx context.Context, _ oapi.RevokeShortcutKeyRequestObject) (oapi.RevokeShortcutKeyResponseObject, error) {
	if err := s.Shortcut.Revoke(ctx, uidFrom(ctx)); err != nil {
		return nil, err
	}
	return oapi.RevokeShortcutKey204Response{}, nil
}

func (s *Server) OpenFromShortcut(ctx context.Context, _ oapi.OpenFromShortcutRequestObject) (oapi.OpenFromShortcutResponseObject, error) {
	key, ok := cutScheme(rawAuthorization(ctx), "Shortcut ")
	if !ok {
		return nil, apperr.Unauthenticated("shortcut.key_malformed")
	}
	n, err := s.Shortcut.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	return oapi.OpenFromShortcut200JSONResponse{Title: n.Title, Message: n.Message, Url: n.URL}, nil
}

// ───────── companion / assistant ─────────

func (s *Server) GetCompanionLine(ctx context.Context, req oapi.GetCompanionLineRequestObject) (oapi.GetCompanionLineResponseObject, error) {
	taskID := ""
	if req.Body.TaskId != nil {
		if err := checkID("taskId", *req.Body.TaskId); err != nil {
			return nil, err
		}
		taskID = *req.Body.TaskId
	}
	if !req.Body.Moment.Valid() {
		return nil, apperr.Invalid("request.invalid", apperr.Field("moment", "not_allowed"))
	}
	line, generated, err := s.Companion.Line(ctx, uidFrom(ctx), string(req.Body.Moment), taskID)
	if err != nil {
		return nil, err
	}
	return oapi.GetCompanionLine200JSONResponse{Line: line, Generated: generated}, nil
}

func (s *Server) ListAssistantTurns(ctx context.Context, _ oapi.ListAssistantTurnsRequestObject) (oapi.ListAssistantTurnsResponseObject, error) {
	turns, err := s.Assistant.List(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.ListAssistantTurns200JSONResponse{Turns: turns}, nil
}

func (s *Server) SendAssistantMessage(ctx context.Context, req oapi.SendAssistantMessageRequestObject) (oapi.SendAssistantMessageResponseObject, error) {
	taskID := ""
	if req.Body.TaskId != nil {
		if err := checkID("taskId", *req.Body.TaskId); err != nil {
			return nil, err
		}
		taskID = *req.Body.TaskId
	}
	turn, err := s.Assistant.Send(ctx, uidFrom(ctx), req.Params.IdempotencyKey, req.Body.Text, taskID)
	if err != nil {
		return nil, err
	}
	return oapi.SendAssistantMessage200JSONResponse(turn), nil
}

// ───────── calendar ─────────

func (s *Server) GetCalendar(ctx context.Context, _ oapi.GetCalendarRequestObject) (oapi.GetCalendarResponseObject, error) {
	st, err := s.Calendar.Status(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.GetCalendar200JSONResponse(st), nil
}

func (s *Server) ConnectCalendar(ctx context.Context, _ oapi.ConnectCalendarRequestObject) (oapi.ConnectCalendarResponseObject, error) {
	p := principalFrom(ctx)
	u, err := s.Calendar.Connect(ctx, p.UID, p.GoogleSubject, p.Email)
	if err != nil {
		return nil, err
	}
	return oapi.ConnectCalendar200JSONResponse{Url: u}, nil
}

func (s *Server) CalendarCallback(ctx context.Context, req oapi.CalendarCallbackRequestObject) (oapi.CalendarCallbackResponseObject, error) {
	code, errParam := "", ""
	if req.Params.Code != nil {
		code = *req.Params.Code
	}
	if req.Params.Error != nil {
		errParam = *req.Params.Error
	}
	location := s.Calendar.Callback(ctx, req.Params.State, code, errParam)
	return oapi.CalendarCallback303Response{Headers: oapi.CalendarCallback303ResponseHeaders{Location: &location}}, nil
}

func (s *Server) SyncCalendar(ctx context.Context, _ oapi.SyncCalendarRequestObject) (oapi.SyncCalendarResponseObject, error) {
	st, err := s.Calendar.SyncNow(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.SyncCalendar200JSONResponse(st), nil
}

func (s *Server) DisconnectCalendar(ctx context.Context, _ oapi.DisconnectCalendarRequestObject) (oapi.DisconnectCalendarResponseObject, error) {
	st, err := s.Calendar.Disconnect(ctx, uidFrom(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.DisconnectCalendar200JSONResponse(st), nil
}

func (s *Server) CalendarWebhook(ctx context.Context, req oapi.CalendarWebhookRequestObject) (oapi.CalendarWebhookResponseObject, error) {
	state := ""
	if req.Params.XGoogResourceState != nil {
		state = *req.Params.XGoogResourceState
	}
	if err := s.Calendar.Webhook(ctx, req.Params.XGoogChannelID, req.Params.XGoogChannelToken, state); err != nil {
		if apperr.As(err).Kind == apperr.KindForbidden {
			return oapi.CalendarWebhook403Response{}, nil
		}
		return nil, err
	}
	return oapi.CalendarWebhook204Response{}, nil
}

// ───────── realtime / internal ─────────

// SubscribeEvents は apiPipeline が直接 SSE で応答するので、ここには来ない。
func (s *Server) SubscribeEvents(context.Context, oapi.SubscribeEventsRequestObject) (oapi.SubscribeEventsResponseObject, error) {
	return nil, apperr.NotFound("not_found")
}

func (s *Server) RunOutboxJob(ctx context.Context, req oapi.RunOutboxJobRequestObject) (oapi.RunOutboxJobResponseObject, error) {
	if s.Outbox == nil || s.Internal == nil {
		return nil, apperr.NotFound("not_found")
	}
	if err := s.Internal.Verify(ctx, rawAuthorization(ctx), s.Config.Calendar.TasksInvoker); err != nil {
		return nil, err
	}
	if err := checkID("jobId", req.JobId); err != nil {
		return nil, err
	}
	if !validUID(req.Body.Uid) {
		return nil, apperr.Invalid("request.invalid", apperr.Field("uid", "invalid_format"))
	}
	if err := s.Outbox.Run(ctx, req.Body.Uid, req.JobId); err != nil {
		return nil, err
	}
	return oapi.RunOutboxJob204Response{}, nil
}

func (s *Server) RenewCalendarChannels(ctx context.Context, _ oapi.RenewCalendarChannelsRequestObject) (oapi.RenewCalendarChannelsResponseObject, error) {
	if s.Internal == nil {
		return nil, apperr.NotFound("not_found")
	}
	if err := s.Internal.Verify(ctx, rawAuthorization(ctx), s.Config.Calendar.SchedulerInvoker); err != nil {
		return nil, err
	}
	if err := s.Calendar.Renew(ctx); err != nil {
		return nil, err
	}
	return oapi.RenewCalendarChannels204Response{}, nil
}
