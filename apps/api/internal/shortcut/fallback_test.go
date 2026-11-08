package shortcut

import (
	"testing"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"
)

func TestFallbackTaskLine(t *testing.T) {
	tests := []struct {
		lang       locale.Locale
		title, why string
		want       string
	}{
		{locale.Ja, "レポート", "単位を取る", "「単位を取る」、覚えてる？ そろそろ「レポート」の一手、いってみよっか。"},
		{locale.Ja, "レポート", "", "そろそろ「レポート」の時間だよ。最初の一手だけ、やってみよっか。"},
		{locale.En, "Report", "Pass the class", "Remember “Pass the class”? It's about time for the next step on “Report”."},
		{locale.En, "Report", "", "It's time for “Report”. How about just the first step?"},
	}
	for _, tt := range tests {
		if got := fallbackTaskLine(tt.lang, tt.title, tt.why); got != tt.want {
			t.Errorf("fallbackTaskLine(%q, %q, %q) = %q, want %q", tt.lang, tt.title, tt.why, got, tt.want)
		}
	}
}

func TestFallbackBalanceZeroLine(t *testing.T) {
	tests := []struct {
		lang locale.Locale
		want string
	}{
		{locale.Ja, "今日の視聴はここで区切ろう。次の一手で、また時間が増えるよ。"},
		{locale.En, "Let's stop watching here for today. Your next step earns more time."},
	}
	for _, tt := range tests {
		if got := fallbackBalanceZeroLine(tt.lang); got != tt.want {
			t.Errorf("fallbackBalanceZeroLine(%q) = %q, want %q", tt.lang, got, tt.want)
		}
	}
}
