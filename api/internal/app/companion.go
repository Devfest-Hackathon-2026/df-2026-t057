package app

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/genai"
)

// CompanionName is the single character who speaks in every Gemini touchpoint.
const CompanionName = "相棒"

//go:embed companion_persona.md
var companionPersona string

func persona() *genai.Content {
	return genai.NewContentFromText(strings.ReplaceAll(companionPersona, "{NAME}", CompanionName), genai.RoleUser)
}

var bannedWords = []string{"依存症", "中毒", "治療", "病気", "意志が弱"}

func speakable(text string, limit int) bool {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > limit {
		return false
	}
	for _, word := range bannedWords {
		if strings.Contains(text, word) {
			return false
		}
	}
	return true
}

func location(state State) *time.Location {
	if loc, err := time.LoadLocation(state.Profile.TimeZone); err == nil {
		return loc
	}
	return time.UTC
}

// streakDays counts consecutive local days with a completed task, ending today or yesterday.
func streakDays(state State, now time.Time) int {
	loc := location(state)
	done := map[string]bool{}
	for _, t := range state.Tasks {
		if t.CompletedAt != nil {
			done[t.CompletedAt.In(loc).Format(time.DateOnly)] = true
		}
	}
	day := now.In(loc)
	if !done[day.Format(time.DateOnly)] {
		day = day.AddDate(0, 0, -1)
	}
	count := 0
	for done[day.Format(time.DateOnly)] {
		count++
		day = day.AddDate(0, 0, -1)
	}
	return count
}

