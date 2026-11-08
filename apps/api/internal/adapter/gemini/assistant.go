package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/genai"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/assistant"
)

const consultTokens = 5000

const consultPrompt = `これは相談チャットです。本人の発言(user)に、{LANGUAGE}の短い平文（ふだん3文以内、最大2000文字）で答えます。
進め方：受け止める → Whyがまだなら本人の言葉で引き出す → 最初の一歩を5分以内の行動にする → 計画を提案する → 「これでいけそう？」と確認する。質問は1回の返答で1つまで。
新しい課題の締切と所要時間がそろったら、Whyや最初の一歩が未定でもその返答で propose_task を出し、残りはカードを見せながら聞く。
道具：予定・課題に触れる前に get_today、前進を認めたり振り返るときは get_progress、カレンダーの話は get_calendar、好みは get_profile を呼んで事実を確かめる。データにない予定や数字を作らない。
提案：propose_task / propose_reschedule / propose_update / propose_memory / propose_sleep は候補カードを出すだけで、本人が押すまで保存されない。予定や情報を保存したとは言わない。カードは1回の返答で最大3枚。
締切や所要時間が不明なら決めつけずに1つだけ質問し、提案の道具は呼ばない。過去の未登録の課題候補を直すときは、元のカードのidをproposalIdに入れて全項目を出し直す。登録済みの課題はpropose_taskで複製しない。開始時刻だけ動かすならpropose_reschedule、課題名・メモ・Why・締切・必要時間・URLを変えるならpropose_updateを使う（変えない項目は現在値のまま）。
selectedTaskがあるとき「この予定」「この課題」はその課題を指す。別の課題を対象にするなら本人に確かめる。
睡眠：夜ふかしや寝る時間の話題では get_today の sleep を確かめ、本人が就寝・起床時刻を決めたら propose_sleep を出す。時刻は本人に決めてもらい、睡眠時間の医学的な指導はしない。
sleep.nightNow が true（就寝前の時間帯）のときは、今夜に作業を詰め込む提案をしない。明日の最初の一手を決めるか、寝る準備の小さな一歩までにとどめる。課題の報酬は翌朝に受け取れると伝えてよい。
日付はtimeZoneとnowを使う。「それ」は直前の会話(history)から読み取る。
入力JSON:
`

// proposalAck は提案の道具に返す結果（まだ保存していないことを AI に伝える）。
var proposalAck = map[string]any{"status": "本人の確認待ちとして表示します。まだ保存されていません"}

func consultTools() []*genai.Tool {
	empty := object(map[string]*genai.Schema{})
	return []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{
		{Name: assistant.ToolGetToday, Description: "今日と明日の未完了の課題（予定時刻・Why・最初の一手）、作業中の課題、残りの視聴時間、就寝・起床の設定と今が夜の時間帯かを読む。", Parameters: empty},
		{Name: assistant.ToolGetProgress, Description: "続いた日数、直近7日の完了数、最近終えた課題、ショートを見ずに寝られた夜の連続数を読む。前進を認めたり振り返るときに使う。", Parameters: empty},
		{Name: assistant.ToolGetCalendar, Description: "DOPAGAKIカレンダーと同期した予定と同期状況を読む。本人の他のカレンダーは読めない。", Parameters: empty},
		{Name: assistant.ToolGetProfile, Description: "本人が保存した作業の傾向・好み・タイムゾーンを読む。", Parameters: empty},
		{Name: assistant.ToolProposeTask, Description: "新しい課題を登録候補として本人に見せる。本人がカードを押すまで保存されない。締切や所要時間が不明なら呼ばずに質問する。登録済みの課題には使わない。", Parameters: object(map[string]*genai.Schema{
			"title":      str("課題名"),
			"notes":      str("メモ（1000文字以内）"),
			"why":        str("本人が言ったやる理由。言っていなければ空文字。あなたが作らない"),
			"deadline":   str("締切。タイムゾーン付きRFC3339"),
			"minutes":    integer("必要な分数"),
			"url":        str("関連URL。なければ空文字"),
			"proposalId": str("修正する未登録カードのid。新規なら省略"),
		}, "title", "deadline", "minutes")},
		{Name: assistant.ToolProposeSchedule, Description: "既存の課題の時間変更を候補として本人に見せる。本人がOKを押すまで変わらない。", Parameters: object(map[string]*genai.Schema{
			"taskId": str("get_todayなどで得た実在する課題のid"),
			"start":  str("新しい開始時刻（タイムゾーン付きRFC3339）。参照だけなら省略"),
		}, "taskId")},
		{Name: assistant.ToolProposeUpdate, Description: "未開始の既存課題の内容（課題名・メモ・Why・締切・必要時間・URL）の変更を候補として本人に見せる。変えない項目は現在値のまま全項目を出す。本人が押すまで変わらない。", Parameters: object(map[string]*genai.Schema{
			"taskId":   str("get_todayなどで得た実在する課題のid"),
			"title":    str("課題名"),
			"notes":    str("メモ"),
			"why":      str("やる理由。本人が言っていなければ現在値"),
			"deadline": str("締切。タイムゾーン付きRFC3339"),
			"minutes":  integer("必要な分数"),
			"url":      str("関連URL。なければ空文字"),
		}, "taskId", "title", "deadline", "minutes")},
		{Name: assistant.ToolProposeMemory, Description: "本人がはっきり言った作業の好み・苦手・時間帯を、覚える候補として見せる。推測した性格や機微な情報は使わない。", Parameters: object(map[string]*genai.Schema{
			"memory": str("短く覚える内容（500文字以内）"),
		}, "memory")},
		{Name: assistant.ToolProposeSleep, Description: "本人が決めた就寝・起床時刻を、毎日の就寝予定としてカレンダーに入れる候補を見せる。本人が押すまで保存されない。就寝1時間前からの報酬は翌朝に受け取りになり、ショートを見ずに寝られた朝は最大15分（残高30分まで）。", Parameters: object(map[string]*genai.Schema{
			"bedtime": str("就寝時刻 HH:MM（24時間表記）"),
			"wake":    str("起床時刻 HH:MM（24時間表記）"),
		}, "bedtime", "wake")},
	}}}
}

