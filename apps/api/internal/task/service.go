package task

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/activity"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/id"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

// MaxInstructionRunes は案の調整の指示の上限。
const MaxInstructionRunes = 2000

// MaxExtractRunes は課題を取り出す文章の上限。
const MaxExtractRunes = 6000

// Deps は Service が使う外部の機能。
type Deps struct {
	Planner  Planner
	Calendar Calendar
	Sessions Sessions
	Profiles Profiles
	Rewards  Rewards
}

// Service は課題と計画のユースケース。
type Service struct {
	runner *store.Runner
	clock  clock.Clock
	ids    id.Generator
	Deps
}

// New は Service を作る。
func New(runner *store.Runner, c clock.Clock, ids id.Generator, deps Deps) *Service {
	return &Service{runner: runner, clock: c, ids: ids, Deps: deps}
}

func tasksCollection(uid string) string { return store.UserCollection(uid, "tasks") }
func taskRef(uid, id string) store.Ref  { return store.UserItem(uid, "tasks", id) }

// All はトランザクションの中で課題をすべて読む（登録の順）。
func (s *Service) All(ctx context.Context, tx *store.Tx, uid string) ([]Task, error) {
	tasks, err := store.List[Task](ctx, tx, store.Query{Collection: tasksCollection(uid)})
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(tasks, func(a, b Task) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return tasks, nil
}

func (s *Service) save(tx *store.Tx, uid string, tasks []Task, ids ...string) error {
	for _, taskID := range ids {
		t, ok := Find(tasks, taskID)
		if !ok {
			continue
		}
		if err := tx.Set(taskRef(uid, t.ID), t); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) touch(ctx context.Context, tx *store.Tx, uid string) (int64, error) {
	return tx.Touch(ctx, uid, store.ResourceTasks)
}

// List は課題の一覧と計画の版、次のスヌーズの案。
type List struct {
	Items      []Task
	Revision   int64
	NextSnooze *Draft
	Linked     map[string]bool
}

func (s *Service) list(ctx context.Context, tx *store.Tx, uid string, tasks []Task, now time.Time) (List, error) {
	rev, err := tx.Revision(ctx, uid, store.ResourceTasks)
	if err != nil {
		return List{}, err
	}
	snooze, err := s.Profiles.SnoozeMinutes(ctx, tx, uid)
	if err != nil {
		return List{}, err
	}
	linked, err := s.Calendar.LinkedTasks(ctx, tx, uid)
	if err != nil {
		return List{}, err
	}
	return List{Items: tasks, Revision: rev, NextSnooze: NextSnooze(tasks, snooze, now, rev), Linked: linked}, nil
}

// List は課題の一覧を返す。
func (s *Service) List(ctx context.Context, uid string) (l List, err error) {
	now := s.clock.Now()
	err = s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		tasks, err := s.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		l, err = s.list(ctx, tx, uid, tasks, now)
		return err
	})
	return l, err
}

// LoadTx はトランザクションの中で課題を1つ読む。無ければ task.not_found。
func (s *Service) LoadTx(ctx context.Context, tx *store.Tx, uid, taskID string) (Task, error) {
	t, found, err := store.Get[Task](ctx, tx, taskRef(uid, taskID))
	if err != nil {
		return Task{}, err
	}
	if !found {
		return Task{}, apperr.NotFound("task.not_found")
	}
	return t, nil
}

// Get は課題を1つ返す。
func (s *Service) Get(ctx context.Context, uid, taskID string) (t Task, linked bool, err error) {
	err = s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		if t, err = s.LoadTx(ctx, tx, uid, taskID); err != nil {
			return err
		}
		links, err := s.Calendar.LinkedTasks(ctx, tx, uid)
		linked = links[taskID]
		return err
	})
	return t, linked, err
}

