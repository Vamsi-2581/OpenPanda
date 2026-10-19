// SPDX-License-Identifier: AGPL-3.0-or-later

package panel

// The profile page's calendar is arithmetic over three stores, and every
// number on it can be wrong in a way that is invisible on screen: a turn
// counted on the wrong day, a peak hour that moves between reloads, a
// "favorite model" that is really a ledger label. These tests pin the
// arithmetic down without a database or a browser.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Xustalis/OpenPanda/internal/core"
	"github.com/Xustalis/OpenPanda/internal/sessions"
)

// at parses a local wall-clock stamp, so a test's expectations hold in every
// timezone the suite runs in.
func at(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return tm
}

func TestAggregateUsageFoldsSessionsTasksAndTokens(t *testing.T) {
	stamped := []*sessions.Session{
		{
			CreatedAt: at(t, "2026-10-16 09:10"),
			UpdatedAt: at(t, "2026-10-17 22:05"),
			Turns: []sessions.Turn{
				{Role: "user", Text: "a", TS: at(t, "2026-10-16 09:10").Unix()},
				{Role: "assistant", Text: "b", TS: at(t, "2026-10-16 09:12").Unix()},
				// Same thread, next day: the turn stamp is what puts this
				// message on the 17th instead of the thread's start day.
				{Role: "user", Text: "c", TS: at(t, "2026-10-17 22:00").Unix()},
			},
		},
		{
			// A transcript written before Turn.TS existed: the user turn
			// falls back to the session's start, the answer to its last
			// update.
			CreatedAt: at(t, "2026-10-17 08:00"),
			UpdatedAt: at(t, "2026-10-17 08:30"),
			Turns: []sessions.Turn{
				{Role: "user", Text: "d"},
				{Role: "assistant", Text: "e"},
			},
		},
	}
	tasks := []core.TaskDayHour{{Day: "2026-10-17", Hour: 22, Count: 2}}
	metrics := []core.DelegationMetric{
		{
			Executor:  "entry:glm-5.3-flash",
			Tokens:    sql.NullInt64{Int64: 1000, Valid: true},
			CreatedAt: at(t, "2026-10-17 22:01").Unix(),
		},
		{
			Executor:  "claude_code",
			Tokens:    sql.NullInt64{Int64: 400, Valid: true},
			CreatedAt: at(t, "2026-10-17 22:02").Unix(),
		},
		{
			// A provider that reported no usage records a null token cell;
			// it must not create a phantom day of activity.
			Executor:  "silent",
			Tokens:    sql.NullInt64{Valid: false},
			CreatedAt: at(t, "2026-10-20 10:00").Unix(),
		},
	}

	got := aggregateUsage(stamped, tasks, metrics, "fallback-model")

	if got.Sessions != 2 || got.Messages != 5 || got.Tasks != 2 || got.Tokens != 1400 {
		t.Fatalf("totals = sessions %d messages %d tasks %d tokens %d, want 2/5/2/1400",
			got.Sessions, got.Messages, got.Tasks, got.Tokens)
	}
	if len(got.Daily) != 2 {
		t.Fatalf("daily = %d days, want 2: %+v", len(got.Daily), got.Daily)
	}
	first, second := got.Daily[0], got.Daily[1]
	if first.Date != "2026-10-16" || first.Sessions != 1 || first.Messages != 2 || first.Tasks != 0 {
		t.Fatalf("day one = %+v, want 2026-10-16 with 1 session / 2 messages", first)
	}
	if second.Date != "2026-10-17" || second.Sessions != 1 || second.Messages != 3 ||
		second.Tasks != 2 || second.Tokens != 1400 {
		t.Fatalf("day two = %+v, want 2026-10-17 with 1 session / 3 messages / 2 tasks / 1400 tokens", second)
	}
	if got.ActiveDays != 2 || got.TotalDays != 2 {
		t.Fatalf("active/total days = %d/%d, want 2/2", got.ActiveDays, got.TotalDays)
	}
	if got.FirstDay != "2026-10-16" || got.LastDay != "2026-10-17" {
		t.Fatalf("span = %s..%s, want 2026-10-16..2026-10-17", got.FirstDay, got.LastDay)
	}
	// Hours 08, 09 and 22 each hold three events; the tie goes to the
	// earliest hour so two identical requests cannot disagree.
	if got.PeakHour == nil || *got.PeakHour != 8 {
		t.Fatalf("peak hour = %v, want 8", got.PeakHour)
	}
	// "entry:" is ledger bookkeeping, not a model name.
	if got.FavoriteModel != "glm-5.3-flash" {
		t.Fatalf("favorite model = %q, want glm-5.3-flash", got.FavoriteModel)
	}
}

