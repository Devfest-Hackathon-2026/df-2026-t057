// Package wallet は視聴時間（台帳・付与・消費・ルール）を扱う。
//
// 台帳（ledger）を正とする。付与・消費・朝の受け取りはすべて台帳への追記で表し、項目は書き換えも削除もしない。
// 口座（Account）は台帳から計算した結果で、同じトランザクションで更新する。Rebuild で台帳から作り直せる。
//
// 夜の間の付与は deferred の項目として追記し、残高には数えない。朝の受け取り（release）は、上限の範囲で残高に加える
// 項目と、精算した保留の秒数（SettledSeconds）を持つ。
package wallet

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"time"
)

// ルールの定数（§2.1）。
const (
	InitialSeconds            = 10 * 60
	BalanceCapSeconds         = 30 * 60
	DailyRewardCapSeconds     = 20 * 60
	WorkSecondsPerReward      = 5 * 60
	RewardSecondsPerUnit      = 60
	RegistrationRewardSeconds = 5 * 60
	DefaultDailyLimitSeconds  = 30 * 60
	// UnlimitedAvailableSeconds は reminder・self で区切りの長さの上限に使う値。
	UnlimitedAvailableSeconds = 10 * 60
	historyDays               = 14
)

// ViewingChoicesSeconds は視聴の区切りの長さ。
var ViewingChoicesSeconds = []int{300, 600}

// DailyLimitChoicesSeconds は1日の視聴上限の選択肢。
var DailyLimitChoicesSeconds = []int{600, 1200, 1800}

// Mode は視聴時間のルール。
type Mode string

const (
	ModeRewards  Mode = "rewards"
	ModeFixed    Mode = "fixed"
	ModeReminder Mode = "reminder"
	ModeSelf     Mode = "self"
)

// Valid は定義済みの値か。
func (m Mode) Valid() bool {
	switch m {
	case ModeRewards, ModeFixed, ModeReminder, ModeSelf:
		return true
	default:
		return false
	}
}

// Limited は残高を減らし、今日の上限を守るルールか。
func (m Mode) Limited() bool { return m == ModeRewards || m == ModeFixed }

// RequiresSession は視聴の前に区切りを選ぶ必要があるか。
func (m Mode) RequiresSession() bool { return m != ModeSelf }

// ValidDailyLimit は1日の上限の選択肢か。
func ValidDailyLimit(seconds int) bool {
	for _, c := range DailyLimitChoicesSeconds {
		if c == seconds {
			return true
		}
	}
	return false
}

// Kind は台帳の項目の種類。
type Kind string

const (
	KindInitial      Kind = "initial"
	KindRegistration Kind = "registration"
	KindDaily        Kind = "daily"
	KindWork         Kind = "work"
	KindSleep        Kind = "sleep"
	KindViewing      Kind = "viewing"
	KindRelease      Kind = "release"
)

// Entry は台帳の項目。ID はべき等キーで、同じ ID の項目は二度書かない。
type Entry struct {
	ID       string    `json:"id" firestore:"id"`
	Seq      int64     `json:"seq" firestore:"seq"`
	Kind     Kind      `json:"kind" firestore:"kind"`
	Seconds  int       `json:"seconds" firestore:"seconds"`
	At       time.Time `json:"at" firestore:"at"`
	Day      string    `json:"day" firestore:"day"`
	Deferred bool      `json:"deferred" firestore:"deferred"`
	// 以下は種類によって持つ。
	TaskID         string `json:"taskId,omitempty" firestore:"taskId,omitempty"`
	TaskTitle      string `json:"taskTitle,omitempty" firestore:"taskTitle,omitempty"`
	WorkSeconds    int    `json:"workSeconds,omitempty" firestore:"workSeconds,omitempty"`
	ViewedSeconds  int    `json:"viewedSeconds,omitempty" firestore:"viewedSeconds,omitempty"`
	SettledSeconds int    `json:"settledSeconds,omitempty" firestore:"settledSeconds,omitempty"`
}

var safeKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// EntryID は種類とべき等キーから台帳の ID を作る。API の Id の形（80文字以内の英数字・_・-）に収める。
func EntryID(kind Kind, key string) string {
	id := string(kind) + "-" + key
	if len(id) <= 80 && safeKey.MatchString(key) {
		return id
	}
	sum := sha256.Sum256([]byte(key))
	return string(kind) + "-" + hex.EncodeToString(sum[:20])
}