func codeOf(err error) string {
	var e *apperr.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// detail は AI で手順を具体化した計画を作る。利用上限なら逆算だけの計画にし、AI が使えない環境では逆算の計画にする。
func (s *Service) detail(ctx context.Context, uid string, t Task, profileContext string, lang locale.Locale) (Plan, error) {
	res, err := s.Planner.Detail(ctx, uid, DetailRequest{Title: t.Title, Notes: t.Notes, Why: t.Why, Minutes: t.Minutes, Context: profileContext, Locale: lang})
	switch code := codeOf(err); {
	case err == nil:
		plan, ok := DetailedPlan(t, res.Steps, res.Note)
		if !ok {
			return Plan{}, apperr.Unavailable("ai.unreadable")
		}
		return plan, nil
	case code == "ai.rate_limited":
		return BackwardPlan(t, ReasonAILimitFallback), nil
	case code == "ai.unavailable":
		return BackwardPlan(t, ReasonBackward), nil
	default:
		return Plan{}, err
	}
}

// CreateRequest は課題の登録。ID を指定すると、その ID で登録する（相談のカードを押し直しても二重に登録しない）。
type CreateRequest struct {
	ID      string
	Input   Input
	IfMatch *int64
}

// Created は登録の結果。
type Created struct {
	Task     Task
	Revision int64
	Linked   bool
}

// Create は課題を登録し、締切から逆算して全体の計画を組み直す。登録の報酬を一度だけ付与する。
func (s *Service) Create(ctx context.Context, uid string, req CreateRequest) (Created, error) {
	now := s.clock.Now()
	if err := ValidateInput(req.Input, now); err != nil {
		return Created{}, err
	}
	taskID := req.ID
	if taskID == "" {
		taskID = s.ids.New()
	}
	var profileContext string
	var lang locale.Locale
	err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		if _, found, err := store.Get[Task](ctx, tx, taskRef(uid, taskID)); err != nil || found {
			if found {
				return apperr.Conflict("task.duplicate")
			}
			return err
		}
		var err error
		if lang, err = s.Profiles.Locale(ctx, tx, uid); err != nil {
			return err
		}
		profileContext, _, err = s.Profiles.Context(ctx, tx, uid)
		return err
	})
	if err != nil {
		return Created{}, err
	}
	t := NewTask(taskID, req.Input, store.Truncate(now))
	if t.Plan, err = s.detail(ctx, uid, t, profileContext, lang); err != nil {
		return Created{}, err
	}
	var out Created
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		if req.IfMatch != nil {
			if err := tx.Expect(ctx, uid, store.ResourceTasks, *req.IfMatch); err != nil {
				return err
			}
		}
		tasks, err := s.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		if _, ok := Find(tasks, taskID); ok {
			return apperr.Conflict("task.duplicate")
		}
		if len(tasks) >= MaxTasks {
			return apperr.Conflict("task.limit_reached")
		}
		if _, found, err := store.Get[Task](ctx, tx, store.UserItem(uid, "archivedTasks", taskID)); err != nil {
			return err
		} else if found {
			return apperr.Conflict("task.duplicate")
		}
		tasks = append(tasks, t)
		changed := Replan(tasks)
		if err := s.save(tx, uid, tasks, append(changed, taskID)...); err != nil {
			return err
		}
		if _, err := s.Rewards.GrantRegistration(ctx, tx, uid, taskID, t.Title, now); err != nil {
			return err
		}
		if err := s.Calendar.TaskContentChanged(ctx, tx, uid, taskID); err != nil {
			return err
		}
		if len(changed) > 1 || (len(changed) == 1 && changed[0] != taskID) {
			if err := s.Calendar.PlansChanged(ctx, tx, uid); err != nil {
				return err
			}
		}
		saved, _ := Find(tasks, taskID)
		out.Task = *saved
		if out.Revision, err = s.touch(ctx, tx, uid); err != nil {
			return err
		}
		links, err := s.Calendar.LinkedTasks(ctx, tx, uid)
		out.Linked = links[taskID]
		return err
	})
	return out, err
}

