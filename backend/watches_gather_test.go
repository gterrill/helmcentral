package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Reading the boat for a watch report (ADR 0227).

func TestWatchLowestNoGoWind(t *testing.T) {
	if got := watchLowestNoGoWind(passageLimits{}); got != 0 {
		t.Fatalf("no limits is 0, not a number to compare with: %v", got)
	}
	l := passageLimits{Ahead: passageSectorLimits{WindKnNoGo: fp(22)}, Abeam: passageSectorLimits{WindKnNoGo: fp(15)}, Astern: passageSectorLimits{WindKnNoGo: fp(30)}}
	if got := watchLowestNoGoWind(l); got != 15 {
		t.Fatalf("a stay has no course, so the lowest limit is the cautious one: %v", got)
	}
}

func stayStatus() vesselStatusStay {
	return vesselStatusStay{Name: "Magnetic Island Marina", Lat: -19.16, Lon: 146.85, Arrive: "2026-10-30", Depart: "2026-11-01"}
}

func stayForecastDeps(hours []weatherHourPoint, err error, limits passageLimits) assistantToolDeps {
	provider := &stubWeatherProvider{id: "stub", bundle: weatherForecastBundle{Hourly: hours}, err: err}
	return assistantToolDeps{
		weather:       func() (weatherProvider, string, error) { return provider, "stub", nil },
		passageLimits: func() (passageLimits, error) { return limits, nil },
	}
}

func TestWatchStayForecastFor_FindsTheStrongestWindOnTheStayDays(t *testing.T) {
	loc := vesselLocalLocation(146.85)
	at := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, loc) }
	ms := func(kn float64) float64 { return kn / metersPerSecondToKnots }
	hours := []weatherHourPoint{
		{Time: at(29, 12), WindSpeedMS: ms(40)}, // before the stay
		{Time: at(30, 9), WindSpeedMS: ms(12)},
		{Time: at(31, 15), WindSpeedMS: ms(26)},
		{Time: at(1+31, 3), WindSpeedMS: ms(10)}, // 1 Nov
		{Time: at(2+31, 9), WindSpeedMS: ms(50)}, // after the stay
	}
	d := stayForecastDeps(hours, nil, passageLimits{Abeam: passageSectorLimits{WindKnNoGo: fp(22)}})
	got := watchStayForecastFor(d, stayStatus(), vesselStatusTestNow)
	if got.Problem != "" || got.Name != "Magnetic Island Marina" || got.NoGoKn != 22 || int(got.MaxWindKn+0.5) != 26 || got.MaxWindDay != "Sat 31 Oct" {
		t.Fatalf("%+v", got)
	}
}

func TestWatchStayForecastFor_SaysWhyItCouldNotBeRead(t *testing.T) {
	now := vesselStatusTestNow
	if got := watchStayForecastFor(stayForecastDeps(nil, nil, passageLimits{}), vesselStatusStay{Missing: "no stay is planned"}, now); got.Name != "" {
		t.Fatalf("no stay, nothing to say: %+v", got)
	}
	got := watchStayForecastFor(stayForecastDeps(nil, errors.New("weather plugin offline"), passageLimits{}), stayStatus(), now)
	if !strings.Contains(got.Problem, "weather plugin offline") {
		t.Fatalf("a provider failure is a problem, not an empty forecast: %+v", got)
	}
	got = watchStayForecastFor(stayForecastDeps(nil, nil, passageLimits{}), stayStatus(), now)
	if !strings.Contains(got.Problem, "does not reach the stay") {
		t.Fatalf("a forecast that stops short says so: %+v", got)
	}
	d := stayForecastDeps(nil, nil, passageLimits{})
	d.passageLimits = func() (passageLimits, error) { return passageLimits{}, errors.New("passage limits unreadable") }
	if got := watchStayForecastFor(d, stayStatus(), now); !strings.Contains(got.Problem, "unreadable") {
		t.Fatalf("%+v", got)
	}
	if got := watchStayForecastFor(assistantToolDeps{}, stayStatus(), now); got.Problem == "" {
		t.Fatalf("unwired forecast is said: %+v", got)
	}
}

