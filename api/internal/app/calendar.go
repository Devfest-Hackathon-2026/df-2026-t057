package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	secretpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/idtoken"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Connection struct {
	UID          string    `firestore:"uid"`
	GoogleID     string    `firestore:"googleId"`
	CalendarID   string    `firestore:"calendarId"`
	SyncToken    string    `firestore:"syncToken"`
	ChannelID    string    `firestore:"channelId"`
	ChannelToken string    `firestore:"channelToken"`
	Expires      time.Time `firestore:"expires"`
}
type Calendar struct {
	DB      *firestore.Client
	Secrets *secretmanager.Client
	OAuth   oauth2.Config
	Project string
	AppURL  string
	Store   Store
	Planner *Planner
}

func NewCalendar(db *firestore.Client, secrets *secretmanager.Client, project, appURL string, store Store, planner *Planner) *Calendar {
	return &Calendar{DB: db, Secrets: secrets, Project: project, AppURL: appURL, Store: store, Planner: planner, OAuth: oauth2.Config{ClientID: os.Getenv("GOOGLE_OAUTH_CLIENT_ID"), ClientSecret: os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"), Endpoint: google.Endpoint, RedirectURL: appURL + "/api/calendar/callback", Scopes: []string{"openid", "email", calendar.CalendarAppCreatedScope}}}
}
func (c *Calendar) connection(ctx context.Context, uid string) (Connection, error) {
	doc, err := c.DB.Collection("calendarConnections").Doc(uid).Get(ctx)
	if err != nil {
		return Connection{}, errors.New("Googleカレンダーを連携してください")
	}
	var conn Connection
	err = doc.DataTo(&conn)
	return conn, err
}
func (c *Calendar) secretName(uid string) string {
	hash := sha256.Sum256([]byte(uid))
	return "projects/" + c.Project + "/secrets/dopagaki-calendar-" + hex.EncodeToString(hash[:16])
}
func (c *Calendar) saveToken(ctx context.Context, uid string, token *oauth2.Token) error {
	name := c.secretName(uid)
	_, err := c.Secrets.CreateSecret(ctx, &secretpb.CreateSecretRequest{Parent: "projects/" + c.Project, SecretId: strings.TrimPrefix(name, "projects/"+c.Project+"/secrets/"), Secret: &secretpb.Secret{Replication: &secretpb.Replication{Replication: &secretpb.Replication_Automatic_{Automatic: &secretpb.Replication_Automatic{}}}}})
	if err != nil && status.Code(err) != codes.AlreadyExists {
		return err
	}
	raw, err := json.Marshal(token)
	if err != nil {
		return err
	}
	_, err = c.Secrets.AddSecretVersion(ctx, &secretpb.AddSecretVersionRequest{Parent: name, Payload: &secretpb.SecretPayload{Data: raw}})
	return err
}
func (c *Calendar) service(ctx context.Context, uid string) (*calendar.Service, error) {
	version, err := c.Secrets.AccessSecretVersion(ctx, &secretpb.AccessSecretVersionRequest{Name: c.secretName(uid) + "/versions/latest"})
	if err != nil {
		return nil, err
	}
	var token oauth2.Token
	if err = json.Unmarshal(version.Payload.Data, &token); err != nil {
		return nil, err
	}
	return calendar.NewService(ctx, option.WithHTTPClient(c.OAuth.Client(ctx, &token)))
}
func (c *Calendar) connect(ctx context.Context, uid, googleID, email string) (string, error) {
	state := newID()
	_, err := c.DB.Collection("oauthStates").Doc(state).Set(ctx, map[string]any{"uid": uid, "googleId": googleID, "expires": time.Now().Add(10 * time.Minute)})
	if err != nil {
		return "", err
	}
	return c.OAuth.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent select_account"), oauth2.SetAuthURLParam("login_hint", email)), nil
}
func (c *Calendar) callback(ctx context.Context, state, code string) error {
	if !tokenIDPattern.MatchString(state) || code == "" {
		return errors.New("連携の有効期限が切れました。もう一度連携してください")
	}
	ref := c.DB.Collection("oauthStates").Doc(state)
	var uid string
	var googleID string
	err := c.DB.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(ref)
		if err != nil {
			return errors.New("連携の有効期限が切れました。もう一度連携してください")
		}
		expires, _ := doc.DataAt("expires")
		if !expires.(time.Time).After(time.Now()) {
			return errors.New("連携の有効期限が切れました")
		}
		value, _ := doc.DataAt("uid")
		uid = value.(string)
		googleID, _ = doc.Data()["googleId"].(string)
		return tx.Delete(ref)
	})
	if err != nil {
		return err
	}
	token, err := c.OAuth.Exchange(ctx, code)
	if err != nil {
		return err
	}
	rawID, _ := token.Extra("id_token").(string)
	identity, err := idtoken.Validate(ctx, rawID, c.OAuth.ClientID)
	if err != nil {
		return err
	}
	if err = validateCalendarAccount(googleID, identity.Subject); err != nil {
		return err
	}
	if token.RefreshToken == "" {
		return errors.New("継続連携の許可を確認して、もう一度連携してください")
	}
	if err = c.saveToken(ctx, uid, token); err != nil {
		return err
	}
	service, err := calendar.NewService(ctx, option.WithHTTPClient(c.OAuth.Client(ctx, token)))
	if err != nil {
		return err
	}
	calendarID := ""
	if old, err := c.connection(ctx, uid); err == nil {
		calendarID = old.CalendarID
	}
	if calendarID == "" {
		created, err := service.Calendars.Insert(&calendar.Calendar{Summary: "DOPAGAKI", Description: "課題と締切、具体的な作業予定", TimeZone: "Asia/Tokyo"}).Context(ctx).Do()
		if err != nil {
			return err
		}
		calendarID = created.Id
	}
	conn := Connection{UID: uid, GoogleID: identity.Subject, CalendarID: calendarID}
	_, err = c.DB.Collection("calendarConnections").Doc(uid).Set(ctx, conn)
	if err != nil {
		return err
	}
	_, err = c.Store.Update(ctx, uid, func(s *State) error { s.CalendarConnected = true; s.CalendarSync = "synced"; return nil })
	if err != nil {
		return err
	}
	return c.watch(ctx, &conn, service)
}