func TestAggregateUsageEmptyFallsBackToConfiguredModel(t *testing.T) {
	got := aggregateUsage(nil, nil, nil, "configured")
	if got.ActiveDays != 0 || got.TotalDays != 0 || len(got.Daily) != 0 {
		t.Fatalf("empty inputs produced activity: %+v", got)
	}
	if got.PeakHour != nil {
		t.Fatalf("peak hour = %v, want nil with no activity", *got.PeakHour)
	}
	// The card still has something to say: the model this node is set to
	// use, even before its first call has been billed.
	if got.FavoriteModel != "configured" {
		t.Fatalf("favorite model = %q, want the configured fallback", got.FavoriteModel)
	}
}

func TestFavoriteModelBreaksTiesByName(t *testing.T) {
	got := favoriteModel(map[string]int64{"b-model": 10, "a-model": 10})
	if got != "a-model" {
		t.Fatalf("favorite model = %q, want the stable a-model", got)
	}
	if favoriteModel(nil) != "" {
		t.Fatalf("expected empty favorite for an empty ledger")
	}
}

// TestStatsEndpointServesActivity walks the real handler: a session with one
// turn and one queued task must both land on today's cell.
func TestStatsEndpointServesActivity(t *testing.T) {
	store := newTestStore(t)
	sessStore := sessions.NewStore(t.TempDir())
	sess, err := sessStore.Create("hello")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := sessStore.AppendTurn(sess.ID, sessions.Turn{Role: "user", Text: "hi"}); err != nil {
		t.Fatalf("append turn: %v", err)
	}
	if _, err := store.Create(context.Background(), "", "proj", "queued", "node", []string{"node"}); err != nil {
		t.Fatalf("create task: %v", err)
	}

	h := New(Deps{Store: store, Sessions: sessStore, Token: testToken})
	code, out := doJSON(t, h, authedReq(http.MethodGet, "/api/stats", nil))
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if n, _ := out["sessions"].(float64); n != 1 {
		t.Fatalf("sessions = %v, want 1", out["sessions"])
	}
	if n, _ := out["messages"].(float64); n != 1 {
		t.Fatalf("messages = %v, want 1", out["messages"])
	}
	if n, _ := out["tasks"].(float64); n != 1 {
		t.Fatalf("tasks = %v, want 1", out["tasks"])
	}
	// AppendTurn stamps the turn, so today is the day both events land on.
	today := time.Now().Local().Format(dayLayout)
	daily, _ := out["daily"].([]any)
	if len(daily) != 1 {
		t.Fatalf("daily = %v, want exactly today", out["daily"])
	}
	day, _ := daily[0].(map[string]any)
	if day["date"] != today {
		t.Fatalf("day = %v, want %s", day["date"], today)
	}
	if _, ok := day["tokens"]; !ok {
		t.Fatalf("day is missing the tokens field: %v", day)
	}
}

// TestAppendTurnStampsTurns guards the one write-path the calendar depends
// on: a turn that arrives without a timestamp gets one, and a caller that
// supplies its own keeps it (replayed transcripts must not be re-dated).
func TestAppendTurnStampsTurns(t *testing.T) {
	store := sessions.NewStore(t.TempDir())
	sess, err := store.Create("t")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	before := time.Now().Unix()
	got, err := store.AppendTurn(sess.ID, sessions.Turn{Role: "user", Text: "a"})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if got.Turns[0].TS < before {
		t.Fatalf("turn ts = %d, want >= %d", got.Turns[0].TS, before)
	}

	// Round-trip through the file so the field is proven to persist, not just
	// to live in memory.
	reloaded, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if reloaded.Turns[0].TS != got.Turns[0].TS {
		t.Fatalf("ts did not persist: %d != %d", reloaded.Turns[0].TS, got.Turns[0].TS)
	}
	raw, err := json.Marshal(reloaded.Turns[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) == "" || !json.Valid(raw) {
		t.Fatalf("turn does not marshal: %s", raw)
	}

	fixed := int64(1_700_000_000)
	kept, err := store.AppendTurn(sess.ID, sessions.Turn{Role: "user", Text: "b", TS: fixed})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if kept.Turns[1].TS != fixed {
		t.Fatalf("caller-supplied ts = %d, want %d", kept.Turns[1].TS, fixed)
	}
}
