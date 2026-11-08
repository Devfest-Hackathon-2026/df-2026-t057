package calendar

// このファイルは、本人の Google カレンダーに書き込む文言をまとめた場所。
//
// サーバーは API のレスポンスに表示用の文言を返さない（AGENTS.md）。ただし、カレンダーの予定は本人のカレンダーに
// 保存される内容そのもので、クライアントが code から文言を引く仕組みが使えない。そのため、予定に書く文言だけは
// サーバーが持ち、ここに1か所にまとめる。旧実装と同じ文言を使う。

const (
	// calendarSummary と calendarDescription は、連携のときに作るカレンダーの名前と説明。
	calendarSummary     = "DOPAGAKI"
	calendarDescription = "課題と締切、具体的な作業予定"

	// blockStart と blockEnd は、予定の説明に書く DOPAGAKI のブロックの区切り。本人のメモと区別するために使う。
	blockStart = "[DOPAGAKI]"
	blockEnd   = "[/DOPAGAKI]"
	// deadlineLabel はブロックの中の締切の行の見出し。
	deadlineLabel = "締切: "

	// sleepSummary は就寝の予定の題名。
	sleepSummary = "🌙 就寝（DOPAGAKI）"
	// sleepDescriptionFormat は就寝の予定の説明（就寝の時刻、起床の時刻）。
	sleepDescriptionFormat = "%sに寝て%sに起きる予定です。就寝1時間前からは、課題の報酬を翌朝に受け取ります。"
)