// DayTotals は1日の集計。
type DayTotals struct {
	Day    string `json:"day" firestore:"day"`
	Viewed int    `json:"viewed" firestore:"viewed"`
	Reward int    `json:"reward" firestore:"reward"`
	Work   int    `json:"work" firestore:"work"`
}

// Account は台帳から計算した口座と、今日の集計・ルール。
type Account struct {
	Balance    int       `json:"balance" firestore:"balance"`
	Deferred   int       `json:"deferred" firestore:"deferred"`
	LastSeq    int64     `json:"lastSeq" firestore:"lastSeq"`
	Mode       Mode      `json:"mode" firestore:"mode"`
	DailyLimit int       `json:"dailyLimit" firestore:"dailyLimit"`
	Day        string    `json:"day" firestore:"day"`
	DayStart   time.Time `json:"dayStart" firestore:"dayStart"`
	ResetAt    time.Time `json:"resetAt" firestore:"resetAt"`
	Today      DayTotals `json:"today" firestore:"today"`
	// History は終わった日の集計（古い順、最大14日）。
	History []DayTotals `json:"history" firestore:"history"`
}

// LocalDay は loc での日付（YYYY-MM-DD）。
func LocalDay(t time.Time, loc *time.Location) string { return t.In(loc).Format(time.DateOnly) }

func dayBounds(day string, loc *time.Location) (start, end time.Time) {
	d, _ := time.ParseInLocation(time.DateOnly, day, loc)
	return d.UTC(), d.AddDate(0, 0, 1).UTC()
}

// NewAccount は新しい口座と、最初の付与の項目（適用済み）を返す。
func NewAccount(now time.Time, loc *time.Location) (Account, Entry) {
	day := LocalDay(now, loc)
	start, end := dayBounds(day, loc)
	a := Account{Mode: ModeRewards, DailyLimit: DefaultDailyLimitSeconds, Day: day, DayStart: start, ResetAt: end, Today: DayTotals{Day: day}, History: []DayTotals{}}
	e := Entry{ID: string(KindInitial), Kind: KindInitial, Seconds: InitialSeconds, At: now}
	e = a.Apply(e)
	return a, e
}

// Apply は項目を口座に反映し、連番と日付を付けた項目を返す。
func (a *Account) Apply(e Entry) Entry {
	a.LastSeq++
	e.Seq = a.LastSeq
	if e.Day == "" {
		e.Day = a.Day
	}
	project(&a.Balance, &a.Deferred, e)
	if e.Day == a.Day {
		addTotals(&a.Today, e)
	}
	return e
}

func project(balance, deferred *int, e Entry) {
	if e.Deferred {
		*deferred += e.Seconds
	} else {
		*balance += e.Seconds
	}
	if e.Kind == KindRelease {
		*deferred -= e.SettledSeconds
	}
}

func addTotals(t *DayTotals, e Entry) {
	t.Work += e.WorkSeconds
	t.Viewed += e.ViewedSeconds
	if e.Kind == KindWork {
		t.Reward += e.Seconds
	}
}

// Room は上限まであと何秒付与できるか（保留中の報酬も上限に数える）。
func (a Account) Room() int { return max(0, BalanceCapSeconds-a.Balance-a.Deferred) }

// Available は今から視聴できる秒数。reminder・self は残高を使わないので、区切りの長さの上限を返す。
func (a Account) Available() int {
	if !a.Mode.Limited() {
		return UnlimitedAvailableSeconds
	}
	return max(0, min(a.Balance, a.DailyLimit-a.Today.Viewed))
}

// Rollover は本人のタイムゾーンで日付が進んでいれば、今日の集計を履歴に移して新しい日にする。
// タイムゾーンが変わったときも、その日付で判定する（日付が戻る方向には切り替えない）。
func (a *Account) Rollover(now time.Time, loc *time.Location) bool {
	day := LocalDay(now, loc)
	if day <= a.Day {
		a.DayStart, a.ResetAt = dayBounds(a.Day, loc)
		return false
	}
	a.History = append(a.History, a.Today)
	if len(a.History) > historyDays {
		a.History = a.History[len(a.History)-historyDays:]
	}
	a.Day = day
	a.DayStart, a.ResetAt = dayBounds(day, loc)
	a.Today = DayTotals{Day: day}
	return true
}

