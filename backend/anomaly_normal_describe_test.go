package main

import (
	"errors"
	"testing"
	"time"
)

func oilStretch(start time.Time, minutes int) unusualStretch {
	return unusualStretch{
		ID: "engines:port@" + start.Format(time.RFC3339), Subject: "port", SubjectName: "Port engine",
		Start: start, End: start.Add(time.Duration(minutes) * time.Minute), Minutes: minutes, Band: "1,800 to 2,000 rpm",
		Top: unusualReading{Key: "oilPressure", Path: "propulsion.port.oilPressure", SIUnit: "Pa", Direction: "low", StretchMean: 280000, Usual: 340000},
	}
}

func mustDescribe(t *testing.T, st unusualStretch, system string, loc *time.Location, now time.Time) unusualStretchView {
	t.Helper()
	v, err := describeUnusualStretch(st, system, loc, now)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestDescribeStretch_SaysWhatRanLowAgainstWhatIsUsualInTheOperatorsUnits(t *testing.T) {
	start := time.Date(2026, 11, 10, 2, 10, 0, 0, time.UTC)
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	v := mustDescribe(t, oilStretch(start, 30), "metric", time.UTC, now)
	want := "Port engine oil pressure ran low from 2.10 AM to 2.40 AM: 280 kPa against a usual 340 kPa at 1,800 to 2,000 rpm."
	if v.Summary != want {
		t.Fatalf("summary:\n got  %q\n want %q", v.Summary, want)
	}
	if v.Reading != "oil pressure" || v.Direction != "low" || v.Unit != "kPa" || v.Value == nil || *v.Value != 280 || v.Usual == nil || *v.Usual != 340 {
		t.Fatalf("view = %+v", v)
	}
	if v.FromISO != "2026-11-10T02:10:00Z" || v.ToISO != "2026-11-10T02:40:00Z" || v.From != "Tue 10 Nov 2.10 AM" || v.To != "Tue 10 Nov 2.40 AM" {
		t.Fatalf("times = %q %q %q %q", v.From, v.To, v.FromISO, v.ToISO)
	}
}

func TestDescribeStretch_ImperialConvertsOnce(t *testing.T) {
	start := time.Date(2026, 11, 10, 2, 10, 0, 0, time.UTC)
	v := mustDescribe(t, oilStretch(start, 30), "imperial", time.UTC, start.Add(time.Hour))
	if want := "Port engine oil pressure ran low from 2.10 AM to 2.40 AM: 40.6 psi against a usual 49.3 psi at 1,800 to 2,000 rpm."; v.Summary != want {
		t.Fatalf("summary = %q", v.Summary)
	}
}

// The times read as the Alarms section reads them: no day for today, "yesterday"
// for the day before, and the end only gets its own day when it falls on another.
func TestDescribeStretch_NamesTheDayTheWayTheAlarmsSectionDoes(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	across := mustDescribe(t, oilStretch(time.Date(2026, 11, 9, 23, 50, 0, 0, time.UTC), 20), "metric", time.UTC, now)
	want := "Port engine oil pressure ran low from yesterday 11.50 PM to 12.10 AM: 280 kPa against a usual 340 kPa at 1,800 to 2,000 rpm."
	if across.Summary != want {
		t.Fatalf("summary:\n got  %q\n want %q", across.Summary, want)
	}
	within := mustDescribe(t, oilStretch(time.Date(2026, 11, 9, 20, 0, 0, 0, time.UTC), 20), "metric", time.UTC, now)
	if want := "Port engine oil pressure ran low from yesterday 8.00 PM to 8.20 PM: 280 kPa against a usual 340 kPa at 1,800 to 2,000 rpm."; within.Summary != want {
		t.Fatalf("summary:\n got  %q\n want %q", within.Summary, want)
	}
}

func TestDescribeStretch_TemperatureAndLoad(t *testing.T) {
	start := time.Date(2026, 11, 10, 2, 10, 0, 0, time.UTC)
	temp := oilStretch(start, 10)
	temp.Top = unusualReading{Key: "temperature", Path: "propulsion.port.temperature", SIUnit: "K", Direction: "high", StretchMean: 363.15, Usual: 353.15}
	if v := mustDescribe(t, temp, "metric", time.UTC, start); v.Summary != "Port engine coolant temperature ran high from 2.10 AM to 2.20 AM: 90 °C against a usual 80 °C at 1,800 to 2,000 rpm." {
		t.Fatalf("summary = %q", v.Summary)
	}
	load := oilStretch(start, 10)
	load.Top = unusualReading{Key: "engineLoad", Path: "propulsion.port.engineLoad", SIUnit: "ratio", Direction: "high", StretchMean: 0.85, Usual: 0.6}
	if v := mustDescribe(t, load, "metric", time.UTC, start); v.Summary != "Port engine load ran high from 2.10 AM to 2.20 AM: 85 % against a usual 60 % at 1,800 to 2,000 rpm." {
		t.Fatalf("summary = %q", v.Summary)
	}
	fuel := oilStretch(start, 10)
	fuel.Top = unusualReading{Key: "fuel.rate", Path: "propulsion.port.fuel.rate", SIUnit: "m3/s", Direction: "high", StretchMean: 2.0e-5, Usual: 1.5e-5}
	if v := mustDescribe(t, fuel, "metric", time.UTC, start); v.Summary != "Port engine fuel rate ran high from 2.10 AM to 2.20 AM: 72 L/h against a usual 54 L/h at 1,800 to 2,000 rpm." {
		t.Fatalf("summary = %q", v.Summary)
	}
}

func TestDescribeStretch_NoNameableReadingSaysSoWithoutInventingOne(t *testing.T) {
	start := time.Date(2026, 11, 10, 2, 10, 0, 0, time.UTC)
	s := oilStretch(start, 10)
	s.Top = unusualReading{}
	v := mustDescribe(t, s, "metric", time.UTC, start)
	if v.Summary != "Port engine's readings were outside their usual pattern from 2.10 AM to 2.20 AM at 1,800 to 2,000 rpm." || v.Reading != "" || v.Value != nil {
		t.Fatalf("view = %+v", v)
	}
}

// A reading that cannot be converted is an error. It is never shown in SI.
func TestDescribeStretch_AReadingThatCannotBeConvertedIsAnErrorNotSI(t *testing.T) {
	old := describeDisplayFor
	describeDisplayFor = func(string, string, string) (string, func(float64) float64, error) {
		return "", nil, errors.New("no display unit for Pa")
	}
	defer func() { describeDisplayFor = old }()
	start := time.Date(2026, 11, 10, 2, 10, 0, 0, time.UTC)
	v, err := describeUnusualStretch(oilStretch(start, 30), "metric", time.UTC, start)
	if err == nil || v.Summary != "" {
		t.Fatalf("view %+v, err %v: want an error and no sentence", v, err)
	}
}
