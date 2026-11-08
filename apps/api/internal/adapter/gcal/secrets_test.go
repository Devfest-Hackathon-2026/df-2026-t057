package gcal

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"testing"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
)

// fakeSecretManager は Secret Manager の偽物（secret ごとの版と状態だけを持つ）。
type fakeSecretManager struct {
	secretmanagerpb.UnimplementedSecretManagerServiceServer
	mu      sync.Mutex
	secrets map[string]*fakeSecret
	// failList が true なら版の一覧に失敗する（古い版の破棄だけが止まる）。
	failList bool
}

type fakeSecret struct {
	versions []*secretmanagerpb.SecretVersion
	payloads map[string][]byte
}

func newFakeSecretManager() *fakeSecretManager {
	return &fakeSecretManager{secrets: map[string]*fakeSecret{}}
}

func (f *fakeSecretManager) CreateSecret(_ context.Context, req *secretmanagerpb.CreateSecretRequest) (*secretmanagerpb.Secret, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := req.GetParent() + "/secrets/" + req.GetSecretId()
	if _, ok := f.secrets[name]; ok {
		return nil, status.Error(codes.AlreadyExists, "exists")
	}
	f.secrets[name] = &fakeSecret{payloads: map[string][]byte{}}
	return &secretmanagerpb.Secret{Name: name}, nil
}

func (f *fakeSecretManager) AddSecretVersion(_ context.Context, req *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.secrets[req.GetParent()]
	if !ok {
		return nil, status.Error(codes.NotFound, "secret")
	}
	v := &secretmanagerpb.SecretVersion{Name: fmt.Sprintf("%s/versions/%d", req.GetParent(), len(s.versions)+1), State: secretmanagerpb.SecretVersion_ENABLED}
	s.versions = append(s.versions, v)
	s.payloads[v.GetName()] = req.GetPayload().GetData()
	return v, nil
}

func (f *fakeSecretManager) ListSecretVersions(_ context.Context, req *secretmanagerpb.ListSecretVersionsRequest) (*secretmanagerpb.ListSecretVersionsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failList {
		return nil, status.Error(codes.Unavailable, "list")
	}
	s, ok := f.secrets[req.GetParent()]
	if !ok {
		return nil, status.Error(codes.NotFound, "secret")
	}
	out := &secretmanagerpb.ListSecretVersionsResponse{}
	for _, v := range s.versions {
		if req.GetFilter() == "state:ENABLED" && v.GetState() != secretmanagerpb.SecretVersion_ENABLED {
			continue
		}
		out.Versions = append(out.Versions, v)
	}
	return out, nil
}

func (f *fakeSecretManager) DestroySecretVersion(_ context.Context, req *secretmanagerpb.DestroySecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parent := req.GetName()[:strings.LastIndex(req.GetName(), "/versions/")]
	s, ok := f.secrets[parent]
	if !ok {
		return nil, status.Error(codes.NotFound, "secret")
	}
	for _, v := range s.versions {
		if v.GetName() == req.GetName() {
			v.State = secretmanagerpb.SecretVersion_DESTROYED
			delete(s.payloads, v.GetName())
			return v, nil
		}
	}
	return nil, status.Error(codes.NotFound, "version")
}

func (f *fakeSecretManager) AccessSecretVersion(_ context.Context, req *secretmanagerpb.AccessSecretVersionRequest) (*secretmanagerpb.AccessSecretVersionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parent := req.GetName()[:strings.LastIndex(req.GetName(), "/versions/")]
	s, ok := f.secrets[parent]
	if !ok {
		return nil, status.Error(codes.NotFound, "secret")
	}
	name := req.GetName()
	if strings.HasSuffix(name, "/versions/latest") {
		name = ""
		for _, v := range s.versions {
			if v.GetState() == secretmanagerpb.SecretVersion_ENABLED {
				name = v.GetName()
			}
		}
	}
	data, ok := s.payloads[name]
	if !ok {
		return nil, status.Error(codes.NotFound, "version")
	}
	return &secretmanagerpb.AccessSecretVersionResponse{Name: name, Payload: &secretmanagerpb.SecretPayload{Data: data}}, nil
}