type companionTask struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Notes     string    `json:"notes,omitempty"`
	URL       string    `json:"url,omitempty"`
	Why       string    `json:"why,omitempty"`
	Deadline  time.Time `json:"deadline"`
	Minutes   int       `json:"minutes"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	FirstStep string    `json:"firstStep,omitempty"`
	Status    string    `json:"status"`
	Calendar  bool      `json:"fromCalendar"`
}

func describeTask(t Task) companionTask {
	c := companionTask{ID: t.ID, Title: t.Title, Notes: t.Notes, URL: t.URL, Why: t.Why, Deadline: t.Deadline, Minutes: t.Minutes, Start: t.Plan.Start, End: t.Plan.End, Status: t.Status, Calendar: t.CalendarEventID != ""}
	if len(t.Plan.Steps) > 0 && t.Plan.Steps[0].Title != t.Title {
		c.FirstStep = t.Plan.Steps[0].Title
	}
	return c
}

func companionProgress(state State, now time.Time) map[string]any {
	week, total := 0, 0
	recent := []Task{}
	for _, t := range state.Tasks {
		if t.CompletedAt == nil {
			continue
		}
		total++
		if now.Sub(*t.CompletedAt) <= 7*24*time.Hour {
			week++
		}
		recent = append(recent, t)
	}
	sort.Slice(recent, func(i, j int) bool { return recent[i].CompletedAt.After(*recent[j].CompletedAt) })
	titles := []string{}
	for i := 0; i < len(recent) && i < 3; i++ {
		titles = append(titles, recent[i].Title)
	}
	return map[string]any{"streakDays": streakDays(state, now), "support": supportLevel(state, now), "completedLast7Days": week, "completedTotal": total, "recentCompleted": titles, "viewingMinutesLeft": state.BalanceSeconds / 60, "sleep": sleepSummary(state, now)}
}

// supportLevel fades the companion as the user keeps going on their own: full, light, then hands-off.
func supportLevel(state State, now time.Time) string {
	days := streakDays(state, now)
	switch {
	case days >= 7:
		return "hands-off"
	case days >= 3:
		return "light"
	}
	return "full"
}

// sleepSummary is what the companion may know about the user's bedtime.
func sleepSummary(state State, now time.Time) map[string]any {
	if state.Sleep == nil {
		return map[string]any{"set": false}
	}
	_, night := state.nightAt(now)
	key, _, _ := state.lastNight(now)
	summary := map[string]any{"set": true, "bedtime": state.Sleep.Bedtime, "wake": state.Sleep.Wake, "keptNights": state.Sleep.Kept, "nightNow": night}
	if state.Sleep.Settled == key {
		summary["lastNightWithoutShorts"] = state.Sleep.Watched != key
	}
	return summary
}

const (
	toolToday      = "get_today"
	toolProgress   = "get_progress"
	toolCalendar   = "get_calendar"
	toolProfile    = "get_profile"
	toolTask       = "propose_task"
	toolReschedule = "propose_reschedule"
	toolMemory     = "propose_memory"
	toolSleep      = "propose_sleep"
	toolUpdate     = "propose_update"
)

// proposalKinds maps proposal tools to the confirmation cards the user must press.
var proposalKinds = map[string]string{toolTask: "create", toolReschedule: "task", toolMemory: "memory", toolSleep: "sleep", toolUpdate: "update"}

func companionTools() []*genai.Tool {
	str := func(description string) *genai.Schema {
		return &genai.Schema{Type: genai.TypeString, Description: description}
	}
	empty := &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{}}
	return []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{
		{Name: toolToday, Description: "今日と明日の未完了の課題（予定時刻・Why・最初の一手）、作業中の課題、残りの視聴時間、就寝・起床の設定と今が夜の時間帯かを読む。", Parameters: empty},
		{Name: toolProgress, Description: "続いた日数、直近7日の完了数、最近終えた課題、ショートを見ずに寝られた夜の連続数を読む。前進を認めたり振り返るときに使う。", Parameters: empty},
		{Name: toolCalendar, Description: "DOPAGAKIカレンダーと同期した予定と同期状況を読む。本人の他のカレンダーは読めない。", Parameters: empty},
		{Name: toolProfile, Description: "本人が保存した作業の傾向・好み・タイムゾーンを読む。", Parameters: empty},
		{Name: toolTask, Description: "新しい課題を登録候補として本人に見せる。本人がカードを押すまで保存されない。締切や所要時間が不明なら呼ばずに質問する。登録済みの課題には使わない。", Parameters: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{
			"title":      str("課題名"),
			"notes":      str("メモ（1000文字以内）"),
			"why":        str("本人が言ったやる理由。言っていなければ空文字。あなたが作らない"),
			"deadline":   str("締切。タイムゾーン付きRFC3339"),
			"minutes":    {Type: genai.TypeInteger, Description: "必要な分数"},
			"url":        str("関連URL。なければ空文字"),
			"proposalId": str("修正する未登録カードのid。新規なら省略"),
		}, Required: []string{"title", "deadline", "minutes"}}},
		{Name: toolReschedule, Description: "既存の課題の時間変更を候補として本人に見せる。本人がOKを押すまで変わらない。", Parameters: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{
			"taskId": str("get_todayなどで得た実在する課題のid"),
			"start":  str("新しい開始時刻（RFC3339）。参照だけなら省略"),
		}, Required: []string{"taskId"}}},
		{Name: toolUpdate, Description: "未開始の既存課題の内容（課題名・メモ・Why・締切・必要時間・URL）の変更を候補として本人に見せる。変えない項目は現在値のまま全項目を出す。本人が押すまで変わらない。", Parameters: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{
			"taskId":   str("get_todayなどで得た実在する課題のid"),
			"title":    str("課題名"),
			"notes":    str("メモ"),
			"why":      str("やる理由。本人が言っていなければ現在値"),
			"deadline": str("締切。タイムゾーン付きRFC3339"),
			"minutes":  {Type: genai.TypeInteger, Description: "必要な分数"},
			"url":      str("関連URL。なければ空文字"),
		}, Required: []string{"taskId", "title", "deadline", "minutes"}}},
		{Name: toolMemory, Description: "本人がはっきり言った作業の好み・苦手・時間帯を、覚える候補として見せる。推測した性格や機微な情報は使わない。", Parameters: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{
			"memory": str("短く覚える内容（500文字以内）"),
		}, Required: []string{"memory"}}},
		{Name: toolSleep, Description: "本人が決めた就寝・起床時刻を、毎日の就寝予定としてカレンダーに入れる候補を見せる。本人が押すまで保存されない。就寝1時間前からの報酬は翌朝に受け取りになり、ショートを見ずに寝られた朝は＋15分。", Parameters: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{
			"bedtime": str("就寝時刻 HH:MM（24時間表記）"),
			"wake":    str("起床時刻 HH:MM（24時間表記）"),
		}, Required: []string{"bedtime", "wake"}}},
	}}}
}

func readTool(state State, name string, now time.Time) map[string]any {
	switch name {
	case toolToday:
		loc := location(state)
		year, month, day := now.In(loc).Date()
		until := time.Date(year, month, day, 0, 0, 0, 0, loc).AddDate(0, 0, 2)
		tasks := []companionTask{}
		for _, t := range state.Tasks {
			if t.Status != "completed" && t.Plan.Start.Before(until) {
				tasks = append(tasks, describeTask(t))
			}
		}
		result := map[string]any{"now": now.In(loc).Format(time.RFC3339), "tasks": tasks, "viewingMinutesLeft": state.BalanceSeconds / 60, "sleep": sleepSummary(state, now)}
		if state.Session != nil {
			result["workingOn"] = state.Session.TaskID
		}
		return result
	case toolProgress:
		return companionProgress(state, now)
	case toolCalendar:
		tasks := []companionTask{}
		for _, t := range state.Tasks {
			if t.CalendarEventID != "" && t.Status != "completed" {
				tasks = append(tasks, describeTask(t))
			}
		}
		return map[string]any{"connected": state.CalendarConnected, "sync": state.CalendarSync, "lastSync": state.LastSync, "events": tasks}
	case toolProfile:
		return map[string]any{"name": state.Profile.Name, "context": state.Profile.Context, "timeZone": state.Profile.TimeZone}
	}
	return map[string]any{"error": "この道具は使えません"}
}

func proposalCard(call *genai.FunctionCall) (consultationCard, error) {
	raw, _ := json.Marshal(call.Args)
	card := consultationCard{Kind: proposalKinds[call.Name]}
	switch call.Name {
	case toolTask:
		var args struct {
			TaskInput
			ProposalID string `json:"proposalId"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return card, errors.New("予定カードの日時を読み取れませんでした")
		}
		args.TaskInput.ID = ""
		card.Input, card.ProposalID = &args.TaskInput, args.ProposalID
	case toolReschedule:
		var args struct {
			TaskID string     `json:"taskId"`
			Start  *time.Time `json:"start"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return card, errors.New("予定の変更候補を読み取れませんでした")
		}
		card.TaskID, card.Start = args.TaskID, args.Start
	case toolMemory:
		var args struct {
			Memory string `json:"memory"`
		}
		_ = json.Unmarshal(raw, &args)
		card.Memory = args.Memory
	case toolUpdate:
		var args struct {
			TaskInput
			TaskID string `json:"taskId"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return card, errors.New("課題の変更候補を読み取れませんでした")
		}
		card.Input, card.TaskID = &args.TaskInput, args.TaskID
		for _, field := range []string{"notes", "why", "url"} {
			if _, ok := call.Args[field]; !ok {
				card.Omitted = append(card.Omitted, field)
			}
		}
	case toolSleep:
		_ = json.Unmarshal(raw, &card.Sleep)
	default:
		return card, errors.New("相談のカード形式を確認できませんでした")
	}
	return card, nil
}

