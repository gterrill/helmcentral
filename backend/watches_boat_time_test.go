package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Boat time on every run and one-off watch (ADR 0227): the Watches page never
// shows the time on the device it is read on. A run carries the boat's clock
// as it was when the run was made, or the UTC time said to be UTC.

func TestWatchRun_ARunCarriesTheBoatClockItWasMadeOn(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Hourly", "0 * * * *")
	f.host.boat = func() (*time.Location, error) { return mustZoneAt(-10, 175).Loc, nil } // UTC+12 offshore
	f.host.tick(context.Background())
	runs := f.runsOf("id-Hourly")
	if len(runs) != 1 {
		t.Fatalf("runs %+v", runs)
	}
	if runs[0].BoatTime != "Thu 29 Oct 22:00 UTC+12" || runs[0].Timezone != "UTC+12" {
		t.Fatalf("boat time %q zone %q", runs[0].BoatTime, runs[0].Timezone)
	}
	raw, _ := json.Marshal(runs[0])
	if !strings.Contains(string(raw), `"boat_time":"Thu 29 Oct 22:00 UTC+12"`) || !strings.Contains(string(raw), `"timezone":"UTC+12"`) {
		t.Fatalf("the API returns both: %s", raw)
	}
}

func TestWatchRun_ASkipForWantOfPositionSaysItIsUTCAndNotBoatTime(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Hourly", "0 * * * *")
	f.zoneErr = errors.New("no fix")
	f.host.tick(context.Background())
	runs := f.runsOf("id-Hourly")
	if len(runs) != 1 || runs[0].Outcome != watchOutcomeSkipped {
		t.Fatalf("runs %+v", runs)
	}
	if runs[0].BoatTime != "Thu 29 Oct 10:00 UTC (boat time not known)" || runs[0].Timezone != "" {
		t.Fatalf("boat time %q zone %q", runs[0].BoatTime, runs[0].Timezone)
	}
}

func TestWatchRun_ARunSavedWithNoZoneIsLabelledUTCRatherThanLeftBlank(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Hourly", "0 * * * *")
	f.ran("id-Hourly", watchTestTime(29, 9, 0))
	runs := f.runsOf("id-Hourly")
	if runs[0].BoatTime != "Thu 29 Oct 09:00 UTC (boat time not known)" {
		t.Fatalf("boat time %q", runs[0].BoatTime)
	}
}

