package main

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"
)

/*
Replay of the captured month (testdata/anomaly/engine-month-1m-slice.json): one
minute means from Pikorua's two engines, 12 October to 10 November 2026. The
learned normal trains on the first three weeks and scores the last week, which
no one had labelled but which holds no fault the operator knows of. The
constants under test are the defaults in anomaly_normal.go: none was tuned to
this slice.

The capture is in SignalK SI units. Its port gear oil temperature is a constant
placeholder, and it must be left out by the data rule and not by a name. It has
no exhaust temperature, which is fine: the reading is a candidate like the rest.
*/

// replayAimPerRunningHour is the plan's aim: about one stretch in ten engine
// running hours on a week that held no known fault.
//
// replayCeilingPerRunningHour is what the replay allows, the aim itself. Before
// the meaningful-change floor (normalMeaningfulChange) the captured month gave
// 0.217: a coolant sender that steps in whole kelvins made every one-kelvin dip
// many deviations out. With the floor it is 0.022 (ADR 0276, "Replay of the
// captured month"). No constant was tuned to this slice.
const (
	replayAimPerRunningHour     = 0.1
	replayCeilingPerRunningHour = replayAimPerRunningHour
)

type captureFile struct {
	Source  string   `json:"source"`
	From    string   `json:"from"`
	To      string   `json:"to"`
	Units   string   `json:"units"`
	Notes   string   `json:"notes"`
	Columns []string `json:"columns"`
	Data    [][]any  `json:"data"`
}

func loadCaptureFeed(t *testing.T) (map[string][]normalMinuteRow, captureFile) {
	t.Helper()
	raw, err := os.ReadFile("testdata/anomaly/engine-month-1m-slice.json")
	if err != nil {
		t.Fatal(err)
	}
	var c captureFile
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Columns) == 0 || c.Columns[0] != "timestamp" {
		t.Fatalf("columns = %v", c.Columns)
	}
	feed := map[string][]normalMinuteRow{}
	for _, row := range c.Data {
		at, err := time.Parse(time.RFC3339, row[0].(string))
		if err != nil {
			t.Fatal(err)
		}
		for i := 1; i < len(row); i++ {
			if v, ok := row[i].(float64); ok {
				feed[c.Columns[i]] = append(feed[c.Columns[i]], normalMinuteRow{Path: c.Columns[i], Source: "capture", At: at, Value: v})
			}
		}
	}
	return feed, c
}

func windowOf(feed map[string][]normalMinuteRow) func(ctx context.Context, src normalSource, paths []string, start, stop time.Time) (map[string][]normalMinuteRow, error) {
	return func(_ context.Context, _ normalSource, paths []string, start, stop time.Time) (map[string][]normalMinuteRow, error) {
		return minutesFrom(feed, paths, start, stop), nil
	}
}

func replayVessel() func() (vesselView, error) {
	return func() (vesselView, error) {
		return vesselView{Engines: []vesselEngineSetting{{Instance: "port", Name: "Port", EquipmentID: "eq-port"}, {Instance: "starboard", Name: "Starboard", EquipmentID: "eq-starboard"}}}, nil
	}
}

func replayTrain(t *testing.T, feed map[string][]normalMinuteRow, split time.Time) normalSubsystemModel {
	t.Helper()
	return replayTrainWith(t, feed, split, nil, nil)
}

