package shortcut

import (
	"testing"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"
)

func TestFallbackLine(t *testing.T) {
	tests := []struct {
		lang       locale.Locale
		title, why string
		want       string
	}{
		{locale.Ja, "レポート", "単位を取る", "「単位を取る」、覚えてる？ 見る前に、まず一手だけ。"},
		{locale.Ja, "レポート", "", "見る前に「レポート」の最初の一手だけ、どう？"},
		{locale.Ja, "", "", "開く前に一呼吸。今日やりたいこと、ひとつ思い浮かべてみよっか。"},
		{locale.En, "Report", "Pass the class", "Remember “Pass the class”? Before you watch, just take the first step."},
		{locale.En, "Report", "", "Before you watch, how about just the first step of “Report”?"},
		{locale.En, "", "", "Take a breath before opening. Think of one thing you want to do today."},
	}
	for _, tt := range tests {
		if got := fallbackLine(tt.lang, tt.title, tt.why); got != tt.want {
			t.Errorf("fallbackLine(%q, %q, %q) = %q, want %q", tt.lang, tt.title, tt.why, got, tt.want)
		}
	}
}