// maxToolRounds bounds the Gemini round trips of one consultation.
const maxToolRounds = 4

// converse lets Gemini read state through tools and collects proposal calls as unconfirmed cards.
func (p *Planner) converse(ctx context.Context, owner, prompt string, state State) (string, []consultationCard, error) {
	contents := []*genai.Content{genai.NewContentFromText(prompt, genai.RoleUser)}
	config := &genai.GenerateContentConfig{SystemInstruction: persona(), Tools: companionTools(), MaxOutputTokens: 5000, ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow}}
	proposals := []consultationCard{}
	for range maxToolRounds {
		result, err := p.generateContents(ctx, owner, contents, config)
		if err != nil {
			return "", nil, err
		}
		if len(result.Candidates) == 0 || result.Candidates[0].Content == nil {
			return "", nil, errors.New("相談の返答を読み取れませんでした")
		}
		calls := result.FunctionCalls()
		reply := strings.TrimSpace(result.Text())
		if len(calls) == 0 || (reply != "" && onlyProposals(calls)) {
			for _, call := range calls {
				card, err := proposalCard(call)
				if err != nil {
					return "", nil, err
				}
				proposals = append(proposals, card)
			}
			return reply, proposals, nil
		}
		contents = append(contents, result.Candidates[0].Content)
		responses := []*genai.Part{}
		for _, call := range calls {
			var response map[string]any
			if _, ok := proposalKinds[call.Name]; ok {
				card, err := proposalCard(call)
				if err != nil {
					return "", nil, err
				}
				proposals = append(proposals, card)
				response = map[string]any{"status": "本人の確認待ちとして表示します。まだ保存されていません"}
			} else {
				response = readTool(state, call.Name, time.Now())
			}
			part := genai.NewPartFromFunctionResponse(call.Name, response)
			part.FunctionResponse.ID = call.ID
			responses = append(responses, part)
		}
		contents = append(contents, genai.NewContentFromParts(responses, genai.RoleUser))
	}
	return "", nil, errors.New("相談の返答がまとまりませんでした。もう一度お試しください")
}

func onlyProposals(calls []*genai.FunctionCall) bool {
	for _, call := range calls {
		if _, ok := proposalKinds[call.Name]; !ok {
			return false
		}
	}
	return true
}

