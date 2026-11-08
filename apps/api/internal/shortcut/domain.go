// Package shortcut は iPhone のショートカット（YouTube を開いたときに相棒の一言を通知する）を扱う。
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
)

// Key は保存する接続キーの情報。
type Key struct {
	Hash      string    `json:"keyHash" firestore:"keyHash"`
	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
}

// AppOpens は YouTube を開いた時刻（14日、最大500件）。
type AppOpens struct {
	Times []time.Time `json:"times" firestore:"times"`
}

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
	return AppOpens{Times: kept}
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
