// This command uses real services and makes at most one Gemini request.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"time"

	"cloud.google.com/go/firestore"
	iampb "cloud.google.com/go/iam/apiv1/iampb"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	secretpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/a-sumikawa/devfest-2026/api/internal/app"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/genai"
)

func event(phase string, values map[string]any) {
	values["phase"] = phase
	_ = json.NewEncoder(os.Stdout).Encode(values)
}

func run(ctx context.Context, project, id string) error {
	owner := "security-smoke-" + id
	db, err := firestore.NewClient(ctx, project)
	if err != nil {
		return err
	}
	defer db.Close()
	crm, err := cloudresourcemanager.NewService(ctx)
	if err != nil {
		return err
	}
	access, err := crm.Projects.TestIamPermissions(project, &cloudresourcemanager.TestIamPermissionsRequest{Permissions: []string{"aiplatform.endpoints.predict", "aiplatform.endpoints.create"}}).Context(ctx).Do()
	if err != nil {
		return err
	}
	if !slices.Contains(access.Permissions, "aiplatform.endpoints.predict") || slices.Contains(access.Permissions, "aiplatform.endpoints.create") {
		return errors.New("Gemini inference permissions differ from the reviewed policy")
	}
	event("vertex-permissions", map[string]any{"predictAllowed": true, "administrationDenied": true})
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := db.Batch().
			Delete(db.Collection("users").Doc(owner)).
			Delete(db.Collection("assistantConversations").Doc(owner).Collection("turns").Doc(id)).
			Delete(db.Collection("assistantConversations").Doc(owner)).Commit(cleanup)
		event("firestore-cleanup", map[string]any{"ok": err == nil})
	}()
	store := &app.FirestoreStore{DB: db}
	state, err := store.Read(ctx, owner)
	if err != nil {
		return err
	}
	if state.BalanceSeconds != 600 || len(state.Tasks) != 0 {
		return errors.New("unexpected smoke fixture state")
	}
	if _, err = store.Update(ctx, owner, func(s *app.State) error { s.Profile.Name = "接続確認用"; return nil }); err != nil {
		return err
	}
	assistant := &app.FirestoreAssistantStore{DB: db}
	turn, runID, err := assistant.Begin(ctx, owner, id, "接続確認です。短く返答し、操作カードは提案しないでください。", "")
	if err != nil {
		return err
	}
	if runID == "" {
		return errors.New("smoke conversation was already processed")
	}
	if _, err := assistant.History(ctx, owner); err != nil {
		return err
	}
	event("firestore", map[string]any{"readWriteAndQuery": true})

	secrets, err := secretmanager.NewClient(ctx)
	if err != nil {
		return err
	}
	defer secrets.Close()
	for _, name := range []string{"dopagaki-calendar-oauth", "dopagaki-scheduler"} {
		resource := "projects/" + project + "/secrets/" + name
		if _, err := secrets.AccessSecretVersion(ctx, &secretpb.AccessSecretVersionRequest{Name: resource + "/versions/1"}); err != nil {
			return err
		}
		permissions, err := secrets.TestIamPermissions(ctx, &iampb.TestIamPermissionsRequest{Resource: resource, Permissions: []string{"secretmanager.versions.add", "secretmanager.secrets.delete"}})
		if err != nil {
			return err
		}
		if len(permissions.Permissions) != 0 {
			return errors.New("static secret has unexpected write or delete access")
		}
	}
	created, err := secrets.CreateSecret(ctx, &secretpb.CreateSecretRequest{Parent: "projects/" + project, SecretId: "dopagaki-calendar-" + id, Secret: &secretpb.Secret{
		Labels:      map[string]string{"purpose": "dopagaki-security-smoke"},
		Replication: &secretpb.Replication{Replication: &secretpb.Replication_Automatic_{Automatic: &secretpb.Replication_Automatic{}}},
	}})
	if err != nil {
		return err
	}
	event("secret-created", map[string]any{"name": created.Name})
	payload := []byte("dopagaki-security-smoke-fixture")
	version, err := secrets.AddSecretVersion(ctx, &secretpb.AddSecretVersionRequest{Parent: created.Name, Payload: &secretpb.SecretPayload{Data: payload}})
	if err != nil {
		return err
	}
	read, err := secrets.AccessSecretVersion(ctx, &secretpb.AccessSecretVersionRequest{Name: version.Name})
	if err != nil {
		return err
	}
	if !bytes.Equal(read.Payload.Data, payload) {
		return errors.New("secret fixture readback differs")
	}
	permissions, err := secrets.TestIamPermissions(ctx, &iampb.TestIamPermissionsRequest{Resource: created.Name, Permissions: []string{"secretmanager.versions.add", "secretmanager.secrets.delete"}})
	if err != nil {
		return err
	}
	if !slices.Contains(permissions.Permissions, "secretmanager.versions.add") || slices.Contains(permissions.Permissions, "secretmanager.secrets.delete") {
		return errors.New("calendar token permissions differ from the reviewed policy")
	}
	event("secrets", map[string]any{"staticRead": true, "staticWriteDenied": true, "tokenCreateAddRead": true, "deleteDenied": true})

	model, err := genai.NewClient(ctx, &genai.ClientConfig{Project: project, Location: os.Getenv("GOOGLE_CLOUD_LOCATION"), Backend: genai.BackendVertexAI})
	if err != nil {
		return err
	}
	planner := &app.Planner{Client: model, Model: os.Getenv("GEMINI_MODEL"), Usage: &app.FirestoreAIUsageStore{DB: db}, Limits: app.DefaultAILimits()}
	event("ai-start", map[string]any{"maximumRequests": 1})
	turn.Reply, turn.Cards, err = planner.Consult(ctx, owner, turn.Text, nil, state, "")
	if err != nil {
		return err
	}
	turn.Status, turn.PendingUntil = "ready", nil
	if err := assistant.Finish(ctx, owner, turn, runID); err != nil {
		return err
	}
	history, err := assistant.History(ctx, owner)
	if err != nil {
		return err
	}
	if len(history) != 1 || history[0].Status != "ready" || history[0].Reply == "" {
		return errors.New("AI response was not persisted")
	}
	event("complete", map[string]any{"aiResponse": true, "conversationReadback": true, "cardsApplied": false, "owner": owner})
	return nil
}

func main() {
	id := os.Getenv("SECURITY_SMOKE_ID")
	if os.Getenv("DOPAGAKI_LIVE_SECURITY_SMOKE") != "1" || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(id) {
		slog.Error("explicit live-security-smoke flag and fixture ID are required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := run(ctx, os.Getenv("GOOGLE_CLOUD_PROJECT"), id); err != nil {
		app.LogFailure("security smoke failed", err)
		os.Exit(1)
	}
}
