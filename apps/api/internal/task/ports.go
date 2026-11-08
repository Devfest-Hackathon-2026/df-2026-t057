package task

import (
	"context"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

// DetailRequest は、手順を具体化するために AI に渡す内容（本人が入力した課題と作業傾向だけ）。
type DetailRequest struct {
	Title   string
	Notes   string
	Why     string
	Minutes int
	Context string
}

// DetailResult は AI が提案した手順。
type DetailResult struct {
	Steps []StepSpec
	Note  string
}

// AdjustRequest は変更案を指示に合わせて調整するための内容。
type AdjustRequest struct {
	Changes     []Change
	Instruction string
	Context     string
	Location    *time.Location
	Now         time.Time
}

// Planner は Gemini による手順の具体化・課題の抽出・案の調整。AI が使えない環境では ai.unavailable を返す。
// 利用上限に達したら ai.rate_limited（apperr）を返す。
type Planner interface {
	Detail(ctx context.Context, uid string, req DetailRequest) (DetailResult, error)
	Extract(ctx context.Context, uid, text string, now time.Time, loc *time.Location) ([]Input, error)
	Adjust(ctx context.Context, uid string, req AdjustRequest) ([]ChangeInput, error)
}

// Calendar は課題の変更を Google カレンダーへ反映する（Outbox に積む）。カレンダーを使わない環境では何もしない。
type Calendar interface {
	// TaskContentChanged は課題の登録・内容の変更（題名・メモ・締切・時間）。
	TaskContentChanged(ctx context.Context, tx *store.Tx, uid, taskID string) error
	// PlansChanged は計画だけが変わった（予定の説明の手順を書き直す）。
	PlansChanged(ctx context.Context, tx *store.Tx, uid string) error
	// TaskRemoved は課題の削除。
	TaskRemoved(ctx context.Context, tx *store.Tx, uid, taskID string) error
	// LinkedTasks はカレンダーの予定と結び付いている課題の ID。
	LinkedTasks(ctx context.Context, tx *store.Tx, uid string) (map[string]bool, error)
}

// Sessions は作業タイマー。課題を消したときに、その課題のタイマーを止める（worksession が実装する）。
type Sessions interface {
	TaskRemoved(ctx context.Context, tx *store.Tx, uid, taskID string) error
}

// Profiles は本人の情報（作業傾向・タイムゾーン・スヌーズの長さ）。
type Profiles interface {
	Context(ctx context.Context, tx *store.Tx, uid string) (context string, since *time.Time, err error)
	Location(ctx context.Context, tx *store.Tx, uid string) (*time.Location, error)
	SnoozeMinutes(ctx context.Context, tx *store.Tx, uid string) (int, error)
}

// Rewards は登録の報酬（wallet が実装する）。
type Rewards interface {
	GrantRegistration(ctx context.Context, tx *store.Tx, uid, taskID, title string, now time.Time) (int, error)
}
