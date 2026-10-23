package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestAlarmKindIgnorable(t *testing.T) {
	cases := []struct {
		kind, state string
		want        bool
	}{
		// Plotter navigation alarms and ordinary device alarms can be ignored.
		{"perpendicularPassed", alarmStateAlarm, true},
		{"arrivalCircleEntered", alarmStateWarn, true},
		{"navigation.arrivalCircleEntered", alarmStateAlarm, true},
		{"electrical.batteries.0.voltage.high", alarmStateAlarm, true},
		{"navigation.anchoring", alarmStateAlarm, true}, // not the anchor alarm

		// The SignalK emergency kinds.
		{"mob", alarmStateAlarm, false},
		{"fire", alarmStateAlarm, false},
		{"sinking", alarmStateAlarm, false},
		{"flooding", alarmStateAlarm, false},
		{"collision", alarmStateAlarm, false},
		{"grounding", alarmStateAlarm, false},
		{"listing", alarmStateAlarm, false},
		{"adrift", alarmStateAlarm, false},
		{"piracy", alarmStateAlarm, false},
		{"abandon", alarmStateAlarm, false},
		{"fire.engineRoom", alarmStateAlarm, false},

		{"navigation.anchor", alarmStateAlarm, false},
		{"navigation.anchor.dragging", alarmStateAlarm, false},
		{"navigation.closestApproach", alarmStateAlarm, false},
		{"received.distress.ais-123456789", alarmStateAlarm, false},
		{"received.distress.dsc-123-1700000000", alarmStateAlarm, false},
		{"radar.furuno1.guardZone.1", alarmStateAlarm, false},

		// Emergency state overrides the kind.
		{"perpendicularPassed", alarmStateEmergency, false},
		{"", alarmStateAlarm, false},
	}
	for _, c := range cases {
		if got := alarmKindIgnorable(c.kind, c.state); got != c.want {
			t.Errorf("alarmKindIgnorable(%q, %q) = %v, want %v", c.kind, c.state, got, c.want)
		}
	}
}

func TestSignalKNotificationsHidesAnIgnoredKindAndShowsItAgainOnRestoreOrEmergency(t *testing.T) {
	store := withTempIgnoreStore(t)
	snapshot := snapshotWithNotification("notifications.perpendicularPassed", map[string]any{
		"state": "alarm", "message": "Perpendicular passed", "method": []any{"visual", "sound"},
	})

	statuses := signalKNotifications(snapshot, ownsNothing, alarmNow)
	if len(statuses) != 1 || !statuses[0].Ignorable {
		t.Fatalf("before ignoring: want one ignorable alarm, got %+v", statuses)
	}

	if err := store.add(ignoredAlarmsList, "perpendicularPassed", alarmNow); err != nil {
		t.Fatal(err)
	}
	if got := signalKNotifications(snapshot, ownsNothing, alarmNow); len(got) != 0 {
		t.Fatalf("ignored kind must be hidden, got %+v", got)
	}

	emergency := snapshotWithNotification("notifications.perpendicularPassed", map[string]any{
		"state": "emergency", "message": "Perpendicular passed",
	})
	got := signalKNotifications(emergency, ownsNothing, alarmNow)
	if len(got) != 1 || got[0].Ignorable {
		t.Fatalf("emergency must show and not be ignorable, got %+v", got)
	}

	if err := store.remove(ignoredAlarmsList, "perpendicularPassed"); err != nil {
		t.Fatal(err)
	}
	if got := signalKNotifications(snapshot, ownsNothing, alarmNow); len(got) != 1 {
		t.Fatalf("restored kind must show, got %+v", got)
	}
}

func TestSignalKNotificationsNeverHidesASafetyKindEvenIfListed(t *testing.T) {
	store := withTempIgnoreStore(t)
	if err := store.add(ignoredAlarmsList, "navigation.anchor", alarmNow); err != nil {
		t.Fatal(err)
	}
	snapshot := snapshotWithNotification("notifications.navigation.anchor", map[string]any{"state": "alarm", "message": "Dragging"})
	got := signalKNotifications(snapshot, ownsNothing, alarmNow)
	if len(got) != 1 || got[0].Ignorable {
		t.Fatalf("got %+v", got)
	}
}

