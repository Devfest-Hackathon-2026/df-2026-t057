package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/companion"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/id"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/profile"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

const (
	// consultTimeout は相談の材料を読み、AI と道具をやり取りする最長の時間。
	consultTimeout = 40 * time.Second
	// finishTimeout は結果を保存する最長の時間（元のリクエストが切れても保存する）。
	finishTimeout = 10 * time.Second
)

// Deps は Service が使う外部の機能。Model が nil なら AI 相談を使えない（デモ）。AppOpens・Calendar は nil でもよい。
type Deps struct {
	Tasks    Tasks
	Profiles Profiles
	Wallet   Wallet
	Sleep    Sleep
	Sessions Sessions
	AppOpens AppOpens
	Calendar CalendarStatus
	Model    Model
}

// Service は AI 相談のユースケース。
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

// turnDoc は保存する相談。カードは種類ごとに形が違うので JSON の文字列で持つ。
type turnDoc struct {
	ID           string     `json:"id" firestore:"id"`
	Text         string     `json:"text" firestore:"text"`
	TaskID       string     `json:"taskId" firestore:"taskId"`
	Status       Status     `json:"status" firestore:"status"`
	Failure      string     `json:"failure" firestore:"failure"`
	Reply        string     `json:"reply" firestore:"reply"`
	Cards        string     `json:"cards" firestore:"cards"`
	CreatedAt    time.Time  `json:"createdAt" firestore:"createdAt"`
	PendingUntil *time.Time `json:"pendingUntil,omitempty" firestore:"pendingUntil,omitempty"`
	RunID        string     `json:"runId" firestore:"runId"`
}

func turnsCollection(uid string) string { return store.UserCollection(uid, "assistantTurns") }
func turnRef(uid, turnID string) store.Ref {
	return store.UserItem(uid, "assistantTurns", turnID)
}
func leaseRef(uid string) store.Ref { return store.UserDoc(uid, "assistantLease") }

func encodeTurn(t Turn, runID string) (turnDoc, error) {
	cards := t.Cards
	if cards == nil {
		cards = []Card{}
	}
	raw, err := json.Marshal(cards)
	if err != nil {
		return turnDoc{}, fmt.Errorf("assistant: encode cards: %w", err)
	}
	return turnDoc{
		ID: t.ID, Text: t.Text, TaskID: t.TaskID, Status: t.Status, Failure: t.Failure, Reply: t.Reply, Cards: string(raw),
		CreatedAt: store.Truncate(t.CreatedAt), PendingUntil: t.PendingUntil, RunID: runID,
	}, nil
}

func (d turnDoc) turn() (Turn, error) {
	t := Turn{ID: d.ID, Text: d.Text, TaskID: d.TaskID, Status: d.Status, Failure: d.Failure, Reply: d.Reply, Cards: []Card{}, CreatedAt: d.CreatedAt, PendingUntil: d.PendingUntil}
	if d.Cards != "" {
		if err := json.Unmarshal([]byte(d.Cards), &t.Cards); err != nil {
			return Turn{}, fmt.Errorf("assistant: decode cards: %w", err)
		}
	}
	return t, nil
}

// recent は直近の相談を古い順に返す（最大 HistoryLimit 件）。
func (s *Service) recent(ctx context.Context, tx *store.Tx, uid string) ([]Turn, error) {
	docs, err := store.List[turnDoc](ctx, tx, store.Query{Collection: turnsCollection(uid), OrderBy: "createdAt", Desc: true, Limit: HistoryLimit})
	if err != nil {
		return nil, err
	}
	turns := make([]Turn, 0, len(docs))
	for i := len(docs) - 1; i >= 0; i-- {
		t, err := docs[i].turn()
		if err != nil {
			return nil, err
		}
		turns = append(turns, t)
	}
	return turns, nil
}

// List は直近20件の相談を古い順に返す。時間内に終わらなかった処理中の相談は、失敗（ai.failed）として見せる。
func (s *Service) List(ctx context.Context, uid string) (turns []Turn, err error) {
	now := s.clock.Now()
	err = s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		turns, err = s.recent(ctx, tx, uid)
		return err
	})
	if err != nil {
		return nil, err
	}
	for i := range turns {
		turns[i] = Present(turns[i], now)
	}
	return turns, nil
}