func validateCalendarAccount(expected, actual string) error {
	if expected == "" || expected != actual {
		return errors.New("ログイン中と同じGoogleアカウントでカレンダーを連携してください")
	}
	return nil
}
func (c *Calendar) watch(ctx context.Context, conn *Connection, svc *calendar.Service) error {
	channel := &calendar.Channel{Id: newID(), Type: "web_hook", Address: c.AppURL + "/api/calendar/webhook", Token: newID(), Expiration: time.Now().Add(6 * 24 * time.Hour).UnixMilli()}
	_, err := c.DB.Collection("calendarChannels").Doc(channel.Id).Set(ctx, map[string]any{"uid": conn.UID, "token": channel.Token, "expires": time.UnixMilli(channel.Expiration)})
	if err != nil {
		return err
	}
	result, err := svc.Events.Watch(conn.CalendarID, channel).Context(ctx).Do()
	if err != nil {
		return err
	}
	conn.ChannelID = channel.Id
	conn.ChannelToken = channel.Token
	conn.Expires = time.UnixMilli(result.Expiration)
	_, err = c.DB.Collection("calendarConnections").Doc(conn.UID).Set(ctx, conn)
	return err
}
func (c *Calendar) CreateEvent(ctx context.Context, uid string, in TaskInput) (*calendar.Event, error) {
	svc, err := c.service(ctx, uid)
	if err != nil {
		return nil, err
	}
	conn, err := c.connection(ctx, uid)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(uid + ":" + in.ID))
	id := hex.EncodeToString(hash[:16])
	event := &calendar.Event{
		Id: id, Summary: in.Title, Description: in.Notes,
		Start:              &calendar.EventDateTime{DateTime: in.Deadline.Add(-time.Duration(in.Minutes) * time.Minute).Format(time.RFC3339)},
		End:                &calendar.EventDateTime{DateTime: in.Deadline.Format(time.RFC3339)},
		ExtendedProperties: &calendar.EventExtendedProperties{Private: map[string]string{"dopagakiTaskId": in.ID}},
	}
	created, err := svc.Events.Insert(conn.CalendarID, event).Context(ctx).Do()
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) && apiErr.Code == 409 {
		return svc.Events.Get(conn.CalendarID, id).Context(ctx).Do()
	}
	if err != nil {
		return nil, sourceError("カレンダー登録", err)
	}
	return created, nil
}
func (c *Calendar) DeleteEvent(ctx context.Context, uid, eventID string) error {
	svc, err := c.service(ctx, uid)
	if err != nil {
		return err
	}
	conn, err := c.connection(ctx, uid)
	if err != nil {
		return err
	}
	err = svc.Events.Delete(conn.CalendarID, eventID).Context(ctx).Do()
	return err
}
func stripPlan(description string) string {
	start := strings.Index(description, "[DOPAGAKI]")
	end := strings.Index(description, "[/DOPAGAKI]")
	if start >= 0 && end > start {
		return strings.TrimSpace(description[:start] + description[end+len("[/DOPAGAKI]"):])
	}
	return strings.TrimSpace(description)
}
func eventHash(e *calendar.Event) string {
	instant := func(value string) string {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return value
		}
		return parsed.UTC().Format(time.RFC3339Nano)
	}
	raw, _ := json.Marshal([]string{e.Summary, stripPlan(e.Description), instant(e.Start.DateTime), instant(e.End.DateTime)})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func (c *Calendar) Sync(ctx context.Context, uid string) (State, error) {
	current, err := c.Store.Read(ctx, uid)
	if err != nil {
		return State{}, err
	}
	// Flush confirmed edits before importing webhook changes from the same events.
	for _, task := range current.Tasks {
		if task.CalendarInputPending {
			if _, err = c.Export(ctx, uid, current); err != nil {
				return State{}, err
			}
			break
		}
	}
	conn, err := c.connection(ctx, uid)
	if err != nil {
		return State{}, err
	}
	svc, err := c.service(ctx, uid)
	if err != nil {
		return State{}, err
	}
	events := []*calendar.Event{}
	page := ""
	nextSync := ""
	for {
		call := svc.Events.List(conn.CalendarID).MaxResults(250).ShowDeleted(true)
		if conn.SyncToken != "" {
			call = call.SyncToken(conn.SyncToken)
		}
		if page != "" {
			call = call.PageToken(page)
		}
		result, err := call.Context(ctx).Do()
		var apiErr *googleapi.Error
		if errors.As(err, &apiErr) && apiErr.Code == 410 {
			conn.SyncToken = ""
			_, _ = c.DB.Collection("calendarConnections").Doc(uid).Set(ctx, conn)
			return c.Sync(ctx, uid)
		}
		if err != nil {
			return State{}, sourceError("カレンダー読み込み", err)
		}
		events = append(events, result.Items...)
		page = result.NextPageToken
		nextSync = result.NextSyncToken
		if page == "" {
			break
		}
	}
	now := time.Now()
	current, err = c.Store.Read(ctx, uid)
	if err != nil {
		return State{}, err
	}
	plans := map[string]Plan{}
	for _, event := range events {
		if event.Status == "cancelled" || len(event.Recurrence) > 0 || event.RecurringEventId != "" || event.Start == nil || event.End == nil || event.Start.DateTime == "" || event.End.DateTime == "" {
			continue
		}
		start, e1 := time.Parse(time.RFC3339, event.Start.DateTime)
		end, e2 := time.Parse(time.RFC3339, event.End.DateTime)
		minutes := int(end.Sub(start).Minutes())
		if e1 != nil || e2 != nil || end.Before(now) || minutes < 1 || minutes > 720 {
			continue
		}
		unchanged := false
		for _, task := range current.Tasks {
			if task.CalendarEventID == event.Id && task.InputHash == eventHash(event) {
				unchanged = true
				break
			}
		}
		if unchanged {
			continue
		}
		plan, err := c.Planner.Detail(ctx, uid, Task{Title: event.Summary, Notes: stripPlan(event.Description), Minutes: minutes, Deadline: end}, current.Profile)
		if err != nil {
			return State{}, err
		}
		plans[event.Id] = plan
	}
	state, err := c.Store.Update(ctx, uid, func(st *State) error {
		applyCalendarEvents(st, events, plans, now)
		return nil
	})
	if err != nil {
		return State{}, err
	}
	conn.SyncToken = nextSync
	if _, err = c.DB.Collection("calendarConnections").Doc(uid).Set(ctx, conn); err != nil {
		return State{}, err
	}
	if state.CalendarSync == "pending" {
		return c.Export(ctx, uid, state)
	}
	return state, nil
}
func applyCalendarEvents(st *State, events []*calendar.Event, plans map[string]Plan, now time.Time) {
	changed := false
	for _, event := range events {
		existing := -1
		for i, t := range st.Tasks {
			if t.CalendarEventID == event.Id {
				existing = i
				break
			}
		}
		if event.Status == "cancelled" {
			if existing >= 0 {
				if st.Session != nil && st.Session.TaskID == st.Tasks[existing].ID {
					st.Session = nil
				}
				st.Tasks = append(st.Tasks[:existing], st.Tasks[existing+1:]...)
				changed = true
			}
			continue
		}
		if len(event.Recurrence) > 0 || event.RecurringEventId != "" || event.Start == nil || event.End == nil || event.Start.DateTime == "" || event.End.DateTime == "" {
			continue
		}
		start, e1 := time.Parse(time.RFC3339, event.Start.DateTime)
		end, e2 := time.Parse(time.RFC3339, event.End.DateTime)
		if e1 != nil || e2 != nil || end.Before(now) {
			continue
		}
		minutes := int(end.Sub(start).Minutes())
		if minutes < 1 || minutes > 720 {
			continue
		}
		hash := eventHash(event)
		if existing >= 0 {
			t := &st.Tasks[existing]
			t.CalendarETag = event.Etag
			if t.InputHash == hash {
				continue
			}
			t.Title = event.Summary
			t.Notes = stripPlan(event.Description)
			t.Deadline = end
			t.Minutes = minutes
			t.InputHash = hash
			t.Plan = plans[event.Id]
			changed = true
		} else {
			if len(st.Tasks) >= 200 {
				continue
			}
			eventIDHash := sha256.Sum256([]byte(event.Id))
			id := "calendar-" + hex.EncodeToString(eventIDHash[:16])
			if event.ExtendedProperties != nil && event.ExtendedProperties.Private["dopagakiTaskId"] != "" {
				id = event.ExtendedProperties.Private["dopagakiTaskId"]
			}
			if _, err := st.task(id); err == nil {
				continue
			}
			st.Tasks = append(st.Tasks, Task{ID: id, Title: event.Summary, Notes: stripPlan(event.Description), Deadline: end, Minutes: minutes, Status: "pending", CreatedAt: now, CalendarEventID: event.Id, CalendarETag: event.Etag, InputHash: hash, Plan: plans[event.Id]})
			st.grant("registration", id, 600, now)
			changed = true
		}
	}
	if changed {
		st.replan()
		prepareDraft(st, now)
		st.CalendarSync = "pending"
	}
	st.LastSync = &now
}