// PreviewNew は課題を登録した場合の計画と、計算に使った計画の版を返す。保存はしない。
func (s *Service) PreviewNew(ctx context.Context, uid string, in Input) (plan Plan, rev int64, err error) {
	now := s.clock.Now()
	if err := ValidateInput(in, now); err != nil {
		return Plan{}, 0, err
	}
	err = s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		tasks, err := s.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		const previewID = "\x00preview"
		tasks = append(tasks, NewTask(previewID, in, now))
		Replan(tasks)
		t, _ := Find(tasks, previewID)
		plan = t.Plan
		rev, err = tx.Revision(ctx, uid, store.ResourceTasks)
		return err
	})
	return plan, rev, err
}

// Extract は自由な文章から課題の候補を AI で取り出す。保存はしない。
func (s *Service) Extract(ctx context.Context, uid, text string) ([]Input, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, apperr.Invalid("request.invalid", apperr.Field("text", "required"))
	}
	if utf8.RuneCountInString(text) > MaxExtractRunes {
		return nil, apperr.Invalid("request.invalid", apperr.Field("text", "too_long"))
	}
	now := s.clock.Now()
	var loc *time.Location
	err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		loc, err = s.Profiles.Location(ctx, tx, uid)
		return err
	})
	if err != nil {
		return nil, err
	}
	inputs, err := s.Planner.Extract(ctx, uid, text, now, loc)
	if err != nil {
		return nil, err
	}
	if len(inputs) == 0 || len(inputs) > 12 {
		return nil, apperr.Invalid("ai.no_tasks")
	}
	for _, in := range inputs {
		if err := ValidateInput(in, now); err != nil {
			return nil, apperr.Invalid("ai.no_tasks")
		}
	}
	return inputs, nil
}

// PreviewEdit は課題の内容を変えた場合の、変更前後の計画を返す。保存はしない。
func (s *Service) PreviewEdit(ctx context.Context, uid, taskID string, in Input) (d Draft, err error) {
	now := s.clock.Now()
	if err := ValidateInput(in, now); err != nil {
		return Draft{}, err
	}
	err = s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		tasks, err := s.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		rev, err := tx.Revision(ctx, uid, store.ResourceTasks)
		if err != nil {
			return err
		}
		d, _, err = EditDraft(tasks, taskID, in, now, rev)
		return err
	})
	return d, err
}

// Update は開始前の課題の内容を変え、未開始の課題を締切から組み直す。サーバーが変更案を計算し直して検証する。
// ifMatch は計画の版。AI の処理中に計画か作業傾向が変わったら 412。
func (s *Service) Update(ctx context.Context, uid, taskID string, in Input, ifMatch int64) (Created, error) {
	now := s.clock.Now()
	if err := ValidateInput(in, now); err != nil {
		return Created{}, err
	}
	var original Task
	var profileContext string
	var since *time.Time
	var lang locale.Locale
	err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.Expect(ctx, uid, store.ResourceTasks, ifMatch); err != nil {
			return err
		}
		tasks, err := s.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		if _, _, err := EditDraft(tasks, taskID, in, now, ifMatch); err != nil {
			return err
		}
		t, _ := Find(tasks, taskID)
		original = *t
		if lang, err = s.Profiles.Locale(ctx, tx, uid); err != nil {
			return err
		}
		profileContext, since, err = s.Profiles.Context(ctx, tx, uid)
		return err
	})
	if err != nil {
		return Created{}, err
	}
	updated := original
	updated.ApplyInput(in)
	updated.Plan = Plan{}
	plan, err := s.detail(ctx, uid, updated, profileContext, lang)
	if err != nil {
		return Created{}, err
	}
	var out Created
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.Expect(ctx, uid, store.ResourceTasks, ifMatch); err != nil {
			return err
		}
		currentContext, currentSince, err := s.Profiles.Context(ctx, tx, uid)
		if err != nil {
			return err
		}
		if currentContext != profileContext || !sameTime(currentSince, since) {
			return apperr.PreconditionFailed("precondition.failed")
		}
		tasks, err := s.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		t, ok := Find(tasks, taskID)
		if !ok {
			return apperr.NotFound("task.not_found")
		}
		if t.Status != StatusPending {
			return apperr.Conflict("task.not_editable")
		}
		t.ApplyInput(in)
		t.Plan = plan
		changed := Replan(tasks)
		if err := ValidateChanges(pendingChanges(tasks), tasks, now); err != nil {
			return err
		}
		if err := s.save(tx, uid, tasks, append(changed, taskID)...); err != nil {
			return err
		}
		if err := s.Calendar.TaskContentChanged(ctx, tx, uid, taskID); err != nil {
			return err
		}
		if len(changed) > 0 {
			if err := s.Calendar.PlansChanged(ctx, tx, uid); err != nil {
				return err
			}
		}
		saved, _ := Find(tasks, taskID)
		out.Task = *saved
		if out.Revision, err = s.touch(ctx, tx, uid); err != nil {
			return err
		}
		links, err := s.Calendar.LinkedTasks(ctx, tx, uid)
		out.Linked = links[taskID]
		return err
	})
	return out, err
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// pendingChanges は未開始の課題の今の計画を、開始の順に並べた変更として返す（組み直した結果の検証用）。
func pendingChanges(tasks []Task) []ChangeInput {
	pending := Pending(tasks)
	out := make([]ChangeInput, len(pending))
	for i, t := range pending {
		out[i] = ChangeInput{TaskID: t.ID, After: t.Plan}
	}
	return out
}

