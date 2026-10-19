package main

import (
	"testing"
	"time"
)

// feed0183 records one update per source with its declared bus type.
func feed0183(snapshot *signalKSnapshot, at time.Time, sources map[string]string) {
	for source, busType := range sources {
		snapshot.applyDelta(signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{{
			SourceRef: source,
			Source:    map[string]any{"type": busType},
			Timestamp: at.Format(time.RFC3339),
			Values:    []signalKValue{{Path: "navigation.speedOverGround", Value: 1.0}},
		}}}, at)
	}
}

var vesperTalkers = []string{
	"Vesper_Cortex.AI", "Vesper_Cortex.GA", "Vesper_Cortex.GB",
	"Vesper_Cortex.GN", "Vesper_Cortex.GP", "Vesper_Cortex.GQ",
}

func vesperHealth(extra ...sourceHealth) []sourceHealth {
	var h []sourceHealth
	for _, s := range vesperTalkers {
		h = append(h, sourceHealth{Source: s, BusType: busTypeNMEA0183})
	}
	return append(h, extra...)
}

func TestSilent0183SourcesGroupByConnection(t *testing.T) {
	namer := newSensorNamer(newAnomalyTestSnapshot(), vesselView{}, nil)
	n2k := sourceHealth{Source: "can0.23", BusType: "NMEA2000"}
	n2kOther := sourceHealth{Source: "can0.24", BusType: "NMEA2000"}

	cases := []struct {
		name   string
		silent []string
		health []sourceHealth
		want   []sensorHealthEntry
	}{
		{
			name:   "every Vesper talker silent is one device",
			silent: vesperTalkers,
			health: vesperHealth(),
			want: []sensorHealthEntry{{Identifier: "Vesper_Cortex", Name: "Vesper Cortex", Label: "Vesper Cortex",
				Text: "Vesper Cortex has stopped sending AIS and satellite positions"}},
		},
		{
			name:   "some talkers still sending lists the silent ones",
			silent: []string{"Vesper_Cortex.GA", "Vesper_Cortex.GB"},
			health: vesperHealth(),
			want: []sensorHealthEntry{{Identifier: "Vesper_Cortex", Name: "Vesper Cortex", Label: "Vesper Cortex",
				Text: "Vesper Cortex has stopped sending Galileo and BeiDou"}},
		},
		{
			name:   "unknown talker code is never printed",
			silent: []string{"Foo-Bar.ZZ"},
			health: []sourceHealth{{Source: "Foo-Bar.ZZ", BusType: busTypeNMEA0183}},
			want: []sensorHealthEntry{{Identifier: "Foo-Bar", Name: "Foo Bar", Label: "Foo Bar",
				Text: "Foo Bar has stopped sending"}},
		},
		{
			name:   "NMEA 2000 sources on one gateway are not grouped",
			silent: []string{"can0.23", "can0.24"},
			health: []sourceHealth{n2k, n2kOther},
			want: []sensorHealthEntry{
				{Identifier: "can0.23", Name: "An instrument", Label: "An instrument", Text: "An instrument has stopped sending"},
				{Identifier: "can0.24", Name: "An instrument", Label: "An instrument", Text: "An instrument has stopped sending"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := namer.silentSourceEntries(tc.silent, tc.health)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d entries %+v, want %d", len(got), got, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("entry %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestSilentSourceNamedFromRecordedBusType(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()
	feed0183(snapshot, time.Now(), map[string]string{"Vesper_Cortex.GP": "NMEA0183", "ydwg.12": "NMEA2000"})
	namer := newSensorNamer(snapshot, vesselView{}, nil)
	if got := namer.silentSourceEntry("Vesper_Cortex.GP").Name; got != "Vesper Cortex" {
		t.Errorf("0183 source name = %q, want Vesper Cortex", got)
	}
	if got := namer.silentSourceEntry("ydwg.12").Name; got != unnamedSourceName {
		t.Errorf("N2K source with no device info = %q, want %q", got, unnamedSourceName)
	}
}

func TestIgnoringAConnectionSilencesItsTalkersOnly(t *testing.T) {
	now := time.Date(2026, 10, 19, 7, 0, 0, 0, time.UTC)
	quiet := func(h sourceHealth) sourceHealth {
		h.First, h.Last, h.Count = now.Add(-time.Hour), now.Add(-10*time.Minute), 500
		return h
	}
	var sources []sourceHealth
	for _, h := range vesperHealth(sourceHealth{Source: "can0.23", BusType: "NMEA2000"}) {
		sources = append(sources, quiet(h))
	}
	if got := silentSources(sources, now, time.Second, nil); len(got) != 7 {
		t.Fatalf("unignored: %v, want 7 silent", got)
	}
	got := silentSources(sources, now, time.Second, map[string]bool{"Vesper_Cortex": true})
	if len(got) != 1 || got[0] != "can0.23" {
		t.Errorf("connection ignored: %v, want only can0.23", got)
	}
}

func TestIgnoredConnectionIsNamedReadably(t *testing.T) {
	namer := newSensorNamer(newAnomalyTestSnapshot(), vesselView{}, nil)
	e := namer.identifierEntry("Vesper_Cortex")
	if e.Label != "Vesper Cortex" || e.Text != "Vesper Cortex" || e.Identifier != "Vesper_Cortex" {
		t.Errorf("identifierEntry = %+v", e)
	}
}

func TestSilentSourceTitleNamesTheDevice(t *testing.T) {
	now := time.Now()
	rule := alarmRule{ID: anomalySilentSourceRuleID, Label: silentSourceLabel, Path: anomalySensorSilentSourceCountPath}
	set := func(t *testing.T, entries ...sensorHealthEntry) {
		prev := globalAnomalySlot
		globalAnomalySlot = &anomalySlot{}
		globalAnomalySlot.set(anomalyReading{ComputedAt: now, Sensors: map[string][]sensorHealthEntry{rule.Path: entries}})
		t.Cleanup(func() { globalAnomalySlot = prev })
	}
	vesper := sensorHealthEntry{Name: "Vesper Cortex", Text: "Vesper Cortex has stopped sending"}
	set(t, vesper)
	if got := alarmTitleFor(rule, now); got != "Vesper Cortex Silent" {
		t.Errorf("one device title = %q", got)
	}
	set(t, vesper, sensorHealthEntry{Name: "Wind"})
	if got := alarmTitleFor(rule, now); got != "Instruments Silent" {
		t.Errorf("two devices title = %q", got)
	}
	set(t, sensorHealthEntry{Name: unnamedSourceName})
	if got := alarmTitleFor(rule, now); got != "Instrument Silent" {
		t.Errorf("unnamed title = %q", got)
	}
	set(t, vesper)
	rule.Label = "My own label"
	if got := alarmTitleFor(rule, now); got != "My own label" {
		t.Errorf("operator label overwritten: %q", got)
	}
}
