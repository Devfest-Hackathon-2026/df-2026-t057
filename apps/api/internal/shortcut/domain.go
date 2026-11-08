// Package shortcut は iPhone のショートカット（YouTube を開いた・閉じたときに相棒の一言を通知する）を扱う。
// アカウントを作らず、接続キーのハッシュだけを保存する。
package shortcut

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
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
	return AppOpens{Times: kept, Uses: a.Uses}
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
	return AppOpens{Times: a.Times, Uses: kept}, use, true
}

// UseMinutes は今日（本人のタイムゾーン）と直近7日に使った分数。
func (a AppOpens) UseMinutes(now time.Time, loc *time.Location) (today, week int) {
	day := now.In(loc).Format(time.DateOnly)
	var t, w time.Duration
	for _, u := range a.Uses {
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
		if t.In(loc).Format(time.DateOnly) == day {
			today++
		}
		if now.Sub(t) < 7*24*time.Hour {
			week++
		}
	}
	return today, week
}
