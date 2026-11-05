package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"google.golang.org/api/idtoken"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
	if err != nil || verified.Claims["email"] != "chat@system.gserviceaccount.com" {
		w.WriteHeader(401)
		return
	}
	var event struct {
		Type string `json:"type"`
		User struct {
			Name string `json:"name"`
		} `json:"user"`
		Message struct {
			Text string `json:"argumentText"`
			Name string `json:"name"`
		} `json:"message"`
	}
	if err = json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&event); err != nil {
		w.WriteHeader(400)
		return
	}
	if event.Type != "MESSAGE" || event.Message.Name == "" {
		writeJSON(w, 200, map[string]string{"text": "課題名・締切・必要時間を送ってください。"})
		return
	}
	googleID := strings.TrimPrefix(event.User.Name, "users/")
	mapping, err := s.Calendar.DB.Collection("googleUsers").Doc(googleID).Get(r.Context())
	if err != nil {
		writeJSON(w, 200, map[string]string{"text": "先に管理画面で同じGoogleアカウントのカレンダーを連携してください。\n" + s.AppURL})
		return
	}
	id, _ := mapping.DataAt("uid")
	state, err := s.Store.Read(r.Context(), id.(string))
	if err != nil {
		w.WriteHeader(500)
		return
	}
	inputs, err := s.chatInputs(r.Context(), id.(string), event.Message.Name, event.Message.Text, state.Profile)
	if err != nil {
		writeJSON(w, 200, map[string]string{"text": err.Error()})
		return
	}
	for _, input := range inputs {
		if _, err = s.createTask(r.Context(), id.(string), input); err != nil {
			writeJSON(w, 200, map[string]string{"text": err.Error()})
			return
		}
	}
	writeJSON(w, 200, map[string]string{"text": fmt.Sprintf("%d件の課題をDOPAGAKIカレンダーへ登録しました。具体的な予定は詳細欄と管理画面で確認できます。", len(inputs))})
}

func (s *Server) chatInputs(ctx context.Context, uid, name, text string, profile Profile) ([]TaskInput, error) {
	hash := sha256.Sum256([]byte(name))
	ref := s.Calendar.DB.Collection("chatRequests").Doc(hex.EncodeToString(hash[:]))
	doc, err := ref.Get(ctx)
	if err == nil {
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
