package shortcut

import "github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"

// このファイルは「サーバーは表示用の文言を返さない」（AGENTS.md）の唯一の例外。
// iPhone のショートカットは応答の message をそのまま通知に出し、文言を引くクライアントの UI が無い。
// そのため、AI が使えないときの相棒の一言と通知の題名だけを、ここでサーバーが本人の言語で組み立てる。

// notificationTitles は通知の題名。
var notificationTitles = map[locale.Locale]string{
	locale.Ja: "DOPAGAKI · 相棒",
	locale.En: "DOPAGAKI · Buddy",
}

// fallbackTaskLine は AI が使えないときの、課題を始める時刻になったときの一言。
func fallbackTaskLine(lang locale.Locale, title, why string) string {
	if lang == locale.En {
		if why != "" {
			return "Remember “" + why + "”? It's about time for the next step on “" + title + "”."
		}
		return "It's time for “" + title + "”. How about just the first step?"
	}
	if why != "" {
		return "「" + why + "」、覚えてる？ そろそろ「" + title + "」の一手、いってみよっか。"
	}
	return "そろそろ「" + title + "」の時間だよ。最初の一手だけ、やってみよっか。"
}

// fallbackBalanceZeroLine は AI が使えないときの、持ち時間を使い切ったときの一言。
func fallbackBalanceZeroLine(lang locale.Locale) string {
	if lang == locale.En {
		return "Let's stop watching here for today. Your next step earns more time."
	}
	return "今日の視聴はここで区切ろう。次の一手で、また時間が増えるよ。"
}
