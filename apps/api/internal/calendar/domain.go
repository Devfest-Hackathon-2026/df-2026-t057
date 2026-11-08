// Package calendar は Google カレンダーとの同期を扱う。
//
// Firestore とカレンダーをまたぐトランザクションはないため、変更は Outbox のジョブとして積み、ジョブの処理で反映する。
// ジョブの処理はべき等にし、常に最新の状態を読んでから反映する。
//
// 保存する場所:
//   - users/{uid}/state/calendar      連携の状態（Connection）
//   - users/{uid}/calendarLinks/{id}  課題と予定の結び付き（Link）
//   - calendarChannels/{channelId}    変更通知のチャンネル（Channel）
//   - oauthStates/{state}             同意画面の state（OAuthState。TTL で消す）
//   - OAuth のトークンは Secret Manager（Google のポートの実装が持つ）
package calendar

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
)

// 予定の拡張プロパティ（private）のキー。
const (
	// PropTaskID は DOPAGAKI が作った予定に、課題の ID を残すキー。
	PropTaskID = "dopagakiTaskId"
	// PropSleep は就寝の予定の目印。
	PropSleep = "dopagakiSleep"
)

// 予定の状態（Google Calendar の status）。
const (
	EventCancelled = "cancelled"
	EventConfirmed = "confirmed"
)

// SyncState は連携の状態の表示用の区分。仕様の CalendarStatus.sync と同じ値。
type SyncState string

const (
	SyncDisconnected SyncState = "disconnected"
	SyncSynced       SyncState = "synced"
	SyncPending      SyncState = "pending"
	SyncFailed       SyncState = "failed"
	SyncReauth       SyncState = "reauth_required"
	SyncConflict     SyncState = "conflict"
)

// DeriveSync は連携の状態を決める。連携していなければ disconnected。反映待ちのジョブがあれば pending、
// 反映待ちがなく失敗したジョブがあれば failed、どちらもなければ synced。
func DeriveSync(connected bool, pending, failed int) SyncState {
	switch {
	case !connected:
		return SyncDisconnected
	case pending > 0:
		return SyncPending
	case failed > 0:
		return SyncFailed
	default:
		return SyncSynced
	}
}

// Connection は users/{uid}/state/calendar に保存する連携の状態。
type Connection struct {
	Reauth     bool   `json:"reauth" firestore:"reauth"`
	Connected  bool   `json:"connected" firestore:"connected"`
	CalendarID string `json:"calendarId" firestore:"calendarId"`
	GoogleID   string `json:"googleId" firestore:"googleId"`
	SyncToken  string `json:"syncToken" firestore:"syncToken"`
	// ChannelID・ChannelToken・ChannelResource は今の変更通知のチャンネル。ChannelResource はチャンネルを止めるときに使う。
	ChannelID       string     `json:"channelId" firestore:"channelId"`
	ChannelToken    string     `json:"channelToken" firestore:"channelToken"`
	ChannelResource string     `json:"channelResource" firestore:"channelResource"`
	ChannelExpires  *time.Time `json:"channelExpires,omitempty" firestore:"channelExpires,omitempty"`
	LastSync        *time.Time `json:"lastSync,omitempty" firestore:"lastSync,omitempty"`
	SleepEventID    string     `json:"sleepEventId" firestore:"sleepEventId"`
	// ContentSeq は課題ごとの内容の変更の連番。予定に反映した連番（Link.ContentSeq）より大きければ、
	// 題名・メモ・時刻を予定に書き直す必要がある。
	ContentSeq map[string]int64 `json:"contentSeq,omitempty" firestore:"contentSeq,omitempty"`
	// ExportJob・SyncJob は、まだ始まっていない書き直し・取り込みのジョブ（同じジョブを重ねて積まないため）。
	// ジョブは始まるときにこの値を消す（始まった後の変更は新しいジョブで反映する）。
	ExportJob string `json:"exportJob,omitempty" firestore:"exportJob,omitempty"`
	SyncJob   string `json:"syncJob,omitempty" firestore:"syncJob,omitempty"`
	// SleepJob は最後に積んだ就寝の予定のジョブ（反映待ちかの表示に使う）。
	SleepJob string `json:"sleepJob,omitempty" firestore:"sleepJob,omitempty"`
	// RemindersSet は本人が通知を選んだか。選ぶまでは Google の設定を変えない。
	// Reminders は予定の開始の何分前に通知するか（早い順。空なら通知しない）。連携を解除しても残す。
	RemindersSet bool  `json:"remindersSet,omitempty" firestore:"remindersSet,omitempty"`
	Reminders    []int `json:"reminders,omitempty" firestore:"reminders,omitempty"`
	// RemindersJob は、まだ始まっていない通知の書き換えのジョブ。
	RemindersJob string `json:"remindersJob,omitempty" firestore:"remindersJob,omitempty"`
}