// replayTrainWith trains as replayTrain does, with the operator's answers and the
// maintenance log's entries. Port is the item "eq-port" in the inventory.
func replayTrainWith(t *testing.T, feed map[string][]normalMinuteRow, now time.Time, answers []normalAnswer, repairs []maintenanceRepair) normalSubsystemModel {
	t.Helper()
	model, err := trainSubsystem(context.Background(), normalEnginesSubsystem(), normalTrainDeps{
		Now: func() time.Time { return now },
		Source: func() (normalSource, error) {
			return normalSource{Kind: normalSourceMinuteBucket, Bucket: "capture"}, nil
		},
		Minutes: windowOf(feed),
		Vessel: func() (vesselView, error) {
			return vesselView{Engines: []vesselEngineSetting{{Instance: "port", Name: "Port", EquipmentID: "eq-port"}, {Instance: "starboard", Name: "Starboard", EquipmentID: "eq-starboard"}}}, nil
		},
		Ignored:  func() map[string]bool { return nil },
		Alarms:   func(time.Time, time.Time) ([]alarmLogEntry, error) { return nil, nil },
		Answers:  func() ([]normalAnswer, error) { return answers, nil },
		Repairs:  func() ([]maintenanceRepair, error) { return repairs, nil },
		BoatLoc:  func() (*time.Location, error) { return time.UTC, nil },
		Classify: classifyRange,
	}, normalSubsystemModel{})
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func replayScore(t *testing.T, feed map[string][]normalMinuteRow, model normalSubsystemModel, from, to time.Time) unusualResult {
	t.Helper()
	return replayScoreWith(t, feed, model, from, to, nil)
}

// replayScoreWith scores as replayScore does, knowing the operator's answers.
func replayScoreWith(t *testing.T, feed map[string][]normalMinuteRow, model normalSubsystemModel, from, to time.Time, answers []normalAnswer) unusualResult {
	t.Helper()
	deps := normalScoreDeps{
		Now:     func() time.Time { return to },
		Source:  func() (normalSource, error) { return normalSource{Kind: normalSourceMainBucket}, nil },
		Minutes: windowOf(feed),
		Vessel:  replayVessel(),
		Model: func() (normalModelFile, error) {
			return normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemEngines: model}}, nil
		},
	}
	return scoreOnly(context.Background(), normalEnginesSubsystem(), deps, from, to, "", answers)
}

// runningMinutes counts the minutes an engine was above 900 rpm in [from, to).
func runningMinutes(feed map[string][]normalMinuteRow, instance string, from, to time.Time) int {
	n := 0
	for _, r := range feed["propulsion."+instance+".revolutions"] {
		if !r.At.Before(from) && r.At.Before(to) && r.Value >= twinGateMinRPMHz {
			n++
		}
	}
	return n
}

func TestReplay_MonthOfCapturedMinutes(t *testing.T) {
	feed, capture := loadCaptureFeed(t)
	end, err := time.Parse(time.RFC3339, capture.To)
	if err != nil {
		t.Fatal(err)
	}
	split := end.Add(-7 * 24 * time.Hour)
	model := replayTrain(t, feed, split)

	// What the data rules left out, and why.
	for _, id := range []string{"port", "starboard"} {
		subject := model.Subjects[id]
		if subject.Problem != "" {
			t.Fatalf("%s: %s", id, subject.Problem)
		}
		var bands []string
		for key, r := range model.Regimes {
			if r.Subject == id && r.Judged {
				bands = append(bands, key)
			}
		}
		sort.Strings(bands)
		t.Logf("%s: %d running minutes learned from; bands learned: %v", id, subject.GatedMinutes, bands)
		for _, v := range subject.Features {
			if !v.Used {
				t.Logf("%s: %s left out: %s", id, v.Key, v.Reason)
			}
		}
	}

	t.Run("the gear oil temperature placeholder is left out by the data, not by name", func(t *testing.T) {
		for _, v := range model.Subjects["port"].Features {
			if v.Key != "transmission.oilTemperature" {
				continue
			}
			if v.Used || v.Reason != "never changed while running" {
				t.Fatalf("port gear oil temperature = %+v, want left out as constant", v)
			}
			return
		}
		t.Fatal("no verdict for the port gear oil temperature")
	})

	t.Run("both engines learn bands above 1,500 rpm", func(t *testing.T) {
		for _, key := range []string{"port/rpm1600", "port/rpm1800", "starboard/rpm1600", "starboard/rpm1800"} {
			if r, ok := model.Regimes[key]; !ok || !r.Judged {
				t.Errorf("%s = %+v", key, r)
			}
		}
	})

	t.Run("a held-out week stands out at most about once in ten running hours", func(t *testing.T) {
		res := replayScore(t, feed, model, split, end)
		if res.Problem != "" {
			t.Fatal(res.Problem)
		}
		minutes := runningMinutes(feed, "port", split, end) + runningMinutes(feed, "starboard", split, end)
		hours := float64(minutes) / 60
		perHour := float64(len(res.Stretches)) / hours
		t.Logf("held-out week: %d engine-running minutes (%.1f engine-hours), %d judged, %d stretches, %.3f per running hour",
			minutes, hours, res.JudgedMinutes, len(res.Stretches), perHour)
		for _, s := range res.Stretches {
			t.Logf("  %s %s %d minutes, peak %.2fx threshold, top %s %s", s.SubjectName, s.Start.Format(time.RFC3339), s.Minutes, s.PeakRatio, s.Top.Key, s.Top.Direction)
		}
		for _, n := range res.NotJudged {
			t.Logf("  not judged: %s %s (%d minutes)", n.Subject, n.Reason, n.Minutes)
		}
		if res.JudgedMinutes == 0 {
			t.Fatal("nothing was judged")
		}
		if perHour > replayAimPerRunningHour {
			t.Logf("ABOVE THE AIM: %.3f per running hour against the plan's %.1f (one in ten hours). See replayCeilingPerRunningHour.", perHour, replayAimPerRunningHour)
		}
		if perHour > replayCeilingPerRunningHour {
			t.Errorf("%.3f stretches per running hour is over the ceiling of %.2f", perHour, replayCeilingPerRunningHour)
		}
	})

	t.Run("an oil pressure drop and a fuel rate step are found and named", func(t *testing.T) {
		start := firstSteadyStretch(t, feed, "port", model, split, end, 70)
		bent := map[string][]normalMinuteRow{}
		for path, rows := range feed {
			bent[path] = append([]normalMinuteRow(nil), rows...)
		}
		scale := func(path string, from, to time.Time, by float64) {
			for i, r := range bent[path] {
				if !r.At.Before(from) && r.At.Before(to) {
					bent[path][i].Value = r.Value * by
				}
			}
		}
		oilFrom, oilTo := start.Add(5*time.Minute), start.Add(25*time.Minute)
		fuelFrom, fuelTo := start.Add(40*time.Minute), start.Add(60*time.Minute)
		scale("propulsion.port.oilPressure", oilFrom, oilTo, 0.6)
		scale("propulsion.port.fuel.rate", fuelFrom, fuelTo, 1.6)

		res := replayScore(t, bent, model, split, end)
		found := func(key, dir string, from, to time.Time) bool {
			for _, s := range res.Stretches {
				if s.Subject == "port" && s.Top.Key == key && s.Top.Direction == dir && s.Start.Before(to) && s.End.After(from) {
					return true
				}
			}
			return false
		}
		if !found("oilPressure", "low", oilFrom, oilTo) {
			t.Errorf("no stretch naming oil pressure low over %v to %v: %+v", oilFrom, oilTo, res.Stretches)
		}
		if !found("fuel.rate", "high", fuelFrom, fuelTo) {
			t.Errorf("no stretch naming fuel rate high over %v to %v: %+v", fuelFrom, fuelTo, res.Stretches)
		}
	})
}

