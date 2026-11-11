package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type unusualToolCall struct {
	since, until time.Time
	subject      unusualSubject
	ctx          context.Context
}

// unusualToolDeps wires the tool to a canned scored window and records what it
// was asked. fixed says the boat has a position, and so a time zone.
func unusualToolDeps(res unusualResult, fixed bool) (assistantToolDeps, *unusualToolCall) {
	call := &unusualToolCall{}
	d := assistantToolDeps{
		now: func() time.Time { return unusualNow },
		vesselState: func() (vesselStateData, error) {
			if !fixed {
				return vesselStateData{}, errors.New("no position yet")
			}
			return vesselStateData{Latitude: -19.25, Longitude: 146.8, PositionLastUpdateAge: 1}, nil
		},
		vesselView: func() (vesselView, error) {
			v := houseVessel()
			v.Engines = []vesselEngineSetting{{Instance: "port", Name: "Port"}, {Instance: "starboard", Name: "Starboard"}}
			return v, nil
		},
		displayUnits:   func() (string, error) { return "metric", nil },
		unusualAnswers: func() ([]normalAnswer, error) { return nil, nil },
		// Like scoreSubsystem, the stand-in cuts a window past seven days.
		unusualReadings: func(ctx context.Context, since, until time.Time, subject unusualSubject, _ []normalAnswer) unusualResult {
			*call = unusualToolCall{since: since, until: until, subject: subject, ctx: ctx}
			r := res
			r.Since, r.Until = since, until
			if floor := until.Add(-normalScoreMaxSince); since.Before(floor) {
				r.Since, r.Clipped = floor, true
			}
			return r
		},
	}
	return d, call
}

func runUnusualTool(t *testing.T, d assistantToolDeps, args string) (map[string]any, string) {
	t.Helper()
	// The boat's zone is remembered between calls in a file; keep this test's
	// fix, or its absence, out of every other test's.
	useFreshBoatZone(t)
	mt := lookupMateTool("get_unusual_readings")
	if mt == nil {
		t.Fatal("the tool is not registered")
	}
	out, _, err := mt.execute(d, context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, out)
	}
	return got, out
}

func TestUnusualTool_GivesBoatTimeLabelsWithIsoBesideThemAndTheFiguresInTheOperatorsUnits(t *testing.T) {
	res := unusualResultWith(oilStretch(time.Date(2026, 11, 9, 16, 10, 0, 0, time.UTC), 30))
	res.Model.Source = normalSource{Kind: normalSourceMinuteBucket, Bucket: "SignalK_1m"}
	res.Model.Bands = map[string]int{"port": 7}
	d, call := unusualToolDeps(res, true)
	got, raw := runUnusualTool(t, d, `{}`)

	if call.until != unusualNow || call.since != unusualNow.Add(-24*time.Hour) || call.subject != (unusualSubject{}) {
		t.Fatalf("asked %+v, want the last 24 hours for every subject", call)
	}
	stretches := got["stretches"].([]any)
	if len(stretches) != 1 {
		t.Fatalf("stretches: %s", raw)
	}
	s := stretches[0].(map[string]any)
	// 16.10 UTC is 2.10 AM the next day in Queensland (UTC+10).
	if s["from"] != "Tue 10 Nov 2.10 AM" || s["to"] != "Tue 10 Nov 2.40 AM" || s["from_iso"] != "2026-11-09T16:10:00Z" {
		t.Fatalf("times = %v %v %v", s["from"], s["to"], s["from_iso"])
	}
	if s["reading"] != "oil pressure" || s["direction"] != "low" || s["value"] != 280.0 || s["usual"] != 340.0 || s["unit"] != "kPa" {
		t.Fatalf("stretch = %v", s)
	}
	if !strings.Contains(s["summary"].(string), "ran low from 2.10 AM") {
		t.Fatalf("summary = %v", s["summary"])
	}
	if _, has := got["timezone_note"]; has {
		t.Fatalf("a boat with a fix needs no timezone note: %s", raw)
	}
	for _, k := range []string{"since", "since_iso", "until", "until_iso", "judged_minutes", "learned_normal", "note"} {
		if _, has := got[k]; !has {
			t.Errorf("result lacks %q: %s", k, raw)
		}
	}
	learned := got["learned_normal"].(map[string]any)
	if learned["source"] != `one-minute bucket "SignalK_1m"` || learned["learned_iso"] == nil {
		t.Fatalf("learned_normal = %v", learned)
	}
	if !strings.Contains(got["note"].(string), "suggestion") || !strings.Contains(got["note"].(string), "not a diagnosis") {
		t.Fatalf("note = %v", got["note"])
	}
}