// ChosenReminders は本人が選んだ通知。選んでいなければ nil（Google の設定のまま）。
func (c Connection) ChosenReminders() []int {
	if !c.RemindersSet {
		return nil
	}
	if c.Reminders == nil {
		return []int{}
	}
	return c.Reminders
}

// ContentPending は課題の内容の変更が予定に未反映か。
func (c Connection) ContentPending(l Link) bool { return c.ContentSeq[l.TaskID] > l.ContentSeq }

// Link は users/{uid}/calendarLinks/{taskId} に保存する、課題と予定の結び付き。
type Link struct {
	TaskID  string `json:"taskId" firestore:"taskId"`
	EventID string `json:"eventId" firestore:"eventId"`
	// ETag と InputHash は最後に読んだ（書いた）予定の版と内容のハッシュ。外部での編集の検出に使う。
	ETag      string `json:"etag" firestore:"etag"`
	InputHash string `json:"inputHash" firestore:"inputHash"`
	// ContentSeq は予定に反映した内容の連番（Connection.ContentSeq と比べる）。
	ContentSeq int64 `json:"contentSeq" firestore:"contentSeq"`
}

// Channel は calendarChannels/{channelId} に保存する変更通知のチャンネル。
type Channel struct {
	UID     string    `json:"uid" firestore:"uid"`
	Token   string    `json:"token" firestore:"token"`
	Expires time.Time `json:"expires" firestore:"expires"`
}

// OAuthState は oauthStates/{state} に保存する、同意画面に渡した state。一度だけ使える。
type OAuthState struct {
	UID       string    `json:"uid" firestore:"uid"`
	GoogleID  string    `json:"googleId" firestore:"googleId"`
	ExpiresAt time.Time `json:"expiresAt" firestore:"expiresAt"`
}

// EventTime は予定の開始・終了。終日の予定は Date だけを持つ。
type EventTime struct {
	DateTime string
	Date     string
	TimeZone string
}

// Event は Google カレンダーの予定（使う項目だけ）。
type Event struct {
	ID               string
	ETag             string
	Status           string
	Summary          string
	Description      string
	Start            EventTime
	End              EventTime
	Recurrence       []string
	RecurringEventID string
	Transparent      bool
	Private          map[string]string
}

// EventPatch は予定の一部の書き換え。nil の項目は変えない。
type EventPatch struct {
	Summary     *string
	Description string
	Start       *EventTime
	End         *EventTime
}

var tokenPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

// ValidToken は state・チャンネルの ID とトークンの形式（16進数32文字）か。ストレージを読む前に確かめる。
func ValidToken(v string) bool { return tokenPattern.MatchString(v) }

func shortHash(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:16])
}

// EventID は課題の予定の ID。同じ課題にはいつも同じ ID を使い、作り直しても二重にならないようにする。
func EventID(uid, taskID string) string { return shortHash(uid + ":" + taskID) }

// SleepEventID は就寝の予定の ID。
func SleepEventID(uid string) string { return shortHash(uid + ":sleep") }

// ImportedTaskID は、カレンダーで直接作られた予定から登録する課題の ID。
func ImportedTaskID(eventID string) string { return "calendar-" + shortHash(eventID) }

// StripBlock は説明から最初の DOPAGAKI のブロックを取り除き、前後の空白を除く。本人のメモはそのまま残す。
func StripBlock(description string) string {
	start := strings.Index(description, blockStart)
	end := strings.Index(description, blockEnd)
	if start >= 0 && end > start {
		return strings.TrimSpace(description[:start] + description[end+len(blockEnd):])
	}
	return strings.TrimSpace(description)
}

