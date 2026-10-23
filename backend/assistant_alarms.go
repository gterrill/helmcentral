package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// get_active_alarms (ADR 0175) hands Mate the alarm list the card and banner
// are built from. The card text names a device; the alarm itself carries the
// SignalK path behind it, which is what Mate must investigate.

type assistantAlarmSensor struct {
	Identifier string `json:"identifier"`
	Name       string `json:"name,omitempty"`
	Quantity   string `json:"quantity,omitempty"`
	Text       string `json:"text,omitempty"`
}

type assistantAlarm struct {
	Label        string                 `json:"label"`
	Message      string                 `json:"message"`
	State        string                 `json:"state"`
	Phase        string                 `json:"phase"`
	RaisedAt     string                 `json:"raised_at,omitempty"`
	AckedAt      string                 `json:"acked_at,omitempty"`
	Silenced     bool                   `json:"silenced"`
	RuleID       string                 `json:"rule_id"`
	Path         string                 `json:"path"`
	Value        *float64               `json:"value,omitempty"`
	Unit         string                 `json:"unit,omitempty"`
	Op           string                 `json:"op,omitempty"`
	Threshold    *float64               `json:"threshold,omitempty"`
	ClearValue   *float64               `json:"clear_value,omitempty"`
	Encounter    string                 `json:"encounter,omitempty"`
	RadarTarget  *alarmRadarTarget      `json:"radar_target,omitempty"`
	Evidence     string                 `json:"evidence,omitempty"`
	LiveEvidence string                 `json:"live_evidence,omitempty"`
	Sensors      []assistantAlarmSensor `json:"sensors,omitempty"`
	LiveSensors  []assistantAlarmSensor `json:"live_sensors,omitempty"`

	// LiveSameAsRaised stands in for live_sensors when the sensors failing now
	// are exactly those listed in sensors, so a missing live_sensors never has
	// to mean two things.
	LiveSameAsRaised bool `json:"live_same_as_raised,omitempty"`
	// Investigate is what to pass to the history tools for this alarm: the
	// bus path without its notifications. prefix, the rule's watched path, or
	// a sensor-health alarm's sensor identifiers (a SignalK path, or a $source
	// id for a silent source).
	Investigate []string `json:"investigate,omitempty"`
	// SensorCount, LiveSensorCount and InvestigateCount are the full length of
	// a list that was cut to fit, and are absent when the list is whole.
	SensorCount      int `json:"sensor_count,omitempty"`
	LiveSensorCount  int `json:"live_sensor_count,omitempty"`
	InvestigateCount int `json:"investigate_count,omitempty"`
}

