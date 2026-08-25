package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

const (
	collisionPluginID         = "signalk-ais-target-prioritizer"
	collisionProfilesLoadPath = "/plugins/" + collisionPluginID + "/loadCollisionProfiles"
	collisionProfilesSavePath = "/plugins/" + collisionPluginID + "/saveCollisionProfiles"

	collisionProfileRuleID = "collision-profile"
	collisionProfileLabel  = "Collision profile"

	// collisionProfilePath is Helmcentral's own, alongside watchdogPath, and is
	// registered in ownedNotificationPaths so the bus watcher does not read it
	// back off the tree and raise it a second time.
	collisionProfilePath = "notifications.helmcentral.collisionProfile"
)

// vesselStateProfiles maps signalk-autostate's vocabulary onto the target
// prioritizer's profiles (ADR 0058).
//
// offshore is deliberately absent. Autostate has no notion of blue water, and
// inferring it from "moving" would be a guess dressed up as a decision, so it
// stays a manual choice.
var vesselStateProfiles = map[string]string{
	"anchored": "anchor",
	"moored":   "harbor",
	"sailing":  "coastal",
	"motoring": "coastal",
}

type collisionTier struct {
	Cpa   float64 `json:"cpa"`
	Tcpa  float64 `json:"tcpa"`
	Speed float64 `json:"speed"`
}

type collisionGuard struct {
	Range float64 `json:"range"`
	Speed float64 `json:"speed"`
}

type collisionProfile struct {
	Warning collisionTier  `json:"warning"`
	Danger  collisionTier  `json:"danger"`
	Guard   collisionGuard `json:"guard"`
}

// canFire reports whether any alarm in this profile is reachable at all.
//
// The plugin compares cpa < profile.cpa and range < profile.guard.range, so a
// profile whose thresholds are all zero can never trip: every comparison is
// against zero and fails. The shipped anchor profile is exactly this. It is not
// a quiet profile, it is a silent one, and adopting it on anchoring would take
// the collision alarm offline at the moment the boat is left unattended
// (ADR 0058).
//
// A guard ring on its own is a working profile, and is the shape ADR 0058
// actually recommends at anchor, so a zero CPA alone must not condemn it.
func (p collisionProfile) canFire() bool {
	return p.Warning.Cpa > 0 || p.Danger.Cpa > 0 || p.Guard.Range > 0
}

// parseCollisionProfiles reads the plugin's profile document, which is a flat
// object of profiles alongside a "current" naming the active one.
func parseCollisionProfiles(raw []byte) (string, map[string]collisionProfile, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		return "", nil, fmt.Errorf("could not read the collision profiles: %w", err)
	}

	rawCurrent, ok := document["current"]
	if !ok {
		return "", nil, fmt.Errorf("the collision profile document names no current profile")
	}
	var current string
	if err := json.Unmarshal(rawCurrent, &current); err != nil {
		return "", nil, fmt.Errorf("could not read the current collision profile: %w", err)
	}

	profiles := map[string]collisionProfile{}
	for name, rawProfile := range document {
		if name == "current" {
			continue
		}
		var profile collisionProfile
		// Keys that are not profiles are skipped rather than fatal: this is
		// another project's document and it is free to grow settings that are
		// none of our business. Only the names we actually select are ever
		// read, and those are validated before use.
		if err := json.Unmarshal(rawProfile, &profile); err != nil {
			continue
		}
		profiles[name] = profile
	}
	return current, profiles, nil
}

// collisionProfileSyncer keeps the target prioritizer's active profile in step
// with navigation.state (ADR 0058).
type collisionProfileSyncer struct {
	snapshot *signalKSnapshot

	// lastState is the navigation.state already acted on. The syncer is
	// edge-triggered on this rather than reconciling continuously, which is
	// what lets a manual override stick: under way the mapping says coastal, so
	// correcting every disagreement would stomp a deliberate switch to offshore
	// on the next tick and the setting would appear to refuse to hold.
	lastState string

	// refused is the profile a previous transition declined to select, held so
	// the raise can be cleared later. Without it the alarm log keeps an open
	// row for a problem that has since been fixed.
	refused string

	load func() (string, map[string]collisionProfile, error)
	save func(profile string) error
}

func newCollisionProfileSyncer(snapshot *signalKSnapshot) *collisionProfileSyncer {
	return &collisionProfileSyncer{
		snapshot: snapshot,
		load:     loadCollisionProfiles,
		save:     saveCollisionProfile,
	}
}