// Delete は課題を消し、計画を組み直す。付与済みの報酬は残す。
func (s *Service) Delete(ctx context.Context, uid, taskID string) error {
	return s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		tasks, err := s.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		t, ok := Find(tasks, taskID)
		if !ok {
			return apperr.NotFound("task.not_found")
		}
		if t.CompletedAt != nil {
			if err := (activity.Journal{}).Complete(ctx, tx, uid, activity.Completion{TaskID: t.ID, Title: t.Title, At: *t.CompletedAt}); err != nil {
				return err
			}
		}
		if err := tx.Delete(taskRef(uid, taskID)); err != nil {
			return err
		}
		if err := s.Sessions.TaskRemoved(ctx, tx, uid, taskID); err != nil {
			return err
		}
		tasks = slices.DeleteFunc(tasks, func(t Task) bool { return t.ID == taskID })
		changed := Replan(tasks)
		if err := s.save(tx, uid, tasks, changed...); err != nil {
			return err
		}
		if err := s.Calendar.TaskRemoved(ctx, tx, uid, taskID); err != nil {
			return err
		}
		if len(changed) > 0 {
			if err := s.Calendar.PlansChanged(ctx, tx, uid); err != nil {
				return err
			}
		}
		_, err = s.touch(ctx, tx, uid)
		return err
	})
}

// RescheduleRequest はスヌーズ・調整の案の求め。Start を指定するときは Revision も必要。
type RescheduleRequest struct {
	Kind     DraftKind
	Start    *time.Time
	Revision *int64
}

// RescheduleDraft は、この課題以降の未開始の課題をずらす案を返す。保存はしない。
func (s *Service) RescheduleDraft(ctx context.Context, uid, taskID string, req RescheduleRequest) (d Draft, err error) {
	if req.Kind != DraftSnooze && req.Kind != DraftAdjust {
		return Draft{}, apperr.Invalid("request.invalid", apperr.Field("mode", "not_allowed"))
	}
	if req.Start != nil && req.Revision == nil {
		return Draft{}, apperr.Invalid("request.invalid", apperr.Field("revision", "required"))
	}
	now := s.clock.Now()
	err = s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		tasks, err := s.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		t, ok := Find(tasks, taskID)
		if !ok {
			return apperr.NotFound("task.not_found")
		}
		rev, err := tx.Revision(ctx, uid, store.ResourceTasks)
		if err != nil {
			return err
		}
		var start time.Time
		switch {
		case req.Start != nil:
			if *req.Revision != rev {
				return apperr.PreconditionFailed("precondition.failed")
			}
			start = *req.Start
		case req.Kind == DraftAdjust:
			start = t.Plan.Start
			if start.Before(now) {
				start = now.Add(time.Minute)
			}
		default:
			snooze, err := s.Profiles.SnoozeMinutes(ctx, tx, uid)
			if err != nil {
				return err
			}
			start = now.Add(time.Duration(snooze) * time.Minute)
		}
		d, err = MakeDraft(tasks, taskID, req.Kind, start, now, rev)
		return err
	})
	return d, err
}