type assistantActiveAlarmsResult struct {
	Count     int    `json:"count"`
	Shown     int    `json:"shown,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Timezone  string `json:"timezone,omitempty"`
	// TimezoneNote is set when the boat's time zone could not be read and the
	// times are UTC, so Mate does not present them as local.
	TimezoneNote string           `json:"timezone_note,omitempty"`
	Alarms       []assistantAlarm `json:"alarms"`
	// IgnoredAlarms are the titles of the network alarms the operator has
	// stopped showing. They are absent from alarms, from the alarm history and
	// from every notification, which is why Mate was not told about them.
	// Mate can read this list and has no tool to change it.
	IgnoredAlarms []string `json:"ignored_alarms,omitempty"`
}

func assistantAlarmSensors(in []sensorHealthEntry) []assistantAlarmSensor {
	var out []assistantAlarmSensor
	for _, e := range in {
		out = append(out, assistantAlarmSensor{Identifier: e.Identifier, Name: e.Name, Quantity: e.Quantity, Text: e.Text})
	}
	return out
}

func (d assistantToolDeps) executeGetActiveAlarms(ctx context.Context, _ json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if d.activeAlarms == nil {
		return "", fmt.Errorf("get_active_alarms: the alarm list is not available")
	}
	alarms := d.activeAlarms()
	result := assistantActiveAlarmsResult{Alarms: make([]assistantAlarm, 0, len(alarms)), IgnoredAlarms: ignoredAlarmTitles()}

	// The vessel's position is a SignalK round trip, needed only to turn
	// timestamps into boat time. Without a timestamp to format, skip it.
	loc := time.UTC
	for _, a := range alarms {
		if a.RaisedAt.IsZero() && a.AckedAt.IsZero() {
			continue
		}
		result.Timezone = "UTC"
		var state vesselStateData
		var verr error
		if d.vesselState == nil {
			verr = fmt.Errorf("vessel state is not wired")
		} else if state, verr = d.vesselState(); verr == nil && !hasUsableVesselPosition(state.Latitude, state.Longitude) {
			verr = fmt.Errorf("no usable vessel position")
		}
		if verr != nil {
			log.Printf("get_active_alarms: vessel time zone unavailable, times are UTC: %v", verr)
			result.TimezoneNote = fmt.Sprintf("vessel time zone unavailable: %v; these times are UTC, not boat time", verr)
		} else {
			loc = vesselLocalLocation(state.Longitude)
			result.Timezone = assistantTimeZoneLabel(state.Longitude)
		}
		break
	}

	for _, a := range alarms {
		item := assistantAlarm{
			Label: a.Label, Message: a.Message, State: a.State, Phase: string(a.Phase),
			Silenced: a.Silenced, RuleID: a.RuleID, Path: a.Path, Unit: a.Unit,
			Op: a.Op, Threshold: a.Threshold, ClearValue: a.ClearValue,
			Encounter: a.Encounter, RadarTarget: a.RadarTarget,
			Evidence: a.Evidence, LiveEvidence: a.LiveEvidence,
			Sensors: assistantAlarmSensors(a.Sensors), LiveSensors: assistantAlarmSensors(a.LiveSensors),
			Investigate: d.alarmInvestigateTargets(a),
		}
		// A reading belongs to a rule this engine holds. Bus notifications
		// carry no op or threshold, and a sensor-health alarm's value is a
		// bare count of failing sensors; neither has a reading worth quoting.
		if a.Threshold != nil && !sensorHealthCountPaths[a.Path] {
			v := a.Value
			item.Value = &v
		}
		if !a.RaisedAt.IsZero() {
			item.RaisedAt = a.RaisedAt.In(loc).Format("Mon 2 Jan 15:04")
		}
		if !a.AckedAt.IsZero() {
			item.AckedAt = a.AckedAt.In(loc).Format("Mon 2 Jan 15:04")
		}
		result.Alarms = append(result.Alarms, item)
	}
	result.Count = len(result.Alarms)

	// A live list that repeats the raised one is always replaced by the
	// marker, so the shape does not depend on how big the result is.
	markLiveSameAsRaised(result.Alarms)

	// Each step shrinks in bulk. List cuts are signalled by the per-alarm
	// counts; truncated and shown are set only when alarms are dropped, and
	// are set inside the step so the size check counts them.
	return capToolResultJSON(&result, func() bool {
		if capLongestAlarmLists(result.Alarms) {
			return true
		}
		if len(result.Alarms) > 1 {
			result.Alarms = result.Alarms[:(len(result.Alarms)+1)/2]
			result.Truncated = true
			result.Shown = len(result.Alarms)
			return true
		}
		return false
	})
}

// alarmInvestigateTargets names what the history tools can be asked about for
// an alarm. A bus notification's path is "notifications.<data path>"; the data
// path is what check_signalk_paths and get_path_history take, when it has
// data, and nothing otherwise. A rule alarm
// watches its Path directly. A sensor-health alarm's Path is a derived count,
// so its sensors are what to look at, raised ones first then any failing now.
func (d assistantToolDeps) alarmInvestigateTargets(a alarmStatus) []string {
	if sensorHealthCountPaths[a.Path] {
		var out []string
		seen := map[string]bool{}
		for _, list := range [][]sensorHealthEntry{a.Sensors, a.LiveSensors} {
			for _, e := range list {
				if e.Identifier != "" && !seen[e.Identifier] {
					seen[e.Identifier] = true
					out = append(out, e.Identifier)
				}
			}
		}
		return out
	}
	if rest, ok := strings.CutPrefix(a.Path, notificationsRoot+"."); ok {
		// Many bus alarms (collision urns, vebus.lowBattery, radar guard
		// zones) name a notification, not a path that carries data. Name the
		// path only when the live tree has a value there.
		if d.pathHasData != nil && d.pathHasData(rest) {
			return []string{rest}
		}
		return nil
	}
	if a.Path == "" {
		return nil
	}
	return []string{a.Path}
}

// markLiveSameAsRaised replaces a live_sensors list that repeats sensors with
// an explicit marker.
func markLiveSameAsRaised(alarms []assistantAlarm) {
	for i := range alarms {
		a := &alarms[i]
		if len(a.LiveSensors) > 0 && sameAlarmSensors(a.Sensors, a.LiveSensors) {
			a.LiveSensors = nil
			a.LiveSameAsRaised = true
		}
	}
}

// capLongestAlarmLists finds the longest sensor or investigate list across
// all alarms and cuts every list longer than half of it down to that half
// (rounded up), recording the full length the first time a list is cut. The
// biggest lists shrink first, so a short list keeps its identifiers while a
// long one still fits. It reports whether it cut anything.
func capLongestAlarmLists(alarms []assistantAlarm) bool {
	longest := 0
	for i := range alarms {
		a := &alarms[i]
		longest = max(longest, len(a.Sensors), len(a.LiveSensors), len(a.Investigate))
	}
	if longest <= 1 {
		return false
	}
	limit := (longest + 1) / 2
	for i := range alarms {
		a := &alarms[i]
		if n := len(a.Sensors); n > limit {
			if a.SensorCount == 0 {
				a.SensorCount = n
			}
			a.Sensors = a.Sensors[:limit]
		}
		if n := len(a.LiveSensors); n > limit {
			if a.LiveSensorCount == 0 {
				a.LiveSensorCount = n
			}
			a.LiveSensors = a.LiveSensors[:limit]
		}
		if n := len(a.Investigate); n > limit {
			if a.InvestigateCount == 0 {
				a.InvestigateCount = n
			}
			a.Investigate = a.Investigate[:limit]
		}
	}
	return true
}

func sameAlarmSensors(a, b []assistantAlarmSensor) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var mateToolGetActiveAlarms = mateTool{
	name: "get_active_alarms",
	function: openRouterFunctionDef{
		Description: "List the alarms active on the boat right now, worst first. Each has its label and " +
			"message, state, when it was raised and acknowledged (boat local time, see timezone; if " +
			"timezone_note is set the times are UTC, say so), the rule id and watched path, and where the rule " +
			"has them the reading, threshold and clear point; collision alarms add the encounter line and radar " +
			"target figures. investigate lists what to pass to the history tools for that alarm: a SignalK path, " +
			"or for a silent source a $source id. When it is empty the alarm has no sensor of its own to read " +
			"(a bus notification with no data behind it, such as a collision or radar guard zone alarm); say so " +
			"rather than guessing a path. Only sensor-health alarms (impossible reading, frozen, silent source) " +
			"list sensors: each sensor's identifier, name and quantity as it stood when the alarm was raised " +
			"(sensors) and now (live_sensors). For those, live_same_as_raised true means the sensors failing now " +
			"are the same as at raise, and live_sensors is left out; no live_sensors and no marker means " +
			"nothing is failing now. Rule, bus and collision alarms never carry live_sensors, so its absence " +
			"says nothing about them. sensor_count, live_sensor_count and investigate_count give the full " +
			"length of a list that was cut to fit. If truncated is true, alarms were left out: shown of count " +
			"are listed. Call this FIRST for any question about an alarm, then investigate what it returns, " +
			"never a path guessed from a device name. An empty list means nothing is alarming. ignored_alarms lists " +
			"the titles of network alarms the operator has chosen to stop showing; they are not in alarms or " +
			"the alarm history, so if asked why you did not hear of one, say it is on that list. You cannot " +
			"change the list; the operator restores a kind in Settings.",
		Parameters: json.RawMessage(`{"type": "object", "properties": {}}`),
	},
	execute: assistantToolDeps.executeGetActiveAlarms,
	describe: func(json.RawMessage) string {
		return "Checking the active alarms…"
	},
}