func (c *Calendar) Export(ctx context.Context, uid string, state State) (State, error) {
	svc, err := c.service(ctx, uid)
	if err != nil {
		return state, err
	}
	conn, err := c.connection(ctx, uid)
	if err != nil {
		return state, err
	}
	location, err := time.LoadLocation(state.Profile.TimeZone)
	if err != nil {
		return state, err
	}
	etags := map[string]string{}
	hashes := map[string]string{}
	for _, task := range state.Tasks {
		if task.CalendarEventID == "" || task.Status == "completed" {
			continue
		}
		event, err := svc.Events.Get(conn.CalendarID, task.CalendarEventID).Context(ctx).Do()
		if err != nil {
			return state, sourceError("予定の確認", err)
		}
		patch, desiredHash, err := calendarUpdate(task, event, location)
		if err != nil {
			return state, err
		}
		hashes[task.ID] = desiredHash
		if eventHash(event) == desiredHash && event.Description == patch.Description {
			etags[task.ID] = event.Etag
			continue
		}
		call := svc.Events.Patch(conn.CalendarID, task.CalendarEventID, patch)
		call.Header().Set("If-Match", event.Etag)
		updated, err := call.Context(ctx).Do()
		var apiErr *googleapi.Error
		if errors.As(err, &apiErr) && apiErr.Code == 412 {
			return state, ErrConflict
		}
		if err != nil {
			return state, sourceError("カレンダー保存", err)
		}
		etags[task.ID] = updated.Etag
		hashes[task.ID] = eventHash(updated)
	}
	return c.Store.Update(ctx, uid, func(st *State) error {
		if st.PlanRevision != state.PlanRevision {
			return ErrConflict
		}
		for i := range st.Tasks {
			if tag, ok := etags[st.Tasks[i].ID]; ok {
				st.Tasks[i].CalendarETag = tag
				st.Tasks[i].InputHash = hashes[st.Tasks[i].ID]
				st.Tasks[i].CalendarInputPending = false
			}
		}
		st.CalendarSync = "synced"
		return nil
	})
}
func calendarUpdate(task Task, event *calendar.Event, location *time.Location) (*calendar.Event, string, error) {
	patch := calendarPatch(task, event, location)
	desired := *event
	desired.Description = patch.Description
	if task.CalendarInputPending {
		desired.Summary, desired.Start, desired.End = patch.Summary, patch.Start, patch.End
	}
	desiredHash := eventHash(&desired)
	actualHash := eventHash(event)
	changed := task.CalendarETag != "" && event.Etag != task.CalendarETag && task.InputHash != "" && actualHash != task.InputHash
	alreadyApplied := task.CalendarInputPending && actualHash == desiredHash
	if changed && !alreadyApplied {
		return nil, "", ErrConflict
	}
	return patch, desiredHash, nil
}