func TestWatchRaisedAlarms(t *testing.T) {
	store, err := newAlarmLogStore(filepath.Join(t.TempDir(), "alarms.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	now := time.Date(2026, 10, 29, 6, 0, 0, 0, time.UTC)
	for _, e := range []alarmLogEntry{
		{RuleID: "a", Label: "Long ago", State: "alarm", RaisedAt: now.Add(-10 * time.Hour)},
		{RuleID: "b", Label: "Anchor Dragging", State: "alarm", RaisedAt: now.Add(-2 * time.Hour)},
	} {
		if _, err := store.RecordRaised(e); err != nil {
			t.Fatal(err)
		}
	}
	d := assistantToolDeps{alarmLog: func() *alarmLogStore { return store }}
	got, problem := watchRaisedAlarms(d, now.Add(-3*time.Hour), now)
	if problem != "" || len(got) != 1 || got[0].Label != "Anchor Dragging" {
		t.Fatalf("%v %q", got, problem)
	}
	if _, problem := watchRaisedAlarms(assistantToolDeps{alarmLog: func() *alarmLogStore { return nil }}, now.Add(-time.Hour), now); problem != "the alarm log is not open" {
		t.Fatalf("an alarm log that is not open is a reason, never an empty list: %q", problem)
	}
}

func TestGatherWatchReportInput_UsesThePreviousRunAndTheStatus(t *testing.T) {
	env := newProposalEnv(t)
	d := statusTestDeps(t)
	d.alarmLog = func() *alarmLogStore { return nil }
	now := vesselStatusTestNow
	w := savedWatch{ID: "w1", Name: "Morning", Kind: watchKindScheduled, Schedule: "0 6 * * *", Sections: allSections(), Thresholds: defaultWatchThresholds(),
		Enabled: true, CreatedAt: now.Add(-72 * time.Hour), UpdatedAt: now.Add(-72 * time.Hour), ScheduleChangedAt: now.Add(-72 * time.Hour)}
	if err := env.docs.RunTx("watch", func(tx *sql.Tx, _ time.Time) error { return insertSavedWatchTx(tx, w) }); err != nil {
		t.Fatal(err)
	}

	in, err := gatherWatchReportInput(d, env.docs, w, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if !in.FirstRun || !in.Since.Equal(now.Add(-24*time.Hour)) || in.Previous != nil || in.Status == nil || in.StatusProblem != "" {
		t.Fatalf("first run: %+v", in)
	}
	if in.RaisedProblem != "the alarm log is not open" {
		t.Errorf("raised problem %q", in.RaisedProblem)
	}

	// A run that produced a report becomes the next report's comparison.
	prevAt := now.Add(-5 * time.Hour)
	report, _ := buildWatchReport(w, watchReportInput{Now: prevAt, Loc: time.UTC, Since: prevAt.Add(-time.Hour), Status: calmStatus()})
	raw, _ := json.Marshal(report)
	if err := env.docs.RunTx("run", func(tx *sql.Tx, _ time.Time) error {
		return insertWatchRunTx(tx, watchRun{WatchID: "w1", StartedAt: prevAt, FinishedAt: prevAt, Outcome: watchOutcomeRan, Report: raw, CostKnown: true}, true)
	}); err != nil {
		t.Fatal(err)
	}
	in, err = gatherWatchReportInput(d, env.docs, w, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if in.FirstRun || !in.Since.Equal(prevAt) || !in.PreviousAt.Equal(prevAt) || in.Previous == nil {
		t.Fatalf("second run: %+v", in)
	}
	if _, ok := watchPreviousTankLevels(in.Previous); !ok {
		t.Error("the previous tank levels survive the round trip")
	}
}

func TestGatherWatchReportInput_NoLiveDataIsAReasonNotAnEmptyBoat(t *testing.T) {
	env := newProposalEnv(t)
	w := savedWatch{ID: "w1", Name: "Morning", Kind: watchKindScheduled, Sections: []string{"power"}, Thresholds: defaultWatchThresholds()}
	in, err := gatherWatchReportInput(assistantToolDeps{}, env.docs, w, vesselStatusTestNow, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if in.Status != nil || !strings.Contains(in.StatusProblem, "not wired") {
		t.Fatalf("%+v", in)
	}
	report, findings := buildWatchReport(w, in)
	if len(findings) != 0 || !strings.Contains(report.Sections[0].Unavailable, "not wired") {
		t.Fatalf("%+v %+v", report, findings)
	}
}
