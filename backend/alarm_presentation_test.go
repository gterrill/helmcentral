package main

import (
	"testing"
	"time"
)

func TestPresentNotificationGuardZone(t *testing.T) {
	title, body := presentNotification("radar.fur6424A.guardZone.1", "Radar fur6424A guard zone 1: target 100000030 acquired", nil)
	if title != "Radar Guard Zone 1" {
		t.Fatalf("title = %q", title)
	}
	if body != "Target in guard zone 1." {
		t.Fatalf("body = %q", body)
	}
}

func TestPresentNotificationGuardZoneIgnoresRadarID(t *testing.T) {
	a, _ := presentNotification("radar.fur6424A.guardZone.2", "x", nil)
	b, _ := presentNotification("radar.fur6424B.guardZone.2", "x", nil)
	if a != "Radar Guard Zone 2" || a != b {
		t.Fatalf("titles = %q, %q", a, b)
	}
}

func TestPresentNotificationGuardZoneNamesTheRadarWhenTwoAreLive(t *testing.T) {
	one := []radarInfo{{ID: "fur6424A", Name: "Bow radar"}}
	two := []radarInfo{{ID: "fur6424A", Name: "Bow radar"}, {ID: "fur6424B", Name: "Mast radar"}}
	if title, _ := presentNotification("radar.fur6424A.guardZone.1", "x", one); title != "Radar Guard Zone 1" {
		t.Fatalf("one radar: title = %q", title)
	}
	if title, _ := presentNotification("radar.fur6424B.guardZone.1", "x", two); title != "Radar Guard Zone 1 · Mast radar" {
		t.Fatalf("two radars: title = %q", title)
	}
	// No operator name: no device key either.
	unnamed := []radarInfo{{ID: "fur6424A"}, {ID: "fur6424B"}}
	if title, _ := presentNotification("radar.fur6424B.guardZone.1", "x", unnamed); title != "Radar Guard Zone 1" {
		t.Fatalf("unnamed: title = %q", title)
	}
}

func TestPresentNotificationGeneric(t *testing.T) {
	cases := []struct{ path, message, title, body string }{
		{"perpendicularPassed", "Perpendicular passed", "Perpendicular Passed", "Perpendicular passed."},
		{"propulsion.starboard.overTemperature", "Engine too hot!", "Propulsion Starboard Over Temperature", "Engine too hot."},
		{"navigation.depth", "Shallow?", "Navigation Depth", "Shallow."},
		{"navigation.depth", "  Shallow!!  ", "Navigation Depth", "Shallow."},
		{"navigation.depth", "Already.", "Navigation Depth", "Already."},
		{"navigation.depth", "", "Navigation Depth", ""},
		{"navigation.depth", "navigation.depth", "Navigation Depth", ""},
	}
	for _, c := range cases {
		title, body := presentNotification(c.path, c.message, nil)
		if title != c.title || body != c.body {
			t.Errorf("%q/%q -> %q / %q, want %q / %q", c.path, c.message, title, body, c.title, c.body)
		}
	}
}

func TestRadarFiguresForGuardZone(t *testing.T) {
	cpa := 540.0
	tcpa := 120.0
	targets := []radarTarget{
		{RadarID: "fur6424A", TargetID: 100000030, BearingRad: 0.73, RangeM: 2593, CpaM: &cpa, TcpaSeconds: &tcpa},
		{RadarID: "fur6424B", TargetID: 100000031, BearingRad: 1, RangeM: 10},
	}
	got := radarFiguresFor("radar.fur6424A.guardZone.1", "Radar fur6424A guard zone 1: target 100000030 acquired", targets)
	if got == nil || got.BearingRad != 0.73 || got.RangeM != 2593 || got.CpaM == nil || *got.CpaM != 540 || got.TcpaSeconds == nil || *got.TcpaSeconds != 120 {
		t.Fatalf("figures = %+v", got)
	}
}

func TestRadarFiguresNilWhenTargetUnknown(t *testing.T) {
	targets := []radarTarget{{RadarID: "fur6424A", TargetID: 5}}
	if got := radarFiguresFor("radar.fur6424A.guardZone.1", "target 100000030 acquired", targets); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
	if got := radarFiguresFor("radar.fur6424A.guardZone.1", "no id here", targets); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
	if got := radarFiguresFor("navigation.anchor", "target 5", targets); got != nil {
		t.Fatalf("expected nil for non-radar, got %+v", got)
	}
}

