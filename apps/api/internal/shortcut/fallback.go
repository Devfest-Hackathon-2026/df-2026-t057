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

// fallbackLine は AI が使えないときの app_open の一言。
func fallbackLine(lang locale.Locale, title, why string) string {
	if lang == locale.En {
		switch {
		case why != "":
			return "Remember “" + why + "”? Before you watch, just take the first step."
		case title != "":
			return "Before you watch, how about just the first step of “" + title + "”?"
		default:
			return "Take a breath before opening. Think of one thing you want to do today."
		}
	}
	switch {
	case why != "":
		return "「" + why + "」、覚えてる？ 見る前に、まず一手だけ。"
	case title != "":
		return "見る前に「" + title + "」の最初の一手だけ、どう？"
	default:
		return "開く前に一呼吸。今日やりたいこと、ひとつ思い浮かべてみよっか。"
	}
}
