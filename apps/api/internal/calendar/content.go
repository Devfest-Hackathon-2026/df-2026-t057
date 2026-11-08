package calendar

import "github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"

// このファイルは、本人の Google カレンダーに書き込む文言をまとめた場所。
//
// サーバーは API のレスポンスに表示用の文言を返さない（AGENTS.md）。ただし、カレンダーの予定は本人のカレンダーに
// 保存される内容そのもので、クライアントが code から文言を引く仕組みが使えない。そのため、予定に書く文言だけは
// サーバーが持ち、ここに1か所にまとめる。本人の言語（プロフィールの locale）で書く。

const (
	// calendarSummary は、連携のときに作るカレンダーの名前。
	calendarSummary = "DOPAGAKI"

	// blockStart と blockEnd は、予定の説明に書く DOPAGAKI のブロックの区切り。本人のメモと区別するために使う。
	// 言語を変えても前のブロックを見つけられるよう、言語によらず同じにする。
	blockStart = "[DOPAGAKI]"
	blockEnd   = "[/DOPAGAKI]"
)

// calendarText は、言語ごとにカレンダーへ書く文言。
type calendarText struct {
	// calendarDescription は、連携のときに作るカレンダーの説明。
	calendarDescription string
	// deadlineLabel はブロックの中の締切の行の見出し。
	deadlineLabel string
	// sleepSummary は就寝の予定の題名。
	sleepSummary string
	// sleepDescriptionFormat は就寝の予定の説明（就寝の時刻、起床の時刻）。
	sleepDescriptionFormat string
}

var texts = map[locale.Locale]calendarText{
	locale.Ja: {
		calendarDescription:    "課題と締切、具体的な作業予定",
		deadlineLabel:          "締切: ",
		sleepSummary:           "🌙 就寝（DOPAGAKI）",
		sleepDescriptionFormat: "%sに寝て%sに起きる予定です。就寝1時間前からは、課題の報酬を翌朝に受け取ります。",
	},
	locale.En: {
		calendarDescription:    "Tasks, deadlines, and concrete work plans",
		deadlineLabel:          "Deadline: ",
		sleepSummary:           "🌙 Bedtime (DOPAGAKI)",
		sleepDescriptionFormat: "Planning to sleep at %s and wake up at %s. From one hour before bedtime, task rewards arrive the next morning.",
	},
}
