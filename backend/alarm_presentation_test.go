package main

import (
	"testing"
)

// A radar guard-zone notification still arrives on the bus from Helmcam's
// mayara plugin; Helmcentral words it without the radar's key or the target id.
func TestPresentNotificationGuardZoneIsWordedWithoutDeviceKeys(t *testing.T) {
	title, body := presentNotification("radar.fur6424A.guardZone.1", "Radar fur6424A guard zone 1: target 100000030 acquired")
	if title != "Radar Guard Zone 1" {
		t.Fatalf("title = %q", title)
	}
	if body != "Target in guard zone 1." {
		t.Fatalf("body = %q", body)
	}
}

// A dual-range radar raises the same zone under each range's key. Both fold to
// one kind, so one Stop showing hides them both and the Settings list names
// the folded kind by the same title.
func TestGuardZoneKindFoldsTheRadarKey(t *testing.T) {
	for _, path := range []string{"notifications.radar.fur6424A.guardZone.1", "notifications.radar.fur6424B.guardZone.1"} {
		if got := alarmKindOf(path); got != "radar.guardZone.1" {
			t.Fatalf("alarmKindOf(%q) = %q, want radar.guardZone.1", path, got)
		}
	}
	if title, _ := presentNotification("radar.guardZone.1", ""); title != "Radar Guard Zone 1" {
		t.Fatalf("folded kind title = %q", title)
	}
}

func TestPresentNotificationStatusKeepsPathAndRuleID(t *testing.T) {
	status := alarmStatus{
		RuleID:  "notifications:radar.fur6424A.guardZone.1",
		Label:   "radar.fur6424A.guardZone.1",
		Path:    "notifications.radar.fur6424A.guardZone.1",
		Message: "Radar fur6424A guard zone 1: target 7 acquired",
	}
	got := presentNotificationStatus(status)
	if got.Label != "Radar Guard Zone 1" || got.Message != "Target in guard zone 1." {
		t.Fatalf("got %q / %q", got.Label, got.Message)
	}
	if got.Path != status.Path || got.RuleID != status.RuleID {
		t.Fatalf("path/rule id changed: %+v", got)
	}
}

func TestPresentNotificationGeneric(t *testing.T) {
	cases := []struct{ path, message, title, body string }{
		{"perpendicularPassed", "Perpendicular passed!", "Waypoint Passed", "The boat has passed the active waypoint."},
		{"arrivalCircleEntered", "WP arrival circle entered!", "Arriving at Waypoint", "The boat is inside the active waypoint's arrival circle."},
		{"navigation.arrivalCircleEntered", "", "Arriving at Waypoint", "The boat is inside the active waypoint's arrival circle."},
		{"propulsion.starboard.overTemperature", "Engine too hot!", "Propulsion Starboard Over Temperature", "Engine too hot."},
		{"navigation.depth", "Shallow?", "Navigation Depth", "Shallow."},
		{"navigation.depth", "  Shallow!!  ", "Navigation Depth", "Shallow."},
		{"navigation.depth", "Already.", "Navigation Depth", "Already."},
		{"navigation.depth", "", "Navigation Depth", ""},
		{"navigation.depth", "navigation.depth", "Navigation Depth", ""},
		{"received.distress.dsc-whatever", "Distress relayed.", "Distress Received", "Distress relayed."},
	}
	for _, c := range cases {
		title, body := presentNotification(c.path, c.message)
		if title != c.title || body != c.body {
			t.Errorf("%q/%q -> %q / %q, want %q / %q", c.path, c.message, title, body, c.title, c.body)
		}
	}
}

// Written titles for the sources that turn up on this boat. The collision
// message shape is the prioritizer plugin's own (ADR 0057): "<NAME> - CPA
// WARNING" on a target, a GPS fault message on self.
func TestPresentNotificationWrittenTitles(t *testing.T) {
	cases := []struct{ path, message, title, body string }{
		{"navigation.closestApproach", "No GPS position received for more than 32 seconds", "Collision Watch Fault", "No GPS position received for more than 32 seconds."},
		{"navigation.anchor", "Anchor dragging!", "Anchor Alarm", "Anchor dragging."},
		{"received.distress.ais-2026-10-16T000510416Z-970028911", "AIS distress: SART active, 3.1 nautical miles south. Monitor channel 16.", "AIS Distress", "AIS distress: SART active, 3.1 nautical miles south. Monitor channel 16."},
	}
	for _, c := range cases {
		title, body := presentNotification(c.path, c.message)
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

func TestCollisionTargetName_ParsesWhatPresentCollisionWrites(t *testing.T) {
	_, body := presentCollision("WHITSUNDAY BULLET - CPA WARNING")
	if name, ok := collisionTargetName(body); !ok || name != "Whitsunday Bullet" {
		t.Errorf("%q parsed as %q, %v", body, name, ok)
	}
	_, body = presentCollision("<319123600> - CPA ALARM")
	if name, ok := collisionTargetName(body); ok {
		t.Errorf("%q is unnamed but parsed as %q", body, name)
	}
}

func TestCollisionTargetName_TitleCaseIsRuneSafe(t *testing.T) {
	_, body := presentCollision("ÉTOILE DU NORD - CPA WARNING")
	if name, ok := collisionTargetName(body); !ok || name != "Étoile Du Nord" {
		t.Errorf("got %q, %v", name, ok)
	}
}
