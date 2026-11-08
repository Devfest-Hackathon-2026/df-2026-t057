package calendar

import (
	"context"
	"errors"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
)

// Google のポートが返す、処理を分けるためのエラー。それ以外の失敗は apperr.External("external.failed") で返す。
var (
	// ErrEventExists は、同じ ID の予定が既にある（作成で 409）。
	ErrEventExists = errors.New("calendar: event already exists")
	// ErrEventGone は、予定が無い・消された（404・410）。
	ErrEventGone = errors.New("calendar: event not found")
	// ErrPreconditionFailed は、If-Match の版が合わない（412）。
	ErrPreconditionFailed = errors.New("calendar: etag mismatch")
	// ErrSyncTokenExpired は、取り込みの syncToken が使えなくなった（410）。最初から取り込み直す。
	ErrSyncTokenExpired = errors.New("calendar: sync token expired")
)

// Grant は同意画面から戻ったときに受け取った認可。Token は保存するための不透明な値（中身はポートの実装が決める）。
type Grant struct {
	// Subject は ID トークン（audience を確認済み）の Google アカウントの ID。
	Subject         string
	HasRefreshToken bool
	Token           []byte
}

// WatchRequest は変更通知のチャンネルの登録。
type WatchRequest struct {
	ChannelID string
	Token     string
	Address   string
	Expires   time.Time
}

// Watch は登録したチャンネル。
type Watch struct {
	ResourceID string
	Expires    time.Time
}

// EventPage は予定の一覧の1ページ。最後のページだけが NextSyncToken を持つ。
type EventPage struct {
	Events        []Event
	NextPageToken string
	NextSyncToken string
}

// Google は Google の OAuth とトークンの保存。
type Google interface {
	// AuthURL は同意画面の URL（scope は openid・email・calendar.app.created、access_type=offline、
	// prompt=consent select_account、login_hint）。
	AuthURL(state, loginHint string) string
	// Exchange は認可コードをトークンに換え、ID トークンを検証する。
	Exchange(ctx context.Context, code string) (Grant, error)
	// SaveToken は利用者のトークンを保存する。前のトークンは破棄する。
	SaveToken(ctx context.Context, uid string, token []byte) error
	// DeleteToken は利用者のトークンを破棄する。保存されていなければ何もしない。
	DeleteToken(ctx context.Context, uid string) error
	// Client は保存したトークンで Calendar API を使う。
	Client(ctx context.Context, uid string) (Client, error)
	// ClientFor は受け取ったトークンで Calendar API を使う。
	ClientFor(ctx context.Context, token []byte) (Client, error)
}

// Client は Calendar API。
type Client interface {
	CreateCalendar(ctx context.Context, summary, description, timeZone string) (string, error)
	InsertEvent(ctx context.Context, calendarID string, e Event) (Event, error)
	GetEvent(ctx context.Context, calendarID, eventID string) (Event, error)
	// UpdateEvent は予定を丸ごと置き換える。
	UpdateEvent(ctx context.Context, calendarID string, e Event) (Event, error)
	// PatchEvent は etag が一致するときだけ書き換える（If-Match）。
	PatchEvent(ctx context.Context, calendarID, eventID, etag string, p EventPatch) (Event, error)
	DeleteEvent(ctx context.Context, calendarID, eventID string) error
	// ListEvents は消された予定を含めて一覧する。syncToken が空なら最初から。
	ListEvents(ctx context.Context, calendarID, syncToken, pageToken string) (EventPage, error)
	Watch(ctx context.Context, calendarID string, req WatchRequest) (Watch, error)
	StopChannel(ctx context.Context, channelID, resourceID string) error
}

// Tasks は課題の機能（task.Service が実装する）。
type Tasks interface {
	All(ctx context.Context, tx *store.Tx, uid string) ([]task.Task, error)
	ApplyImport(ctx context.Context, tx *store.Tx, uid string, items []task.Imported, now time.Time) (bool, error)
}

// Sleeps は睡眠の機能（sleep.Service が実装する）。
type Sleeps interface {
	GetTx(ctx context.Context, tx *store.Tx, uid string, now time.Time) (sleep.State, error)
}

// Profiles は本人の情報（profile.Service が実装する）。
type Profiles interface {
	Location(ctx context.Context, tx *store.Tx, uid string) (*time.Location, error)
	Context(ctx context.Context, tx *store.Tx, uid string) (string, *time.Time, error)
}
