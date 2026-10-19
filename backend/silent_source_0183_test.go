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

// vesperSentences are the sentences the boat's Vesper Cortex publishes under
// each talker (captured from the live sources tree): the AI talker relays the
// whole instrument network.
var vesperSentences = map[string][]string{
	"AI": {"MWV", "VDM", "RMC", "HDG", "VHW", "DPT", "VLW", "MDA", "VDO"},
	"GN": {"VTG", "GGA", "GLL"},
	"GP": {"GSV"}, "GA": {"GSV"}, "GB": {"GSV"}, "GQ": {"GSV"},
}

var entryNow = time.Date(2026, 10, 19, 7, 0, 0, 0, time.UTC)

// vesperHealth lists the six talkers, each sending now except those named in
// silent, which last published ten minutes ago.
func vesperHealth(silent ...string) []sourceHealth {
	var h []sourceHealth
	for _, s := range vesperTalkers {
		set := map[string]bool{}
		for _, sent := range vesperSentences[s[len(s)-2:]] {
			set[sent] = true
		}
		last := entryNow
		for _, q := range silent {
			if q == s {
				last = entryNow.Add(-10 * time.Minute)
			}
		}
		h = append(h, sourceHealth{Source: s, BusType: busTypeNMEA0183, Sentences: set,
			First: entryNow.Add(-time.Hour), Last: last, Count: 500})
	}
	return h
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
			health: vesperHealth(vesperTalkers...),
			want: []sensorHealthEntry{{Identifier: "Vesper_Cortex", Name: "Vesper Cortex", Label: "Vesper Cortex",
				Text: "Vesper Cortex has stopped sending AIS, position, heading, wind, depth, boat speed, log and weather"}},
		},
		{
			name:   "silent talkers whose data others also carry",
			silent: []string{"Vesper_Cortex.GA", "Vesper_Cortex.GB"},
			health: vesperHealth("Vesper_Cortex.GA", "Vesper_Cortex.GB"),
			want: []sensorHealthEntry{{Identifier: "Vesper_Cortex", Name: "Vesper Cortex", Label: "Vesper Cortex", Partial: true,
				Text: "Vesper Cortex has stopped sending some of its data"}},
		},
		{
			name:   "only the AI talker silent lists what only it carries",
			silent: []string{"Vesper_Cortex.AI"},
			health: vesperHealth("Vesper_Cortex.AI"),
			want: []sensorHealthEntry{{Identifier: "Vesper_Cortex", Name: "Vesper Cortex", Label: "Vesper Cortex", Partial: true,
				Text: "Vesper Cortex has stopped sending AIS, heading, wind, depth, boat speed, log and weather"}},
		},
		{
			name:   "no known sentences says nothing more",
			silent: []string{"Foo-Bar.ZZ"},
			health: []sourceHealth{{Source: "Foo-Bar.ZZ", BusType: busTypeNMEA0183, Sentences: map[string]bool{"ZZZ": true}, Last: entryNow.Add(-time.Hour)}},
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
			got := namer.silentSourceEntries(tc.silent, tc.health, entryNow)
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

func TestIgnoringAConnectionHidesTheAlarmButNotTheTrustGate(t *testing.T) {
	now := entryNow
	sources := vesperHealth(vesperTalkers...)
	sources = append(sources, sourceHealth{Source: "can0.23", BusType: "NMEA2000",
		First: now.Add(-time.Hour), Last: now.Add(-10 * time.Minute), Count: 500})
	if got := silentSources(sources, now, time.Second, nil); len(got) != 7 {
		t.Fatalf("unignored: %v, want 7 silent", got)
	}
	ignored := map[string]bool{"Vesper_Cortex": true}
	got := silentSources(sources, now, time.Second, ignored)
	if len(got) != 1 || got[0] != "can0.23" {
		t.Errorf("connection ignored: %v, want only can0.23", got)
	}
	// The set that gates which readings the detectors trust is untouched:
	// a stale path from the ignored connection is still distrusted.
	if got := quietSources(sources, now, time.Second, ignored); len(got) != 7 {
		t.Errorf("quietSources with a connection ignored = %d sources, want all 7 still quiet", len(got))
	}
}

func TestUnwatchedOrIgnoredSiblingIsNotStillSending(t *testing.T) {
	namer := newSensorNamer(newAnomalyTestSnapshot(), vesselView{}, nil)
	health := vesperHealth(vesperTalkers...)
	// GN is quiet but excluded (not in the silent list); it must not count as
	// sending, so the device reads as fully stopped.
	got := namer.silentSourceEntries([]string{"Vesper_Cortex.AI"}, health, entryNow)
	if len(got) != 1 || got[0].Partial {
		t.Errorf("entries = %+v, want one non-partial entry", got)
	}
}

func TestDottedConnectionIdsSplitAtTheLastDot(t *testing.T) {
	c, talker := split0183Source("My.Gps.GP")
	if c != "My.Gps" || talker != "GP" {
		t.Errorf("split = %q %q", c, talker)
	}
	h := sourceHealth{Source: "My.Gps.GP", BusType: busTypeNMEA0183}
	if healthConnection(h) != "My.Gps" {
		t.Errorf("0183 connection = %q", healthConnection(h))
	}
	if sourceConnection("venus.com.victronenergy.battery.278") != "venus" {
		t.Errorf("N2K/venus connection keeps the first-dot split")
	}
}

func TestSilentCountIsTheNumberOfDevices(t *testing.T) {
	namer := newSensorNamer(newAnomalyTestSnapshot(), vesselView{}, nil)
	got := namer.silentSourceEntries(vesperTalkers, vesperHealth(vesperTalkers...), entryNow)
	if len(got) != 1 {
		t.Errorf("six silent talkers = %d entries, want 1 (the count the rule reads)", len(got))
	}
}

func TestAlarmTitleIsFixedAtRaiseAndUsedByClearedMessage(t *testing.T) {
	rule := alarmRule{ID: anomalySilentSourceRuleID, Label: silentSourceLabel, Path: anomalySensorSilentSourceCountPath,
		Op: alarmOpAbove, Value: 0.5, State: alarmStateWarn}
	vesper := sensorHealthEntry{Name: "Vesper Cortex", Text: "Vesper Cortex has stopped sending"}
	partial := vesper
	partial.Partial = true
	if got := alarmTitleFor(rule, []sensorHealthEntry{partial}); got != "Vesper Cortex Partly Silent" {
		t.Errorf("partial title = %q", got)
	}
	if got := alarmTitleFor(rule, []sensorHealthEntry{vesper}); got != "Vesper Cortex Silent" {
		t.Errorf("title = %q", got)
	}

	now := time.Now()
	withSlot := func(entries []sensorHealthEntry, count float64) {
		prev := globalAnomalySlot
		globalAnomalySlot = &anomalySlot{}
		globalAnomalySlot.set(anomalyReading{ComputedAt: now, Sensors: map[string][]sensorHealthEntry{rule.Path: entries}})
		t.Cleanup(func() { globalAnomalySlot = prev })
	}
	withSlot([]sensorHealthEntry{vesper}, 1)
	status := &alarmStatus{RuleID: rule.ID, Phase: alarmPhaseNormal}
	// Raise.
	status.Label = rule.Label
	ev, ok := advanceAlarmRule(rule, status, alarmSample{Present: true, Value: 1.0, LastSeen: now}, now)
	if !ok || ev.Kind != alarmEventRaised || status.Label != "Vesper Cortex Silent" {
		t.Fatalf("raise: ok=%v kind=%v label=%q", ok, ev.Kind, status.Label)
	}
	// The slot now says two devices; the alarm keeps its title.
	withSlot([]sensorHealthEntry{vesper, {Name: "Wind"}}, 2)
	// Clear.
	ev, ok = advanceAlarmRule(rule, status, alarmSample{Present: true, Value: 0, LastSeen: now}, now)
	if !ok || ev.Kind != alarmEventCleared || ev.Status.Message != "Vesper Cortex Silent cleared" {
		t.Errorf("cleared: ok=%v message=%q", ok, ev.Status.Message)
	}
}

func TestIgnoredConnectionIsNamedReadably(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()
	feed0183(snapshot, time.Now(), map[string]string{"Vesper_Cortex.GP": "NMEA0183"})
	namer := newSensorNamer(snapshot, vesselView{}, nil)
	e := namer.identifierEntry("Vesper_Cortex")
	if e.Label != "Vesper Cortex" || e.Text != "Vesper Cortex" || e.Identifier != "Vesper_Cortex" {
		t.Errorf("identifierEntry = %+v", e)
	}
}

func TestSilentSourceTitleCounts(t *testing.T) {
	rule := alarmRule{ID: anomalySilentSourceRuleID, Label: silentSourceLabel}
	vesper := sensorHealthEntry{Name: "Vesper Cortex"}
	if got := alarmTitleFor(rule, []sensorHealthEntry{vesper, {Name: "Wind"}}); got != "Instruments Silent" {
		t.Errorf("two devices title = %q", got)
	}
	if got := alarmTitleFor(rule, []sensorHealthEntry{{Name: unnamedSourceName}}); got != "Instrument Silent" {
		t.Errorf("unnamed title = %q", got)
	}
	rule.Label = "My own label"
	if got := alarmTitleFor(rule, []sensorHealthEntry{vesper}); got != "My own label" {
		t.Errorf("operator label overwritten: %q", got)
	}
}

func TestSnapshotRecordsSentencesPerSource(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()
	at := time.Now()
	for _, sent := range []string{"MWV", "DPT", "MWV"} {
		snapshot.applyDelta(signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{{
			SourceRef: "Vesper_Cortex.AI",
			Source:    map[string]any{"type": "NMEA0183", "sentence": sent},
			Timestamp: at.Format(time.RFC3339),
			Values:    []signalKValue{{Path: "environment.depth.belowTransducer", Value: 3.0}},
		}}}, at)
	}
	got := snapshot.sourcesFor(snapshot.selfContext())["Vesper_Cortex.AI"].Sentences
	if len(got) != 2 || !got["MWV"] || !got["DPT"] {
		t.Errorf("sentences = %v, want MWV and DPT", got)
	}
}
