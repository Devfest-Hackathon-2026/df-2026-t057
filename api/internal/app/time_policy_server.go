package app

import (
	"errors"
	"net/http"
)

func (s *Server) timePolicy(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Mode              string `json:"mode"`
		DailyLimitSeconds int    `json:"dailyLimitSeconds"`
	}
	if !decode(w, r, &in) {
		return
	}
	if (in.Mode != "rewards" && in.Mode != "fixed" && in.Mode != "reminder" && in.Mode != "self") ||
		(in.DailyLimitSeconds != 600 && in.DailyLimitSeconds != 1200 && in.DailyLimitSeconds != 1800) {
		respond(w, nil, errors.New("視聴時間の設定を確認してください"))
		return
	}
	st, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		if st.Session != nil {
			return errors.New("作業を記録してから視聴時間の設定を変更してください")
		}
		if st.TimePolicy == nil {
			st.TimePolicy = newTimePolicy(s.now(), st.Profile.TimeZone)
		}
		st.TimePolicy.Mode, st.TimePolicy.DailyLimitSeconds = in.Mode, in.DailyLimitSeconds
		return nil
	})
	respond(w, st, err)
}

func (s *Server) confirmSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &in) {
		return
	}
	st, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		if in.ID == "" {
			return errors.New("記録する作業を確認してください")
		}
		for _, entry := range st.Ledger {
			if entry.ID == "work:"+in.ID {
				return nil
			}
		}
		if st.TimePolicy == nil || st.Session == nil || st.Session.ID != in.ID {
			return ErrConflict
		}
		st.confirmWork(s.now())
		t, err := st.task(st.Session.TaskID)
		if err != nil {
			return err
		}
		t.Status = "pending"
		st.Session = nil
		st.PlanRevision++
		prepareDraft(st, s.now())
		return nil
	})
	respond(w, st, err)
}

func (s *Server) beginViewing(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID         string `json:"id"`
		Seconds    int    `json:"seconds"`
		ExtendFrom string `json:"extendFrom"`
	}
	if !decode(w, r, &in) {
		return
	}
	st, err := s.Store.Update(r.Context(), uid(r), func(st *State) error { return st.startViewing(in.ID, in.Seconds, in.ExtendFrom, s.now()) })
	respond(w, st, err)
}

func (s *Server) finishViewing(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Reason != "stop" && in.Reason != "limit" {
		respond(w, nil, errors.New("終了方法を確認してください"))
		return
	}
	st, err := s.Store.Update(r.Context(), uid(r), func(st *State) error { return st.endViewing(in.ID, in.Reason, s.now()) })
	respond(w, st, err)
}
