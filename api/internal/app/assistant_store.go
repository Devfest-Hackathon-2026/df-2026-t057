package app

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var errAssistantBusy = errors.New("前の相談を処理しています。少し待ってから再試行してください")

type AssistantCard struct {
	ID       string     `json:"id"`
	Kind     string     `json:"kind"`
	Input    *TaskInput `json:"input,omitempty"`
	TaskID   string     `json:"taskId,omitempty"`
	Preview  *Plan      `json:"preview,omitempty"`
	Revision int        `json:"revision,omitempty"`
	Change   *TaskEdit  `json:"change,omitempty"`
	Memory   string     `json:"memory,omitempty"`
	Sleep    *SleepTime `json:"sleep,omitempty"`
}

// SleepTime is a proposed bedtime and wake time, both HH:MM.
type SleepTime struct {
	Bedtime string `json:"bedtime"`
	Wake    string `json:"wake"`
}

type AssistantTurn struct {
	ID           string          `json:"id"`
	Text         string          `json:"text"`
	TaskID       string          `json:"taskId,omitempty"`
	Reply        string          `json:"reply"`
	Cards        []AssistantCard `json:"cards"`
	Status       string          `json:"status"`
	Failure      string          `json:"failure,omitempty"`
	CreatedAt    time.Time       `json:"createdAt"`
	PendingUntil *time.Time      `json:"pendingUntil,omitempty"`
}

type AssistantStore interface {
	History(context.Context, string) ([]AssistantTurn, error)
	Begin(context.Context, string, string, string, string) (AssistantTurn, string, error)
	Finish(context.Context, string, AssistantTurn, string) error
}

type assistantLease struct {
	Deleting bool
	RunID    string
	Until    time.Time
}
type memoryAssistantRecord struct {
	Turn  AssistantTurn
	RunID string
}
type MemoryAssistantStore struct {
	closed map[string]bool
	mu     sync.Mutex
	turns  map[string]map[string]memoryAssistantRecord
	leases map[string]assistantLease
}

func NewMemoryAssistantStore() *MemoryAssistantStore {
	return &MemoryAssistantStore{closed: map[string]bool{}, turns: map[string]map[string]memoryAssistantRecord{}, leases: map[string]assistantLease{}}
}
func copyAssistantTurn(t AssistantTurn) AssistantTurn {
	raw, _ := json.Marshal(t)
	var out AssistantTurn
	_ = json.Unmarshal(raw, &out)
	return out
}
func assistantHistory(turns []AssistantTurn) []AssistantTurn {
	sort.Slice(turns, func(i, j int) bool { return turns[i].CreatedAt.Before(turns[j].CreatedAt) })
	if len(turns) > 20 {
		turns = turns[len(turns)-20:]
	}
	for i := range turns {
		if turns[i].Status == "pending" && turns[i].PendingUntil != nil && time.Now().After(*turns[i].PendingUntil) {
			turns[i].Status = "error"
			turns[i].Failure = "応答を確認できませんでした。同じ相談を再試行できます。"
		}
	}
	return turns
}
func (m *MemoryAssistantStore) History(_ context.Context, uid string) ([]AssistantTurn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	turns := []AssistantTurn{}
	for _, record := range m.turns[uid] {
		turns = append(turns, copyAssistantTurn(record.Turn))
	}
	return assistantHistory(turns), nil
}
func beginAssistantTurn(previous AssistantTurn, id, text, taskID string, lease assistantLease) (AssistantTurn, string, error) {
	if lease.Deleting {
		return AssistantTurn{}, "", errAssistantBusy
	}
	if previous.ID != "" && (previous.Text != text || previous.TaskID != taskID) {
		return previous, "", ErrConflict
	}
	if previous.Status == "ready" {
		return previous, "", nil
	}
	if time.Now().Before(lease.Until) {
		return previous, "", errAssistantBusy
	}
	until := time.Now().Add(90 * time.Second)
	created := previous.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	return AssistantTurn{ID: id, Text: text, TaskID: taskID, Status: "pending", Cards: []AssistantCard{}, CreatedAt: created, PendingUntil: &until}, newID(), nil
}
func (m *MemoryAssistantStore) Begin(_ context.Context, uid, id, text, taskID string) (AssistantTurn, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed[uid] {
		return AssistantTurn{}, "", errAccountClosing
	}
	if m.turns[uid] == nil {
		m.turns[uid] = map[string]memoryAssistantRecord{}
	}
	turn, runID, err := beginAssistantTurn(m.turns[uid][id].Turn, id, text, taskID, m.leases[uid])
	if err == nil && runID != "" {
		m.turns[uid][id] = memoryAssistantRecord{Turn: copyAssistantTurn(turn), RunID: runID}
		m.leases[uid] = assistantLease{RunID: runID, Until: *turn.PendingUntil}
	}
	return copyAssistantTurn(turn), runID, err
}
func (m *MemoryAssistantStore) Finish(_ context.Context, uid string, turn AssistantTurn, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed[uid] {
		return errAccountClosing
	}
	if m.turns[uid][turn.ID].RunID != runID || m.leases[uid].RunID != runID {
		return ErrConflict
	}
	m.turns[uid][turn.ID] = memoryAssistantRecord{Turn: copyAssistantTurn(turn), RunID: runID}
	delete(m.leases, uid)
	return nil
}

