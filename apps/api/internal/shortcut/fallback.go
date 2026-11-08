package shortcut

// このファイルは「サーバーは表示用の文言を返さない」（AGENTS.md）の唯一の例外。
// iPhone のショートカットは応答の message をそのまま通知に出し、文言を引くクライアントの UI が無い。
// そのため、AI が使えないときの相棒の一言と通知の題名だけを、ここでサーバーが組み立てる（旧実装の文言のまま）。

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
