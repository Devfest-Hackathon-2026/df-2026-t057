package cloudtasks

import (
	"context"
	"encoding/json"
	"testing"

	"cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
)

type fakeCreator struct {
	requests []*cloudtaskspb.CreateTaskRequest
	err      error
	closed   bool
}

func (f *fakeCreator) CreateTask(_ context.Context, req *cloudtaskspb.CreateTaskRequest) error {
	f.requests = append(f.requests, req)
	return f.err
}

func (f *fakeCreator) Close() error {
	f.closed = true
	return nil
}

const queue = "projects/p/locations/asia-northeast1/queues/dopagaki-outbox"

func TestDispatchCreatesAuthenticatedHTTPTask(t *testing.T) {
	f := &fakeCreator{}
	d := newDispatcher(f, queue, "https://app.example/", "invoker@p.iam.gserviceaccount.com", "https://app.example")
	if err := d.Dispatch(context.Background(), "user-1", "job1"); err != nil {
		t.Fatal(err)
	}
	req := f.requests[0]
	if req.GetParent() != queue || req.GetTask().GetName() != queue+"/tasks/job1" {
		t.Fatalf("names: %v %v", req.GetParent(), req.GetTask().GetName())
	}
	h := req.GetTask().GetHttpRequest()
	if h.GetUrl() != "https://app.example/api/internal/outbox/job1" || h.GetHttpMethod() != cloudtaskspb.HttpMethod_POST || h.GetHeaders()["Content-Type"] != "application/json" {
		t.Fatalf("request: %v", h)
	}
	var body map[string]string
	if err := json.Unmarshal(h.GetBody(), &body); err != nil || body["uid"] != "user-1" || len(body) != 1 {
		t.Fatalf("body: %s", h.GetBody())
	}
	oidc := h.GetOidcToken()
	if oidc.GetServiceAccountEmail() != "invoker@p.iam.gserviceaccount.com" || oidc.GetAudience() != "https://app.example" {
		t.Fatalf("oidc: %v", oidc)
	}
}

func TestDispatchTreatsExistingTaskAsDispatched(t *testing.T) {
	f := &fakeCreator{err: status.Error(codes.AlreadyExists, "exists")}
	d := newDispatcher(f, queue, "https://app.example", "i", "a")
	if err := d.Dispatch(context.Background(), "u", "job1"); err != nil {
		t.Fatalf("already exists: %v", err)
	}
	f.err = status.Error(codes.PermissionDenied, "detail about the project")
	err := d.Dispatch(context.Background(), "u", "job1")
	if apperr.As(err).Code != "external.failed" {
		t.Fatalf("permission denied: %v", err)
	}
	if err := d.Close(); err != nil || !f.closed {
		t.Fatal("close")
	}
}
