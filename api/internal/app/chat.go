package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"google.golang.org/api/idtoken"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type chatEvent struct {
	Chat struct {
		User struct {
			Name string `json:"name"`
		} `json:"user"`
		MessagePayload *struct {
			Message struct {
				Name         string    `json:"name"`
				Text         string    `json:"text"`
				ArgumentText string    `json:"argumentText"`
				Space        chatSpace `json:"space"`
			} `json:"message"`
		} `json:"messagePayload"`
		AddedToSpacePayload *struct{} `json:"addedToSpacePayload"`
	} `json:"chat"`
}

type chatSpace struct {
	SpaceType string `json:"spaceType"`
	Type      string `json:"type"`
}

func (s chatSpace) directMessage() bool {
	if s.SpaceType != "" {
		return s.SpaceType == "DIRECT_MESSAGE"
	}
	return s.Type == "DM"
}

func chatReply(w http.ResponseWriter, text string) {
	writeJSON(w, 200, map[string]any{
		"hostAppDataAction": map[string]any{
			"chatDataAction": map[string]any{
				"createMessageAction": map[string]any{"message": map[string]string{"text": text}},
			},
		},
	})
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	audience := os.Getenv("CHAT_AUDIENCE")
	if s.Calendar == nil || audience == "" {
		http.NotFound(w, r)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	verified, err := idtoken.Validate(r.Context(), token, audience)
	serviceAccount := "service-" + os.Getenv("GOOGLE_CLOUD_PROJECT_NUMBER") + "@gcp-sa-gsuiteaddons.iam.gserviceaccount.com"
	if err != nil || verified.Claims["email_verified"] != true || verified.Claims["email"] != serviceAccount {
		w.WriteHeader(401)
		return
	}
	var event chatEvent
	if err = json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&event); err != nil {
		slog.Warn("invalid Chat event", "error", err)
		w.WriteHeader(400)
		return
	}
	if event.Chat.MessagePayload == nil {
		if event.Chat.AddedToSpacePayload != nil {
			chatReply(w, "課題名・締切・必要時間を送ってください。")
		} else {
			writeJSON(w, 200, map[string]any{})
		}
		return
	}
	message := event.Chat.MessagePayload.Message
	if !message.Space.directMessage() {
		chatReply(w, "DOPAGAKIとの1対1のダイレクトメッセージから課題を送ってください。")
		return
	}
	if message.Name == "" || event.Chat.User.Name == "" {
		slog.Warn("incomplete Chat event", "hasMessageName", message.Name != "", "hasUserName", event.Chat.User.Name != "")
		w.WriteHeader(400)
		return
	}
	googleID := strings.TrimPrefix(event.Chat.User.Name, "users/")
	mapping, err := s.Calendar.DB.Collection("googleUsers").Doc(googleID).Get(r.Context())
	if err != nil {
		chatReply(w, "先に管理画面で同じGoogleアカウントのカレンダーを連携してください。\n"+s.AppURL)
		return
	}
	id, _ := mapping.DataAt("uid")
	state, err := s.Store.Read(r.Context(), id.(string))
	if err != nil {
		w.WriteHeader(500)
		return
	}
	text := message.ArgumentText
	if text == "" {
		text = message.Text
	}
	inputs, err := s.chatInputs(r.Context(), id.(string), message.Name, text, state.Profile)
	if err != nil {
		chatReply(w, err.Error())
		return
	}
	for _, input := range inputs {
		if _, err = s.createTask(r.Context(), id.(string), input); err != nil {
			chatReply(w, err.Error())
			return
		}
	}
	chatReply(w, fmt.Sprintf("%d件の課題をDOPAGAKIカレンダーへ登録しました。具体的な予定は詳細欄と管理画面で確認できます。", len(inputs)))
}

func (s *Server) chatInputs(ctx context.Context, uid, name, text string, profile Profile) ([]TaskInput, error) {
	hash := sha256.Sum256([]byte(name))
	ref := s.Calendar.DB.Collection("chatRequests").Doc(hex.EncodeToString(hash[:]))
	doc, err := ref.Get(ctx)
	if err == nil {
		if doc.Data()["uid"] != uid {
			return nil, errors.New("このメッセージの登録先を確認できませんでした")
		}
		raw, err := doc.DataAt("inputs")
		if err != nil {
			return nil, err
		}
		var inputs []TaskInput
		err = json.Unmarshal([]byte(raw.(string)), &inputs)
		return inputs, err
	}
	if status.Code(err) != codes.NotFound {
		return nil, err
	}
	inputs, err := s.Planner.Parse(ctx, text, profile)
	if err != nil {
		return nil, err
	}
	for i := range inputs {
		hash := sha256.Sum256([]byte(name + fmt.Sprint(i)))
		inputs[i].ID = "chat-" + hex.EncodeToString(hash[:16])
	}
	raw, err := json.Marshal(inputs)
	if err != nil {
		return nil, err
	}
	_, err = ref.Create(ctx, map[string]any{"uid": uid, "inputs": string(raw), "createdAt": time.Now()})
	if status.Code(err) == codes.AlreadyExists {
		return s.chatInputs(ctx, uid, name, text, profile)
	}
	return inputs, err
}