// Adjust は確認中の案を、指示に合わせて AI で調整する。保存はしない。
func (s *Service) Adjust(ctx context.Context, uid string, draft Draft, instruction string) (Draft, error) {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return Draft{}, apperr.Invalid("request.invalid", apperr.Field("instruction", "required"))
	}
	if utf8.RuneCountInString(instruction) > MaxInstructionRunes {
		return Draft{}, apperr.Invalid("request.invalid", apperr.Field("instruction", "too_long"))
	}
	if len(draft.Changes) == 0 {
		return Draft{}, apperr.Invalid("plan.invalid_changes", apperr.Field("draft/changes", "plan.empty"))
	}
	now := s.clock.Now()
	var tasks []Task
	var profileContext string
	var lang locale.Locale
	var loc *time.Location
	err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.Expect(ctx, uid, store.ResourceTasks, draft.BaseRevision); err != nil {
			return err
		}
		var err error
		if tasks, err = s.All(ctx, tx, uid); err != nil {
			return err
		}
		if profileContext, _, err = s.Profiles.Context(ctx, tx, uid); err != nil {
			return err
		}
		if lang, err = s.Profiles.Locale(ctx, tx, uid); err != nil {
			return err
		}
		loc, err = s.Profiles.Location(ctx, tx, uid)
		return err
	})
	if err != nil {
		return Draft{}, err
	}
	// クライアントが送った題名・変更前の計画は使わず、保存している内容で置き換える。
	base := make([]Change, len(draft.Changes))
	for i, c := range draft.Changes {
		t, ok := Find(tasks, c.TaskID)
		if !ok {
			return Draft{}, apperr.NotFound("task.not_found")
		}
		base[i] = Change{TaskID: t.ID, Title: t.Title, Before: t.Plan, After: c.After, OriginalMinutes: t.Minutes, PlannedSeconds: c.After.Seconds()}
	}
	proposed, err := s.Planner.Adjust(ctx, uid, AdjustRequest{Changes: base, Instruction: instruction, Context: profileContext, Locale: lang, Location: loc, Now: now})
	if err != nil {
		return Draft{}, err
	}
	if len(proposed) != len(base) {
		return Draft{}, apperr.Invalid("plan.changes_mismatch")
	}
	expected := map[string]bool{}
	for _, c := range base {
		expected[c.TaskID] = true
	}
	changes := make([]Change, len(proposed))
	inputs := make([]ChangeInput, len(proposed))
	for i, p := range proposed {
		if !expected[p.TaskID] {
			return Draft{}, apperr.Invalid("plan.changes_mismatch")
		}
		delete(expected, p.TaskID)
		t, _ := Find(tasks, p.TaskID)
		after := p.After
		after.Reason = ReasonAIAdjusted
		changes[i] = Change{TaskID: t.ID, Title: t.Title, Before: t.Plan, After: after, OriginalMinutes: t.Minutes, PlannedSeconds: after.Seconds()}
		inputs[i] = ChangeInput{TaskID: t.ID, After: after}
	}
	if err := ValidateChanges(inputs, tasks, now); err != nil {
		return Draft{}, err
	}
	start := changes[0].After.Start
	return Draft{ID: draftID(draft.Kind, draft.TaskID, draft.BaseRevision, start, instruction), Kind: draft.Kind, TaskID: draft.TaskID, BaseRevision: draft.BaseRevision, CreatedAt: now, Start: &start, Shortened: draft.Shortened, Changes: changes}, nil
}

