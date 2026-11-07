package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Server) assistantHistory(w http.ResponseWriter, r *http.Request) {
	if s.Assistant == nil {
		respond(w, nil, errors.New("相談の保存先が設定されていません"))
		return
	}
	turns, err := s.Assistant.History(r.Context(), uid(r))
	respond(w, map[string]any{"turns": turns}, err)
}
func (s *Server) assistantMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID     string `json:"id"`
		Text   string `json:"text"`
		TaskID string `json:"taskId"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Text = strings.TrimSpace(in.Text)
	if !requestIDPattern.MatchString(in.ID) || in.Text == "" || utf8.RuneCountInString(in.Text) > 2000 {
		respond(w, nil, errors.New("相談を2000文字以内で入力してください"))
		return
	}
	if s.Assistant == nil || s.Planner == nil || s.Planner.Client == nil {
		respond(w, nil, errors.New("この環境ではAI相談を利用できません"))
		return
	}
	turn, runID, err := s.Assistant.Begin(r.Context(), uid(r), in.ID, in.Text, in.TaskID)
	if err != nil || runID == "" {
		respond(w, turn, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	state, err := s.Store.Read(ctx, uid(r))
	var history []AssistantTurn
	if err == nil {
		history, err = s.Assistant.History(ctx, uid(r))
	}
	if err == nil {
		turn.Reply, turn.Cards, err = s.Planner.Consult(ctx, uid(r), in.Text, history, state, in.TaskID)
	}
	if err == nil {
		latest, readErr := s.Store.Read(ctx, uid(r))
		err = readErr
		if err == nil && latest.Profile.Context != state.Profile.Context {
			err = errors.New("作業の傾向が更新されました。最新の内容で再試行してください")
		}
	}
	turn.PendingUntil = nil
	turn.Status = "ready"
	if err != nil {
		turn.Status = "error"
		turn.Reply, turn.Cards = "", []AssistantCard{}
		turn.Failure = sourceError("AIへの相談", err).Error()
	}
	// The original request may have been cancelled while Gemini was responding.
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer finishCancel()
	if saveErr := s.Assistant.Finish(finishCtx, uid(r), turn, runID); saveErr != nil {
		respond(w, nil, saveErr)
		return
	}
	var limit *AILimitError
	if errors.As(err, &limit) || errors.Is(err, ErrAIUsageUnavailable) {
		respond(w, nil, err)
		return
	}
	respond(w, turn, nil)
}
func (s *Server) remember(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
		ID   string `json:"id"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Text = strings.TrimSpace(in.Text)
	if !requestIDPattern.MatchString(in.ID) || in.Text == "" || utf8.RuneCountInString(in.Text) > 500 {
		respond(w, nil, errors.New("覚える内容を500文字以内で入力してください"))
		return
	}
	state, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		if st.MemoryReceipts[in.ID] {
			return nil
		}
		value := st.Profile.Context
		if !strings.Contains(value, in.Text) {
			value = strings.TrimSpace(value + "\n" + in.Text)
		}
		if utf8.RuneCountInString(value) > 2000 {
			return errors.New("覚えている内容が多くなっています。作業の傾向を編集して整理してください")
		}
		st.Profile.Context = value
		if st.MemoryReceipts == nil {
			st.MemoryReceipts = map[string]bool{}
		}
		st.MemoryReceipts[in.ID] = true
		return nil
	})
	respond(w, state, err)
}

type consultationCard struct {
	Kind       string     `json:"kind"`
	Input      *TaskInput `json:"input,omitempty"`
	TaskID     string     `json:"taskId,omitempty"`
	Start      *time.Time `json:"start,omitempty"`
	Memory     string     `json:"memory,omitempty"`
	Sleep      *SleepTime `json:"sleep,omitempty"`
	ProposalID string     `json:"proposalId,omitempty"`
}

func previewTask(state State, input TaskInput) Plan {
	preview := clone(state)
	preview.Tasks = append(preview.Tasks, Task{ID: input.ID, Title: input.Title, Minutes: input.Minutes, Deadline: input.Deadline, Status: "pending"})
	preview.replan()
	task, _ := preview.task(input.ID)
	return task.Plan
}
func (s *Server) assistantPreview(w http.ResponseWriter, r *http.Request) {
	var input TaskInput
	if !decode(w, r, &input) {
		return
	}
	if err := validateInput(input, time.Now()); err != nil {
		respond(w, nil, err)
		return
	}
	state, err := s.Store.Read(r.Context(), uid(r))
	if err != nil {
		respond(w, nil, err)
		return
	}
	input.ID = newID()
	plan := previewTask(state, input)
	respond(w, map[string]any{"plan": plan, "revision": state.PlanRevision}, nil)
}