// firstSteadyStretch finds the first minute of n minutes in a row in which the
// engine passes the gate in a band the model judged.
func firstSteadyStretch(t *testing.T, feed map[string][]normalMinuteRow, id string, model normalSubsystemModel, from, to time.Time, n int) time.Time {
	t.Helper()
	sub := normalEnginesSubsystem()
	subject := sub.Subjects(vesselView{Engines: []vesselEngineSetting{{Instance: id, Name: id}}})[0]
	byTime := map[time.Time]*normalMinute{}
	for _, f := range subject.Features {
		for _, r := range feed[f.Path] {
			if r.At.Before(from) || !r.At.Before(to) {
				continue
			}
			m := byTime[r.At]
			if m == nil {
				m = &normalMinute{At: r.At, Values: map[string]float64{}}
				byTime[r.At] = m
			}
			m.Values[f.Key] = r.Value
		}
	}
	var minutes []normalMinute
	for _, m := range byTime {
		_, hasRPM := m.Values["revolutions"]
		_, hasTemp := m.Values["temperature"]
		if hasRPM && hasTemp {
			minutes = append(minutes, *m)
		}
	}
	sort.Slice(minutes, func(i, j int) bool { return minutes[i].At.Before(minutes[j].At) })
	gate := sub.Gate(minutes)
	run := 0
	for i := range minutes {
		key, _, ok := sub.Regime(subject, minutes[i])
		consecutive := i > 0 && minutes[i].At.Sub(minutes[i-1].At) == time.Minute
		if gate[i] && ok && model.Regimes[key].Judged && (run == 0 || consecutive) {
			run++
		} else if gate[i] && ok && model.Regimes[key].Judged {
			run = 1
		} else {
			run = 0
		}
		if run == n {
			return minutes[i-n+1].At
		}
	}
	t.Fatalf("no %d steady minutes in a judged band in the held-out week", n)
	return time.Time{}
}