var companionMoments = map[string]string{
	"notice":       "ショート動画を見ている途中で、予定の開始時刻が来た。責めずに、課題のWhyや最初の一手に触れて、動画から離れて始めるよう背中を押す",
	"snooze":       "本人がスヌーズを選んだ。責めずに受け止め、次に始める時刻へ気持ちをつなぐ",
	"complete":     "本人が課題を完了した。具体的に認めて、続いた日数があれば触れる",
	"balance_zero": "今日の視聴時間を使い切った。罰ではなく区切りとして伝え、次の一手で時間が増えることに触れる",
	"night":        "就寝1時間前を過ぎてもショートを見ている。責めずに、スマホを置く・歯をみがくなど寝る準備の小さな一歩を促す。今夜の報酬は朝に受け取れる",
	"morning":      "朝、アプリを開いた。sleep.lastNightWithoutShortsがtrueなら区切れたことを具体的に認め、falseでも責めずに今日の最初の一手へつなぐ",
}

func fallbackLine(moment string, task *Task) string {
	switch moment {
	case "notice":
		if task != nil && task.Why != "" {
			return "「" + task.Why + "」のために、まず一手だけ。"
		}
		return "そろそろ、一緒に始めよっか。"
	case "snooze":
		return "了解、少し後ろにずらそう。次の時間に、また声かけるね。"
	case "complete":
		return "やりきったね。その積み重ね、ちゃんと効いてる。"
	case "balance_zero":
		return "今日の視聴はここで区切ろう。次の一手で、また時間が増えるよ。"
	case "night":
		return "そろそろ寝る準備しよっか。今夜の分は、朝ちゃんと受け取れるよ。"
	case "morning":
		return "おはよう。今日の最初の一手、一緒に決めよっか。"
	}
	return ""
}

// lineTimeout keeps a remark from delaying the moment it belongs to.
var lineTimeout = 6 * time.Second

// Line asks the companion for one short remark at a moment, falling back to fixed text.
// lineReuse is how long a generated remark is reused for the same moment, task and plan.
const lineReuse = 6 * time.Hour

type cachedLine struct {
	text    string
	expires time.Time
}

func lineKey(owner, moment string, state State, task *Task, now time.Time) string {
	key := owner + "|" + moment + "|" + now.In(location(state)).Format(time.DateOnly)
	if task != nil {
		key += "|" + task.ID + "|" + task.Plan.Start.Format(time.RFC3339)
	}
	return key
}

func (p *Planner) Line(ctx context.Context, owner, moment string, state State, task *Task) (string, bool) {
	fallback := fallbackLine(moment, task)
	if p == nil || p.Client == nil {
		return fallback, false
	}
	key := lineKey(owner, moment, state, task, time.Now())
	if cached, ok := p.lines.Load(key); ok && time.Now().Before(cached.(cachedLine).expires) {
		return cached.(cachedLine).text, true
	}
	ctx, cancel := context.WithTimeout(ctx, lineTimeout)
	defer cancel()
	now := time.Now()
	data := map[string]any{"moment": companionMoments[moment], "progress": companionProgress(state, now), "profileContext": state.Profile.Context}
	if task != nil {
		data["task"] = describeTask(*task)
	}
	raw, _ := json.Marshal(data)
	prompt := "次の場面で、本人に向けたひとことを日本語で1つだけ返してください。40文字以内、かぎかっこや説明は付けない。\n" + string(raw)
	result, err := p.generate(ctx, owner, prompt, &genai.GenerateContentConfig{SystemInstruction: persona(), MaxOutputTokens: 1000, ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow}})
	if err != nil {
		slog.Warn("companion line fell back", "moment", moment, "error", err)
		return fallback, false
	}
	line := strings.Trim(strings.TrimSpace(result.Text()), "「」\"")
	if !speakable(line, 60) {
		return fallback, false
	}
	p.lines.Range(func(k, v any) bool {
		if time.Now().After(v.(cachedLine).expires) {
			p.lines.Delete(k)
		}
		return true
	})
	p.lines.Store(key, cachedLine{line, time.Now().Add(lineReuse)})
	return line, true
}

func (s *Server) companionLine(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Moment string `json:"moment"`
		TaskID string `json:"taskId"`
	}
	if !decode(w, r, &in) {
		return
	}
	if _, ok := companionMoments[in.Moment]; !ok {
		respond(w, nil, errors.New("声かけの場面を確認できませんでした"))
		return
	}
	state, err := s.Store.Read(r.Context(), uid(r))
	if err != nil {
		respond(w, nil, err)
		return
	}
	var task *Task
	if in.TaskID != "" {
		if task, err = state.task(in.TaskID); err != nil {
			respond(w, nil, err)
			return
		}
	}
	line, generated := s.Planner.Line(r.Context(), uid(r), in.Moment, state, task)
	respond(w, map[string]any{"line": line, "name": CompanionName, "generated": generated}, nil)
}
