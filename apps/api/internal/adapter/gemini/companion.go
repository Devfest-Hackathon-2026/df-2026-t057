package gemini

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/genai"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/companion"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"
)

const lineTokens = 1000

// lineRunes は AI に頼む一言の長さ。companion.MaxLineRunes より短くして、少し超えても表示できるようにする。
var lineRunes = map[locale.Locale]int{locale.Ja: 40, locale.En: 100}

// momentDescriptions は場面ごとに、相棒が何を意識して声をかけるか（AI への指示）。
var momentDescriptions = map[companion.Moment]string{
	companion.MomentNotice:      "ショート動画を見ている途中で、予定の開始時刻が来た。責めずに、課題のWhyや最初の一手に触れて、動画から離れて始めるよう背中を押す",
	companion.MomentSnooze:      "本人がスヌーズを選んだ。責めずに受け止め、次に始める時刻へ気持ちをつなぐ",
	companion.MomentComplete:    "本人が課題を完了した。具体的に認めて、続いた日数があれば触れる",
	companion.MomentBalanceZero: "今日の視聴時間を使い切った。罰ではなく区切りとして伝え、次の一手で時間が増えることに触れる",
	companion.MomentAppOpen:     "スマホでYouTubeアプリを開いた瞬間。止めたり責めたりせず、次の課題のWhyか最初の一手に触れて、見る前に一呼吸おけるよう声をかける。課題がなければ、今日やりたいことを一つ思い浮かべてもらう",
	companion.MomentNight:       "就寝1時間前を過ぎてもショートを見ている。責めずに、スマホを置く・歯をみがくなど寝る準備の小さな一歩を促す。今夜の報酬は朝に受け取れる",
	companion.MomentMorning:     "朝、アプリを開いた。sleep.lastNightWithoutShortsがtrueなら区切れたことを具体的に認め、falseでも責めずに今日の最初の一手へつなぐ",
}

type lineData struct {
	Moment         string                 `json:"moment"`
	Progress       companion.Progress     `json:"progress"`
	ProfileContext string                 `json:"profileContext"`
	Task           *companion.TaskSummary `json:"task,omitempty"`
}

// Line は場面に合わせた一言を1つ作る。返す文の検証（長さ・使わない言葉）は companion.Service が行う。
func (c *Client) Line(ctx context.Context, uid string, req companion.LineRequest) (string, error) {
	description, ok := momentDescriptions[req.Moment]
	if !ok {
		return "", unreadable()
	}
	raw, err := json.Marshal(lineData{Moment: description, Progress: req.Progress, ProfileContext: req.ProfileContext, Task: req.Task})
	if err != nil {
		return "", fmt.Errorf("gemini: encode line: %w", err)
	}
	prompt := fmt.Sprintf("次の場面で、本人に向けたひとことを%sで1つだけ返してください。%d文字以内、かぎかっこや説明は付けない。\n%s", languageNames[req.Locale], lineRunes[req.Locale], raw)
	res, err := c.generate(ctx, uid, genai.Text(prompt), &genai.GenerateContentConfig{
		SystemInstruction: persona(req.Locale), MaxOutputTokens: lineTokens, ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow},
	})
	if err != nil {
		return "", err
	}
	return responseText(res), nil
}