func TestIgnoreAlarmHandlers(t *testing.T) {
	withTempIgnoreStore(t)
	e := echo.New()
	e.GET("/api/alarms/ignored-alarms", listIgnoredAlarmsHandler)
	e.POST("/api/alarms/ignored-alarms", ignoreAlarmHandler)
	e.DELETE("/api/alarms/ignored-alarms/:kind", unignoreAlarmHandler)

	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/alarms/ignored-alarms", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		e.ServeHTTP(rec, req)
		return rec
	}

	for i := 0; i < 2; i++ { // idempotent
		rec := post(`{"kind":"perpendicularPassed"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("post %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	var body struct {
		Alarms []ignoredAlarmEntry `json:"alarms"`
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/alarms/ignored-alarms", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Alarms) != 1 || body.Alarms[0].Kind != "perpendicularPassed" || body.Alarms[0].IgnoredAt == "" || body.Alarms[0].Title == "" {
		t.Fatalf("list: %+v", body)
	}

	for _, kind := range []string{"navigation.anchor", "mob", "radar.x.guardZone.1", "received.distress.ais-1"} {
		if rec := post(`{"kind":"` + kind + `"}`); rec.Code != http.StatusBadRequest {
			t.Errorf("safety kind %q: got %d, want 400", kind, rec.Code)
		}
	}
	if rec := post(`{"kind":"  "}`); rec.Code != http.StatusBadRequest {
		t.Errorf("blank: got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/alarms/ignored-alarms/navigation.arrivalCircleEntered", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete of an unknown kind is idempotent: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/alarms/ignored-alarms/perpendicularPassed", nil))
	if rec.Code != http.StatusNoContent || len(globalIgnoreStore.entries(ignoredAlarmsList)) != 0 {
		t.Fatalf("restore: %d", rec.Code)
	}
}

// Ignoring a raised alarm takes it off the list, which the watcher reports like
// any other departure: an "over" message and a closed log row. A restore while
// the condition is still live raises it afresh.
func TestBusWatcherIgnoreClearsAndRestoreRaisesAgain(t *testing.T) {
	store := withTempIgnoreStore(t)
	snapshot := snapshotWithNotification("notifications.perpendicularPassed", busTestNotification(alarmStateAlarm, "Perpendicular passed"))
	watcher := newBusNotificationWatcher(snapshot)
	watcher.dwell = 5 * time.Second

	watcher.check(alarmNow)
	if ev := watcher.check(alarmNow.Add(6 * time.Second)); len(ev) != 1 || ev[0].Kind != alarmEventRaised {
		t.Fatalf("setup: %+v", ev)
	}

	if err := store.add(ignoredAlarmsList, "perpendicularPassed", alarmNow); err != nil {
		t.Fatal(err)
	}
	if ev := watcher.check(alarmNow.Add(7 * time.Second)); len(ev) != 1 || ev[0].Kind != alarmEventCleared {
		t.Fatalf("ignoring must send the normal cleared event, got %+v", ev)
	}
	if ev := watcher.check(alarmNow.Add(60 * time.Second)); len(ev) != 0 {
		t.Fatalf("an ignored kind must stay silent, got %+v", ev)
	}

	if err := store.remove(ignoredAlarmsList, "perpendicularPassed"); err != nil {
		t.Fatal(err)
	}
	restoredAt := alarmNow.Add(120 * time.Second)
	if ev := watcher.check(restoredAt); len(ev) != 0 {
		t.Fatalf("a restored alarm serves the dwell first, got %+v", ev)
	}
	ev := watcher.check(restoredAt.Add(6 * time.Second))
	if len(ev) != 1 || ev[0].Kind != alarmEventRaised {
		t.Fatalf("restore must raise afresh, got %+v", ev)
	}
}

// An ignored kind that arrives in emergency shows, and when it genuinely
// clears the crew is told.
func TestBusWatcherEmergencyOfIgnoredKindClearsNormally(t *testing.T) {
	store := withTempIgnoreStore(t)
	if err := store.add(ignoredAlarmsList, "perpendicularPassed", alarmNow); err != nil {
		t.Fatal(err)
	}
	snapshot := snapshotWithNotification("notifications.perpendicularPassed", busTestNotification(alarmStateEmergency, "Perpendicular passed"))
	watcher := newBusNotificationWatcher(snapshot)
	watcher.dwell = 5 * time.Second

	watcher.check(alarmNow)
	if ev := watcher.check(alarmNow.Add(6 * time.Second)); len(ev) != 1 || ev[0].Kind != alarmEventRaised {
		t.Fatalf("emergency must raise despite the ignore, got %+v", ev)
	}
	gone := newSignalKSnapshot()
	gone.setSelfContext("vessels.self")
	watcher.snapshot = gone
	if ev := watcher.check(alarmNow.Add(8 * time.Second)); len(ev) != 1 || ev[0].Kind != alarmEventCleared {
		t.Fatalf("a genuine clear must be dispatched, got %+v", ev)
	}
}

func TestIgnoreAlarmHandlerNormalisesAPrefixedKind(t *testing.T) {
	withTempIgnoreStore(t)
	e := echo.New()
	e.POST("/i", ignoreAlarmHandler)
	e.DELETE("/i/:kind", unignoreAlarmHandler)

	post := func(kind string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/i", strings.NewReader(`{"kind":"`+kind+`"}`))
		req.Header.Set("Content-Type", "application/json")
		e.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post("notifications.perpendicularPassed"); code != http.StatusOK {
		t.Fatalf("got %d", code)
	}
	got := globalIgnoreStore.entries(ignoredAlarmsList)
	if len(got) != 1 || got[0].Key != "perpendicularPassed" {
		t.Fatalf("stored %+v", got)
	}
	if code := post("notifications.navigation.anchor"); code != http.StatusBadRequest {
		t.Fatalf("a prefixed safety kind must be refused, got %d", code)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/i/notifications.perpendicularPassed", nil))
	if len(globalIgnoreStore.entries(ignoredAlarmsList)) != 0 {
		t.Fatal("DELETE must accept the prefixed kind")
	}
}

func TestActiveAlarmsResultListsIgnoredTitlesForMate(t *testing.T) {
	store := withTempIgnoreStore(t)
	if err := store.add(ignoredAlarmsList, "perpendicularPassed", alarmNow); err != nil {
		t.Fatal(err)
	}
	deps := assistantToolDeps{activeAlarms: func() []alarmStatus { return nil }}
	out, err := deps.execute(context.Background(), "get_active_alarms", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Ignored []string `json:"ignored_alarms"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Ignored) != 1 || got.Ignored[0] != "Waypoint Passed" {
		t.Fatalf("got %s", out)
	}
}
