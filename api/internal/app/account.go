package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	firebaseauth "firebase.google.com/go/v4/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type accountStore interface {
	DeleteAccount(context.Context, string) error
}
type accountAssistantStore interface {
	DeleteHistory(context.Context, string) error
	AllHistory(context.Context, string) ([]AssistantTurn, error)
}

func (m *MemoryStore) DeleteAccount(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleted == nil {
		m.deleted = map[string]bool{}
	}
	m.deleted[id] = true
	delete(m.states, id)
	delete(m.histories, id)
	delete(m.archived, id)
	return nil
}
func deleteCollection(ctx context.Context, db *firestore.Client, col *firestore.CollectionRef) error {
	for {
		docs, err := col.Limit(100).Documents(ctx).GetAll()
		if err != nil {
			return err
		}
		if len(docs) == 0 {
			return nil
		}
		batch := db.Batch()
		for _, doc := range docs {
			batch.Delete(doc.Ref)
		}
		if _, err = batch.Commit(ctx); err != nil {
			return err
		}
	}
}
func (f *FirestoreStore) DeleteAccount(ctx context.Context, id string) error {
	root := f.DB.Collection("users").Doc(id)
	for _, name := range []string{"completions", "activityEvents", "archivedTasks"} {
		if err := deleteCollection(ctx, f.DB, root.Collection(name)); err != nil {
			return err
		}
	}
	hash := sha256.Sum256([]byte(id))
	if _, err := f.DB.Collection("aiUsage").Doc("user-" + hex.EncodeToString(hash[:])).Delete(ctx); err != nil {
		return err
	}
	for _, name := range []string{"oauthStates", "calendarChannels"} {
		docs, err := f.DB.Collection(name).Where("uid", "==", id).Documents(ctx).GetAll()
		if err != nil {
			return err
		}
		for _, doc := range docs {
			if _, err = doc.Ref.Delete(ctx); err != nil {
				return err
			}
		}
	}
	if _, err := f.DB.Collection("calendarConnections").Doc(id).Delete(ctx); err != nil {
		return err
	}
	_, err := root.Delete(ctx)
	return err
}
func (m *MemoryAssistantStore) DeleteHistory(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.turns, id)
	delete(m.leases, id)
	return nil
}
func (m *MemoryAssistantStore) AllHistory(_ context.Context, id string) ([]AssistantTurn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []AssistantTurn{}
	for _, r := range m.turns[id] {
		out = append(out, copyAssistantTurn(r.Turn))
	}
	return out, nil
}
func (f *FirestoreAssistantStore) DeleteHistory(ctx context.Context, id string) error {
	root := f.DB.Collection("assistantConversations").Doc(id)
	// Invalidate the lease first, so an earlier Finish cannot recreate a deleted turn.
	if _, err := root.Set(ctx, assistantLease{Deleting: true}); err != nil {
		return err
	}
	if err := deleteCollection(ctx, f.DB, root.Collection("turns")); err != nil {
		return err
	}
	_, err := root.Set(ctx, assistantLease{})
	return err
}
func (f *FirestoreAssistantStore) AllHistory(ctx context.Context, id string) ([]AssistantTurn, error) {
	docs, err := f.DB.Collection("assistantConversations").Doc(id).Collection("turns").OrderBy("createdAt", firestore.Asc).Documents(ctx).GetAll()
	out := []AssistantTurn{}
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		turn, err := assistantTurnFrom(doc)
		if err != nil {
			return nil, err
		}
		out = append(out, turn)
	}
	return out, nil
}
func (s *Server) deleteHistory(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DeleteMemory bool `json:"deleteMemory"`
	}
	if !decode(w, r, &in) {
		return
	}
	assistant, ok := s.Assistant.(accountAssistantStore)
	if !ok {
		respond(w, nil, errors.New("会話を削除できません"))
		return
	}
	_, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		now := s.now()
		st.AssistantContextSince = &now
		if in.DeleteMemory {
			st.Profile.Context = ""
		}
		return nil
	})
	if err == nil {
		err = assistant.DeleteHistory(r.Context(), uid(r))
	}
	respond(w, map[string]bool{"deleted": err == nil}, err)
}
func (s *Server) disconnectCalendar(w http.ResponseWriter, r *http.Request) {
	if _, err := s.Store.Update(r.Context(), uid(r), func(st *State) error { st.CalendarConnected = false; st.CalendarSync = "disconnecting"; return nil }); err != nil {
		respond(w, nil, err)
		return
	}
	if s.Calendar != nil {
		if err := s.Calendar.Disconnect(r.Context(), uid(r), false); err != nil {
			respond(w, nil, err)
			return
		}
	}
	st, err := s.Store.Update(r.Context(), uid(r), func(st *State) error { st.CalendarConnected = false; st.CalendarSync = "disconnected"; return nil })
	respond(w, st, err)
}
func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DeleteCalendar bool `json:"deleteCalendar"`
	}
	if !decode(w, r, &in) {
		return
	}
	if s.Mode != "demo" {
		at, ok := r.Context().Value(authTimeKey{}).(time.Time)
		if !ok || s.now().Sub(at) > 5*time.Minute {
			writeError(w, 401, errors.New("Googleで本人確認をやり直してください"))
			return
		}
	}
	id := uid(r)
	if s.Mode != "demo" && !s.AccountCleanupEnabled {
		writeError(w, 503, errors.New("退会処理の運用設定が完了していません。運営へお問い合わせください"))
		return
	}
	if in.DeleteCalendar && s.Calendar != nil {
		svc, accessErr := s.Calendar.service(r.Context(), id)
		if accessErr != nil {
			respond(w, nil, accessErr)
			return
		}
		conn, accessErr := s.Calendar.connection(r.Context(), id)
		if accessErr != nil {
			respond(w, nil, accessErr)
			return
		}
		if _, accessErr = svc.Calendars.Get(conn.CalendarID).Context(r.Context()).Do(); accessErr != nil {
			respond(w, nil, sourceError("専用カレンダーの削除前の確認", accessErr))
			return
		}
	}
	receipt := newID()
	receiptDigest := sha256.Sum256([]byte(receipt))
	receiptHash := hex.EncodeToString(receiptDigest[:])
	_, err := s.Store.Update(r.Context(), id, func(st *State) error {
		st.AccountClosing = true
		st.DeleteCalendar = in.DeleteCalendar
		st.AccountReceiptHash = receiptHash
		return nil
	})
	if err != nil && !errors.Is(err, errAccountClosing) {
		respond(w, nil, err)
		return
	}
	if s.Mode != "demo" {
		if errors.Is(err, errAccountClosing) {
			if f, ok := s.Store.(*FirestoreStore); ok {
				if _, updateErr := f.DB.Collection("accountJobs").Doc(id).Set(r.Context(), map[string]any{"receiptHash": receiptHash}, firestore.MergeAll); updateErr != nil {
					respond(w, nil, updateErr)
					return
				}
			}
		}
		writeJSON(w, 202, map[string]any{"accepted": true, "status": "closing", "receipt": receipt})
		return
	}
	if err = s.finishAccountDeletion(r.Context(), id, in.DeleteCalendar); err != nil {
		respond(w, nil, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "dopagaki_demo", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeJSON(w, 200, map[string]bool{"deleted": true})
}
func (s *Server) finishAccountDeletion(ctx context.Context, id string, removeCalendar bool) error {
	if s.Calendar != nil {
		if err := s.Calendar.Disconnect(ctx, id, removeCalendar); err != nil {
			return err
		}
	}
	if assistant, ok := s.Assistant.(accountAssistantStore); ok {
		if memory, ok := assistant.(*MemoryAssistantStore); ok {
			memory.mu.Lock()
			if memory.closed == nil {
				memory.closed = map[string]bool{}
			}
			memory.closed[id] = true
			memory.mu.Unlock()
		}
		if err := assistant.DeleteHistory(ctx, id); err != nil {
			return err
		}
		if f, ok := assistant.(*FirestoreAssistantStore); ok {
			if _, err := f.DB.Collection("assistantConversations").Doc(id).Delete(ctx); err != nil {
				return err
			}
		}
	}
	store, ok := s.Store.(accountStore)
	if !ok {
		return errors.New("削除に対応していません")
	}
	if err := store.DeleteAccount(ctx, id); err != nil {
		return err
	}
	if s.Auth != nil {
		if err := s.Auth.DeleteUser(ctx, id); err != nil && !firebaseauth.IsUserNotFound(err) {
			return err
		}
	}
	if f, ok := s.Store.(*FirestoreStore); ok {
		_, err := f.DB.Collection("accountJobs").Doc(id).Set(ctx, map[string]any{"status": "completed", "deleteCalendar": removeCalendar, "expiresAt": s.now().Add(7 * 24 * time.Hour)}, firestore.MergeAll)
		return err
	}
	return nil
}
func (s *Server) cleanupAccounts(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeMaintenance(r) {
		w.WriteHeader(403)
		return
	}
	f, ok := s.Store.(*FirestoreStore)
	if !ok {
		writeJSON(w, 200, map[string]int{"pending": 0})
		return
	}
	docs, err := f.DB.Collection("accountJobs").Where("status", "==", "closing").Documents(r.Context()).GetAll()
	if err != nil {
		respond(w, nil, err)
		return
	}
	sort.Slice(docs, func(i, j int) bool {
		a, _ := docs[i].Data()["lastAttemptAt"].(time.Time)
		b, _ := docs[j].Data()["lastAttemptAt"].(time.Time)
		if a.Equal(b) {
			return docs[i].Ref.ID < docs[j].Ref.ID
		}
		return a.Before(b)
	})
	failed := 0
	processed := 0
	for _, doc := range docs {
		if ready, ok := doc.Data()["notBefore"].(time.Time); ok && s.now().Before(ready) {
			continue
		}
		if processed >= 10 {
			break
		}
		processed++
		if _, saveErr := doc.Ref.Set(r.Context(), map[string]any{"lastAttemptAt": s.now()}, firestore.MergeAll); saveErr != nil {
			failed++
			continue
		}
		remove, _ := doc.Data()["deleteCalendar"].(bool)
		if err = s.finishAccountDeletion(r.Context(), doc.Ref.ID, remove); err != nil {
			failed++
			if _, saveErr := doc.Ref.Set(r.Context(), map[string]any{"failed": true}, firestore.MergeAll); saveErr != nil {
				LogFailure("record cleanup retry", saveErr)
			}
			LogFailure("account cleanup", err)
		}
	}
	if failed > 0 {
		writeJSON(w, 503, map[string]int{"failed": failed})
		return
	}
	writeJSON(w, 200, map[string]int{"processed": processed})
}
func closingDocument(doc *firestore.DocumentSnapshot, err error) bool {
	return err == nil && (doc.Data()["status"] == "closing" || doc.Data()["status"] == "completed")
}
func missingOrNil(err error) error {
	if status.Code(err) == codes.NotFound {
		return nil
	}
	return err
}

func (s *Server) deletionStatus(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Deletion ")
	if !ok || !tokenIDPattern.MatchString(token) {
		writeError(w, 404, ErrNotFound)
		return
	}
	f, ok := s.Store.(*FirestoreStore)
	if !ok {
		writeError(w, 404, ErrNotFound)
		return
	}
	digest := sha256.Sum256([]byte(token))
	docs, err := f.DB.Collection("accountJobs").Where("receiptHash", "==", hex.EncodeToString(digest[:])).Limit(1).Documents(r.Context()).GetAll()
	if err != nil {
		respond(w, nil, err)
		return
	}
	if len(docs) == 0 {
		writeError(w, 404, ErrNotFound)
		return
	}
	record := docs[0].Data()
	writeJSON(w, 200, map[string]any{"status": record["status"], "retrying": record["failed"] == true})
}
