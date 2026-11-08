package http

import (
	"slices"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/http/oapi"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/profile"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/records"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/viewing"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/worksession"
)

// この画面の変換は、機能の型と仕様の型を対応させるだけで、ルールを持たない。

func ptr[T any](v T) *T { return &v }

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func date(day string) openapi_types.Date {
	t, _ := time.Parse(time.DateOnly, day)
	return openapi_types.Date{Time: t}
}

func profileOut(p profile.Profile, rev int64) oapi.Profile {
	return oapi.Profile{Name: p.Name, Context: p.Context, TimeZone: p.TimeZone, Revision: int(rev)}
}

func settingsOut(s profile.Settings, rev int64) oapi.Settings {
	return oapi.Settings{SnoozeMinutes: s.SnoozeMinutes, Mode: oapi.NoticeMode(s.Mode), Sound: s.Sound, ReducedMotion: s.ReducedMotion, Grime: s.Grime, Revision: int(rev)}
}

func planOut(p task.Plan) oapi.Plan {
	steps := make([]oapi.Step, len(p.Steps))
	for i, s := range p.Steps {
		steps[i] = oapi.Step{Title: s.Title, Start: s.Start, End: s.End}
	}
	return oapi.Plan{Start: p.Start, End: p.End, Steps: steps, Reason: oapi.PlanReason(p.Reason), Note: optString(p.Note)}
}

func planIn(p oapi.Plan) task.Plan {
	steps := make([]task.Step, len(p.Steps))
	for i, s := range p.Steps {
		steps[i] = task.Step{Title: s.Title, Start: s.Start, End: s.End}
	}
	out := task.Plan{Start: p.Start, End: p.End, Steps: steps, Reason: task.Reason(p.Reason)}
	if p.Note != nil {
		out.Note = *p.Note
	}
	return out
}

func taskOut(t task.Task, linked bool) oapi.Task {
	return oapi.Task{
		Id: t.ID, Title: t.Title, Notes: t.Notes, Why: t.Why, Deadline: t.Deadline, Minutes: t.Minutes, Url: t.URL,
		Status: oapi.TaskStatus(t.Status), CreatedAt: t.CreatedAt, StartedAt: t.StartedAt, ScheduledStartAt: t.ScheduledStartAt,
		CompletedAt: t.CompletedAt, Plan: planOut(t.Plan), CalendarLinked: linked,
	}
}

func taskInputIn(in oapi.TaskInput) task.Input {
	return task.Input{Title: in.Title, Notes: in.Notes, Why: in.Why, Deadline: in.Deadline, Minutes: in.Minutes, URL: in.Url}
}

func taskInputOut(in task.Input) oapi.TaskInput {
	return oapi.TaskInput{Title: in.Title, Notes: in.Notes, Why: in.Why, Deadline: in.Deadline, Minutes: in.Minutes, Url: in.URL}
}

func draftOut(d task.Draft) oapi.Draft {
	changes := make([]oapi.PlanChange, len(d.Changes))
	for i, c := range d.Changes {
		changes[i] = oapi.PlanChange{TaskId: c.TaskID, Title: c.Title, Before: planOut(c.Before), After: planOut(c.After), OriginalMinutes: c.OriginalMinutes, PlannedSeconds: c.PlannedSeconds}
	}
	return oapi.Draft{Id: d.ID, Kind: oapi.DraftKind(d.Kind), TaskId: d.TaskID, BaseRevision: int(d.BaseRevision), CreatedAt: d.CreatedAt, Start: d.Start, Shortened: d.Shortened, Changes: changes}
}

func draftIn(d oapi.Draft) task.Draft {
	changes := make([]task.Change, len(d.Changes))
	for i, c := range d.Changes {
		changes[i] = task.Change{TaskID: c.TaskId, Title: c.Title, Before: planIn(c.Before), After: planIn(c.After), OriginalMinutes: c.OriginalMinutes, PlannedSeconds: c.PlannedSeconds}
	}
	return task.Draft{ID: d.Id, Kind: task.DraftKind(d.Kind), TaskID: d.TaskId, BaseRevision: int64(d.BaseRevision), CreatedAt: d.CreatedAt, Start: d.Start, Shortened: d.Shortened, Changes: changes}
}

func taskListOut(l task.List) oapi.TaskList {
	items := make([]oapi.Task, len(l.Items))
	for i, t := range l.Items {
		items[i] = taskOut(t, l.Linked[t.ID])
	}
	out := oapi.TaskList{Items: items, Revision: int(l.Revision)}
	if l.NextSnooze != nil {
		out.NextSnooze = ptr(draftOut(*l.NextSnooze))
	}
	return out
}

func workSessionOut(s worksession.Snapshot) oapi.WorkSession {
	x := s.Session
	return oapi.WorkSession{Id: x.ID, TaskId: x.TaskID, StartedAt: x.StartedAt, LimitSeconds: x.LimitSeconds, ElapsedSeconds: x.ElapsedSeconds(s.AsOf), Running: x.Running(), AsOf: s.AsOf, PausedAt: x.PausedAt}
}

func workRecordOut(r wallet.WorkResult) oapi.WorkRecord {
	return oapi.WorkRecord{SessionId: r.SessionID, TaskId: r.TaskID, WorkSeconds: r.WorkSeconds, RewardSeconds: r.RewardSeconds, Deferred: r.Deferred}
}