// instant は日時の表記を UTC にそろえる。同じ時刻の別の表記（+09:00 と Z）を同じものとして扱うため。
func instant(v string) string {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return v
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// Hash は予定の内容（題名・ブロックを除いた説明・開始・終了）のハッシュ。DOPAGAKI のブロックの書き直しでは変わらない。
func Hash(e Event) string {
	raw, _ := json.Marshal([]string{e.Summary, StripBlock(e.Description), instant(e.Start.DateTime), instant(e.End.DateTime)}) //nolint:errchkjson // 文字列の配列は必ず変換できる。
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// RenderDescription は本人のメモの後に、計画の手順と締切を書いたブロックを付ける。
// 計画の理由（Reason）は表示の文言をクライアントが持つ code なので、予定には書かない。
func RenderDescription(notes string, t task.Task, loc *time.Location, lang locale.Locale) string {
	var b strings.Builder
	b.WriteString(blockStart + "\n")
	for _, step := range t.Plan.Steps {
		fmt.Fprintf(&b, "%s–%s %s\n", step.Start.In(loc).Format("15:04"), step.End.In(loc).Format("15:04"), step.Title)
	}
	b.WriteString(texts[lang].deadlineLabel + t.Deadline.In(loc).Format("01/02 15:04") + "\n" + blockEnd)
	return strings.TrimSpace(notes + "\n\n" + b.String())
}

// taskTimes は課題の予定の時刻（締切から課題の時間だけ前 〜 締切）。スヌーズした計画の時刻には合わせない。
func taskTimes(t task.Task) (EventTime, EventTime) {
	end := t.Deadline.UTC()
	start := end.Add(-time.Duration(t.Minutes) * time.Minute)
	return EventTime{DateTime: start.Format(time.RFC3339)}, EventTime{DateTime: end.Format(time.RFC3339)}
}

// NewTaskEvent は課題から作る予定。
func NewTaskEvent(uid string, t task.Task, loc *time.Location, lang locale.Locale) Event {
	start, end := taskTimes(t)
	return Event{
		ID: EventID(uid, t.ID), Summary: t.Title, Description: RenderDescription(t.Notes, t, loc, lang),
		Start: start, End: end, Private: map[string]string{PropTaskID: t.ID},
	}
}

// PlanPatch は計画だけを書き直す（本人のメモと、予定の題名・時刻はそのまま）。
func PlanPatch(t task.Task, e Event, loc *time.Location, lang locale.Locale) EventPatch {
	return EventPatch{Description: RenderDescription(StripBlock(e.Description), t, loc, lang)}
}

// ContentPatch は課題の内容（題名・メモ・時刻）と計画を書き直す。
func ContentPatch(t task.Task, loc *time.Location, lang locale.Locale) EventPatch {
	start, end := taskTimes(t)
	title := t.Title
	return EventPatch{Summary: &title, Description: RenderDescription(t.Notes, t, loc, lang), Start: &start, End: &end}
}

// Apply は書き換えた後の予定。
func (p EventPatch) Apply(e Event) Event {
	e.Description = p.Description
	if p.Summary != nil {
		e.Summary = *p.Summary
	}
	if p.Start != nil {
		e.Start = *p.Start
	}
	if p.End != nil {
		e.End = *p.End
	}
	return e
}

// Changes は書き換えで予定が変わるか（同じ時刻の別の表記は変わらないとみなす）。
func (p EventPatch) Changes(e Event) bool {
	after := p.Apply(e)
	return after.Description != e.Description || Hash(after) != Hash(e)
}

// ExternallyEdited は、最後に読んだ後に予定がカレンダーで編集されたか。版が変わり、かつ内容のハッシュも変わったときだけ
// 編集とみなす（DOPAGAKI のブロックの書き直しや、時刻の表記の違いでは版だけが変わる）。
func ExternallyEdited(l Link, e Event) bool {
	return l.ETag != "" && e.ETag != l.ETag && l.InputHash != "" && Hash(e) != l.InputHash
}

// EventChange はカレンダーから取り込む予定の変更。
type EventChange struct {
	EventID   string
	Cancelled bool
	// TaskIDHint は予定に残した課題の ID（DOPAGAKI が作った予定だけ）。
	TaskIDHint string
	ETag       string
	Title      string
	Notes      string
	Deadline   time.Time
	Minutes    int
	Hash       string
}

// Classify は予定を取り込みの対象として読む。取り込まない予定（繰り返し・終日・読めない時刻・終わった予定・
// 課題として扱えない長さ）は false。消された予定は Cancelled を返す。
func Classify(e Event, now time.Time) (EventChange, bool) {
	c := EventChange{EventID: e.ID, ETag: e.ETag, TaskIDHint: e.Private[PropTaskID]}
	if e.Status == EventCancelled {
		c.Cancelled = true
		return c, true
	}
	if len(e.Recurrence) > 0 || e.RecurringEventID != "" || e.Start.DateTime == "" || e.End.DateTime == "" {
		return EventChange{}, false
	}
	start, err1 := time.Parse(time.RFC3339, e.Start.DateTime)
	end, err2 := time.Parse(time.RFC3339, e.End.DateTime)
	if err1 != nil || err2 != nil || end.Before(now) {
		return EventChange{}, false
	}
	minutes := int(end.Sub(start).Minutes())
	if minutes < task.MinMinutes || minutes > task.MaxMinutes {
		return EventChange{}, false
	}
	c.Title, c.Notes, c.Deadline, c.Minutes, c.Hash = e.Summary, StripBlock(e.Description), end.UTC(), minutes, Hash(e)
	return c, true
}

// TaskIDFor は予定に対応する課題の ID。結び付きがあればそれを、なければ予定に残した ID を、どちらもなければ予定の ID から作る。
// eventTask は予定の ID → 課題の ID、taskEvent は課題の ID → 予定の ID の結び付き。予定に残した ID の課題が別の予定と
// 結び付いているとき（カレンダーで予定を複製したときなど）は、別の課題として扱う。
func TaskIDFor(c EventChange, eventTask, taskEvent map[string]string) string {
	if id := eventTask[c.EventID]; id != "" {
		return id
	}
	if c.TaskIDHint != "" {
		if other := taskEvent[c.TaskIDHint]; other == "" || other == c.EventID {
			return c.TaskIDHint
		}
	}
	return ImportedTaskID(c.EventID)
}

// LatestByID は同じ予定が何度も含まれるとき、最後のものだけを残す（順番は最後に現れた位置）。
func LatestByID(events []Event) []Event {
	last := map[string]int{}
	for i, e := range events {
		last[e.ID] = i
	}
	out := make([]Event, 0, len(last))
	for i, e := range events {
		if last[e.ID] == i {
			out = append(out, e)
		}
	}
	return out
}

// SameContent は、予定の内容が課題と同じか（自分で書いた予定の通知を取り込み直さないため）。
func SameContent(t task.Task, c EventChange) bool {
	return t.Title == strings.TrimSpace(c.Title) && t.Notes == c.Notes && t.Deadline.Equal(c.Deadline) && t.Minutes == c.Minutes
}

// Import は取り込みの変更を課題の機能に渡す形にする。
func (c EventChange) Import(taskID string, detail *task.DetailResult) task.Imported {
	if c.Cancelled {
		return task.Imported{TaskID: taskID, Removed: true}
	}
	return task.Imported{TaskID: taskID, Title: c.Title, Notes: c.Notes, Deadline: c.Deadline, Minutes: c.Minutes, Detail: detail}
}

// SleepEvent は就寝の予定。毎日繰り返し、ほかの予定の邪魔をしない（transparent）。本人が消した予定も、書き直すと戻る（confirmed）。
func SleepEvent(uid string, s sleep.Schedule, now time.Time, loc *time.Location, lang locale.Locale) Event {
	night := s.NightOf(now, loc)
	start := night.Start.Add(sleep.WindDown).In(loc)
	end := night.End.In(loc)
	return Event{
		ID: SleepEventID(uid), Status: EventConfirmed, Summary: texts[lang].sleepSummary,
		Description: fmt.Sprintf(texts[lang].sleepDescriptionFormat, s.Bedtime, s.Wake),
		Start:       EventTime{DateTime: start.Format(time.RFC3339), TimeZone: loc.String()},
		End:         EventTime{DateTime: end.Format(time.RFC3339), TimeZone: loc.String()},
		Recurrence:  []string{"RRULE:FREQ=DAILY"}, Transparent: true,
		Private: map[string]string{PropSleep: "1"},
	}
}

// Google カレンダーの通知の上限（1つのカレンダーの既定の通知は5件まで、4週間前まで）。
const (
	MaxReminders       = 5
	MaxReminderMinutes = 4 * 7 * 24 * 60
)

// NormalizeReminders は、予定の開始の何分前に通知するかの一覧を検証し、重複を除いて早い順（大きい順）に並べる。
// 空の一覧は「通知しない」。
func NormalizeReminders(minutes []int) ([]int, error) {
	seen := map[int]bool{}
	out := make([]int, 0, len(minutes))
	for _, m := range minutes {
		if m < 0 || m > MaxReminderMinutes {
			return nil, apperr.Invalid("request.invalid", apperr.Field("minutes", "out_of_range"))
		}
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	if len(out) > MaxReminders {
		return nil, apperr.Invalid("request.invalid", apperr.Field("minutes", "too_long"))
	}
	slices.Sort(out)
	slices.Reverse(out)
	return out, nil
}
