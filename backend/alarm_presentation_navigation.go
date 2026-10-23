package main

import "strings"

// The plotter's own route alarms, raised from the APB sentence. The sentence
// carries only two flags, so nothing in the notification names the waypoint or
// gives a distance; the body says what the flag says and no more. Titles are in
// writtenTitles.
var navigationNotificationBodies = map[string]string{
	"perpendicularPassed":  "The boat has passed the active waypoint.",
	"arrivalCircleEntered": "The boat is inside the active waypoint's arrival circle.",
}

// presentNavigationNotification words the plotter's route alarms, reporting
// whether path is one of them.
func presentNavigationNotification(path string) (title, body string, ok bool) {
	key := strings.TrimPrefix(path, "navigation.")
	body, ok = navigationNotificationBodies[key]
	if !ok {
		return "", "", false
	}
	return writtenTitles[key], body, true
}
