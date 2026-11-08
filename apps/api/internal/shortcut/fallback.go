package shortcut

// このファイルは「サーバーは表示用の文言を返さない」（AGENTS.md）の唯一の例外。
// iPhone のショートカットは応答の message をそのまま通知に出し、文言を引くクライアントの UI が無い。
// そのため、通知の題名と、AI が使えないときの相棒の一言だけを、ここでサーバーが組み立てる。

// notificationTitle は通知の題名。
const notificationTitle = "DOPAGAKI · 相棒"

// fallbackTaskLine は AI が使えないときの、課題を始める時刻になったときの一言。
func fallbackTaskLine(title, why string) string {
	if why != "" {
		return "「" + why + "」、覚えてる？ そろそろ「" + title + "」の一手、いってみよっか。"
	}
	return "そろそろ「" + title + "」の時間だよ。最初の一手だけ、やってみよっか。"
}

// fallbackBalanceZeroLine は AI が使えないときの、持ち時間を使い切ったときの一言。
func fallbackBalanceZeroLine() string {
	return "今日の視聴はここで区切ろう。次の一手で、また時間が増えるよ。"
}
