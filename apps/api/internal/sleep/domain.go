// Package sleep は就寝と起床の時刻、夜の時間帯、朝の精算を扱う。
//
// 夜の時間帯（就寝1時間前から起床まで）に得た報酬は朝まで保留する。Shorts を見ずに眠れた夜には、朝にボーナスを付与する。
package sleep

import (
	"regexp"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
)

const (
	// WindDown は就寝の何時間前から夜とみなすか。
	WindDown = time.Hour
	// BonusSeconds は Shorts を見ずに眠れた夜の朝に付与する秒数。
	BonusSeconds = 900
	minHours     = 4
	maxHours     = 12
)

var clockPattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// Schedule は就寝と起床の時刻（HH:MM、本人のタイムゾーン）。
type Schedule struct {
	Bedtime string `json:"bedtime" firestore:"bedtime"`
	Wake    string `json:"wake" firestore:"wake"`
}

// Sleep は保存する睡眠の状態。
type Sleep struct {
	Schedule
	// Since は今の時刻を設定した時刻。これより後に始まった夜だけを判定する。
	Since        time.Time `json:"since" firestore:"since"`
	WatchedNight string    `json:"watchedNight,omitempty" firestore:"watchedNight,omitempty"`
	SettledNight string    `json:"settledNight,omitempty" firestore:"settledNight,omitempty"`
	KeptNights   int       `json:"keptNights" firestore:"keptNights"`
}

// Night は1つの夜。Key は夜が始まる日の日付（YYYY-MM-DD）。
type Night struct {
	Key   string
	Start time.Time
	End   time.Time
}

func parseClock(v string) (hour, minute int, ok bool) {
	if !clockPattern.MatchString(v) {
		return 0, 0, false
	}
	return int(v[0]-'0')*10 + int(v[1]-'0'), int(v[3]-'0')*10 + int(v[4]-'0'), true
}

// Validate は時刻の形式と、睡眠時間が4〜12時間かを確かめる。
func (s Schedule) Validate() error {
	bh, bm, ok1 := parseClock(s.Bedtime)
	wh, wm, ok2 := parseClock(s.Wake)
	if !ok1 || !ok2 {
		var fields []apperr.FieldError
		if !ok1 {
			fields = append(fields, apperr.Field("bedtime", "sleep.time_format"))
		}
		if !ok2 {
			fields = append(fields, apperr.Field("wake", "sleep.time_format"))
		}
		return apperr.Invalid("request.invalid", fields...)
	}
	minutes := ((wh*60 + wm) - (bh*60 + bm) + 24*60) % (24 * 60)
	if minutes < minHours*60 || minutes > maxHours*60 {
		return apperr.Invalid("request.invalid", apperr.Field("wake", "sleep.duration_range"))
	}
	return nil
}

// NightOf は、day の日付に始まる夜を返す。正午より前の就寝（例 01:00）は、前の晩の夜として翌日の日付に置く。
func (s Schedule) NightOf(day time.Time, loc *time.Location) Night {
	bh, bm, _ := parseClock(s.Bedtime)
	wh, wm, _ := parseClock(s.Wake)
	y, m, d := day.In(loc).Date()
	bed := time.Date(y, m, d, bh, bm, 0, 0, loc)
	if bh < 12 {
		bed = bed.AddDate(0, 0, 1)
	}
	end := time.Date(bed.Year(), bed.Month(), bed.Day(), wh, wm, 0, 0, loc)
	if !end.After(bed) {
		end = end.AddDate(0, 0, 1)
	}
	return Night{Key: time.Date(y, m, d, 0, 0, 0, 0, loc).Format(time.DateOnly), Start: bed.Add(-WindDown).UTC(), End: end.UTC()}
}

// NightAt は now を含む夜を返す。
func (s Schedule) NightAt(now time.Time, loc *time.Location) (Night, bool) {
	for _, back := range []int{0, -1} {
		n := s.NightOf(now.In(loc).AddDate(0, 0, back), loc)
		if !now.Before(n.Start) && now.Before(n.End) {
			return n, true
		}
	}
	return Night{}, false
}

// LastNight は、終わった夜のうち最も新しいもの。
func (s Schedule) LastNight(now time.Time, loc *time.Location) Night {
	for back := -1; ; back-- {
		if n := s.NightOf(now.In(loc).AddDate(0, 0, back), loc); !now.Before(n.End) {
			return n
		}
	}
}

// CurrentOrNext は表示用に、今の夜か、次に来る夜を返す。
func (s Schedule) CurrentOrNext(now time.Time, loc *time.Location) (Night, bool) {
	if n, ok := s.NightAt(now, loc); ok {
		return n, true
	}
	for ahead := 0; ahead <= 2; ahead++ {
		if n := s.NightOf(now.In(loc).AddDate(0, 0, ahead), loc); now.Before(n.Start) {
			return n, false
		}
	}
	return s.NightOf(now.In(loc).AddDate(0, 0, 1), loc), false
}

// Set は時刻を設定した後の状態を返す。同じ時刻なら判定の履歴を引き継ぐ。初めての設定では、終わった夜を判定しない。
func Set(current *Sleep, next Schedule, now time.Time, loc *time.Location) Sleep {
	if current == nil {
		return Sleep{Schedule: next, Since: now, SettledNight: next.LastNight(now, loc).Key}
	}
	s := *current
	if s.Schedule != next {
		s.Schedule, s.Since = next, now
	}
	return s
}

// Settlement は朝の精算の判定。
type Settlement struct {
	Night Night
	Kept  bool
}

// Settle は前の夜を精算するかを判定する。夜の途中、精算済み、未設定なら false。
func Settle(s *Sleep, now time.Time, loc *time.Location) (Settlement, bool) {
	if s == nil {
		return Settlement{}, false
	}
	if _, in := s.NightAt(now, loc); in {
		return Settlement{}, false
	}
	n := s.LastNight(now, loc)
	if s.SettledNight == n.Key {
		return Settlement{}, false
	}
	return Settlement{Night: n, Kept: s.Since.Before(n.Start) && s.WatchedNight != n.Key}, true
}

// Apply は精算を記録した状態を返す。
func (st Settlement) Apply(s Sleep) Sleep {
	if st.Kept {
		s.KeptNights++
	} else {
		s.KeptNights = 0
	}
	s.SettledNight = st.Night.Key
	return s
}