// Commit は変更案を確定する。本人が選んだ順番と時刻をそのまま保存し、組み直しはしない。ifMatch は案の元にした計画の版。
func (s *Service) Commit(ctx context.Context, uid string, changes []ChangeInput, ifMatch int64) (List, error) {
	return s.CommitKind(ctx, uid, changes, ifMatch, "")
}
func (s *Service) CommitKind(ctx context.Context, uid string, changes []ChangeInput, ifMatch int64, kind string) (l List, err error) {
	now := s.clock.Now()
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.Expect(ctx, uid, store.ResourceTasks, ifMatch); err != nil {
			return err
		}
		tasks, err := s.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		if err := ValidateChanges(changes, tasks, now); err != nil {
			return err
		}
		if kind == "snooze" && len(changes) > 0 {
			if err := (activity.Journal{}).Record(ctx, tx, uid, activity.Event{ID: fmt.Sprintf("snooze-%d", ifMatch), TaskID: changes[0].TaskID, Kind: activity.Snooze, At: now}); err != nil {
				return err
			}
		}
		ids := Commit(tasks, changes)
		if err := s.save(tx, uid, tasks, ids...); err != nil {
			return err
		}
		if err := s.Calendar.PlansChanged(ctx, tx, uid); err != nil {
			return err
		}
		if _, err := s.touch(ctx, tx, uid); err != nil {
			return err
		}
		l, err = s.list(ctx, tx, uid, tasks, now)
		return err
	})
	return l, err
}

// AddSamples はデモで、課題がないときにサンプルの課題を3件登録する。
func (s *Service) AddSamples(ctx context.Context, uid string) (l List, err error) {
	now := s.clock.Now()
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		tasks, err := s.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		if len(tasks) > 0 {
			return apperr.Conflict("demo.tasks_exist")
		}
		lang, err := s.Profiles.Locale(ctx, tx, uid)
		if err != nil {
			return err
		}
		created := store.Truncate(now)
		for i, in := range SampleInputs(now, lang) {
			t := NewTask(s.ids.New(), in, created.Add(time.Duration(i)*time.Millisecond))
			t.Plan = BackwardPlan(t, ReasonBackward)
			tasks = append(tasks, t)
		}
		Replan(tasks)
		for _, t := range tasks {
			if err := tx.Set(taskRef(uid, t.ID), t); err != nil {
				return err
			}
			if _, err := s.Rewards.GrantRegistration(ctx, tx, uid, t.ID, t.Title, now); err != nil {
				return err
			}
		}
		if _, err := s.touch(ctx, tx, uid); err != nil {
			return err
		}
		l, err = s.list(ctx, tx, uid, tasks, now)
		return err
	})
	return l, err
}

// MarkActive は作業の開始で課題を作業中にする。初めての開始なら、開始した時刻と予定の開始を記録する。
func (s *Service) MarkActive(ctx context.Context, tx *store.Tx, uid, taskID string, now time.Time) (Task, error) {
	t, err := s.LoadTx(ctx, tx, uid, taskID)
	if err != nil {
		return Task{}, err
	}
	if t.Status == StatusCompleted {
		return Task{}, apperr.Conflict("task.already_completed")
	}
	if t.StartedAt == nil {
		started, scheduled := store.Truncate(now), t.Plan.Start
		t.StartedAt, t.ScheduledStartAt = &started, &scheduled
	}
	if t.Status == StatusActive {
		return t, nil
	}
	t.Status = StatusActive
	if err := tx.Set(taskRef(uid, taskID), t); err != nil {
		return Task{}, err
	}
	_, err = s.touch(ctx, tx, uid)
	return t, err
}

// MarkPending は作業を記録した後に、課題を未開始に戻す。
func (s *Service) MarkPending(ctx context.Context, tx *store.Tx, uid, taskID string) error {
	t, found, err := store.Get[Task](ctx, tx, taskRef(uid, taskID))
	if err != nil || !found || t.Status != StatusActive {
		return err
	}
	t.Status = StatusPending
	if err := tx.Set(taskRef(uid, taskID), t); err != nil {
		return err
	}
	_, err = s.touch(ctx, tx, uid)
	return err
}

// MarkCompleted は課題を完了にする。完了済みなら changed は false。
func (s *Service) MarkCompleted(ctx context.Context, tx *store.Tx, uid, taskID string, now time.Time) (t Task, changed bool, err error) {
	if t, err = s.LoadTx(ctx, tx, uid, taskID); err != nil {
		return Task{}, false, err
	}
	if t.Status == StatusCompleted {
		return t, false, nil
	}
	completed := store.Truncate(now)
	if err := (activity.Journal{}).Complete(ctx, tx, uid, activity.Completion{TaskID: t.ID, Title: t.Title, At: completed}); err != nil {
		return Task{}, false, err
	}
	t.Status, t.CompletedAt = StatusCompleted, &completed
	if err := tx.Set(taskRef(uid, taskID), t); err != nil {
		return Task{}, false, err
	}
	_, err = s.touch(ctx, tx, uid)
	return t, true, err
}