type FirestoreAssistantStore struct{ DB *firestore.Client }

func assistantTurnFrom(doc *firestore.DocumentSnapshot) (AssistantTurn, error) {
	var turn AssistantTurn
	raw, err := doc.DataAt("turn")
	if err == nil {
		err = json.Unmarshal([]byte(raw.(string)), &turn)
	}
	return turn, err
}
func (f *FirestoreAssistantStore) History(ctx context.Context, uid string) ([]AssistantTurn, error) {
	docs, err := f.DB.Collection("assistantConversations").Doc(uid).Collection("turns").OrderBy("createdAt", firestore.Desc).Limit(20).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	turns := []AssistantTurn{}
	for _, doc := range docs {
		turn, err := assistantTurnFrom(doc)
		if err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	return assistantHistory(turns), nil
}
func (f *FirestoreAssistantStore) Begin(ctx context.Context, uid, id, text, taskID string) (AssistantTurn, string, error) {
	root := f.DB.Collection("assistantConversations").Doc(uid)
	ref := root.Collection("turns").Doc(id)
	var turn AssistantTurn
	var runID string
	err := f.DB.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		job, jobErr := tx.Get(f.DB.Collection("accountJobs").Doc(uid))
		if closingDocument(job, jobErr) {
			return errAccountClosing
		}
		if err := missingOrNil(jobErr); err != nil {
			return err
		}
		previous := AssistantTurn{}
		doc, err := tx.Get(ref)
		if err == nil {
			previous, err = assistantTurnFrom(doc)
		}
		if err != nil && status.Code(err) != codes.NotFound {
			return err
		}
		lease := assistantLease{}
		meta, err := tx.Get(root)
		if err == nil {
			if err = meta.DataTo(&lease); err != nil {
				return err
			}
		} else if status.Code(err) != codes.NotFound {
			return err
		}
		turn, runID, err = beginAssistantTurn(previous, id, text, taskID, lease)
		if err != nil || runID == "" {
			return err
		}
		raw, err := json.Marshal(turn)
		if err != nil {
			return err
		}
		if err = tx.Set(ref, map[string]any{"turn": string(raw), "runId": runID, "createdAt": turn.CreatedAt}); err != nil {
			return err
		}
		return tx.Set(root, assistantLease{RunID: runID, Until: *turn.PendingUntil})
	})
	return turn, runID, err
}
func (f *FirestoreAssistantStore) Finish(ctx context.Context, uid string, turn AssistantTurn, runID string) error {
	root := f.DB.Collection("assistantConversations").Doc(uid)
	ref := root.Collection("turns").Doc(turn.ID)
	return f.DB.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		job, jobErr := tx.Get(f.DB.Collection("accountJobs").Doc(uid))
		if closingDocument(job, jobErr) {
			return errAccountClosing
		}
		if err := missingOrNil(jobErr); err != nil {
			return err
		}
		doc, err := tx.Get(ref)
		if err != nil {
			return err
		}
		meta, err := tx.Get(root)
		if err != nil {
			return err
		}
		var lease assistantLease
		if err = meta.DataTo(&lease); err != nil {
			return err
		}
		if doc.Data()["runId"] != runID || lease.RunID != runID {
			return ErrConflict
		}
		raw, err := json.Marshal(turn)
		if err != nil {
			return err
		}
		if err = tx.Update(ref, []firestore.Update{{Path: "turn", Value: string(raw)}}); err != nil {
			return err
		}
		return tx.Set(root, assistantLease{})
	})
}