func (s *collisionProfileSyncer) check(now time.Time) []alarmEvent {
	state := vesselNavigationState(s.snapshot)
	if state == "" || state == s.lastState {
		return nil
	}
	// Recorded before the work below, so a refusal is reported once for the
	// transition rather than retried on every tick for as long as it lasts.
	s.lastState = state

	want, mapped := vesselStateProfiles[state]
	if !mapped {
		return nil
	}

	current, profiles, err := s.load()
	if err != nil {
		return s.refuse(want, fmt.Sprintf("could not read the collision profiles: %v", err))
	}
	if current == want {
		return s.resolve()
	}

	profile, defined := profiles[want]
	if !defined {
		return s.refuse(want, fmt.Sprintf(
			"the %q collision profile is not defined, and selecting it would stop the plugin evaluating any target", want))
	}
	if !profile.canFire() {
		return s.refuse(want, fmt.Sprintf(
			"the %q collision profile has no reachable alarm (every CPA and guard range is zero), so selecting it would silence AIS collision alarms", want))
	}
	if err := s.save(want); err != nil {
		return s.refuse(want, fmt.Sprintf("could not select the %q collision profile: %v", want, err))
	}

	log.Printf("collision profile: vessel is %s, selected %q", state, want)
	return s.resolve()
}

// refuse leaves the plugin alone and raises Helmcentral's own alarm instead.
// Staying loud on the wrong profile is a worse night and a better outcome than
// going quiet on the right one.
func (s *collisionProfileSyncer) refuse(profile, message string) []alarmEvent {
	log.Printf("collision profile: refusing %q: %s", profile, message)
	s.refused = profile
	return []alarmEvent{collisionProfileEvent(alarmEventRaised, alarmStateWarn, message)}
}

func (s *collisionProfileSyncer) resolve() []alarmEvent {
	if s.refused == "" {
		return nil
	}
	s.refused = ""
	return []alarmEvent{collisionProfileEvent(alarmEventCleared, alarmStateNormal, "")}
}

func collisionProfileEvent(kind, state, message string) alarmEvent {
	phase := alarmPhaseActive
	if kind == alarmEventCleared {
		phase = alarmPhaseNormal
	}

	status := alarmStatus{
		RuleID:  collisionProfileRuleID,
		Label:   collisionProfileLabel,
		Path:    collisionProfilePath,
		Phase:   phase,
		State:   state,
		Message: message,
	}
	return alarmEvent{
		Kind: kind,
		Rule: alarmRule{
			ID:      collisionProfileRuleID,
			Enabled: true,
			Label:   collisionProfileLabel,
			Path:    collisionProfilePath,
			State:   state,
		},
		Status: status,
		Source: alarmSourceRule,
	}
}

// vesselNavigationState reads navigation.state, which signalk-autostate keeps
// current from anchor position, movement and engine state.
func vesselNavigationState(snapshot *signalKSnapshot) string {
	tree := snapshot.selfTree()
	if tree == nil {
		return ""
	}
	navigation, ok := tree["navigation"].(map[string]any)
	if !ok {
		return ""
	}
	node, ok := navigation["state"].(map[string]any)
	if !ok {
		return ""
	}
	state, _ := node["value"].(string)
	return state
}

func loadCollisionProfiles() (string, map[string]collisionProfile, error) {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		return "", nil, fmt.Errorf("could not read the SignalK connection settings: %w", err)
	}

	status, body, err := signalkRequestJSONWithAuthBody(
		buildSignalKURL(address, port), settingsPath,
		collisionProfilesLoadPath, http.MethodGet, nil,
	)
	if err != nil {
		return "", nil, err
	}
	if status < 200 || status >= 300 {
		return "", nil, fmt.Errorf("signalk returned status %d: %s", status, string(body))
	}
	return parseCollisionProfiles(body)
}

// saveCollisionProfile switches the active profile and nothing else.
//
// The plugin's save handler is Object.assign(collisionProfiles, req.body), a
// shallow merge, so a body of exactly {"current": name} leaves all four
// threshold sets untouched. Those are the operator's, informed by their own
// waters (ADR 0058).
func saveCollisionProfile(profile string) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		return fmt.Errorf("could not read the SignalK connection settings: %w", err)
	}

	return signalkRequestJSONWithAuth(
		buildSignalKURL(address, port), settingsPath,
		collisionProfilesSavePath, http.MethodPut,
		map[string]string{"current": profile},
	)
}
