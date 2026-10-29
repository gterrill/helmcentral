package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real coordinates, real zones (ADR 0228). A fixed offset from longitude gets
// every row of the daylight-saving and half-hour cases wrong.
func TestZoneAt_PlacesAndOcean(t *testing.T) {
	cases := []struct {
		name     string
		lat, lon float64
		want     string
	}{
		{"Sydney", -33.87, 151.21, "Australia/Sydney"},
		{"Shute Harbour", -20.29, 148.78, "Australia/Brisbane"},
		{"Adelaide", -34.93, 138.60, "Australia/Adelaide"},
		{"Darwin", -12.46, 130.84, "Australia/Darwin"},
		{"Lord Howe Island", -31.55, 159.08, "Australia/Lord_Howe"},
		{"Coral Sea", -18, 158, "Etc/GMT-11"},
		{"mid Pacific", 0, -150, "Etc/GMT+10"},
		{"5 nm off Sydney", -33.86, 151.35, "Australia/Sydney"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			z, err := zoneAt(c.lat, c.lon)
			if err != nil {
				t.Fatalf("zoneAt: %v", err)
			}
			if z.Name != c.want {
				t.Fatalf("zone = %q, want %q", z.Name, c.want)
			}
			if z.Loc == nil || z.Loc.String() != c.want {
				t.Fatalf("location = %v, want %q", z.Loc, c.want)
			}
		})
	}
}

func TestZoneAt_RejectsImpossibleCoordinates(t *testing.T) {
	for _, p := range [][2]float64{{91, 0}, {-91, 0}, {0, 181}, {0, -181}} {
		if _, err := zoneAt(p[0], p[1]); err == nil {
			t.Errorf("zoneAt(%v, %v): expected an error", p[0], p[1])
		}
	}
}

func TestZoneLabel_FollowsDaylightSavingAndHalfHours(t *testing.T) {
	syd, err := zoneAt(-33.87, 151.21)
	if err != nil {
		t.Fatal(err)
	}
	summer := time.Date(2026, 1, 15, 2, 0, 0, 0, time.UTC)
	winter := time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC)
	if got := syd.Label(summer); got != "AEDT (UTC+11)" {
		t.Errorf("Sydney in January = %q", got)
	}
	if got := syd.Label(winter); got != "AEST (UTC+10)" {
		t.Errorf("Sydney in July = %q", got)
	}
	adl, _ := zoneAt(-34.93, 138.60)
	if got := adl.Label(winter); got != "ACST (UTC+9:30)" {
		t.Errorf("Adelaide in July = %q", got)
	}
	coral, _ := zoneAt(-18, 158)
	if got := coral.Label(winter); got != "UTC+11" {
		t.Errorf("Coral Sea = %q (an offshore zone has no abbreviation)", got)
	}
	west, _ := zoneAt(0, -150)
	if got := west.Label(winter); got != "UTC-10" {
		t.Errorf("mid Pacific = %q", got)
	}
}

func TestZoneDescribe(t *testing.T) {
	bne, _ := zoneAt(-27.47, 153.03)
	at := time.Date(2026, 10, 29, 0, 34, 0, 0, time.UTC)
	if got := bne.Describe(at); got != "Australia/Brisbane, UTC+10" {
		t.Errorf("Describe = %q", got)
	}
	ofs, _ := zoneAt(-18, 158)
	if got := ofs.Describe(at); got != "Etc/GMT-11, UTC+11" {
		t.Errorf("offshore Describe = %q", got)
	}
}

func useFreshBoatZone(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "boat_zone.json")
	t.Setenv("BOAT_ZONE_FILE", path)
	resetBoatZoneState()
	t.Cleanup(resetBoatZoneState)
	return path
}

func TestBoatTime_LiveFixUsesItsOwnZoneAndIsRemembered(t *testing.T) {
	path := useFreshBoatZone(t)
	bt := boatTimeAt(-33.87, 151.21, true)
	if bt.Name != "Australia/Sydney" || bt.LastFix || bt.NoFix {
		t.Fatalf("live fix: %+v", bt)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), "Australia/Sydney") {
		t.Fatalf("zone not persisted: %v %q", err, raw)
	}
}

func TestBoatTime_NoFixUsesLastFixSurvivingARestart(t *testing.T) {
	useFreshBoatZone(t)
	boatTimeAt(-20.29, 148.78, true)
	resetBoatZoneState() // a restart: memory gone, file stays
	bt := boatTimeAt(-1, -1, false)
	if bt.Name != "Australia/Brisbane" || !bt.LastFix || bt.NoFix {
		t.Fatalf("no fix after a fix: %+v", bt)
	}
	if got := bt.Source(); got != "last_fix" {
		t.Errorf("Source = %q", got)
	}
}

func TestBoatTime_NeverAFixIsUTCAndSaysSo(t *testing.T) {
	useFreshBoatZone(t)
	bt := boatTimeAt(-1, -1, false)
	if bt.Name != "UTC" || bt.Loc != time.UTC || !bt.NoFix || bt.LastFix {
		t.Fatalf("never a fix: %+v", bt)
	}
	if got := bt.Source(); got != "none" {
		t.Errorf("Source = %q", got)
	}
}