// Send は相談を送り、AI の返答を保存して返す。turnID はリクエストの Idempotency-Key。
// 返答済みの相談を同じ ID で送ると、AI を呼ばずに同じ結果を返す。AI が失敗しても status=error の相談を返す（error は nil）。
// 利用上限（ai.rate_limited）と AI が使えない（ai.unavailable）ときは、失敗した相談を保存してからそのエラーを返す。
func (s *Service) Send(ctx context.Context, uid, turnID, text, taskID string) (Turn, error) {
	text, err := ValidateText(text)
	if err != nil {
		return Turn{}, err
	}
	if s.Model == nil {
		return Turn{}, apperr.Unavailable("ai.unavailable")
	}
	turn, runID, err := s.begin(ctx, uid, turnID, text, taskID)
	if err != nil || runID == "" {
		return turn, err
	}
	consultCtx, cancel := context.WithTimeout(ctx, consultTimeout)
	defer cancel()
	snap, reply, cards, consultErr := s.consult(consultCtx, uid, turn)
	if consultErr == nil {
		turn = Succeed(turn, reply, cards)
	} else {
		turn = Fail(turn, FailureCode(consultErr))
	}
	// 元のリクエストが AI の応答中に切れても、結果は保存してリースを外す。
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer finishCancel()
	if turn, err = s.finish(finishCtx, uid, turn, runID, snap.Profile); err != nil {
		return Turn{}, err
	}
	switch code := apperr.As(consultErr).Code; {
	case consultErr == nil:
		return turn, nil
	case code == "ai.rate_limited" || code == "ai.unavailable":
		return turn, consultErr
	default:
		return turn, nil
	}
}

// begin は相談を処理中として保存し、リースを取る。返答済みなら runID は空で、その相談を返す。
func (s *Service) begin(ctx context.Context, uid, turnID, text, taskID string) (turn Turn, runID string, err error) {
	now := s.clock.Now()
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		var existing *Turn
		doc, found, err := store.Get[turnDoc](ctx, tx, turnRef(uid, turnID))
		if err != nil {
			return err
		}
		if found {
			t, err := doc.turn()
			if err != nil {
				return err
			}
			existing = &t
		}
		lease, _, err := store.Get[Lease](ctx, tx, leaseRef(uid))
		if err != nil {
			return err
		}
		var run bool
		if turn, run, err = Begin(existing, lease, turnID, text, taskID, store.Truncate(now)); err != nil || !run {
			return err
		}
		runID = s.ids.New()
		next, err := encodeTurn(turn, runID)
		if err != nil {
			return err
		}
		if err := tx.Set(turnRef(uid, turnID), next); err != nil {
			return err
		}
		if err := tx.Set(leaseRef(uid), Lease{RunID: runID, Until: *turn.PendingUntil}); err != nil {
			return err
		}
		_, err = tx.Touch(ctx, uid, store.ResourceAssistant)
		return err
	})
	if err != nil {
		return Turn{}, "", err
	}
	return turn, runID, nil
}

// finish は結果を保存してリースを外す。処理の ID が相談とリースの両方に一致しなければ assistant.turn_conflict。
// 相談の間に作業傾向が書き換わっていたら、返答を捨てて assistant.context_changed の失敗にする（忘れた内容を残さない）。
func (s *Service) finish(ctx context.Context, uid string, turn Turn, runID string, before profile.Profile) (saved Turn, err error) {
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		doc, found, err := store.Get[turnDoc](ctx, tx, turnRef(uid, turn.ID))
		if err != nil {
			return err
		}
		lease, _, err := store.Get[Lease](ctx, tx, leaseRef(uid))
		if err != nil {
			return err
		}
		if !found || doc.RunID != runID || lease.RunID != runID {
			return apperr.Conflict("assistant.turn_conflict")
		}
		saved = turn
		if saved.Status == StatusReady {
			current, err := s.Profiles.ProfileTx(ctx, tx, uid)
			if err != nil {
				return err
			}
			if current.Context != before.Context || !sameTime(current.ContextSince, before.ContextSince) {
				saved = Fail(saved, "assistant.context_changed")
			}
		}
		next, err := encodeTurn(saved, runID)
		if err != nil {
			return err
		}
		if err := tx.Set(turnRef(uid, turn.ID), next); err != nil {
			return err
		}
		if err := tx.Delete(leaseRef(uid)); err != nil {
			return err
		}
		_, err = tx.Touch(ctx, uid, store.ResourceAssistant)
		return err
	})
	return saved, err
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// consult は相談の材料を読み、AI と道具をやり取りして、返答とカードを作る。カードは何も保存しない。
func (s *Service) consult(ctx context.Context, uid string, turn Turn) (Snapshot, string, []Card, error) {
	snap, history, err := s.snapshot(ctx, uid)
	if err != nil {
		return Snapshot{}, "", nil, err
	}
	in := ConsultInput{
		Locale: snap.Profile.Locale, TimeZone: snap.Loc.String(), History: History(history, snap.Profile.ContextSince, turn.ID),
		User: turn.Text, Now: snap.Now.In(snap.Loc).Format(time.RFC3339),
	}
	if turn.TaskID != "" {
		t, ok := task.Find(snap.Tasks, turn.TaskID)
		if !ok {
			return snap, "", nil, apperr.NotFound("task.not_found")
		}
		in.SelectedTask = Select(*t, snap.Loc)
	}
	reply, proposals, err := s.converse(ctx, uid, in, snap)
	if err != nil {
		return snap, "", nil, err
	}
	if reply, err = ValidateReply(reply, len(proposals)); err != nil {
		return snap, "", nil, err
	}
	cards := make([]Card, 0, len(proposals))
	for _, p := range proposals {
		card, err := BuildCard(p, snap, history, s.ids.New())
		if err != nil {
			return snap, "", nil, err
		}
		cards = append(cards, card)
	}
	return snap, reply, cards, nil
}

