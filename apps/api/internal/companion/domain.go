// Package companion は相棒の一言（§2.16）と、相棒（AI）に渡す進み具合の要約を扱う。
//
// 一言は AI が作った文だけを返す。AI が使えない・失敗した・出力が不適切なときは空で返し、
// 決まった文はクライアントが packages/messages から表示する（サーバーは表示の文言を組み立てない）。
package companion

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

// Moment は一言を出す場面。
type Moment string

const (
	MomentNotice      Moment = "notice"
	MomentSnooze      Moment = "snooze"
	MomentComplete    Moment = "complete"
	MomentBalanceZero Moment = "balance_zero"
	MomentAppOpen     Moment = "app_open"
	MomentNight       Moment = "night"
	MomentMorning     Moment = "morning"
)

// Valid は定義済みの値か。
func (m Moment) Valid() bool {
	switch m {
	case MomentNotice, MomentSnooze, MomentComplete, MomentBalanceZero, MomentAppOpen, MomentNight, MomentMorning:
		return true
	default:
		return false
	}
}

// MaxLineRunes は表示できる一言の長さの上限。英語は同じ内容でも文字数が多くなるので、上限を広げる。
var MaxLineRunes = map[locale.Locale]int{locale.Ja: 60, locale.En: 140}

// bannedWords は相棒が使わない言葉（診断や医学的な判断に聞こえるもの）。英語は小文字にしてから比べる。
// AI が指示と違う言語で返すこともあるので、言語によらずすべてを確かめる。
var bannedWords = []string{"依存症", "中毒", "治療", "病気", "意志が弱", "addict", "treatment", "disease", "illness", "weak-willed", "weak willed"}

// Speakable は AI の文が表示してよい長さで、使わない言葉を含まないか。
func Speakable(text string, limit int) bool {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > limit {
		return false
	}
	lower := strings.ToLower(text)
	for _, word := range bannedWords {
		if strings.Contains(lower, word) {
			return false
		}
	}
	return true
}

// CleanLine は AI が返した一言を整える（前後の空白とかぎかっこ・引用符を除く）。表示できなければ ok は false。
func CleanLine(raw string, lang locale.Locale) (string, bool) {
	line := strings.TrimSpace(strings.Trim(strings.TrimSpace(raw), "「」\"“”"))
	if !Speakable(line, MaxLineRunes[lang]) {
		return "", false
	}
	return line, true
}

// Support は相棒の手助けの強さ。続いた日数が増えるほど控えめにする（卒業に向けて）。
type Support string

const (
	SupportFull     Support = "full"
	SupportLight    Support = "light"
	SupportHandsOff Support = "hands-off"
)

// SupportLevel は続いた日数から手助けの強さを決める（0〜2日 full、3〜6日 light、7日以上 hands-off）。
func SupportLevel(streak int) Support {
	switch {
	case streak >= 7:
		return SupportHandsOff
	case streak >= 3:
		return SupportLight
	default:
		return SupportFull
	}
}

