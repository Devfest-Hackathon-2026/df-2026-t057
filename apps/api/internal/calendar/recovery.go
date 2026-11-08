package calendar

import (
	"context"
	"errors"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/outbox"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
)

type Conflict struct {
	Task       task.Task `json:"task" firestore:"task"`
	Remote     Event     `json:"remote" firestore:"remote"`
	Deleted    bool      `json:"deleted" firestore:"deleted"`
	Resolution string    `json:"resolution" firestore:"resolution"`
	Revision   int64     `json:"revision" firestore:"revision"`
}

const kindResolve outbox.Kind = "calendar.resolve"

func conflictRef(uid, id string) store.Ref { return store.UserItem(uid, "calendarConflicts", id) }
func captureTx(ctx context.Context, tx *store.Tx, uid string, t task.Task, e Event, deleted bool) error {
	rev, err := tx.Revision(ctx, uid, store.ResourceTasks)
	if err != nil {
		return err
	}
	prior, found, err := store.Get[Conflict](ctx, tx, conflictRef(uid, t.ID))
	if err != nil {
		return err
	}
	if found && prior.Revision == rev && prior.Remote.ETag == e.ETag && prior.Deleted == deleted {
		return nil
	}
	if err := tx.Set(conflictRef(uid, t.ID), Conflict{Task: t, Remote: e, Deleted: deleted, Revision: rev}); err != nil {
		return err
	}
	_, err = tx.Touch(ctx, uid, store.ResourceCalendar)
	return err
}
func (s *Service) captureConflict(ctx context.Context, uid string, t task.Task, e Event, deleted bool) error {
	return s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error { return captureTx(ctx, tx, uid, t, e, deleted) })
}
func (s *Service) Conflicts(ctx context.Context, uid string) (items []Conflict, err error) {
	err = s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		items, err = store.List[Conflict](ctx, tx, store.Query{Collection: store.UserCollection(uid, "calendarConflicts")})
		if err != nil {
			return err
		}
		rev, err := tx.Revision(ctx, uid, store.ResourceTasks)
		if err != nil {
			return err
		}
		tasks, err := s.Tasks.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		for i := range items {
			if t, ok := findTask(tasks, items[i].Task.ID); ok {
				items[i].Task = t
				items[i].Revision = rev
			}
		}
		return nil
	})
	return
}
func (s *Service) Resolve(ctx context.Context, uid, id, choice, etag string, revision int64) error {
	if choice != "app" && choice != "google" {
		return apperr.Invalid("request.invalid")
	}
	return s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.Expect(ctx, uid, store.ResourceTasks, revision); err != nil {
			return err
		}
		c, found, err := store.Get[Conflict](ctx, tx, conflictRef(uid, id))
		if err != nil {
			return err
		}
		if !found {
			return apperr.NotFound("not_found")
		}
		if c.Remote.ETag != etag || c.Resolution != "" {
			return apperr.PreconditionFailed("precondition.failed")
		}
		tasks, err := s.Tasks.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		current, ok := findTask(tasks, id)
		if !ok {
			return apperr.NotFound("task.not_found")
		}
		c.Task, c.Revision = current, revision
		c.Resolution = choice
		if err := tx.Set(conflictRef(uid, id), c); err != nil {
			return err
		}
		_, err = s.outbox.Enqueue(ctx, tx, uid, kindResolve, id)
		return err
	})
}
func (s *Service) resolveJob(ctx context.Context, job outbox.Job) error {
	uid, id := job.UID, job.Subject
	var c Connection
	var conflict Conflict
	var found bool
	if err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		if err = store.EnsureAccountOpen(ctx, tx, uid); err != nil {
			return err
		}
		c, err = loadConnection(ctx, tx, uid)
		if err != nil {
			return err
		}
		conflict, found, err = store.Get[Conflict](ctx, tx, conflictRef(uid, id))
		return err
	}); err != nil {
		return err
	}
	if !found || !c.Connected || conflict.Resolution == "" {
		return nil
	}
	client, err := s.Google.Client(ctx, uid)
	if err != nil {
		return err
	}
	eventID := conflict.Remote.ID
	if conflict.Deleted && conflict.Resolution == "app" {
		eventID = shortHash(uid + ":" + id + ":restore:" + conflict.Remote.ETag)
	}
	ev, err := client.GetEvent(ctx, c.CalendarID, eventID)
	gone := errors.Is(err, ErrEventGone)
	if err != nil && !gone {
		return err
	}
	if gone {
		ev = conflict.Remote
		ev.ID = eventID
		ev.Status = EventCancelled
	}
	// Read current task/revision before touching Google. A later concurrent edit remains pending in ContentSeq.
	var current task.Task
	var rev int64
	var loc *time.Location
	if err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		items, err := s.Tasks.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		var ok bool
		current, ok = findTask(items, id)
		if !ok {
			return apperr.NotFound("task.not_found")
		}
		rev, err = tx.Revision(ctx, uid, store.ResourceTasks)
		if err != nil {
			return err
		}
		loc, err = s.Profiles.Location(ctx, tx, uid)
		return err
	}); err != nil {
		return err
	}
	expected := ContentPatch(current, loc).Apply(conflict.Remote)
	alreadyApplied := conflict.Resolution == "app" && !gone && Hash(ev) == Hash(expected) && ev.Description == expected.Description
	if rev != conflict.Revision || (!alreadyApplied && (gone != conflict.Deleted || (!gone && ev.ETag != conflict.Remote.ETag))) {
		return s.captureConflict(ctx, uid, current, ev, gone)
	}
	if conflict.Resolution == "app" && !alreadyApplied {
		if gone {
			next := NewTaskEvent(uid, current, loc)
			next.ID = eventID
			ev, err = client.InsertEvent(ctx, c.CalendarID, next)
		} else {
			ev, err = client.PatchEvent(ctx, c.CalendarID, ev.ID, ev.ETag, ContentPatch(current, loc))
		}
		if errors.Is(err, ErrPreconditionFailed) || errors.Is(err, ErrEventExists) {
			fresh, e := client.GetEvent(ctx, c.CalendarID, eventID)
			if e != nil {
				return e
			}
			return s.captureConflict(ctx, uid, current, fresh, false)
		}
		if err != nil {
			return err
		}
	}
	return s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		saved, ok, err := store.Get[Conflict](ctx, tx, conflictRef(uid, id))
		if err != nil || !ok {
			return err
		}
		if saved.Remote.ETag != conflict.Remote.ETag || saved.Resolution != conflict.Resolution {
			return nil
		}
		// A plan edit during the Google call must be reviewed again.
		currentRev, err := tx.Revision(ctx, uid, store.ResourceTasks)
		if err != nil {
			return err
		}
		if currentRev != rev {
			return captureTx(ctx, tx, uid, current, ev, gone)
		}
		if conflict.Resolution == "google" {
			ch, ok := Classify(ev, s.clock.Now())
			if !ok {
				return apperr.Conflict("calendar.invalid_event")
			}
			if _, err := s.Tasks.ApplyImport(ctx, tx, uid, []task.Imported{ch.Import(id, nil)}, s.clock.Now()); err != nil {
				return err
			}
		}
		conn, err := loadConnection(ctx, tx, uid)
		if err != nil {
			return err
		}
		if conflict.Resolution == "google" {
			if _, err := enqueueOnce(ctx, tx, s.outbox, uid, outbox.KindExport, &conn.ExportJob); err != nil {
				return err
			}
		}
		if gone && conflict.Resolution == "google" {
			if err := tx.Delete(linkRef(uid, id)); err != nil {
				return err
			}
		} else {
			if err := tx.Set(linkRef(uid, id), Link{TaskID: id, EventID: ev.ID, ETag: ev.ETag, InputHash: Hash(ev), ContentSeq: conn.ContentSeq[id]}); err != nil {
				return err
			}
		}
		if err := tx.Delete(conflictRef(uid, id)); err != nil {
			return err
		}
		return saveConnection(ctx, tx, uid, conn)
	})
}
func (s *Service) Retry(ctx context.Context, uid string) (Status, error) {
	if err := s.outbox.Retry(ctx, uid); err != nil {
		return Status{}, err
	}
	return s.SyncNow(ctx, uid)
}
func (s *Service) handle(o *outbox.Outbox, kind outbox.Kind, h outbox.Handler) {
	o.Handle(kind, func(ctx context.Context, j outbox.Job) error {
		err := h(ctx, j)
		if err != nil && apperr.As(err).Code == "calendar.reauth_required" {
			if saveErr := s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
				c, e := loadConnection(ctx, tx, j.UID)
				if e != nil {
					return e
				}
				c.Reauth = true
				return saveConnection(ctx, tx, j.UID, c)
			}); saveErr != nil {
				return saveErr
			}
		}
		return err
	})
}

// Cleanup is only called after the write fence and grace period. Failures are retried before identity deletion.
func (s *Service) Cleanup(ctx context.Context, uid string) error {
	var c Connection
	if err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		c, err = loadConnection(ctx, tx, uid)
		return err
	}); err != nil {
		return err
	}
	if c.ChannelID != "" {
		client, err := s.Google.Client(ctx, uid)
		if err == nil {
			if err = client.StopChannel(ctx, c.ChannelID, c.ChannelResource); err != nil && !errors.Is(err, ErrEventGone) {
				return err
			}
		} else if apperr.As(err).Code != "calendar.reauth_required" {
			return err
		}
	}
	return s.Google.DeleteToken(ctx, uid)
}