// converse は AI に道具で本人のデータを読ませ、提案の道具の呼び出しを未確認のカードの候補として集める。
// 道具を呼ばずに返答したとき、または返答があって呼んだ道具がすべて提案のときに終わる。
func (s *Service) converse(ctx context.Context, uid string, in ConsultInput, snap Snapshot) (string, []Proposal, error) {
	conv := s.Model.Consult(in)
	var results []ToolResult
	var proposals []Proposal
	for range MaxRounds {
		step, err := conv.Next(ctx, uid, results)
		if err != nil {
			return "", nil, err
		}
		reply := strings.TrimSpace(step.Reply)
		if len(step.Calls) == 0 || (reply != "" && !slices.ContainsFunc(step.Calls, func(c ToolCall) bool { return !IsProposal(c.Name) })) {
			for _, call := range step.Calls {
				p, err := ParseProposal(call)
				if err != nil {
					return "", nil, err
				}
				proposals = append(proposals, p)
			}
			return reply, proposals, nil
		}
		results = make([]ToolResult, 0, len(step.Calls))
		for _, call := range step.Calls {
			result := ToolResult{ID: call.ID, Name: call.Name}
			if IsProposal(call.Name) {
				p, err := ParseProposal(call)
				if err != nil {
					return "", nil, err
				}
				proposals = append(proposals, p)
				result.Proposal = true
			} else {
				result.Data = ReadTool(call.Name, snap)
			}
			results = append(results, result)
		}
	}
	return "", nil, apperr.Unavailable("ai.unreadable")
}

// snapshot は相談の間に AI が読む本人のデータと、直近の相談を読む。
func (s *Service) snapshot(ctx context.Context, uid string) (snap Snapshot, history []Turn, err error) {
	now := s.clock.Now()
	err = s.Wallet.Read(ctx, uid, func(ctx context.Context, tx *store.Tx, a *wallet.Account) error {
		snap = Snapshot{Now: now, Account: *a}
		var err error
		if snap.Profile, err = s.Profiles.ProfileTx(ctx, tx, uid); err != nil {
			return err
		}
		snap.Loc = profile.LoadLocation(snap.Profile.TimeZone)
		if snap.Tasks, err = s.Tasks.All(ctx, tx, uid); err != nil {
			return err
		}
		if snap.Revision, err = tx.Revision(ctx, uid, store.ResourceTasks); err != nil {
			return err
		}
		st, err := s.Sleep.GetTx(ctx, tx, uid, now)
		if err != nil {
			return err
		}
		snap.Sleep = companion.SummarizeSleep(st, now, snap.Loc)
		session, err := s.Sessions.Get(ctx, tx, uid)
		if err != nil {
			return err
		}
		if session != nil {
			snap.WorkingOn = session.TaskID
		}
		if snap.AppOpens, err = companion.ReadAppOpens(ctx, tx, s.AppOpens, uid, now); err != nil {
			return err
		}
		snap.Calendar = CalendarInfo{Sync: "disconnected"}
		if s.Calendar != nil {
			if snap.Calendar, err = s.Calendar.Status(ctx, tx, uid); err != nil {
				return err
			}
		}
		history, err = s.recent(ctx, tx, uid)
		return err
	})
	return snap, history, err
}
