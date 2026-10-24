package main

import (
	"strings"
	"testing"
	"time"
)

// The tests in planned_stay_test.go book stays around late October 2026.
func init() {
	plannedStayNow = func() time.Time { return time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC) }
}

func TestPlannedStay_DepartureAlreadyPastAtThePlaceIsRefused(t *testing.T) {
	store := newTestDocumentStore(t)
	prevNow := plannedStayNow
	plannedStayNow = func() time.Time { return time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC) } // 1 Nov at UTC+10
	t.Cleanup(func() { plannedStayNow = prevNow })

	_, err := store.SetPlannedStay(validStay()) // departs 31 Oct
	var verr *plannedStayError
	if err == nil || !asPlannedStayError(err, &verr) || verr.Field != "depart" {
		t.Fatalf("a departure already past at the place must be refused on depart, got %v", err)
	}
	if _, ok, _ := store.GetPlannedStay(); ok {
		t.Fatal("a refused stay must not be saved")
	}
}

func TestPlannedStayRecord_SharesTheSetValidation(t *testing.T) {
	env := newProposalEnv(t)
	msg := proposeError(t, env.deps, `{"operations":[{"type":"planned_stay","action":"update","id":"stay","fields":{"name":"   ","lat":1,"lon":2,"arrive":"2026-10-27","depart":"2026-10-31"}}]}`)
	if !strings.Contains(msg, "name") {
		t.Fatalf("a blank name must be refused on the Mate path, got %s", msg)
	}
	msg = proposeError(t, env.deps, `{"operations":[{"type":"planned_stay","action":"update","id":"stay","fields":{"name":"X","lat":1,"lon":2,"arrive":"2026-09-01","depart":"2026-10-01"}}]}`)
	if !strings.Contains(msg, "depart") {
		t.Fatalf("a past departure must be refused on the Mate path, got %s", msg)
	}

	p := env.propose(t, `{"operations":[{"type":"planned_stay","action":"update","id":"stay","fields":{"name":"  Coral Coast Marina  ","lat":-27.2,"lon":153.1,"arrive":"2026-10-27","depart":"2026-10-31"}}]}`)
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got, _, _ := env.docs.GetPlannedStay(); got.Name != "Coral Coast Marina" {
		t.Fatalf("the name is trimmed like the Forecast page's save, got %q", got.Name)
	}
}
