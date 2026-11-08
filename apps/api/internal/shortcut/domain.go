// Package shortcut は iPhone のショートカットを扱う。YouTube アプリを開いた・閉じた時刻を記録し、
// 持ち時間を使い切るときや課題を始める時刻に、まだ開いたままなら相棒の一言を通知する。
// アカウントを作らず、接続キーのハッシュだけを保存する。
package shortcut

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

const (
	// PerAccountPerMinute と TotalPerMinute は呼び出しの上限（インスタンスごと）。
	PerAccountPerMinute = 10
	TotalPerMinute      = 300
	appOpenRetention    = 14 * 24 * time.Hour
	maxAppOpens         = 500
	secretLength        = 64
	// maxAppUse より長く「閉じた」が来なければ、閉じた通知を取りこぼしたとみなし、使った時間に数えない。
	maxAppUse = 3 * time.Hour
	// minCountedUse より短く開いていた回は、回数と時間に数えない（ほかのアプリへ移るときに一瞬通っただけの回）。
	minCountedUse = 30 * time.Second
	// FirstWaitMin は開いてから最初に確かめるまでの最短の待ち。一瞬だけ開いた回に反応しないため。
	FirstWaitMin = 60 * time.Second
	// MaxWait はショートカットが1回に待つ最長。実機で15分まで待てることを確かめた。
	MaxWait = 15 * time.Minute
	// minRewait は聞き直したあと待ち直すときの最短（呼び出しが詰まらないように）。
	minRewait = 30 * time.Second
	// dueTolerance だけ手前なら、通知の時刻になったとみなす（ショートカットの待ちのずれを吸収する）。
	dueTolerance = 5 * time.Second
)

// Key は保存する接続キーの情報。
type Key struct {
	Hash      string    `json:"keyHash" firestore:"keyHash"`
	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
}

// AppOpens は YouTube を開いた時刻と、開いてから閉じるまでの記録（どちらも14日、最大500件）。
type AppOpens struct {
	Times []time.Time `json:"times" firestore:"times"`
	Uses  []AppUse    `json:"uses,omitempty" firestore:"uses,omitempty"`
	// Notified は最後に通知した「開いた」の時刻。同じ回で2回通知しない。
	Notified *time.Time `json:"notified,omitempty" firestore:"notified,omitempty"`
}

// AppUse は YouTube アプリが前面にあった1回分。振り返りにだけ使い、視聴時間の残高からは引かない
// （アプリでは Shorts 以外の動画も見るため）。
type AppUse struct {
	Start time.Time `json:"start" firestore:"start"`
	End   time.Time `json:"end" firestore:"end"`
}

// Minutes は使った分数（切り捨て）。
func (u AppUse) Minutes() int { return int(u.End.Sub(u.Start).Minutes()) }

// FormatKey は "<base64url(uid)>.<secret>" の接続キーを作る。uid の部分でアカウントを特定し、索引を持たない。
func FormatKey(uid, secret string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(uid)) + "." + secret
}

// HashSecret は秘密の部分のハッシュ。
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// ParseKey は接続キーを uid と秘密のハッシュに分ける。
func ParseKey(key string) (uid, hash string, ok bool) {
	owner, secret, found := strings.Cut(key, ".")
	if !found || len(owner) > 200 || len(secret) != secretLength {
		return "", "", false
	}
	for _, r := range secret {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return "", "", false
		}
	}
	raw, err := base64.RawURLEncoding.DecodeString(owner)
	if err != nil || len(raw) == 0 {
		return "", "", false
	}
	return string(raw), HashSecret(secret), true
}

// Matches は保存したハッシュと一致するか（時間で推測されないよう、一定時間で比べる）。
func (k Key) Matches(hash string) bool {
	return subtle.ConstantTimeCompare([]byte(k.Hash), []byte(hash)) == 1
}

