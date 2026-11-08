package calendar

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
)

func at(v string) time.Time {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		panic(err)
	}
	return t
}

func TestStripBlockKeepsNotesAroundTheBlock(t *testing.T) {
	cases := []struct{ in, want string }{
		{"本人のメモ\n\n[DOPAGAKI]\n古い予定\n[/DOPAGAKI]\n追加のメモ", "本人のメモ\n\n\n追加のメモ"},
		{"  メモだけ  ", "メモだけ"},
		{"[DOPAGAKI]\n予定\n[/DOPAGAKI]", ""},
		{"閉じていない [DOPAGAKI] ブロック", "閉じていない [DOPAGAKI] ブロック"},
		{"[/DOPAGAKI] 逆順 [DOPAGAKI]", "[/DOPAGAKI] 逆順 [DOPAGAKI]"},
	}
	for _, c := range cases {
		if got := StripBlock(c.in); got != c.want {
			t.Errorf("StripBlock(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHashIgnoresOwnBlockButNotUserEdits(t *testing.T) {
	e := Event{Summary: "課題", Description: "メモ", Start: EventTime{DateTime: "2026-11-06T13:00:00+09:00"}, End: EventTime{DateTime: "2026-11-06T14:00:00+09:00"}}
	h := Hash(e)
	e.Description += "\n\n[DOPAGAKI]\n予定\n[/DOPAGAKI]"
	if Hash(e) != h {
		t.Fatal("own block update changed the hash (sync loop)")
	}
	edited := e
	edited.Summary = "変更された課題"
	if Hash(edited) == h {
		t.Fatal("summary edit did not change the hash")
	}
	edited = e
	edited.Description = "別のメモ\n\n[DOPAGAKI]\n予定\n[/DOPAGAKI]"
	if Hash(edited) == h {
		t.Fatal("notes edit did not change the hash")
	}
	edited = e
	edited.End.DateTime = "2026-11-06T14:30:00+09:00"
	if Hash(edited) == h {
		t.Fatal("time edit did not change the hash")
	}
}

func TestHashTreatsEquivalentTimeZonesAsSameEvent(t *testing.T) {
	e := Event{Summary: "課題", Description: "メモ", Start: EventTime{DateTime: "2026-12-01T12:00:00+09:00"}, End: EventTime{DateTime: "2026-12-01T13:00:00+09:00"}}
	h := Hash(e)
	e.Start.DateTime, e.End.DateTime = "2026-12-01T03:00:00.000Z", "2026-12-01T04:00:00Z"
	if Hash(e) != h {
		t.Fatal("equivalent renderings changed the hash")
	}
}

func sampleTask(end time.Time, minutes int) task.Task {
	tk := task.Task{ID: "t1", Title: "変更後", Notes: "新しいメモ", Deadline: end, Minutes: minutes, Status: task.StatusPending}
	tk.Plan = task.BackwardPlan(tk, task.ReasonBackward)
	return tk
}

func TestRenderDescription(t *testing.T) {
	end := at("2026-12-01T05:00:00Z")
	tk := sampleTask(end, 45)
	tk.Plan.Steps = []task.Step{
		{Title: "調べる", Start: end.Add(-45 * time.Minute), End: end.Add(-15 * time.Minute)},
		{Title: "書く", Start: end.Add(-15 * time.Minute), End: end},
	}
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	want := "メモ\n\n[DOPAGAKI]\n13:15–13:45 調べる\n13:45–14:00 書く\n締切: 12/01 14:00\n[/DOPAGAKI]"
	if got := RenderDescription("メモ", tk, tokyo); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if got := RenderDescription("", tk, tokyo); !strings.HasPrefix(got, "[DOPAGAKI]") {
		t.Fatalf("empty notes should start with the block: %q", got)
	}
}

func TestContentPatchAndPlanOnlyPatch(t *testing.T) {
	end := at("2026-12-01T05:00:00Z")
	tk := sampleTask(end, 45)
	ev := Event{Summary: "変更前", Description: "Google側のメモ", Start: EventTime{DateTime: end.Add(-30 * time.Minute).Format(time.RFC3339)}, End: EventTime{DateTime: end.Format(time.RFC3339)}}

	planOnly := PlanPatch(tk, ev, time.UTC)
	if planOnly.Summary != nil || planOnly.Start != nil || planOnly.End != nil || StripBlock(planOnly.Description) != "Google側のメモ" {
		t.Fatalf("plan edit changed original event fields: %+v", planOnly)
	}
	if Hash(planOnly.Apply(ev)) != Hash(ev) {
		t.Fatal("plan-only patch changed the content hash")
	}

	content := ContentPatch(tk, time.UTC)
	if *content.Summary != tk.Title || StripBlock(content.Description) != tk.Notes ||
		content.Start.DateTime != tk.Plan.Start.Format(time.RFC3339) || content.End.DateTime != end.Format(time.RFC3339) {
		t.Fatalf("content patch did not update the event: %+v", content)
	}
	updated := content.Apply(ev)
	if Hash(updated) == Hash(ev) {
		t.Fatal("content change hidden from the hash")
	}
	// 書き込んだ後に再試行しても、ブロックが重複せず、変更なしと判定される。
	if retry := PlanPatch(tk, updated, time.UTC); retry.Description != content.Description || retry.Changes(updated) {
		t.Fatal("retry duplicated the plan text")
	}
	if ContentPatch(tk, time.UTC).Changes(updated) {
		t.Fatal("content patch should be a no-op after it was applied")
	}
	// 同じ時刻の別の表記は変更とみなさない。
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	rendered := updated
	rendered.Start.DateTime = tk.Plan.Start.In(tokyo).Format(time.RFC3339)
	if ContentPatch(tk, time.UTC).Changes(rendered) {
		t.Fatal("equivalent time rendering treated as a change")
	}
}

func TestExternallyEdited(t *testing.T) {
	base := Event{Summary: "元の課題", Description: "元のメモ", ETag: "v1", Start: EventTime{DateTime: "2026-12-01T04:30:00Z"}, End: EventTime{DateTime: "2026-12-01T05:00:00Z"}}
	link := Link{ETag: "v1", InputHash: Hash(base)}
	edited := base
	edited.ETag, edited.Summary = "v2", "Googleで変更"
	ownBlock := base
	ownBlock.ETag, ownBlock.Description = "v2", "元のメモ\n\n[DOPAGAKI]\nx\n[/DOPAGAKI]"
	sameVersion := edited
	sameVersion.ETag = "v1"
	cases := []struct {
		name string
		link Link
		ev   Event
		want bool
	}{
		{"unchanged", link, base, false},
		{"content edited", link, edited, true},
		{"only our block changed", link, ownBlock, false},
		{"same etag", link, sameVersion, false},
		{"no etag recorded", Link{InputHash: link.InputHash}, edited, false},
		{"no hash recorded", Link{ETag: "v1"}, edited, false},
	}
	for _, c := range cases {
		if got := ExternallyEdited(c.link, c.ev); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestClassify(t *testing.T) {
	now := at("2026-12-01T00:00:00Z")
	timed := func(start, end string) Event {
		return Event{ID: "e", ETag: "1", Summary: " 課題 ", Description: "メモ\n\n[DOPAGAKI]\nx\n[/DOPAGAKI]", Start: EventTime{DateTime: start}, End: EventTime{DateTime: end}}
	}
	ok := timed("2026-12-01T09:00:00+09:00", "2026-12-01T10:30:00+09:00")
	allDay := Event{ID: "all-day", Start: EventTime{Date: "2026-12-01"}, End: EventTime{Date: "2026-12-02"}}
	recurring := ok
	recurring.Recurrence = []string{"RRULE:FREQ=DAILY"}
	instance := ok
	instance.RecurringEventID = "parent"
	cancelled := Event{ID: "gone", Status: EventCancelled, Private: map[string]string{PropTaskID: "t9"}}
	cases := []struct {
		name string
		ev   Event
		ok   bool
	}{
		{"timed", ok, true},
		{"all-day", allDay, false},
		{"recurring", recurring, false},
		{"recurring instance", instance, false},
		{"cancelled", cancelled, true},
		{"ended", timed("2026-11-30T08:00:00Z", "2026-11-30T09:00:00Z"), false},
		{"ends exactly now", timed("2026-11-30T23:00:00Z", "2026-12-01T00:00:00Z"), true},
		{"zero length", timed("2026-12-01T09:00:00Z", "2026-12-01T09:00:00Z"), false},
		{"720 minutes", timed("2026-12-01T00:00:00Z", "2026-12-01T12:00:00Z"), true},
		{"721 minutes", timed("2026-12-01T00:00:00Z", "2026-12-01T12:01:00Z"), false},
		{"unparseable", timed("tomorrow", "2026-12-01T12:00:00Z"), false},
	}
	for _, c := range cases {
		if _, got := Classify(c.ev, now); got != c.ok {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
	got, _ := Classify(ok, now)
	if got.Minutes != 90 || !got.Deadline.Equal(at("2026-12-01T01:30:00Z")) || got.Notes != "メモ" || got.Hash != Hash(ok) {
		t.Fatalf("unexpected change: %+v", got)
	}
	gone, _ := Classify(cancelled, now)
	if !gone.Cancelled || gone.TaskIDHint != "t9" {
		t.Fatalf("cancelled: %+v", gone)
	}
	imported := got.Import("x", nil)
	if imported.Title != " 課題 " || imported.Removed || gone.Import("x", nil).Removed != true {
		t.Fatal("import conversion")
	}
}

func TestTaskIDFor(t *testing.T) {
	eventTask := map[string]string{"linked-event": "t1"}
	taskEvent := map[string]string{"t1": "linked-event"}
	cases := []struct {
		name string
		c    EventChange
		want string
	}{
		{"linked", EventChange{EventID: "linked-event"}, "t1"},
		{"hint", EventChange{EventID: "e2", TaskIDHint: "t2"}, "t2"},
		{"hint linked to another event (duplicated)", EventChange{EventID: "copy", TaskIDHint: "t1"}, ImportedTaskID("copy")},
		{"manual event", EventChange{EventID: "manual"}, ImportedTaskID("manual")},
	}
	for _, c := range cases {
		if got := TaskIDFor(c.c, eventTask, taskEvent); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
	if !strings.HasPrefix(ImportedTaskID("m"), "calendar-") || len(ImportedTaskID("m")) != len("calendar-")+32 {
		t.Fatal("imported id format")
	}
}

func TestIdentifiers(t *testing.T) {
	sum := sha256.Sum256([]byte("u1:t1"))
	if EventID("u1", "t1") != hex.EncodeToString(sum[:16]) || EventID("u1", "t1") == EventID("u2", "t1") {
		t.Fatal("event id must be hex(sha256(uid:taskId)[:16])")
	}
	sum = sha256.Sum256([]byte("u1:sleep"))
	if SleepEventID("u1") != hex.EncodeToString(sum[:16]) {
		t.Fatal("sleep event id must be hex(sha256(uid:sleep)[:16])")
	}
	for v, want := range map[string]bool{
		strings.Repeat("a", 32): true, strings.Repeat("0", 32): true,
		strings.Repeat("A", 32): false, strings.Repeat("a", 31): false, strings.Repeat("g", 32): false, "": false,
	} {
		if ValidToken(v) != want {
			t.Errorf("ValidToken(%q) != %v", v, want)
		}
	}
}

func TestDeriveSync(t *testing.T) {
	cases := []struct {
		connected       bool
		pending, failed int
		want            SyncState
	}{
		{false, 0, 0, SyncDisconnected},
		{false, 2, 1, SyncDisconnected},
		{true, 0, 0, SyncSynced},
		{true, 1, 0, SyncPending},
		{true, 1, 3, SyncPending},
		{true, 0, 3, SyncFailed},
	}
	for _, c := range cases {
		if got := DeriveSync(c.connected, c.pending, c.failed); got != c.want {
			t.Errorf("%+v: got %s", c, got)
		}
	}
}

func TestLatestByID(t *testing.T) {
	got := LatestByID([]Event{{ID: "a", ETag: "1"}, {ID: "b"}, {ID: "a", ETag: "2"}})
	if len(got) != 2 || got[0].ID != "b" || got[1].ETag != "2" {
		t.Fatalf("got %+v", got)
	}
}

func TestSameContent(t *testing.T) {
	end := at("2026-12-01T05:00:00Z")
	tk := task.Task{Title: "課題", Notes: "メモ", Deadline: end, Minutes: 30}
	c := EventChange{Title: " 課題 ", Notes: "メモ", Deadline: end, Minutes: 30}
	if !SameContent(tk, c) {
		t.Fatal("same content not detected")
	}
	c.Minutes = 31
	if SameContent(tk, c) {
		t.Fatal("different minutes treated as same")
	}
}

func TestSleepEvent(t *testing.T) {
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	now := at("2026-12-01T03:00:00Z") // 12:00 JST
	ev := SleepEvent("u1", sleep.Schedule{Bedtime: "23:30", Wake: "07:00"}, now, tokyo)
	if ev.ID != SleepEventID("u1") || ev.Start.DateTime != "2026-12-01T23:30:00+09:00" || ev.End.DateTime != "2026-12-02T07:00:00+09:00" {
		t.Fatalf("times: %+v %+v", ev.Start, ev.End)
	}
	if ev.Start.TimeZone != "Asia/Tokyo" || !ev.Transparent || ev.Status != EventConfirmed || len(ev.Recurrence) != 1 || ev.Private[PropSleep] != "1" {
		t.Fatalf("event: %+v", ev)
	}
	if !strings.Contains(ev.Description, "23:30") || !strings.Contains(ev.Description, "07:00") {
		t.Fatalf("description: %q", ev.Description)
	}
	// 正午より前の就寝は、前の晩の夜として翌日に置く。
	early := SleepEvent("u1", sleep.Schedule{Bedtime: "01:00", Wake: "08:00"}, now, tokyo)
	if early.Start.DateTime != "2026-12-02T01:00:00+09:00" {
		t.Fatalf("early bedtime: %s", early.Start.DateTime)
	}
	if _, ok := Classify(ev, now); ok {
		t.Fatal("the sleep event must never be imported as a task")
	}
}