// StreakDays は、課題を1つ以上完了した本人の暦日が、今日（今日が無ければ昨日）から何日続いているか。
func StreakDays(tasks []task.Task, now time.Time, loc *time.Location) int {
	done := map[string]bool{}
	for _, t := range tasks {
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

// TaskSummary は AI に渡す課題の内容。
type TaskSummary struct {
	ID           string      `json:"id"`
	Title        string      `json:"title"`
	Notes        string      `json:"notes,omitempty"`
	URL          string      `json:"url,omitempty"`
	Why          string      `json:"why,omitempty"`
	Deadline     time.Time   `json:"deadline"`
	Minutes      int         `json:"minutes"`
	Start        time.Time   `json:"start"`
	End          time.Time   `json:"end"`
	FirstStep    string      `json:"firstStep,omitempty"`
	Status       task.Status `json:"status"`
	FromCalendar bool        `json:"fromCalendar"`
}

// Describe は課題を AI に渡す形にする。最初の手順が課題名と同じなら省く。時刻は本人のタイムゾーンで表す。
func Describe(t task.Task, fromCalendar bool, loc *time.Location) TaskSummary {
	s := TaskSummary{
		ID: t.ID, Title: t.Title, Notes: t.Notes, URL: t.URL, Why: t.Why, Deadline: t.Deadline.In(loc), Minutes: t.Minutes,
		Start: t.Plan.Start.In(loc), End: t.Plan.End.In(loc), Status: t.Status, FromCalendar: fromCalendar,
	}
	if len(t.Plan.Steps) > 0 && t.Plan.Steps[0].Title != t.Title {
		s.FirstStep = t.Plan.Steps[0].Title
	}
	return s
}

// TimePolicy は視聴時間のルール（AI が時間の増やし方を勧めてよいかの判断に使う）。
type TimePolicy struct {
	Mode              wallet.Mode `json:"mode"`
	DailyLimitMinutes int         `json:"dailyLimitMinutes"`
}

// PolicyOf は口座のルール。
func PolicyOf(a wallet.Account) TimePolicy {
	return TimePolicy{Mode: a.Mode, DailyLimitMinutes: a.DailyLimit / 60}
}

// SleepSummary は相棒が知ってよい睡眠の情報。
type SleepSummary struct {
	Set        bool   `json:"set"`
	Bedtime    string `json:"bedtime,omitempty"`
	Wake       string `json:"wake,omitempty"`
	KeptNights int    `json:"keptNights,omitempty"`
	NightNow   bool   `json:"nightNow,omitempty"`
	// LastNightWithoutShorts は昨夜の判定が済んでいるときだけ入る。
	LastNightWithoutShorts *bool `json:"lastNightWithoutShorts,omitempty"`
}

// SummarizeSleep は睡眠の状態を要約する。
func SummarizeSleep(st sleep.State, now time.Time, loc *time.Location) SleepSummary {
	if st.Sleep == nil {
		return SleepSummary{}
	}
	s := SleepSummary{Set: true, Bedtime: st.Sleep.Bedtime, Wake: st.Sleep.Wake, KeptNights: st.Sleep.KeptNights, NightNow: st.NightActive}
	if last := st.Sleep.LastNight(now, loc); st.Sleep.SettledNight == last.Key {
		without := st.Sleep.WatchedNight != last.Key
		s.LastNightWithoutShorts = &without
	}
	return s
}

// OpenCounts は iPhone で YouTube アプリを開いた回数（ショートカットを設定している場合だけ）。
type OpenCounts struct {
	Today     int `json:"today"`
	Last7Days int `json:"last7Days"`
}

// Progress は進み具合の要約（相棒の一言と相談の get_progress で使う）。
type Progress struct {
	StreakDays         int          `json:"streakDays"`
	Support            Support      `json:"support"`
	CompletedLast7Days int          `json:"completedLast7Days"`
	CompletedTotal     int          `json:"completedTotal"`
	RecentCompleted    []string     `json:"recentCompleted"`
	TimePolicy         TimePolicy   `json:"timePolicy"`
	ViewingMinutesLeft int          `json:"viewingMinutesLeft"`
	Sleep              SleepSummary `json:"sleep"`
	YouTubeAppOpens    *OpenCounts  `json:"youtubeAppOpens,omitempty"`
}

// ProgressInput は進み具合を計算する材料。
type ProgressInput struct {
	Tasks    []task.Task
	Account  wallet.Account
	Sleep    SleepSummary
	AppOpens *OpenCounts
	Now      time.Time
	Loc      *time.Location
}

// maxRecentCompleted は最近終えた課題として渡す件数。
const maxRecentCompleted = 3

// BuildProgress は進み具合を計算する。
func BuildProgress(in ProgressInput) Progress {
	streak := StreakDays(in.Tasks, in.Now, in.Loc)
	p := Progress{
		StreakDays: streak, Support: SupportLevel(streak), RecentCompleted: []string{},
		TimePolicy: PolicyOf(in.Account), ViewingMinutesLeft: in.Account.Available() / 60, Sleep: in.Sleep, YouTubeAppOpens: in.AppOpens,
	}
	var completed []task.Task
	for _, t := range in.Tasks {
		if t.CompletedAt == nil {
			continue
		}
		p.CompletedTotal++
		if in.Now.Sub(*t.CompletedAt) <= 7*24*time.Hour {
			p.CompletedLast7Days++
		}
		completed = append(completed, t)
	}
	slices.SortStableFunc(completed, func(a, b task.Task) int { return b.CompletedAt.Compare(*a.CompletedAt) })
	for i := 0; i < len(completed) && i < maxRecentCompleted; i++ {
		p.RecentCompleted = append(p.RecentCompleted, completed[i].Title)
	}
	return p
}

// LineRequest は一言を作るために AI に渡す内容。
type LineRequest struct {
	Moment         Moment
	Progress       Progress
	ProfileContext string
	Task           *TaskSummary
	Locale         locale.Locale
}

// Line は相棒の一言。Generated が false なら Text は空で、クライアントが決まった文を表示する。
type Line struct {
	Text      string
	Generated bool
}

// LineReuse は、同じ場面・課題・計画で作った一言を使い回す時間（同じ画面を開き直すたびに AI の回数を使わないように）。
const LineReuse = 6 * time.Hour

// lineKey は一言を使い回す単位。本人の暦日・場面・課題・計画の開始・言語が同じなら同じ一言にする。
func lineKey(uid string, moment Moment, t *task.Task, now time.Time, loc *time.Location, lang locale.Locale) string {
	key := uid + "|" + string(lang) + "|" + string(moment) + "|" + now.In(loc).Format(time.DateOnly)
	if t != nil {
		key += "|" + t.ID + "|" + t.Plan.Start.UTC().Format(time.RFC3339)
	}
	return key
}