// Record は開いた時刻を加え、古いものを捨てる。
func (a AppOpens) Record(now time.Time) AppOpens {
	kept := make([]time.Time, 0, len(a.Times)+1)
	for _, t := range a.Times {
		if now.Sub(t) < appOpenRetention {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	if len(kept) > maxAppOpens {
		kept = kept[len(kept)-maxAppOpens:]
	}
	return AppOpens{Times: kept, Uses: a.Uses, Notified: a.Notified}
}

// RecordClose は閉じた時刻を、まだ閉じていない最後の「開いた」と組にする。
// 組にできない（開いた記録が無い、もう組にした、3時間を超えた）ときは ok が false で、何も変えない。
func (a AppOpens) RecordClose(now time.Time) (next AppOpens, use AppUse, ok bool) {
	if len(a.Times) == 0 {
		return a, AppUse{}, false
	}
	start := a.Times[len(a.Times)-1]
	if n := len(a.Uses); n > 0 && !start.After(a.Uses[n-1].Start) {
		return a, AppUse{}, false
	}
	if !now.After(start) || now.Sub(start) > maxAppUse {
		return a, AppUse{}, false
	}
	use = AppUse{Start: start, End: now}
	kept := make([]AppUse, 0, len(a.Uses)+1)
	for _, u := range a.Uses {
		if now.Sub(u.End) < appOpenRetention {
			kept = append(kept, u)
		}
	}
	kept = append(kept, use)
	if len(kept) > maxAppOpens {
		kept = kept[len(kept)-maxAppOpens:]
	}
	return AppOpens{Times: a.Times, Uses: kept, Notified: a.Notified}, use, true
}

// UseMinutes は今日（本人のタイムゾーン）と直近7日に使った分数。
func (a AppOpens) UseMinutes(now time.Time, loc *time.Location) (today, week int) {
	day := now.In(loc).Format(time.DateOnly)
	var t, w time.Duration
	for _, u := range a.Uses {
		if u.End.Sub(u.Start) < minCountedUse {
			continue
		}
		if u.End.In(loc).Format(time.DateOnly) == day {
			t += u.End.Sub(u.Start)
		}
		if now.Sub(u.End) < 7*24*time.Hour {
			w += u.End.Sub(u.Start)
		}
	}
	return int(t.Minutes()), int(w.Minutes())
}

// Counts は今日（本人のタイムゾーン）と直近7日の回数。
func (a AppOpens) Counts(now time.Time, loc *time.Location) (today, week int) {
	day := now.In(loc).Format(time.DateOnly)
	for _, t := range a.Times {
		if a.passedThrough(t) {
			continue
		}
		if t.In(loc).Format(time.DateOnly) == day {
			today++
		}
		if now.Sub(t) < 7*24*time.Hour {
			week++
		}
	}
	return today, week
}

// passedThrough は、開いてから minCountedUse 未満で閉じた回か。
func (a AppOpens) passedThrough(opened time.Time) bool {
	for _, u := range a.Uses {
		if u.Start.Equal(opened) {
			return u.End.Sub(u.Start) < minCountedUse
		}
	}
	return false
}

// FormatOpenID は「開いた」1回分の番号。開いた時刻（マイクロ秒）をそのまま使う。
func FormatOpenID(opened time.Time) string { return strconv.FormatInt(opened.UnixMicro(), 10) }

// ParseOpenID は番号を開いた時刻に戻す。
func ParseOpenID(id string) (time.Time, bool) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}, false
	}
	return time.UnixMicro(n).UTC(), true
}

// Watching は、その「開いた」がまだ続いていて、通知していない最新の回か。
// もっと新しい「開いた」がある、閉じた、もう通知した、のどれかなら false。
func (a AppOpens) Watching(opened time.Time) bool {
	if len(a.Times) == 0 || !a.Times[len(a.Times)-1].Equal(opened) {
		return false
	}
	if a.Notified != nil && a.Notified.Equal(opened) {
		return false
	}
	for _, u := range a.Uses {
		if u.Start.Equal(opened) {
			return false
		}
	}
	return true
}

// MarkNotified は、その「開いた」で通知したことを記録する。
func (a AppOpens) MarkNotified(opened time.Time) AppOpens {
	a.Notified = &opened
	return a
}

// DueReason は通知する理由。
type DueReason string

const (
	// DueBalance は持ち時間を使い切った。
	DueBalance DueReason = "balance"
	// DueTask は課題を始める時刻になった。
	DueTask DueReason = "task"
)

// Due は通知する時刻と理由。
type Due struct {
	At     time.Time
	Reason DueReason
}

// NextDue は通知する時刻を決める。持ち時間を使い切る時刻と、課題を始める時刻の早い方（同じなら持ち時間）。
// 持ち時間は開いた時刻から数える。iPhone で見た時間は残高から引かないので、待っている間に作業して増えた分も反映される。
// limited が false（残高を使わないモード）なら持ち時間は使わない。taskStart がゼロなら課題は使わない。
func NextDue(opened time.Time, availableSeconds int, limited bool, taskStart time.Time) (Due, bool) {
	var due Due
	found := false
	if limited {
		due, found = Due{At: opened.Add(time.Duration(availableSeconds) * time.Second), Reason: DueBalance}, true
	}
	if !taskStart.IsZero() && (!found || taskStart.Before(due.At)) {
		due, found = Due{At: taskStart, Reason: DueTask}, true
	}
	return due, found
}

// FirstWait は開いたときに返す待ち時間。FirstWaitMin〜MaxWait に収める。通知する時刻が無ければ 0（待たない）。
func FirstWait(due Due, found bool, now time.Time) time.Duration {
	if !found {
		return 0
	}
	return min(max(due.At.Sub(now), FirstWaitMin), MaxWait)
}

// Action は聞き直したときにショートカットがすること。
type Action string

const (
	ActionNotify Action = "notify"
	ActionWait   Action = "wait"
	ActionNone   Action = "none"
)

// Decide は聞き直した時点で、通知するか、待ち直すか、何もしないかを決める。
// 待っている間に持ち時間が増えたりスヌーズしたりして時刻が後ろにずれていれば、その分だけ待ち直す。
func Decide(due Due, found bool, now time.Time) (Action, time.Duration) {
	if !found {
		return ActionNone, 0
	}
	left := due.At.Sub(now)
	if left <= dueTolerance {
		return ActionNotify, 0
	}
	return ActionWait, min(max(left, minRewait), MaxWait)
}