func calendarPatch(task Task, event *calendar.Event, location *time.Location) *calendar.Event {
	notes := stripPlan(event.Description)
	patch := &calendar.Event{}
	if task.CalendarInputPending {
		notes = task.Notes
		patch.Summary = task.Title
		patch.Start = &calendar.EventDateTime{DateTime: task.Deadline.Add(-time.Duration(task.Minutes) * time.Minute).Format(time.RFC3339)}
		patch.End = &calendar.EventDateTime{DateTime: task.Deadline.Format(time.RFC3339)}
	}
	var block strings.Builder
	block.WriteString("[DOPAGAKI]\n")
	for _, step := range task.Plan.Steps {
		fmt.Fprintf(&block, "%s–%s %s\n", step.Start.In(location).Format("15:04"), step.End.In(location).Format("15:04"), step.Title)
	}
	fmt.Fprintf(&block, "締切: %s\n%s\n[/DOPAGAKI]", task.Deadline.In(location).Format("01/02 15:04"), task.Plan.Reason)
	patch.Description = strings.TrimSpace(notes + "\n\n" + block.String())
	return patch
}

func (s *Server) connectCalendar(w http.ResponseWriter, r *http.Request) {
	if s.Calendar == nil {
		respond(w, nil, errors.New("デモでは実際のカレンダーへ接続しません"))
		return
	}
	account, _ := r.Context().Value(googleAccountKey{}).(googleAccount)
	if account.Subject == "" {
		respond(w, nil, errors.New("Googleアカウントでログインし直してください"))
		return
	}
	location, err := s.Calendar.connect(r.Context(), uid(r), account.Subject, account.Email)
	respond(w, map[string]string{"url": location}, err)
}
func (s *Server) calendarCallback(w http.ResponseWriter, r *http.Request) {
	if s.Calendar == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.Calendar.callback(r.Context(), r.URL.Query().Get("state"), r.URL.Query().Get("code")); err != nil {
		http.Redirect(w, r, s.AppURL+"/?calendar=error", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, s.AppURL+"/?calendar=connected", http.StatusSeeOther)
}
func (s *Server) syncCalendar(w http.ResponseWriter, r *http.Request) {
	if s.Calendar == nil {
		respond(w, nil, errors.New("Googleカレンダーが未設定です"))
		return
	}
	st, err := s.Calendar.Sync(r.Context(), uid(r))
	respond(w, st, err)
}
func (s *Server) calendarWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Calendar == nil {
		http.NotFound(w, r)
		return
	}
	id := r.Header.Get("X-Goog-Channel-ID")
	if !tokenIDPattern.MatchString(id) || !tokenIDPattern.MatchString(r.Header.Get("X-Goog-Channel-Token")) {
		w.WriteHeader(403)
		return
	}
	doc, err := s.Calendar.DB.Collection("calendarChannels").Doc(id).Get(r.Context())
	if err != nil {
		w.WriteHeader(403)
		return
	}
	token, _ := doc.DataAt("token")
	expires, _ := doc.DataAt("expires")
	if token != r.Header.Get("X-Goog-Channel-Token") || time.Now().After(expires.(time.Time)) {
		w.WriteHeader(403)
		return
	}
	if r.Header.Get("X-Goog-Resource-State") == "sync" {
		w.WriteHeader(204)
		return
	}
	idValue, _ := doc.DataAt("uid")
	if _, err = s.Calendar.Sync(r.Context(), idValue.(string)); err != nil {
		w.WriteHeader(500)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) renewCalendar(w http.ResponseWriter, r *http.Request) {
	secret := os.Getenv("SCHEDULER_SECRET")
	if s.Calendar == nil || secret == "" || r.Header.Get("Authorization") != "Bearer "+secret {
		w.WriteHeader(403)
		return
	}
	docs, err := s.Calendar.DB.Collection("calendarConnections").Where("expires", "<", time.Now().Add(24*time.Hour)).Documents(r.Context()).GetAll()
	if err != nil {
		respond(w, nil, err)
		return
	}
	for _, doc := range docs {
		var conn Connection
		if err = doc.DataTo(&conn); err != nil {
			break
		}
		var svc *calendar.Service
		svc, err = s.Calendar.service(r.Context(), conn.UID)
		if err != nil {
			break
		}
		if err = s.Calendar.watch(r.Context(), &conn, svc); err != nil {
			break
		}
	}
	respond(w, map[string]bool{"ok": err == nil}, err)
}
