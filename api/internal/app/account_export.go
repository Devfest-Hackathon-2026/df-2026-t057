package app

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) setPause(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Paused *bool `json:"paused"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Paused == nil {
		writeError(w, 400, errors.New("一時停止か再開を指定してください"))
		return
	}
	st, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		if st.Session == nil {
			return errors.New("進行中の課題がありません")
		}
		if (st.Session.PausedAt != nil) == *in.Paused {
			return nil
		}
		now := s.now()
		if *in.Paused {
			st.closeWorkInterval(now)
			st.Session.PausedAt = &now
		} else {
			st.Session.PausedSeconds += int(now.Sub(*st.Session.PausedAt).Seconds())
			st.Session.PausedAt = nil
			st.Session.RunningSince = &now
		}
		return nil
	})
	respond(w, st, err)
}
func csvCell(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0])) || strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") {
		return "'" + value
	}
	return value
}
func (s *Server) exportAccount(w http.ResponseWriter, r *http.Request) {
	st, err := s.Store.Read(r.Context(), uid(r))
	if err != nil {
		respond(w, nil, err)
		return
	}
	store, ok := s.Store.(historyStore)
	if !ok {
		respond(w, nil, errors.New("書き出しに対応していません"))
		return
	}
	history, err := store.History(r.Context(), uid(r))
	if err != nil {
		respond(w, nil, err)
		return
	}
	archived, err := store.Archived(r.Context(), uid(r))
	if err != nil {
		respond(w, nil, err)
		return
	}
	var conversations []AssistantTurn
	if assistant, ok := s.Assistant.(accountAssistantStore); ok {
		conversations, err = assistant.AllHistory(r.Context(), uid(r))
		if err != nil {
			respond(w, nil, err)
			return
		}
	}
	latest, err := s.Store.Read(r.Context(), uid(r))
	if err != nil {
		respond(w, nil, err)
		return
	}
	if latest.Revision != st.Revision {
		respond(w, nil, ErrConflict)
		return
	}
	st.ViewingReceipts = nil
	st.MemoryReceipts = nil
	w.Header().Set("Content-Disposition", `attachment; filename="dopagaki-export.json"`)
	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="dopagaki-tasks.csv"`)
		csvw := csv.NewWriter(w)
		rows := [][]string{{"id", "title", "status", "deadline", "minutes"}}
		for _, t := range append(st.Tasks, archived...) {
			rows = append(rows, []string{csvCell(t.ID), csvCell(t.Title), t.Status, t.Deadline.Format(time.RFC3339), strconv.Itoa(t.Minutes)})
		}
		if err := csvw.WriteAll(rows); err != nil {
			LogFailure("export csv", err)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(map[string]any{"version": 1, "exportedAt": s.now(), "state": st, "archivedTasks": archived, "records": history, "conversations": conversations}); err != nil {
		LogFailure("export json", err)
	}
}
