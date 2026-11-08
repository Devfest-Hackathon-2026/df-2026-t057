// Package profile は本人の情報（表示名・作業傾向・タイムゾーン・言語）と、表示と通知の設定を扱う。
package profile

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"
)

const (
	MaxNameRunes    = 100
	MaxContextRunes = 2000
	MaxMemoryRunes  = 500
	// DefaultTimeZone は新しい利用者のタイムゾーン。
	DefaultTimeZone = "Asia/Tokyo"
	// DefaultLocale は新しい利用者の言語。管理画面は、初めての設定でブラウザーの言語を保存し直す。
	DefaultLocale = locale.Ja
)

// Profile は本人の情報。
type Profile struct {
	Name     string        `json:"name" firestore:"name"`
	Context  string        `json:"context" firestore:"context"`
	TimeZone string        `json:"timeZone" firestore:"timeZone"`
	Locale   locale.Locale `json:"locale" firestore:"locale"`
	// ContextSince は作業傾向を書き換えた時刻。これより前の相談は AI に渡さない（「忘れる」）。
	ContextSince *time.Time `json:"contextSince,omitempty" firestore:"contextSince,omitempty"`
}

// Input は本人が変えられる項目。
type Input struct {
	Name     string
	Context  string
	TimeZone string
	Locale   locale.Locale
}

// Default は新しい利用者の情報。表示名は空にして、表示はクライアントに任せる。
func Default() Profile { return Profile{TimeZone: DefaultTimeZone, Locale: DefaultLocale} }

// ValidateInput は入力を確かめる。タイムゾーンが読み込めるかは validZone で確かめる（tzdata の読み込みは I/O のため）。
func ValidateInput(in Input, validZone bool) error {
	var fields []apperr.FieldError
	if utf8.RuneCountInString(in.Name) > MaxNameRunes {
		fields = append(fields, apperr.Field("name", "too_long"))
	}
	if utf8.RuneCountInString(in.Context) > MaxContextRunes {
		fields = append(fields, apperr.Field("context", "too_long"))
	}
	if in.TimeZone == "" || !validZone {
		fields = append(fields, apperr.Field("timeZone", "profile.timezone"))
	}
	if !in.Locale.Valid() {
		fields = append(fields, apperr.Field("locale", "not_allowed"))
	}
	if len(fields) > 0 {
		return apperr.Invalid("request.invalid", fields...)
	}
	return nil
}

// Apply は入力を反映した情報を返す。作業傾向が変わったら、その時刻を「忘れる」区切りにする。
func (p Profile) Apply(in Input, now time.Time) Profile {
	next := p
	next.Name, next.TimeZone, next.Locale = in.Name, in.TimeZone, in.Locale
	if in.Context != p.Context {
		next.Context = in.Context
		next.ContextSince = &now
	}
	return next
}

// Remember は作業傾向に1行加える。同じ内容が既にあれば加えない。上限を超えるなら memory.full。
func (p Profile) Remember(text string) (Profile, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return p, apperr.Invalid("request.invalid", apperr.Field("text", "required"))
	}
	if utf8.RuneCountInString(text) > MaxMemoryRunes {
		return p, apperr.Invalid("request.invalid", apperr.Field("text", "too_long"))
	}
	if strings.Contains(p.Context, text) {
		return p, nil
	}
	next := strings.TrimSpace(p.Context + "\n" + text)
	if utf8.RuneCountInString(next) > MaxContextRunes {
		return p, apperr.Conflict("memory.full")
	}
	p.Context = next
	return p, nil
}

// NoticeMode は通知の見た目。
type NoticeMode string

const (
	ModePachinko NoticeMode = "pachinko"
	ModeMahjong  NoticeMode = "mahjong"
	ModeSimple   NoticeMode = "simple"
)

// Valid は定義済みの値か。
func (m NoticeMode) Valid() bool {
	switch m {
	case ModePachinko, ModeMahjong, ModeSimple:
		return true
	default:
		return false
	}
}

// Settings は表示と通知の設定。
type Settings struct {
	SnoozeMinutes int        `json:"snoozeMinutes" firestore:"snoozeMinutes"`
	Mode          NoticeMode `json:"mode" firestore:"mode"`
	Sound         bool       `json:"sound" firestore:"sound"`
	ReducedMotion bool       `json:"reducedMotion" firestore:"reducedMotion"`
	Grime         bool       `json:"grime" firestore:"grime"`
}

const (
	MinSnoozeMinutes = 1
	MaxSnoozeMinutes = 120
)

// DefaultSettings は新しい利用者の設定。
func DefaultSettings() Settings { return Settings{SnoozeMinutes: 10, Mode: ModePachinko} }

// ValidateSettings は設定を確かめる。
func ValidateSettings(s Settings) error {
	var fields []apperr.FieldError
	if s.SnoozeMinutes < MinSnoozeMinutes || s.SnoozeMinutes > MaxSnoozeMinutes {
		fields = append(fields, apperr.Field("snoozeMinutes", "out_of_range"))
	}
	if !s.Mode.Valid() {
		fields = append(fields, apperr.Field("mode", "not_allowed"))
	}
	if len(fields) > 0 {
		return apperr.Invalid("request.invalid", fields...)
	}
	return nil
}