func TestBoatTime_ABadStoredFileIsReportedNotIgnored(t *testing.T) {
	path := useFreshBoatZone(t)
	if err := os.WriteFile(path, []byte(`{"zone":"Not/AZone"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	bt := boatTimeAt(-1, -1, false)
	if bt.Err == nil {
		t.Fatalf("expected the unreadable stored zone to be reported: %+v", bt)
	}
}

func TestBoatTimeForState_UsesTheSharedFixChecks(t *testing.T) {
	useFreshBoatZone(t)
	good := vesselStateData{Latitude: -33.87, Longitude: 151.21}
	if bt := boatTimeForState(good, nil); bt.Name != "Australia/Sydney" || bt.LastFix {
		t.Fatalf("good: %+v", bt)
	}
	sentinel := vesselStateData{Latitude: -1, Longitude: -1}
	if bt := boatTimeForState(sentinel, nil); bt.Name != "Australia/Sydney" || !bt.LastFix {
		t.Fatalf("sentinel should fall to last fix: %+v", bt)
	}
	alert := vesselStateData{Latitude: 51.5, Longitude: -0.1, GNSSCriticalAlert: true}
	if bt := boatTimeForState(alert, nil); bt.Name != "Australia/Sydney" || !bt.LastFix {
		t.Fatalf("critical alert should fall to last fix: %+v", bt)
	}
}

// queenslandLoc is the zone of the test boat's home waters: Australia/Brisbane,
// UTC+10 all year (no daylight saving), so a test that fixes a UTC+10 clock
// stays a real zone.
func queenslandLoc(testing.TB) *time.Location { return mustZoneAt(-27.47, 153.03).Loc }

// mustZoneAt is for package-level test fixtures that cannot take a *testing.T.
func mustZoneAt(lat, lon float64) placeZone {
	z, err := zoneAt(lat, lon)
	if err != nil {
		panic(err)
	}
	return z
}

func TestBuildAssistantSystemPrompt_AZeroBoatTimeDoesNotPanicInWarningsOrWatchLines(t *testing.T) {
	pc := assistantPromptContext{
		Now: time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), WarningOK: true, WarningFetchedAt: time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC),
		ActiveWatch: &assistantWatchInfo{Subject: "the bilge", EndsAt: time.Date(2026, 10, 29, 3, 0, 0, 0, time.UTC)},
	}
	prompt := buildAssistantSystemPrompt(pc)
	if !strings.Contains(prompt, "(UTC, no position yet)") || !strings.Contains(prompt, "as of 00:00") || !strings.Contains(prompt, "until 03:00") {
		t.Fatalf("zero boat time should read as UTC with no position:\n%s", prompt)
	}
}

func TestCurrentVesselLocalLocation_ACriticalAlertFixIsNotRememberedAsTheLastFix(t *testing.T) {
	path := useFreshBoatZone(t)
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)
	resolveGNSSPosition(0, 0, false, gnssPositionValidation{Status: "critical", Reason: "test"})

	orig := globalSignalKSnapshot
	globalSignalKSnapshot = snapshotWithSelfValues(map[string]any{
		"navigation.position": map[string]any{"latitude": -33.87, "longitude": 151.21},
	})
	t.Cleanup(func() { globalSignalKSnapshot = orig })

	if _, ok := currentVesselLocalLocation(); ok {
		t.Fatal("a fix under a critical GNSS alert is not a fix")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a critical-alert fix must not be stored as the last trusted zone")
	}
}

func TestBoatTimeForState_ACriticalAlertFixIsNotPersisted(t *testing.T) {
	path := useFreshBoatZone(t)
	bt := boatTimeForState(vesselStateData{Latitude: -33.87, Longitude: 151.21, GNSSCriticalAlert: true}, nil)
	if !bt.NoFix {
		t.Fatalf("%+v", bt)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("persisted")
	}
}

func TestBoatTimeAt_AWriteFailureStillUpdatesTheZoneInMemory(t *testing.T) {
	useFreshBoatZone(t)
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BOAT_ZONE_FILE", filepath.Join(blocker, "boat_zone.json")) // a directory can't be made under a file
	resetBoatZoneState()
	boatTimeAt(-33.87, 151.21, true)
	bt := boatTimeAt(-1, -1, false)
	if bt.Name != "Australia/Sydney" || !bt.LastFix {
		t.Fatalf("the zone should be held in memory despite the failed write: %+v", bt)
	}
}

func TestBoatTimeAt_NearbyFixesShareOneLookup(t *testing.T) {
	useFreshBoatZone(t)
	a, err := zoneAtCached(-33.8701, 151.2101)
	if err != nil {
		t.Fatal(err)
	}
	zoneCells.mu.RLock()
	n := len(zoneCells.m)
	zoneCells.mu.RUnlock()
	b, _ := zoneAtCached(-33.8704, 151.2098)
	zoneCells.mu.RLock()
	m := len(zoneCells.m)
	zoneCells.mu.RUnlock()
	if a.Name != b.Name || n != 1 || m != 1 {
		t.Fatalf("cells %d then %d, zones %q %q", n, m, a.Name, b.Name)
	}
}

func TestLabelSpan_NamesBothSidesOfTheClocksChanging(t *testing.T) {
	syd := boatTime{placeZone: mustZoneAt(-33.87, 151.21)}
	// Sydney's clocks went forward at 02:00 on Sunday 4 October 2026.
	before := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	after := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	if got := syd.LabelSpan(after, before); got != "AEST (UTC+10) then AEDT (UTC+11)" {
		t.Errorf("across the change: %q", got)
	}
	if got := syd.LabelSpan(after, after.Add(time.Hour)); got != "AEDT (UTC+11)" {
		t.Errorf("one side: %q", got)
	}
}