// Consult は相談の会話を始める。AI は Next を呼んだときに呼ぶ。
func (c *Client) Consult(in assistant.ConsultInput) assistant.Conversation {
	return &conversation{client: c, input: in}
}

// conversation は1回の相談の会話。AI の応答（考えの署名を含む）をそのまま次の呼び出しに渡す。
type conversation struct {
	client   *Client
	input    assistant.ConsultInput
	contents []*genai.Content
}

func (v *conversation) Next(ctx context.Context, uid string, results []assistant.ToolResult) (assistant.Step, error) {
	if v.contents == nil {
		raw, err := json.Marshal(v.input)
		if err != nil {
			return assistant.Step{}, fmt.Errorf("gemini: encode consultation: %w", err)
		}
		prompt := strings.ReplaceAll(consultPrompt, "{LANGUAGE}", languageNames[v.input.Locale])
		v.contents = []*genai.Content{genai.NewContentFromText(prompt+string(raw), genai.RoleUser)}
	}
	if len(results) > 0 {
		parts := make([]*genai.Part, 0, len(results))
		for _, r := range results {
			response := proposalAck
			if !r.Proposal {
				var err error
				if response, err = toMap(r.Data); err != nil {
					return assistant.Step{}, err
				}
			}
			part := genai.NewPartFromFunctionResponse(r.Name, response)
			part.FunctionResponse.ID = r.ID
			parts = append(parts, part)
		}
		v.contents = append(v.contents, genai.NewContentFromParts(parts, genai.RoleUser))
	}
	res, err := v.client.generate(ctx, uid, v.contents, &genai.GenerateContentConfig{
		SystemInstruction: persona(v.input.Locale), Tools: consultTools(), MaxOutputTokens: consultTokens,
		ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow},
	})
	if err != nil {
		return assistant.Step{}, err
	}
	if len(res.Candidates) == 0 || res.Candidates[0].Content == nil {
		return assistant.Step{}, unreadable()
	}
	content := res.Candidates[0].Content
	v.contents = append(v.contents, content)
	step := assistant.Step{Reply: responseText(res)}
	for _, part := range content.Parts {
		if part != nil && part.FunctionCall != nil {
			step.Calls = append(step.Calls, assistant.ToolCall{ID: part.FunctionCall.ID, Name: part.FunctionCall.Name, Args: part.FunctionCall.Args})
		}
	}
	return step, nil
}

// toMap は道具の結果を、関数の応答に入れる形（JSON のオブジェクト）にする。
func toMap(v any) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("gemini: encode tool result: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("gemini: encode tool result: %w", err)
	}
	return out, nil
}