// NextPending は次に始める課題（未開始のうち計画の開始が最も早いもの）。
func (s *Service) NextPending(ctx context.Context, tx *store.Tx, uid string) (*Task, error) {
	tasks, err := s.All(ctx, tx, uid)
	if err != nil {
		return nil, err
	}
	pending := Pending(tasks)
	if len(pending) == 0 {
		return nil, nil
	}
	return &pending[0], nil
}

// Imported はカレンダーから取り込む予定。
type Imported struct {
	TaskID   string
	Removed  bool
	Title    string
	Notes    string
	Deadline time.Time
	Minutes  int
	// Detail は AI が具体化した手順。nil なら逆算だけの計画にする。
	Detail *DetailResult
}

// ApplyImport はカレンダーの予定を課題に反映する。新しい予定は課題として登録し、登録の報酬を付与する（同じ ID には一度だけ）。
// 消えた予定の課題は消す。完了した課題は書き換えない。変わったものがあれば true を返す。
func (s *Service) ApplyImport(ctx context.Context, tx *store.Tx, uid string, items []Imported, now time.Time) (bool, error) {
	tasks, err := s.All(ctx, tx, uid)
	if err != nil {
		return false, err
	}
	var touched []string
	removed := false
	created := store.Truncate(now)
	for _, item := range items {
		if _, found, err := store.Get[Task](ctx, tx, store.UserItem(uid, "archivedTasks", item.TaskID)); err != nil {
			return false, err
		} else if found {
			continue
		}
		t, exists := Find(tasks, item.TaskID)
		switch {
		case item.Removed:
			if !exists {
				continue
			}
			if t.CompletedAt != nil {
				if err := (activity.Journal{}).Complete(ctx, tx, uid, activity.Completion{TaskID: t.ID, Title: t.Title, At: *t.CompletedAt}); err != nil {
					return false, err
				}
			}
			if err := tx.Delete(taskRef(uid, item.TaskID)); err != nil {
				return false, err
			}
			if err := s.Sessions.TaskRemoved(ctx, tx, uid, item.TaskID); err != nil {
				return false, err
			}
			tasks = slices.DeleteFunc(tasks, func(x Task) bool { return x.ID == item.TaskID })
			removed = true
		case exists:
			if t.Status == StatusCompleted {
				continue
			}
			t.Title, t.Notes, t.Deadline, t.Minutes = strings.TrimSpace(item.Title), item.Notes, item.Deadline.UTC(), item.Minutes
			t.Plan = importedPlan(*t, item.Detail)
			touched = append(touched, t.ID)
		default:
			if len(tasks) >= MaxTasks {
				continue
			}
			nt := NewTask(item.TaskID, Input{Title: item.Title, Notes: item.Notes, Deadline: item.Deadline, Minutes: item.Minutes}, created)
			nt.Plan = importedPlan(nt, item.Detail)
			tasks = append(tasks, nt)
			touched = append(touched, nt.ID)
			if _, err := s.Rewards.GrantRegistration(ctx, tx, uid, nt.ID, nt.Title, now); err != nil {
				return false, err
			}
		}
	}
	if len(touched) == 0 && !removed {
		return false, nil
	}
	changed := Replan(tasks)
	if err := s.save(tx, uid, tasks, append(changed, touched...)...); err != nil {
		return false, err
	}
	_, err = s.touch(ctx, tx, uid)
	return true, err
}

func importedPlan(t Task, detail *DetailResult) Plan {
	if detail != nil {
		if p, ok := DetailedPlan(t, detail.Steps, detail.Note); ok {
			return p
		}
	}
	return BackwardPlan(t, ReasonBackward)
}
