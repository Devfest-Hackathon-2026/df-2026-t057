// Package cloudtasks は outbox.Dispatcher を Cloud Tasks で実装する。
//
// ジョブごとに、POST {APP_URL}/api/internal/outbox/{jobId}（本文 {"uid": ...}）を呼ぶ HTTP タスクを作る。
// タスクには OIDC トークン（呼び出し用のサービスアカウント・audience）を付け、API 側で googleoidc が検証する。
// タスク名はジョブの ID にする。同じジョブを登録し直しても（Redispatch）、Cloud Tasks が重複を除く。
package cloudtasks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	cloudtasksapi "cloud.google.com/go/cloudtasks/apiv2"
	"cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/outbox"
)

// creator はタスクを作る（テストで差し替える）。
type creator interface {
	CreateTask(ctx context.Context, req *cloudtaskspb.CreateTaskRequest) error
	Close() error
}

type apiCreator struct{ client *cloudtasksapi.Client }

func (a apiCreator) CreateTask(ctx context.Context, req *cloudtaskspb.CreateTaskRequest) error {
	_, err := a.client.CreateTask(ctx, req)
	return err //nolint:wrapcheck // Dispatch で包む。
}

func (a apiCreator) Close() error { return a.client.Close() } //nolint:wrapcheck // Close で包む。

// Dispatcher は Cloud Tasks にジョブを登録する。
type Dispatcher struct {
	tasks    creator
	queue    string
	target   string
	invoker  string
	audience string
}

var _ outbox.Dispatcher = (*Dispatcher)(nil)

// New は Dispatcher を作る。queue は projects/<p>/locations/<l>/queues/<q>、appURL は API のオリジン、
// invokerEmail は OIDC トークンに使うサービスアカウント、audience はトークンの audience（OIDC_AUDIENCE）。
func New(ctx context.Context, queue, appURL, invokerEmail, audience string) (*Dispatcher, error) {
	c, err := cloudtasksapi.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("cloudtasks: client: %w", err)
	}
	return newDispatcher(apiCreator{client: c}, queue, appURL, invokerEmail, audience), nil
}

func newDispatcher(c creator, queue, appURL, invokerEmail, audience string) *Dispatcher {
	return &Dispatcher{tasks: c, queue: queue, target: strings.TrimRight(appURL, "/"), invoker: invokerEmail, audience: audience}
}

// Close は接続を閉じる。
func (d *Dispatcher) Close() error {
	if err := d.tasks.Close(); err != nil {
		return fmt.Errorf("cloudtasks: close: %w", err)
	}
	return nil
}

// request はジョブを実行する HTTP タスクの作成の求め。
func (d *Dispatcher) request(uid, jobID string) (*cloudtaskspb.CreateTaskRequest, error) {
	body, err := json.Marshal(map[string]string{"uid": uid})
	if err != nil {
		return nil, fmt.Errorf("cloudtasks: encode body: %w", err)
	}
	return &cloudtaskspb.CreateTaskRequest{
		Parent: d.queue,
		Task: &cloudtaskspb.Task{
			Name: d.queue + "/tasks/" + jobID,
			MessageType: &cloudtaskspb.Task_HttpRequest{HttpRequest: &cloudtaskspb.HttpRequest{
				Url:        d.target + "/api/internal/outbox/" + url.PathEscape(jobID),
				HttpMethod: cloudtaskspb.HttpMethod_POST,
				Headers:    map[string]string{"Content-Type": "application/json"},
				Body:       body,
				AuthorizationHeader: &cloudtaskspb.HttpRequest_OidcToken{OidcToken: &cloudtaskspb.OidcToken{
					ServiceAccountEmail: d.invoker, Audience: d.audience,
				}},
			}},
		},
	}, nil
}

// Dispatch はジョブを実行するタスクを作る。同じ名前のタスクが既にあれば、登録済みとして成功にする。
func (d *Dispatcher) Dispatch(ctx context.Context, uid, jobID string) error {
	req, err := d.request(uid, jobID)
	if err != nil {
		return err
	}
	if err := d.tasks.CreateTask(ctx, req); err != nil {
		if status.Code(err) == codes.AlreadyExists {
			return nil
		}
		return apperr.External("external.failed", fmt.Errorf("cloudtasks: create task: %s", status.Code(err)))
	}
	return nil
}