func TestUnusualTool_WithNoPositionEverTheTimesAreUTCAndSaySo(t *testing.T) {
	d, _ := unusualToolDeps(unusualResultWith(), false)
	got, raw := runUnusualTool(t, d, `{}`)
	if note, _ := got["timezone_note"].(string); !strings.Contains(note, "UTC") {
		t.Fatalf("result: %s", raw)
	}
}

func TestUnusualTool_ASinceBeyondSevenDaysIsClippedAndSaid(t *testing.T) {
	d, call := unusualToolDeps(unusualResultWith(), true)
	got, _ := runUnusualTool(t, d, `{"since_iso":"2026-10-01T00:00:00Z"}`)
	// The tool passes what was asked. The cut is the score's.
	if !call.since.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("since = %v", call.since)
	}
	if got["since_iso"] != unusualNow.Add(-7*24*time.Hour).Format(time.RFC3339) {
		t.Fatalf("the window it reports is the cut one: %v", got["since_iso"])
	}
	if note, _ := got["since_note"].(string); !strings.Contains(note, "7 days") {
		t.Fatalf("a clipped window says so: %v", got["since_note"])
	}
	got, _ = runUnusualTool(t, d, `{"since_iso":"2026-11-10T06:00:00Z"}`)
	if _, has := got["since_note"]; has || !call.since.Equal(time.Date(2026, 11, 10, 6, 0, 0, 0, time.UTC)) {
		t.Fatalf("an in-range since is used as given: %v %v", call.since, got["since_note"])
	}
}

func TestUnusualTool_LimitDefaultsToFiveAndSaysWhatItLeftOut(t *testing.T) {
	var many []unusualStretch
	for i := 0; i < 7; i++ {
		many = append(many, oilStretch(unusualNow.Add(-time.Duration(20-i)*time.Hour), 5))
	}
	d, _ := unusualToolDeps(unusualResultWith(many...), true)
	got, _ := runUnusualTool(t, d, `{}`)
	if n := len(got["stretches"].([]any)); n != 5 {
		t.Fatalf("returned %d, want the default 5", n)
	}
	if note, _ := got["stretches_note"].(string); note != "2 more unusual stretches are not shown." {
		t.Fatalf("stretches_note = %v", got["stretches_note"])
	}
	got, _ = runUnusualTool(t, d, `{"limit":1}`)
	if n := len(got["stretches"].([]any)); n != 1 {
		t.Fatalf("returned %d, want 1", n)
	}
}

func TestUnusualTool_RefusesAnOutOfRangeLimitAndAnUnknownSubject(t *testing.T) {
	d, _ := unusualToolDeps(unusualResultWith(), true)
	mt := lookupMateTool("get_unusual_readings")
	for _, args := range []string{`{"limit":0}`, `{"limit":11}`, `{"since_iso":"yesterday"}`, `{"since_iso":"2027-01-01T00:00:00Z"}`, `{"subject":"centre"}`} {
		if _, _, err := mt.execute(d, context.Background(), json.RawMessage(args)); err == nil {
			t.Errorf("%s must be refused", args)
		}
	}
}

func TestUnusualTool_AnEngineIsNamedByItsInstanceOrItsName(t *testing.T) {
	d, call := unusualToolDeps(unusualResultWith(), true)
	for _, name := range []string{"port", "Port", "Port engine", "starboard"} {
		runUnusualTool(t, d, `{"subject":"`+name+`"}`)
		want := unusualSubject{Subsystem: "engines", ID: strings.ToLower(strings.Fields(name)[0])}
		if call.subject != want {
			t.Errorf("subject %q asked for %+v, want %+v", name, call.subject, want)
		}
	}
}