// Refill は日が変わったときの補充の項目（rewards は10分、fixed は1日の上限まで）。補充しなければ false。
func (a Account) Refill(now time.Time, night bool) (Entry, bool) {
	base := InitialSeconds
	switch a.Mode {
	case ModeRewards:
	case ModeFixed:
		base = a.DailyLimit
	case ModeReminder, ModeSelf:
		return Entry{}, false
	}
	seconds := max(0, min(base, BalanceCapSeconds)-a.Balance-a.Deferred)
	if seconds == 0 {
		return Entry{}, false
	}
	return Entry{ID: EntryID(KindDaily, a.Day), Kind: KindDaily, Seconds: seconds, At: now, Deferred: night}, true
}

// Grant は上限を反映した付与の項目。夜の間は保留にする（睡眠のボーナスは朝に付与するので保留しない）。
func (a Account) Grant(kind Kind, key string, requested int, now time.Time, night bool) Entry {
	return Entry{ID: EntryID(kind, key), Kind: kind, Seconds: max(0, min(requested, a.Room())), At: now, Deferred: night && kind != KindSleep}
}

// Interval は作業した区間。
type Interval struct {
	Start time.Time `json:"start" firestore:"start"`
	End   time.Time `json:"end" firestore:"end"`
}

// TodaysWork は区間のうち今日の分の秒数（日をまたいだ作業は今日の分だけを数える）。
func (a Account) TodaysWork(intervals []Interval) int {
	seconds := 0
	for _, iv := range intervals {
		start, end := iv.Start, iv.End
		if start.Before(a.DayStart) {
			start = a.DayStart
		}
		if end.After(a.ResetAt) {
			end = a.ResetAt
		}
		if end.After(start) {
			seconds += int(end.Sub(start) / time.Second)
		}
	}
	return seconds
}

// WorkReward は作業の記録の項目。今日の作業の累計5分ごとに1分。今日の報酬の上限と残高の上限を守る。
// 端数は次の記録に持ち越し、上限で削った分は後から付与しない。
func (a Account) WorkReward(sessionID, taskID, title string, workSeconds int, now time.Time, night bool) Entry {
	reward := 0
	if a.Mode == ModeRewards {
		units := (a.Today.Work+workSeconds)/WorkSecondsPerReward - a.Today.Work/WorkSecondsPerReward
		reward = max(0, min(units*RewardSecondsPerUnit, DailyRewardCapSeconds-a.Today.Reward, a.Room()))
	}
	return Entry{ID: EntryID(KindWork, sessionID), Kind: KindWork, Seconds: reward, WorkSeconds: workSeconds, TaskID: taskID, TaskTitle: title, At: now, Deferred: night}
}

// Viewing は視聴の項目。残高を減らすルールでは残高を超えて減らさない。視聴した秒数はどのルールでも数える。
func (a Account) Viewing(receipt string, viewed int, now time.Time) Entry {
	debit := 0
	if a.Mode.Limited() {
		debit = min(viewed, a.Balance)
	}
	return Entry{ID: EntryID(KindViewing, receipt), Kind: KindViewing, Seconds: -debit, ViewedSeconds: viewed, At: now}
}

// Release は保留中の報酬を受け取る項目。上限を超える分は受け取れない（精算はする）。保留が無ければ false。
func (a Account) Release(key string, now time.Time) (Entry, bool) {
	if a.Deferred <= 0 {
		return Entry{}, false
	}
	return Entry{ID: EntryID(KindRelease, key), Kind: KindRelease, Seconds: min(a.Deferred, max(0, BalanceCapSeconds-a.Balance)), SettledSeconds: a.Deferred, At: now}, true
}

// Projection は台帳だけから計算した結果。
type Projection struct {
	Balance  int
	Deferred int
	LastSeq  int64
	Days     map[string]DayTotals
}

// Rebuild は台帳から口座を作り直す。entries は連番の順でなくてもよい。
func Rebuild(entries []Entry) Projection {
	p := Projection{Days: map[string]DayTotals{}}
	for _, e := range entries {
		project(&p.Balance, &p.Deferred, e)
		t := p.Days[e.Day]
		t.Day = e.Day
		addTotals(&t, e)
		p.Days[e.Day] = t
		p.LastSeq = max(p.LastSeq, e.Seq)
	}
	return p
}
