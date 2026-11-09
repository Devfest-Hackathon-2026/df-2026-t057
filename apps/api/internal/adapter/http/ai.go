package http

import (
	"context"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/http/oapi"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/assistant"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/companion"
)

// CompanionLines は companion.Service を、HTTP とショートカットから使う形にする。
type CompanionLines struct{ Service *companion.Service }

// Line は相棒の一言。
func (c CompanionLines) Line(ctx context.Context, uid, moment, taskID string) (string, bool, error) {
	l, err := c.Service.Line(ctx, uid, companion.Moment(moment), taskID)
	return l.Text, l.Generated, err
}

// AppOpenLine は YouTube を開いたときの一言（ショートカット用）。
func (c CompanionLines) AppOpenLine(ctx context.Context, uid, taskID string) (string, error) {
	l, err := c.Service.Line(ctx, uid, companion.MomentAppOpen, taskID)
	return l.Text, err
}

// AssistantTurns は assistant.Service を仕様の型に変換する。
type AssistantTurns struct{ Service *assistant.Service }

// List は直近の相談。
func (a AssistantTurns) List(ctx context.Context, uid string) ([]oapi.AssistantTurn, error) {
	turns, err := a.Service.List(ctx, uid)
	if err != nil {
		return nil, err
	}
	out := make([]oapi.AssistantTurn, 0, len(turns))
	for _, t := range turns {
		v, err := turnOut(t)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// Send は相談を送る。id は Idempotency-Key（仕様の Id の形を満たす）。
func (a AssistantTurns) Send(ctx context.Context, uid, id, text, taskID string, historyIDs *[]string) (oapi.AssistantTurn, error) {
	if err := checkID("Idempotency-Key", id); err != nil {
		return oapi.AssistantTurn{}, err
	}
	t, err := a.Service.Send(ctx, uid, id, text, taskID, historyIDs)
	if err != nil {
		return oapi.AssistantTurn{}, err
	}
	return turnOut(t)
}

func turnOut(t assistant.Turn) (oapi.AssistantTurn, error) {
	out := oapi.AssistantTurn{Id: t.ID, Text: t.Text, TaskId: optString(t.TaskID), Status: oapi.AssistantTurnStatus(t.Status), Reply: t.Reply, CreatedAt: t.CreatedAt, Cards: []oapi.AssistantCard{}}
	if t.Failure != "" {
		code := oapi.ProblemCode(t.Failure)
		if !code.Valid() {
			code = oapi.Internal
		}
		out.Failure = &code
	}
	for _, c := range t.Cards {
		card, ok, err := cardOut(c)
		if err != nil {
			return oapi.AssistantTurn{}, err
		}
		if ok {
			out.Cards = append(out.Cards, card)
		}
	}
	return out, nil
}

func cardOut(c assistant.Card) (oapi.AssistantCard, bool, error) {
	var card oapi.AssistantCard
	var err error
	switch c.Kind {
	case assistant.CardCreate:
		if c.Input == nil || c.Preview == nil {
			return card, false, nil
		}
		in := taskInputOut(*c.Input)
		inputID := c.InputID
		if inputID == "" {
			inputID = c.ID
		}
		err = card.FromCreateCard(oapi.CreateCard{Kind: oapi.Create, Id: c.ID, Revision: int(c.Revision), Preview: planOut(*c.Preview),
			Input: oapi.TaskCreateInput{Id: &inputID, Title: in.Title, Notes: in.Notes, Why: in.Why, Deadline: in.Deadline, Minutes: in.Minutes, Url: in.Url}})
	case assistant.CardReschedule:
		rc := oapi.RescheduleCard{Kind: oapi.Reschedule, Id: c.ID, TaskId: c.TaskID, Start: c.Start, Revision: int(c.Revision)}
		if c.Preview != nil {
			rc.Preview = ptr(planOut(*c.Preview))
		}
		err = card.FromRescheduleCard(rc)
	case assistant.CardUpdate:
		if c.Input == nil || c.Draft == nil {
			return card, false, nil
		}
		err = card.FromUpdateCard(oapi.UpdateCard{Kind: oapi.Update, Id: c.ID, TaskId: c.TaskID, Input: taskInputOut(*c.Input), Draft: draftOut(*c.Draft), Revision: int(c.Revision)})
	case assistant.CardMemory:
		err = card.FromMemoryCard(oapi.MemoryCard{Kind: oapi.Memory, Id: c.ID, Memory: c.Memory})
	case assistant.CardSleep:
		if c.Sleep == nil {
			return card, false, nil
		}
		err = card.FromSleepCard(oapi.SleepCard{Kind: oapi.SleepCardKindSleep, Id: c.ID, Sleep: oapi.SleepInput{Bedtime: c.Sleep.Bedtime, Wake: c.Sleep.Wake}})
	default:
		return card, false, nil
	}
	return card, err == nil, err //nolint:wrapcheck // 生成した型の変換の失敗はそのまま返す。
}

func (a AssistantTurns) DeleteHistory(ctx context.Context, uid string) error {
	return a.Service.DeleteHistory(ctx, uid)
}
func (assistantOff) DeleteHistory(context.Context, string) error { return nil }