func walletOut(a wallet.Account, rev int64) oapi.Wallet {
	w := oapi.Wallet{Revision: int(rev), BalanceSeconds: a.Balance, AvailableSeconds: a.Available(), DeferredSeconds: a.Deferred}
	w.Policy.Mode = oapi.TimePolicyMode(a.Mode)
	w.Policy.DailyLimitSeconds = a.DailyLimit
	w.Policy.Day = date(a.Day)
	w.Policy.ResetAt = a.ResetAt
	w.Policy.ViewedSeconds = a.Today.Viewed
	w.Policy.RewardSeconds = a.Today.Reward
	w.Policy.WorkSeconds = a.Today.Work
	w.Rules.BalanceCapSeconds = wallet.BalanceCapSeconds
	w.Rules.WorkSecondsPerReward = wallet.WorkSecondsPerReward
	w.Rules.RewardSecondsPerUnit = wallet.RewardSecondsPerUnit
	w.Rules.DailyRewardCapSeconds = wallet.DailyRewardCapSeconds
	w.Rules.RegistrationRewardSeconds = wallet.RegistrationRewardSeconds
	w.Rules.ViewingChoicesSeconds = slices.Clone(wallet.ViewingChoicesSeconds)
	w.Rules.DailyRefillSeconds = wallet.InitialSeconds
	w.Rules.SleepBonusSeconds = sleep.BonusSeconds
	return w
}

func ledgerOut(e wallet.Entry) oapi.LedgerEntry {
	out := oapi.LedgerEntry{Id: e.ID, Kind: oapi.LedgerKind(e.Kind), Seconds: e.Seconds, At: e.At, Deferred: e.Deferred, TaskId: optString(e.TaskID), TaskTitle: optString(e.TaskTitle)}
	if e.Kind == wallet.KindWork {
		out.WorkSeconds = ptr(e.WorkSeconds)
	}
	return out
}

func viewingSessionOut(x viewing.Session) oapi.ViewingSession {
	out := oapi.ViewingSession{Id: x.ID, ExtendFrom: optString(x.ExtendFrom), StartedAt: x.StartedAt, PlannedSeconds: x.PlannedSeconds, ConsumedSeconds: x.ConsumedSeconds, EndedAt: x.EndedAt}
	if x.EndReason != "" {
		out.EndReason = ptr(oapi.ViewingSessionEndReason(x.EndReason))
	}
	return out
}

func viewingOut(v viewing.View) oapi.Viewing {
	out := oapi.Viewing{AvailableSeconds: v.Available, BalanceSeconds: v.Balance, RequiresSession: v.RequiresSession}
	if v.Session != nil {
		out.Session = ptr(viewingSessionOut(*v.Session))
	}
	return out
}

func recordsOut(r records.Records) oapi.Records {
	var out oapi.Records
	weeks := make([]struct {
		OnTime      int                `json:"onTime"`
		Snoozes     int                `json:"snoozes"`
		Start       openapi_types.Date `json:"start"`
		Starts      int                `json:"starts"`
		WorkSeconds int                `json:"workSeconds"`
	}, len(r.Weeks))
	for i, w := range r.Weeks {
		weeks[i].Start = date(w.Start)
		weeks[i].Starts = w.Starts
		weeks[i].OnTime = w.OnTime
		weeks[i].Snoozes = w.Snoozes
		weeks[i].WorkSeconds = w.WorkSeconds
	}
	out.Weeks = &weeks

	out.Days = slices.Grow(out.Days, len(r.Days))[:len(r.Days)]
	for i, d := range r.Days {
		out.Days[i].Date, out.Days[i].Completed, out.Days[i].EarnedSeconds = date(d.Date), d.Completed, d.EarnedSeconds
	}
	out.Totals.Completed, out.Totals.EarnedSeconds = r.TotalCompleted, r.TotalEarned
	p := r.Practice
	out.Practice.ViewingSessions, out.Practice.EndedSessions, out.Practice.Extensions = p.ViewingSessions, p.EndedSessions, p.Extensions
	out.Practice.StartedTasks, out.Practice.OnTimeStarts, out.Practice.MissedStarts = p.StartedTasks, p.OnTimeStarts, p.MissedStarts
	out.Practice.History = slices.Grow(out.Practice.History, len(p.History))[:len(p.History)]
	for i, h := range p.History {
		out.Practice.History[i].Date, out.Practice.History[i].ViewedSeconds, out.Practice.History[i].WorkSeconds = date(h.Date), h.ViewedSeconds, h.WorkSeconds
	}
	return out
}

func sleepStateOut(st sleep.State) oapi.SleepState {
	out := oapi.SleepState{Revision: int(st.Revision)}
	if st.Sleep != nil {
		s := oapi.Sleep{Bedtime: st.Sleep.Bedtime, Wake: st.Sleep.Wake, KeptNights: st.Sleep.KeptNights, CalendarPending: st.CalendarPending}
		s.Night.Start, s.Night.End, s.Night.Active = st.Night.Start, st.Night.End, st.NightActive
		out.Sleep = &s
	}
	return out
}

func morningOut(m *sleep.Morning) *oapi.Morning {
	if m == nil {
		return nil
	}
	return &oapi.Morning{Night: date(m.Night), Kept: m.Kept, BonusSeconds: m.BonusSeconds, ReleasedSeconds: m.ReleasedSeconds}
}