func TestPresentNotificationStatusKeepsPathAndRuleID(t *testing.T) {
	status := alarmStatus{
		RuleID:  "notifications:radar.fur6424A.guardZone.1",
		Label:   "radar.fur6424A.guardZone.1",
		Path:    "notifications.radar.fur6424A.guardZone.1",
		Message: "Radar fur6424A guard zone 1: target 7 acquired",
	}
	got := presentNotificationStatus(status, []radarTarget{{RadarID: "fur6424A", TargetID: 7, RangeM: 100}}, nil, time.Time{})
	if got.Label != "Radar Guard Zone 1" || got.Message != "Target in guard zone 1." {
		t.Fatalf("got %q / %q", got.Label, got.Message)
	}
	if got.Path != status.Path || got.RuleID != status.RuleID {
		t.Fatalf("path/rule id changed: %+v", got)
	}
	if got.RadarTarget == nil || got.RadarTarget.RangeM != 100 {
		t.Fatalf("radar target = %+v", got.RadarTarget)
	}
}

// Written titles for the sources that turn up on this boat. The collision
// message shape is the prioritizer plugin's own (ADR 0057): "<NAME> - CPA
// WARNING" on a target, a GPS fault message on self.
func TestPresentNotificationWrittenTitles(t *testing.T) {
	cases := []struct{ path, message, title, body string }{
		{"navigation.closestApproach", "No GPS position received for more than 32 seconds", "Collision Watch Fault", "No GPS position received for more than 32 seconds."},
		{"navigation.anchor", "Anchor dragging!", "Anchor Alarm", "Anchor dragging."},
	}
	for _, c := range cases {
		title, body := presentNotification(c.path, c.message, nil)
		if title != c.title || body != c.body {
			t.Errorf("%q/%q -> %q / %q, want %q / %q", c.path, c.message, title, body, c.title, c.body)
		}
	}
}

func TestCollisionNotificationsArePresented(t *testing.T) {
	snapshot := snapshotWithTargets(map[string]any{
		"vessels.urn:mrn:imo:mmsi:503016440": collisionNotification("warn", "TASHTEGO - CPA WARNING"),
	})
	got := signalKCollisionNotifications(snapshot, alarmNow)
	if len(got) != 1 {
		t.Fatalf("got %d statuses", len(got))
	}
	if got[0].Label != "Collision Risk" || got[0].Message != "TASHTEGO inside the CPA limit." {
		t.Fatalf("label/message = %q / %q", got[0].Label, got[0].Message)
	}
	if got[0].Path != "notifications.navigation.closestApproach" {
		t.Fatalf("path changed: %q", got[0].Path)
	}
}

// A target's collision alarm is always a collision risk, whatever its
// message: an unnamed target must never read as our own GPS fault.
func TestPresentCollision(t *testing.T) {
	cases := []struct{ message, title, body string }{
		{"TASHTEGO - CPA WARNING", "Collision Risk", "TASHTEGO inside the CPA limit."},
		{"WINGS 12 - CPA ALARM", "Collision Risk", "WINGS 12 inside the CPA limit."},
		{"- CPA WARNING", "Collision Risk", "- CPA WARNING."},
		{"Something new", "Collision Risk", "Something new."},
	}
	for _, c := range cases {
		title, body := presentCollision(c.message)
		if title != c.title || body != c.body {
			t.Errorf("%q -> %q / %q, want %q / %q", c.message, title, body, c.title, c.body)
		}
	}
}

func TestNeedsRadarTargets(t *testing.T) {
	if needsRadarTargets([]alarmStatus{{Label: "navigation.anchor"}}) {
		t.Fatal("no guard zone live, should not read the radar store")
	}
	if !needsRadarTargets([]alarmStatus{{Label: "navigation.anchor"}, {Label: "radar.fur6424A.guardZone.1"}}) {
		t.Fatal("guard zone live, should read the radar store")
	}
}