func TestUnusualTool_TheHouseBankIsNamedByItsIdOrItsName(t *testing.T) {
	d, call := unusualToolDeps(unusualResultWith(), true)
	for _, name := range []string{"house", "House bank", "house bank"} {
		got, _ := runUnusualTool(t, d, `{"subject":"`+name+`"}`)
		if want := (unusualSubject{Subsystem: "electrical", ID: "house"}); call.subject != want {
			t.Errorf("subject %q asked for %+v, want %+v", name, call.subject, want)
		}
		if got["subject"] != "House bank" {
			t.Errorf("the result names the subject: %v", got["subject"])
		}
	}
}

// An unknown subject is refused with the names it could have been.
func TestUnusualTool_AnUnknownSubjectNamesTheOnesThereAre(t *testing.T) {
	d, _ := unusualToolDeps(unusualResultWith(), true)
	useFreshBoatZone(t)
	_, _, err := lookupMateTool("get_unusual_readings").execute(d, context.Background(), json.RawMessage(`{"subject":"centre"}`))
	if err == nil || !strings.Contains(err.Error(), "Port engine") || !strings.Contains(err.Error(), "House bank") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnusualTool_TheOldEngineArgumentIsNotAccepted(t *testing.T) {
	d, _ := unusualToolDeps(unusualResultWith(), true)
	useFreshBoatZone(t)
	_, _, err := lookupMateTool("get_unusual_readings").execute(d, context.Background(), json.RawMessage(`{"engine":"port"}`))
	if err == nil {
		t.Fatal("an argument the tool no longer has must be refused, not ignored")
	}
}

func TestUnusualTool_TheOldNameIsGone(t *testing.T) {
	if lookupMateTool("get_unusual_engine_readings") != nil {
		t.Fatal("the tool was renamed")
	}
	if _, ok := mcpPolicyFor("get_unusual_engine_readings"); ok {
		t.Fatal("the outside assistant policy was renamed too")
	}
}

func TestUnusualTool_AMissingLearnedNormalIsAReasonNotAnEmptyList(t *testing.T) {
	res := unusualResult{ProblemKind: unusualNoInflux, Problem: "InfluxDB is not set up"}
	d, _ := unusualToolDeps(res, true)
	got, raw := runUnusualTool(t, d, `{}`)
	if got["learned_normal_missing"] != "InfluxDB is not set up" {
		t.Fatalf("result: %s", raw)
	}
	if _, has := got["learned_normal"]; has {
		t.Fatalf("no model to describe: %s", raw)
	}
	if s, _ := got["stretches"].([]any); len(s) != 0 {
		t.Fatalf("stretches: %s", raw)
	}
}

func TestUnusualTool_NotJudgedAndATrainingProblemAreGiven(t *testing.T) {
	res := unusualResultWith()
	res.TrainingProblem = "InfluxDB could not be read: timeout"
	res.NotJudged = []unusualNotJudged{
		{Kind: "not_steady", Reason: "not running steadily", Minutes: 10},
		{Kind: "thin_band", Reason: "not enough history for 1,400 to 1,600 rpm: 22 minutes learned, needs 60", Minutes: 40},
	}
	d, _ := unusualToolDeps(res, true)
	got, raw := runUnusualTool(t, d, `{}`)
	nj := got["not_judged"].([]any)
	if len(nj) != 2 || nj[1].(map[string]any)["minutes"] != 40.0 || !strings.Contains(nj[1].(map[string]any)["reason"].(string), "22 minutes learned") {
		t.Fatalf("not_judged: %s", raw)
	}
	if got["training_problem"] != "InfluxDB could not be read: timeout" {
		t.Fatalf("training_problem: %s", raw)
	}
}

// The figures are in the operator's units. Units that cannot be read stop the
// call, and a reading that cannot be converted does too. Neither falls back.
func TestUnusualTool_UnitsThatCannotBeReadOrAppliedAreAnErrorNotAFallback(t *testing.T) {
	useFreshBoatZone(t)
	mt := lookupMateTool("get_unusual_readings")
	res := unusualResultWith(oilStretch(time.Date(2026, 11, 9, 16, 10, 0, 0, time.UTC), 30))

	d, call := unusualToolDeps(res, true)
	d.displayUnits = func() (string, error) { return "", errors.New("settings unreadable") }
	if _, _, err := mt.execute(d, context.Background(), json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "display units") {
		t.Fatalf("unreadable units: err = %v", err)
	}
	if call.until != (time.Time{}) {
		t.Fatal("the query ran before the units were known")
	}
	d.displayUnits = nil
	if _, _, err := mt.execute(d, context.Background(), json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "not wired") {
		t.Fatalf("unwired units: err = %v", err)
	}

	d, _ = unusualToolDeps(res, true)
	old := describeDisplayFor
	describeDisplayFor = func(string, string, string) (string, func(float64) float64, error) {
		return "", nil, errors.New("no display unit for Pa")
	}
	defer func() { describeDisplayFor = old }()
	if out, _, err := mt.execute(d, context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatalf("a stretch in no known unit was given anyway: %s", out)
	}
}

func TestUnusualTool_PassesItsContextToTheQuery(t *testing.T) {
	type key struct{}
	d, call := unusualToolDeps(unusualResultWith(), true)
	useFreshBoatZone(t)
	ctx := context.WithValue(context.Background(), key{}, "mark")
	if _, _, err := lookupMateTool("get_unusual_readings").execute(d, ctx, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if call.ctx == nil || call.ctx.Value(key{}) != "mark" {
		t.Fatal("the tool's context did not reach the query")
	}
}

func TestUnusualTool_NotWiredIsAnError(t *testing.T) {
	mt := lookupMateTool("get_unusual_readings")
	if _, _, err := mt.execute(assistantToolDeps{now: func() time.Time { return unusualNow }}, context.Background(), json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "not wired") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnusualTool_IsAReadOnlyToolForReviewsAndOutsideAssistants(t *testing.T) {
	mt := lookupMateTool("get_unusual_readings")
	if mt == nil || mt.effect != effectReads || mt.drawsCard || mt.collect != nil {
		t.Fatalf("tool = %+v", mt)
	}
	for _, a := range []mateAudience{audienceAlarmReview, audienceWatchReview, audienceMCP} {
		if !mt.reaches(a) {
			t.Errorf("audience %d cannot reach it", a)
		}
	}
	if !strings.Contains(strings.ToLower(mt.function.Description), "suggestion") || !strings.Contains(strings.ToLower(mt.function.Description), "never a diagnosis") {
		t.Fatalf("description = %q", mt.function.Description)
	}
	p, ok := mcpPolicyFor("get_unusual_readings")
	if !ok || p.Status != mcpOfferedOff || p.Plain != "Unusual readings" {
		t.Fatalf("policy = %+v %v", p, ok)
	}
	if mcpToolDefByName("get_unusual_readings") == nil {
		t.Fatal("not built into the outside assistant server")
	}
	for _, n := range mcpDefaultTools() {
		if n == "get_unusual_readings" {
			t.Fatal("it starts off")
		}
	}
}

// A stray key is not the tool's business. Only the old engine argument is refused,
// and it says what to use.
func TestUnusualTool_AStrayKeyIsIgnoredButTheOldEngineArgumentSaysToUseSubject(t *testing.T) {
	d, _ := unusualToolDeps(unusualResultWith(), true)
	runUnusualTool(t, d, `{"note":"hello"}`)
	useFreshBoatZone(t)
	_, _, err := lookupMateTool("get_unusual_readings").execute(d, context.Background(), json.RawMessage(`{"engine":"port"}`))
	if err == nil || !strings.Contains(err.Error(), "subject") {
		t.Fatalf("err = %v", err)
	}
}
