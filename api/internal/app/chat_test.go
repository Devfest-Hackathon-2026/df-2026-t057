package app

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWorkspaceChatMessageContract(t *testing.T) {
	// Workspace add-ons nest messages under chat.messagePayload, unlike legacy Chat apps.
	raw := `{"commonEventObject":{"hostApp":"CHAT"},"chat":{"user":{"name":"users/123"},"messagePayload":{"message":{"name":"spaces/one/messages/two","text":"明日18時までにレポート、必要時間30分"}}}}`
	var event chatEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatal(err)
	}
	if event.Chat.User.Name != "users/123" || event.Chat.MessagePayload == nil || event.Chat.MessagePayload.Message.Name != "spaces/one/messages/two" || event.Chat.MessagePayload.Message.Text == "" {
		t.Fatal("Workspace message context was lost")
	}
	w := httptest.NewRecorder()
	chatReply(w, "登録しました")
	var reply struct {
		HostAppDataAction struct {
			ChatDataAction struct {
				CreateMessageAction struct {
					Message struct{ Text string }
				}
			}
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || reply.HostAppDataAction.ChatDataAction.CreateMessageAction.Message.Text != "登録しました" {
		t.Fatal("response does not create a Workspace Chat message")
	}
}

func TestChatRejectsUnauthenticatedRequests(t *testing.T) {
	t.Setenv("CHAT_AUDIENCE", "https://dopagaki.example/api/chat")
	s := &Server{Calendar: &Calendar{}}
	w := httptest.NewRecorder()
	s.chat(w, httptest.NewRequest("POST", "/api/chat", nil))
	if w.Code != 401 {
		t.Fatalf("unauthenticated Chat request: %d", w.Code)
	}
}
