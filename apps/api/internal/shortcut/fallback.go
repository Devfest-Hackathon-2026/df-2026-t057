package shortcut

// このファイルは「サーバーは表示用の文言を返さない」（AGENTS.md）の唯一の例外。
// iPhone のショートカットは応答の message をそのまま通知に出し、文言を引くクライアントの UI が無い。
// そのため、AI が使えないときの相棒の一言と通知の題名（旧実装の文言のまま）、残り時間の前置き、閉じたときの一言だけを、ここでサーバーが組み立てる。

import "strconv"

// notificationTitle は通知の題名。
const notificationTitle = "DOPAGAKI · 相棒"

// fallbackLine は AI が使えないときの app_open の一言。
func fallbackLine(title, why string) string {
	switch {
	case why != "":
		return "「" + why + "」、覚えてる？ 見る前に、まず一手だけ。"
	case title != "":
		return "見る前に「" + title + "」の最初の一手だけ、どう？"
	default:
		return "開く前に一呼吸。今日やりたいこと、ひとつ思い浮かべてみよっか。"
	}
}

// fallbackBalanceZeroLine は AI が使えないときの、視聴時間が0分のときの一言。
func fallbackBalanceZeroLine() string {
	return "今日の視聴はここで区切ろう。次の一手で、また時間が増えるよ。"
}

// minutesLeftPrefix は開いたときの一言の前に付ける、今日の残り時間。
func minutesLeftPrefix(minutes int) string {
	return "今日あと" + strconv.Itoa(minutes) + "分。"
}

// closeLine は閉じたときの一言。AI の回数は開いたときのために残し、決まった文にする。
func closeLine(use *AppUse, nextTitle string) string {
	message := "おつかれ。"
	if use != nil {
		if m := use.Minutes(); m < 1 {
			message = "YouTube、1分未満だったね。"
		} else {
			message = "YouTube、" + strconv.Itoa(m) + "分だったね。"
		}
	}
	if nextTitle != "" {
		return message + "次は「" + nextTitle + "」の一手、いってみよっか。"
	}
	return message + "次にやりたいこと、ひとつ思い浮かべてみよっか。"
}