func (p *Planner) Consult(ctx context.Context, owner, text string, history []AssistantTurn, state State, taskID string) (string, []AssistantCard, error) {
	past := []map[string]any{}
	for _, turn := range history {
		if turn.Status == "ready" && (state.AssistantContextSince == nil || !turn.CreatedAt.Before(*state.AssistantContextSince)) {
			past = append(past, map[string]any{"user": turn.Text, "assistant": turn.Reply, "cards": turn.Cards, "taskId": turn.TaskID})
		}
	}
	if len(past) > 4 {
		past = past[len(past)-4:]
	}
	var selected map[string]any
	if taskID != "" {
		task, err := state.task(taskID)
		if err != nil {
			return "", nil, errors.New("相談する課題が見つかりません。カレンダーで選び直してください")
		}
		selected = map[string]any{"id": task.ID, "title": task.Title, "deadline": task.Deadline, "minutes": task.Minutes, "plan": task.Plan, "notes": task.Notes, "why": task.Why, "url": task.URL, "status": task.Status}
	}
	data, _ := json.Marshal(map[string]any{"timeZone": state.Profile.TimeZone, "history": past, "selectedTask": selected, "user": text, "now": time.Now().Format(time.RFC3339)})
	prompt := `これは相談チャットです。本人の発言(user)に、日本語の短い平文（ふだん3文以内、最大2000文字）で答えます。
進め方：受け止める → Whyがまだなら本人の言葉で引き出す → 最初の一歩を5分以内の行動にする → 計画を提案する → 「これでいけそう？」と確認する。質問は1回の返答で1つまで。
新しい課題の締切と所要時間がそろったら、Whyや最初の一歩が未定でもその返答で propose_task を出し、残りはカードを見せながら聞く。
道具：予定・課題に触れる前に get_today、前進を認めたり振り返るときは get_progress、カレンダーの話は get_calendar、好みは get_profile を呼んで事実を確かめる。データにない予定や数字を作らない。
提案：propose_task / propose_reschedule / propose_update / propose_memory / propose_sleep は候補カードを出すだけで、本人が押すまで保存されない。予定や情報を保存したとは言わない。カードは1回の返答で最大3枚。
締切や所要時間が不明なら決めつけずに1つだけ質問し、提案の道具は呼ばない。過去の未登録の課題候補を直すときは、元のカードのidをproposalIdに入れて全項目を出し直す。登録済みの課題はpropose_taskで複製しない。開始時刻だけ動かすならpropose_reschedule、課題名・メモ・Why・締切・必要時間・URLを変えるならpropose_updateを使う（変えない項目は現在値のまま）。
selectedTaskがあるとき「この予定」「この課題」はその課題を指す。別の課題を対象にするなら本人に確かめる。
睡眠：夜ふかしや寝る時間の話題では get_today の sleep を確かめ、本人が就寝・起床時刻を決めたら propose_sleep を出す。時刻は本人に決めてもらい、睡眠時間の医学的な指導はしない。
日付はtimeZoneとnowを使う。「それ」は直前の会話(history)から読み取る。
入力JSON:
` + string(data)
	reply, candidates, err := p.converse(ctx, owner, prompt, state)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(reply) == "" || utf8.RuneCountInString(reply) > 2000 || len(candidates) > 3 {
		return "", nil, errors.New("相談の返答を確認できませんでした")
	}
	cards := []AssistantCard{}
	for _, candidate := range candidates {
		card := AssistantCard{ID: newID(), Kind: candidate.Kind, Revision: state.PlanRevision}
		switch candidate.Kind {
		case "create":
			if candidate.Input == nil {
				return "", nil, errors.New("予定カードの入力がありません")
			}
			if utf8.RuneCountInString(candidate.Input.Notes) > 1000 {
				return "", nil, errors.New("予定カードのメモかWhyが長すぎます")
			}
			candidate.Input.ID = card.ID
			if candidate.ProposalID != "" {
				found := false
				for _, turn := range history {
					for _, old := range turn.Cards {
						if old.ID == candidate.ProposalID && old.Kind == "create" && old.Input != nil {
							candidate.Input.ID, found = old.Input.ID, true
						}
					}
				}
				if !found {
					return "", nil, errors.New("修正する候補が見つかりません。課題名と日時を教えてください")
				}
			}
			candidate.Input.BaseRevision = nil
			if err := validateInput(*candidate.Input, time.Now()); err != nil {
				return "", nil, err
			}
			if _, err := state.task(candidate.Input.ID); err == nil {
				return "", nil, errors.New("この候補は登録済みです。既存の予定として調整してください")
			}
			plan := previewTask(state, *candidate.Input)
			card.Input, card.Preview = candidate.Input, &plan
		case "task":
			task, err := state.task(candidate.TaskID)
			if err != nil {
				return "", nil, errors.New("相談に含まれた予定が見つかりません。最新の予定を確認してください")
			}
			card.TaskID = task.ID
			if candidate.Start != nil {
				draft, err := MakeDraft(state, task.ID, *candidate.Start, time.Now())
				if err != nil {
					return "", nil, err
				}
				card.Preview = &draft.Changes[0].After
			}
		case "update":
			task, err := state.task(candidate.TaskID)
			if err != nil {
				return "", nil, ErrNotFound
			}
			if candidate.Input == nil {
				return "", nil, errors.New("変更する課題の内容がありません")
			}
			candidate.Input.ID = task.ID
			change, err := previewTaskEdit(state, *candidate.Input)
			if err != nil {
				return "", nil, err
			}
			card.TaskID, card.Change = task.ID, &change
		case "memory":
			card.Memory = strings.TrimSpace(candidate.Memory)
			if card.Memory == "" || utf8.RuneCountInString(card.Memory) > 500 {
				return "", nil, errors.New("覚える内容を確認できませんでした")
			}
		case "sleep":
			if candidate.Sleep == nil || validateSleep(candidate.Sleep.Bedtime, candidate.Sleep.Wake) != nil {
				return "", nil, errors.New("就寝と起床の時刻を確認できませんでした")
			}
			card.Sleep = candidate.Sleep
		default:
			return "", nil, errors.New("相談のカード形式を確認できませんでした")
		}
		cards = append(cards, card)
	}
	return reply, cards, nil
}
