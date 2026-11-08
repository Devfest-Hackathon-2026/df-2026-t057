package gcal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
)

// SecretStore は OAuth のトークンを Secret Manager に保存する。
// 利用者ごとに secret を1つ作り（projects/<p>/secrets/dopagaki-calendar-<hex(sha256(uid))[:32]>）、つなぎ直すたびに版を加える。
// 古い版は加えた直後に破棄する（使わないトークンを残さない）。
type SecretStore struct {
	client  *secretmanager.Client
	project string
}

// NewSecretStore は SecretStore を作る。実行環境のサービスアカウントで接続する。
func NewSecretStore(ctx context.Context, project string) (*SecretStore, error) {
	c, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcal: secret manager client: %w", err)
	}
	return &SecretStore{client: c, project: project}, nil
}

// Close は接続を閉じる。
func (s *SecretStore) Close() error {
	if err := s.client.Close(); err != nil {
		return fmt.Errorf("gcal: close secret manager: %w", err)
	}
	return nil
}

// SecretID は利用者のトークンを置く secret の ID（uid を名前に出さない）。
func SecretID(uid string) string {
	sum := sha256.Sum256([]byte(uid))
	return "dopagaki-calendar-" + hex.EncodeToString(sum[:16])
}

func (s *SecretStore) name(uid string) string {
	return "projects/" + s.project + "/secrets/" + SecretID(uid)
}

// secretError は Secret Manager の失敗を、gRPC のコードだけを残して包む。
func secretError(op string, err error) error {
	return apperr.External("external.failed", fmt.Errorf("secret manager %s: %s", op, status.Code(err)))
}

// Save はトークンを新しい版として保存し、それより古い版を破棄する。
func (s *SecretStore) Save(ctx context.Context, uid string, token []byte) error {
	_, err := s.client.CreateSecret(ctx, &secretmanagerpb.CreateSecretRequest{
		Parent: "projects/" + s.project, SecretId: SecretID(uid),
		Secret: &secretmanagerpb.Secret{Replication: &secretmanagerpb.Replication{
			Replication: &secretmanagerpb.Replication_Automatic_{Automatic: &secretmanagerpb.Replication_Automatic{}},
		}},
	})
	if err != nil && status.Code(err) != codes.AlreadyExists {
		return secretError("create", err)
	}
	added, err := s.client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{Parent: s.name(uid), Payload: &secretmanagerpb.SecretPayload{Data: token}})
	if err != nil {
		return secretError("add version", err)
	}
	s.destroyOlder(ctx, uid, added.GetName())
	return nil
}

// destroyOlder は新しい版より前の有効な版を破棄する。失敗しても保存は成功しているので、続ける。
func (s *SecretStore) destroyOlder(ctx context.Context, uid, keep string) {
	it := s.client.ListSecretVersions(ctx, &secretmanagerpb.ListSecretVersionsRequest{Parent: s.name(uid), Filter: "state:ENABLED"})
	for {
		v, err := it.Next()
		if err != nil {
			// iterator.Done（終わり）か、一覧の失敗。どちらでも破棄をやめる。
			return
		}
		if v.GetName() == keep {
			continue
		}
		_, _ = s.client.DestroySecretVersion(ctx, &secretmanagerpb.DestroySecretVersionRequest{Name: v.GetName()})
	}
}

// Load は最新のトークンを読む。保存されていなければ calendar.reauth_required。
func (s *SecretStore) Load(ctx context.Context, uid string) ([]byte, error) {
	v, err := s.client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{Name: s.name(uid) + "/versions/latest"})
	if status.Code(err) == codes.NotFound {
		return nil, apperr.Forbidden("calendar.reauth_required")
	}
	if err != nil {
		return nil, secretError("access", err)
	}
	return v.GetPayload().GetData(), nil
}
