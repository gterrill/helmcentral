package main

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

/*
Ignored network alarms.

A network alarm is a SignalK notification raised by another device on the
boat. A plotter's "perpendicular passed" stays lit for the whole leg and
nothing on the helm can be done about it, so the operator can stop showing
that kind of alarm. The kind is the notification path relative to the boat
("perpendicularPassed", "navigation.anchor"), whichever device raises it.

An ignored kind is dropped where signalKNotifications builds the list, so the
alarm board, banner, SSE stream, Mate, push and other notification
transports, and the alarm history all stop seeing it together. Nothing is
acknowledged on the SignalK server: the other devices keep their own alarm.

Safety alarms can never be ignored, and any alarm in the emergency state is
shown whatever its kind.
*/

// standardEmergencyKinds are the SignalK specification's emergency
// notifications.
var standardEmergencyKinds = []string{
	"mob", "fire", "sinking", "flooding", "collision",
	"grounding", "listing", "adrift", "piracy", "abandon",
}

// safetyKindPrefixes are kinds, and everything beneath them, that must always
// reach the crew.
var safetyKindPrefixes = append([]string{
	"navigation.anchor",
	"navigation.closestApproach",
}, standardEmergencyKinds...)

// alarmKindIgnorable reports whether the operator may stop showing an alarm of
// this kind in this state. It is the one place the safety list lives; the
// alarm card reads the answer from the alarm status rather than repeating it.
func alarmKindIgnorable(kind, state string) bool {
	if state == alarmStateEmergency {
		return false
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return false
	}
	for _, prefix := range safetyKindPrefixes {
		if kind == prefix || strings.HasPrefix(kind, prefix+".") {
			return false
		}
	}
	if strings.HasPrefix(kind, distressReceivedPrefix) || radarGuardZonePath.MatchString(kind) {
		return false
	}
	return true
}

// canonicalAlarmKind folds the plotter's route alarms, which a device may raise
// as either "perpendicularPassed" or "navigation.perpendicularPassed" and which
// are worded as one alarm, to the bare spelling. Every other kind, the
// navigation safety kinds among them, is left as it is.
func canonicalAlarmKind(kind string) string {
	bare := strings.TrimPrefix(kind, "navigation.")
	if _, ok := navigationNotificationBodies[bare]; ok {
		return bare
	}
	return kind
}

// alarmKindOf is the kind of a bus alarm, from its path ("notifications.x").
func alarmKindOf(path string) string {
	return canonicalAlarmKind(strings.TrimPrefix(path, notificationsRoot+"."))
}

// alarmKindHidden reports whether an alarm of this kind and state is hidden by
// the operator's list. A safety kind is never hidden, even if it somehow got
// onto the list, and an ignored kind in the emergency state shows.
func alarmKindHidden(kind, state string) bool {
	return alarmKindIgnorable(kind, state) && globalIgnoreStore.has(ignoredAlarmsList, canonicalAlarmKind(kind))
}

// ignoredAlarmEntry is one row of the Settings list.
type ignoredAlarmEntry struct {
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	IgnoredAt string `json:"ignored_at"`
}

func ignoredAlarmsResponse() map[string]any {
	entries := globalIgnoreStore.entries(ignoredAlarmsList)
	out := make([]ignoredAlarmEntry, 0, len(entries))
	for _, e := range entries {
		title, _ := presentNotification(e.Key, "", nil)
		out = append(out, ignoredAlarmEntry{Kind: e.Key, Title: title, IgnoredAt: e.IgnoredAt.Format(time.RFC3339)})
	}
	return map[string]any{"alarms": out}
}

// GET /api/alarms/ignored-alarms
func listIgnoredAlarmsHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, ignoredAlarmsResponse())
}

// POST /api/alarms/ignored-alarms — the alarm card's "Stop showing" action.
// Idempotent. A safety kind is refused: the card never offers it, so this is
// only reachable by a client that skipped the card.
func ignoreAlarmHandler(c echo.Context) error {
	var req struct {
		Kind string `json:"kind"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}
	kind := alarmKindOf(strings.TrimSpace(req.Kind))
	if kind == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "kind required"})
	}
	if !alarmKindIgnorable(kind, "") {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "a safety alarm cannot be ignored"})
	}
	if err := globalIgnoreStore.add(ignoredAlarmsList, kind, time.Now().UTC()); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, ignoredAlarmsResponse())
}

// DELETE /api/alarms/ignored-alarms/:kind — Restore.
func unignoreAlarmHandler(c echo.Context) error {
	// Echo leaves a path parameter percent-encoded; see unignoreSensorHandler.
	kind, err := url.PathUnescape(c.Param("kind"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "malformed kind"})
	}
	if err := globalIgnoreStore.remove(ignoredAlarmsList, alarmKindOf(strings.TrimSpace(kind))); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

// ignoredAlarmTitles is the operator-facing titles of the ignored kinds, for
// Mate to explain why an alarm it was not told about is not on the board.
func ignoredAlarmTitles() []string {
	entries := globalIgnoreStore.entries(ignoredAlarmsList)
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		title, _ := presentNotification(e.Key, "", nil)
		out = append(out, title)
	}
	return out
}
