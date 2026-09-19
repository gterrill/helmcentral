package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

// displayProbeMaxChecks and displayProbeMaxFieldLen bound the request body so a
// misbehaving or malicious browser can't grow the backend log unbounded
// from a single POST.
const (
	displayProbeMaxChecks   = 32
	displayProbeMaxFieldLen = 512
	// The probe keeps only the last few presses on screen, so a report
	// carrying more than a handful is a client that is not the probe.
	displayProbeMaxKeys = 8
)

// displayProbeCheck is one row of frontend/public/display-probe.html's
// self-test (WebGL2, :has(), structuredClone, and so on).
type displayProbeCheck struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail"`
}

// displayProbeKey is one captured keydown from the probe's live key readout.
// What a television remote emits is not knowable off the device (ADR 0110
// section 12), and the wall display's step and pause keys have to be
// written against real codes rather than an assumed table. The readout is
// on screen for immediate feedback while pressing buttons; these lines put
// the same codes in the log, which is where every other probe result is
// read from and the only place a code can be copied out of.
type displayProbeKey struct {
	Key     string `json:"key"`
	Code    string `json:"code"`
	KeyCode int    `json:"keyCode"`
	At      string `json:"at"`
}

// displayProbeRequest is the body POSTed by display-probe.html.
type displayProbeRequest struct {
	UserAgent string `json:"user_agent"`
	Viewport  struct {
		W int `json:"w"`
		H int `json:"h"`
	} `json:"viewport"`
	Rotate int `json:"rotate"`
	// Height is the panel's real height when the probe URL carried one,
	// for a browser reporting a viewport taller than the screen it drives.
	// A pointer distinguishes "not set" from the zero value rather than
	// guessing from a magic number. The probe keeps this as a URL option
	// deliberately: it runs before any display record exists, and is how
	// the operator learns which numbers to enter on one (ADR 0110).
	Height *int                `json:"height,omitempty"`
	Checks []displayProbeCheck `json:"checks"`
	// Keys is absent or empty on a device with no input at all, which is
	// the normal case for the flybridge strip.
	Keys []displayProbeKey `json:"keys,omitempty"`
}

// displayProbeHandler logs a wall display browser's capability probe
// (frontend/public/display-probe.html) to the backend's log instead of
// rendering it on-screen: a wall screen has no keyboard, mouse, or scroll,
// and the narrowest one this project serves is a 1920x360 strip, so a
// report of more than a handful of lines can't be read on the device
// itself. The operator instead reads the result with
// `docker compose logs helmcentral | grep 'display probe'`
// (docs/how-to/set-up-a-wall-display.md). The exception is the captured
// key lines, which are also on screen because pressing a remote button and
// looking up is the whole point of that readout.
//
// This is a public-tier route: the screen may never sign in, and the
// endpoint's only effect is writing a log line, so there is nothing here
// worth gating behind a session.
func displayProbeHandler(c echo.Context) error {
	var req displayProbeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}

	if len(req.Checks) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "at least one check is required"})
	}
	if len(req.Checks) > displayProbeMaxChecks {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("too many checks (max %d)", displayProbeMaxChecks),
		})
	}
	if len(req.UserAgent) > displayProbeMaxFieldLen {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("user_agent exceeds %d characters", displayProbeMaxFieldLen),
		})
	}
	for _, check := range req.Checks {
		if len(check.Name) > displayProbeMaxFieldLen || len(check.Detail) > displayProbeMaxFieldLen {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("check name and detail must each be %d characters or fewer", displayProbeMaxFieldLen),
			})
		}
	}
	if len(req.Keys) > displayProbeMaxKeys {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("too many keys (max %d)", displayProbeMaxKeys),
		})
	}
	for _, key := range req.Keys {
		if len(key.Key) > displayProbeMaxFieldLen || len(key.Code) > displayProbeMaxFieldLen || len(key.At) > displayProbeMaxFieldLen {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("key, code and at must each be %d characters or fewer", displayProbeMaxFieldLen),
			})
		}
	}

	if req.Height != nil {
		log.Printf("display probe: ua=%q viewport=%dx%d rotate=%d height=%d", req.UserAgent, req.Viewport.W, req.Viewport.H, req.Rotate, *req.Height)
	} else {
		log.Printf("display probe: ua=%q viewport=%dx%d rotate=%d", req.UserAgent, req.Viewport.W, req.Viewport.H, req.Rotate)
	}
	passed := 0
	for _, check := range req.Checks {
		status := "FAIL"
		if check.Pass {
			status = "PASS"
			passed++
		}
		log.Printf("display probe: [%s] %q: %q", status, check.Name, check.Detail)
	}
	log.Printf("display probe: %d/%d passed", passed, len(req.Checks))
	// Newest first, as the probe itself lists them. No line at all when
	// nothing was pressed: the strip has no input device and reports every
	// 60 seconds forever, so an empty-list line would be pure noise in the
	// one log an operator greps.
	for _, key := range req.Keys {
		log.Printf("display probe: key at=%q key=%q code=%q keyCode=%d", key.At, key.Key, key.Code, key.KeyCode)
	}

	return c.NoContent(http.StatusNoContent)
}