func TestOneOffWatch_ListEntryGivesStartAndEndOnTheBoatClock(t *testing.T) {
	f := newOneOffFixture(t)
	if _, err := f.h.reg.start(watchRequest("conv-1", oneOffArgs)); err != nil {
		t.Fatal(err)
	}
	w := f.watches(t)[0]
	var e watchListEntry
	if err := f.docs.Read(func(q sqlQueryer) error {
		var err error
		e, err = watchEntryFor(q, w, time.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if e.StartedBoatTime == "" || e.EndsBoatTime == "" {
		t.Fatalf("entry %+v", e)
	}
	// The registry harness starts the watch on a Brisbane boat (UTC+10, AEST).
	if !strings.HasSuffix(e.StartedBoatTime, "AEST") || !strings.HasSuffix(e.EndsBoatTime, "AEST") {
		t.Fatalf("started %q ends %q", e.StartedBoatTime, e.EndsBoatTime)
	}
	w.Config = json.RawMessage(`{"zone_name":"UTC","offset_seconds":0}`)
	if loc := watchOneOffLocation(w); loc != nil {
		t.Fatalf("a watch begun with no boat position has no zone: %v", loc)
	}
}

func TestWatchRuns_PageByCursorNotOffset(t *testing.T) {
	env := newProposalEnv(t)
	env.applyOK(t, watchCreateArgs)
	w := firstWatch(t, env.docs)
	base := watchTestTime(29, 0, 0)
	seed := func(i int) {
		at := base.Add(time.Duration(i) * time.Hour)
		if err := env.docs.RunTx("seed", func(tx *sql.Tx, _ time.Time) error {
			return insertWatchRunTx(tx, watchRun{WatchID: w.ID, StartedAt: at, FinishedAt: at, Outcome: watchOutcomeRan, CostKnown: true}, true)
		}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		seed(i)
	}
	type page struct {
		Runs     []watchRun `json:"runs"`
		HasOlder bool       `json:"has_older"`
	}
	get := func(query string) page {
		c, rec := watchRequestTo(http.MethodGet, "/api/watches/"+w.ID+query, "", "id", w.ID)
		if err := getWatchHandler(c); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("%s: %v %d %s", query, err, rec.Code, rec.Body.String())
		}
		var p page
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	first := get("?limit=2")
	if len(first.Runs) != 2 || !first.HasOlder {
		t.Fatalf("first %+v", first)
	}
	// A newer run lands between the two page loads: an offset would repeat a run.
	seed(10)
	last := first.Runs[len(first.Runs)-1]
	second := get("?limit=2&before=" + last.StartedAt.Format(time.RFC3339Nano) + "," + last.ID)
	if len(second.Runs) != 2 || !second.HasOlder {
		t.Fatalf("second %+v", second)
	}
	seen := map[string]bool{}
	for _, r := range append(first.Runs, second.Runs...) {
		if seen[r.ID] {
			t.Fatalf("run %s came twice", r.ID)
		}
		seen[r.ID] = true
	}
	last = second.Runs[1]
	third := get("?limit=2&before=" + last.StartedAt.Format(time.RFC3339Nano) + "," + last.ID)
	if len(third.Runs) != 1 || third.HasOlder {
		t.Fatalf("third %+v", third)
	}

	c, rec := watchRequestTo(http.MethodGet, "/api/watches/"+w.ID+"?before=nonsense", "", "id", w.ID)
	_ = getWatchHandler(c)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a malformed cursor is refused: %d", rec.Code)
	}
	c, rec = watchRequestTo(http.MethodGet, "/api/watches/"+w.ID+"?offset=2", "", "id", w.ID)
	_ = getWatchHandler(c)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("offset is gone: %d %s", rec.Code, rec.Body.String())
	}
}

// The saved config keeps the zone's IANA name, and the clock is rebuilt from
// that name, so a label reads "AEDT" and never "Australia/Sydney".
func TestOneOffWatch_ARebuiltClockCarriesTheZonesOwnAbbreviation(t *testing.T) {
	summer := time.Date(2026, 1, 15, 2, 0, 0, 0, time.UTC)
	winter := time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC)
	cases := []struct {
		zone   string
		at     time.Time
		suffix string
	}{
		{"Australia/Sydney", summer, "AEDT"},
		{"Australia/Sydney", winter, "AEST"},
		{"Etc/GMT-10", summer, "UTC+10"}, // tzdata names this zone "+10"
	}
	for _, c := range cases {
		w := savedWatch{Config: json.RawMessage(`{"zone_name":"` + c.zone + `","offset_seconds":36000}`)}
		loc := watchOneOffLocation(w)
		if loc == nil {
			t.Fatalf("%s: no clock", c.zone)
		}
		if got := watchBoatClock(c.at, loc); !strings.HasSuffix(got, " "+c.suffix) {
			t.Errorf("%s at %s: boat clock %q, want suffix %q", c.zone, c.at.Format("Jan"), got, c.suffix)
		}
		if got := watchBoatZone(c.at, loc); got != c.suffix {
			t.Errorf("%s at %s: zone %q, want %q", c.zone, c.at.Format("Jan"), got, c.suffix)
		}
		started, ends := c.at, c.at.Add(time.Hour)
		info, err := oneOffInfo(savedWatch{ID: "w1", StartedAt: &started, EndsAt: &ends},
			watchOneOffConfig{Today: "2026-01-15", ZoneName: c.zone, OffsetSeconds: 36000})
		if err != nil {
			t.Fatalf("%s: %v", c.zone, err)
		}
		if got := zoneAbbreviation(c.at, info.Location); got != c.suffix {
			t.Errorf("%s at %s: resumed watch abbreviates its zone as %q, want %q", c.zone, c.at.Format("Jan"), got, c.suffix)
		}
	}
}

func TestOneOffWatch_AZoneThatCannotBeLoadedIsAnErrorNotAGuessedLabel(t *testing.T) {
	started, ends := time.Date(2026, 1, 15, 2, 0, 0, 0, time.UTC), time.Date(2026, 1, 15, 3, 0, 0, 0, time.UTC)
	_, err := oneOffInfo(savedWatch{ID: "w1", StartedAt: &started, EndsAt: &ends},
		watchOneOffConfig{Today: "2026-01-15", ZoneName: "Mars/Olympus", OffsetSeconds: 36000})
	if err == nil || !strings.Contains(err.Error(), "Mars/Olympus") {
		t.Fatalf("an unreadable zone must fail the resume and name the zone: %v", err)
	}
	w := savedWatch{Config: json.RawMessage(`{"zone_name":"Mars/Olympus","offset_seconds":36000}`)}
	if loc := watchOneOffLocation(w); loc != nil {
		t.Fatalf("an unreadable zone leaves the boat time unknown, got %v", loc)
	}
}