func (f *fakeSecretManager) DeleteSecret(_ context.Context, req *secretmanagerpb.DeleteSecretRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.secrets[req.GetName()]; !ok {
		return nil, status.Error(codes.NotFound, "secret")
	}
	delete(f.secrets, req.GetName())
	return &emptypb.Empty{}, nil
}

// enabledVersions は secret の有効な版の名前（番号順）。
func (f *fakeSecretManager) enabledVersions(name string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.secrets[name]
	if !ok {
		return nil
	}
	var out []string
	for _, v := range s.versions {
		if v.GetState() == secretmanagerpb.SecretVersion_ENABLED {
			out = append(out, v.GetName())
		}
	}
	sort.Strings(out)
	return out
}

func newSecretStore(t *testing.T) (*SecretStore, *fakeSecretManager) {
	t.Helper()
	fake := newFakeSecretManager()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	secretmanagerpb.RegisterSecretManagerServiceServer(srv, fake)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufconn", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	client, err := secretmanager.NewClient(context.Background(), option.WithGRPCConn(conn), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return NewSecretStoreWithClient(client, "p"), fake
}

func TestSecretStoreKeepsOnlyLatestVersion(t *testing.T) {
	store, fake := newSecretStore(t)
	ctx := context.Background()
	name := "projects/p/secrets/" + SecretID("u1")
	if _, err := store.Load(ctx, "u1"); !errors.Is(err, apperr.Forbidden("calendar.reauth_required")) {
		t.Fatalf("load before save: %v", err)
	}
	if err := store.Save(ctx, "u1", []byte("token-1")); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "u1", []byte("token-2")); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "u1", []byte("token-3")); err != nil {
		t.Fatal(err)
	}
	if got := fake.enabledVersions(name); len(got) != 1 || !strings.HasSuffix(got[0], "/versions/3") {
		t.Fatalf("older versions not destroyed: %v", got)
	}
	if raw, err := store.Load(ctx, "u1"); err != nil || string(raw) != "token-3" {
		t.Fatalf("load: %s %v", raw, err)
	}
	// 一覧に失敗しても保存は成功し、次の保存で改めて古い版を破棄する。
	fake.failList = true
	if err := store.Save(ctx, "u1", []byte("token-4")); err != nil {
		t.Fatal(err)
	}
	if got := fake.enabledVersions(name); len(got) != 2 {
		t.Fatalf("expected the destroy to be skipped: %v", got)
	}
	fake.failList = false
	if err := store.Save(ctx, "u1", []byte("token-5")); err != nil {
		t.Fatal(err)
	}
	if got := fake.enabledVersions(name); len(got) != 1 || !strings.HasSuffix(got[0], "/versions/5") {
		t.Fatalf("older versions not destroyed after recovery: %v", got)
	}
}

func TestSecretStoreDeleteRemovesSecret(t *testing.T) {
	store, fake := newSecretStore(t)
	ctx := context.Background()
	// 無ければ何もしない。
	if err := store.Delete(ctx, "u1"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
	if err := store.Save(ctx, "u1", []byte("token")); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if got := fake.enabledVersions("projects/p/secrets/" + SecretID("u1")); got != nil {
		t.Fatalf("secret still exists: %v", got)
	}
	if _, err := store.Load(ctx, "u1"); !errors.Is(err, apperr.Forbidden("calendar.reauth_required")) {
		t.Fatalf("load after delete: %v", err)
	}
	// つなぎ直すと secret を作り直す。
	if err := store.Save(ctx, "u1", []byte("token-again")); err != nil {
		t.Fatal(err)
	}
	if raw, err := store.Load(ctx, "u1"); err != nil || string(raw) != "token-again" {
		t.Fatalf("load after recreate: %s %v", raw, err)
	}
}
