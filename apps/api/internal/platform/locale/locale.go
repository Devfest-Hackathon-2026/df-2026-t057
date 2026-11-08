// Package locale は画面の言語を表す。プロフィールが持ち、サーバーが作る文（カレンダーの予定、AI の返答、通知）もこの言語にする。
//
// 複数の機能（profile・calendar・shortcut・companion・assistant・task）が同じ型を使うため、機能のパッケージではなく platform に置く。
package locale

// Locale は画面の言語。
type Locale string

const (
	Ja Locale = "ja"
	En Locale = "en"
)

// Valid は定義済みの値か。
func (l Locale) Valid() bool {
	switch l {
	case Ja, En:
		return true
	default:
		return false
	}
}
