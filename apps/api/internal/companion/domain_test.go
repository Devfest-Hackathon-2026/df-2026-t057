package companion

import (
	"strings"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

func completedAt(at time.Time) task.Task {
	return task.Task{Status: task.StatusCompleted, CompletedAt: &at}
}

func TestStreakDaysUsesLocalCalendarDays(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 7, 9, 0, 0, 0, loc)
	var tasks []task.Task
	for _, back := range []int{1, 2, 4} {
		tasks = append(tasks, completedAt(now.AddDate(0, 0, -back)))
	}
	tests := []struct {
		name  string
		tasks []task.Task
		now   time.Time
		want  int
	}{
		{"昨日までの連続", tasks, now, 2},
		{"今日を含む", append(tasks, completedAt(now)), now, 3},
		{"記録なし", nil, now, 0},
		// UTC では前日の 23:30 でも、東京では当日の 08:30。
		{"本人のタイムゾーンで数える", []task.Task{completedAt(time.Date(2026, 11, 6, 23, 30, 0, 0, time.UTC))}, now, 1},
		{"一昨日で途切れる", []task.Task{completedAt(now.AddDate(0, 0, -2))}, now, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StreakDays(tt.tasks, tt.now, loc); got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSupportFadesWithStreak(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		days int
		want Support
	}{{0, SupportFull}, {2, SupportFull}, {3, SupportLight}, {6, SupportLight}, {7, SupportHandsOff}, {30, SupportHandsOff}} {
		var tasks []task.Task
		for d := range tt.days {
			tasks = append(tasks, completedAt(now.AddDate(0, 0, -d)))
		}
		if got := SupportLevel(StreakDays(tasks, now, time.UTC)); got != tt.want {
			t.Errorf("%d days: %s, want %s", tt.days, got, tt.want)
		}
	}
}

func TestCleanLine(t *testing.T) {
	tests := []struct {
		raw  string
		lang locale.Locale
		want string
		ok   bool
	}{
		{"よし、まず資料を開こう。", locale.Ja, "よし、まず資料を開こう。", true},
		{" 「よし、始めよう」 ", locale.Ja, "よし、始めよう", true},
		{`"引用符つき"`, locale.Ja, "引用符つき", true},
		{"", locale.Ja, "", false},
		{"「」", locale.Ja, "", false},
		{"それは依存症ですね", locale.Ja, "", false},
		{"意志が弱いわけじゃない", locale.Ja, "", false},
		{"治療しよう", locale.Ja, "", false},
		{strings.Repeat("あ", 60), locale.Ja, strings.Repeat("あ", 60), true},
		{strings.Repeat("あ", 61), locale.Ja, "", false},
		{"“Open the doc first.”", locale.En, "Open the doc first.", true},
		{"You're not an Addict.", locale.En, "", false},
		{"No treatment needed", locale.En, "", false},
		{"This is not a disease", locale.En, "", false},
		{strings.Repeat("a", 140), locale.En, strings.Repeat("a", 140), true},
		{strings.Repeat("a", 141), locale.En, "", false},
	}
	for _, tt := range tests {
		got, ok := CleanLine(tt.raw, tt.lang)
		if got != tt.want || ok != tt.ok {
			t.Errorf("CleanLine(%q, %q) = %q, %v", tt.raw, tt.lang, got, ok)
		}
	}
}

func TestMomentValid(t *testing.T) {
	for _, m := range []Moment{MomentNotice, MomentSnooze, MomentComplete, MomentBalanceZero, MomentAppOpen, MomentNight, MomentMorning} {
		if !m.Valid() {
			t.Errorf("%s", m)
		}
	}
	if Moment("lecture").Valid() || Moment("").Valid() {
		t.Fatal("unknown moment accepted")
	}
}

func TestBuildProgress(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	at := func(days int) *time.Time { v := now.AddDate(0, 0, -days); return &v }
	tasks := []task.Task{
		{Title: "a", CompletedAt: at(0)},
		{Title: "b", CompletedAt: at(1)},
		{Title: "c", CompletedAt: at(10)},
		{Title: "d", CompletedAt: at(2)},
		{Title: "pending"},
	}
	p := BuildProgress(ProgressInput{
		Tasks: tasks, Account: wallet.Account{Mode: wallet.ModeRewards, Balance: 900, DailyLimit: 1800}, Now: now, Loc: time.UTC,
		AppOpens: &OpenCounts{Today: 2, Last7Days: 9},
	})
	if p.StreakDays != 3 || p.Support != SupportLight || p.CompletedTotal != 4 || p.CompletedLast7Days != 3 || p.ViewingMinutesLeft != 15 ||
		strings.Join(p.RecentCompleted, ",") != "a,b,d" || p.YouTubeAppOpens.Today != 2 || p.TimePolicy.DailyLimitMinutes != 30 {
		t.Fatalf("progress: %+v", p)
	}
}

func TestSummarizeSleep(t *testing.T) {
	now := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	if s := SummarizeSleep(sleep.State{}, now, time.UTC); s.Set {
		t.Fatal("unset sleep")
	}
	sched := sleep.Schedule{Bedtime: "23:00", Wake: "07:00"}
	last := sched.LastNight(now, time.UTC).Key
	st := sleep.State{Sleep: &sleep.Sleep{Schedule: sched, KeptNights: 2, SettledNight: last}}
	s := SummarizeSleep(st, now, time.UTC)
	if !s.Set || s.KeptNights != 2 || s.LastNightWithoutShorts == nil || !*s.LastNightWithoutShorts {
		t.Fatalf("settled without shorts: %+v", s)
	}
	st.Sleep.WatchedNight = last
	if s := SummarizeSleep(st, now, time.UTC); *s.LastNightWithoutShorts {
		t.Fatal("watched night")
	}
	st.Sleep.SettledNight = ""
	if s := SummarizeSleep(st, now, time.UTC); s.LastNightWithoutShorts != nil {
		t.Fatal("unsettled night was reported")
	}
}

func TestDescribeOmitsFirstStepEqualToTitle(t *testing.T) {
	tk := task.Task{ID: "x", Title: "レポート", Plan: task.Plan{Steps: []task.Step{{Title: "レポート"}}}}
	if Describe(tk, false, time.UTC).FirstStep != "" {
		t.Fatal("duplicate first step")
	}
	tk.Plan.Steps[0].Title = "資料を開く"
	if Describe(tk, true, time.UTC).FirstStep != "資料を開く" {
		t.Fatal("first step lost")
	}
}
