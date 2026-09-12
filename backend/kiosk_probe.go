package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

// kioskProbeMaxChecks and kioskProbeMaxFieldLen bound the request body so a
// misbehaving or malicious kiosk browser can't grow the backend log
// unbounded from a single POST.
const (
	kioskProbeMaxChecks   = 32
	kioskProbeMaxFieldLen = 512
)

// kioskProbeCheck is one row of frontend/public/kiosk-probe.html's
// self-test (WebGL2, :has(), structuredClone, and so on).
type kioskProbeCheck struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail"`
}

// kioskProbeRequest is the body POSTed by kiosk-probe.html.
type kioskProbeRequest struct {
	UserAgent string `json:"user_agent"`
	Viewport  struct {
		W int `json:"w"`
		H int `json:"h"`
	} `json:"viewport"`
	Rotate int `json:"rotate"`
	// Height mirrors the kiosk route's own ?height=<px> option (ADR 0089):
	// present only when the probe URL carried a valid height, so a pointer
	// distinguishes "not set" from the zero value rather than guessing from
	// a magic number.
	Height *int              `json:"height,omitempty"`
	Checks []kioskProbeCheck `json:"checks"`
}

// kioskProbeHandler logs a wall-display kiosk browser's capability probe
// (frontend/public/kiosk-probe.html) to the backend's log instead of
// rendering it on-screen: the kiosk is a 1920x360 strip with no keyboard,
// mouse, or scroll, so a report of more than a handful of lines in 48px
// text can't be read on the device itself. The operator instead reads the
// result with `docker compose logs backend | grep 'kiosk probe'`
// (docs/how-to/set-up-a-wall-display.md).
//
// This is a public-tier route: the kiosk device may never sign in, and the
// endpoint's only effect is writing a log line, so there is nothing here
// worth gating behind a session.
func kioskProbeHandler(c echo.Context) error {
	var req kioskProbeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}

	if len(req.Checks) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "at least one check is required"})
	}
	if len(req.Checks) > kioskProbeMaxChecks {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("too many checks (max %d)", kioskProbeMaxChecks),
		})
	}
	if len(req.UserAgent) > kioskProbeMaxFieldLen {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("user_agent exceeds %d characters", kioskProbeMaxFieldLen),
		})
	}
	for _, check := range req.Checks {
		if len(check.Name) > kioskProbeMaxFieldLen || len(check.Detail) > kioskProbeMaxFieldLen {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("check name and detail must each be %d characters or fewer", kioskProbeMaxFieldLen),
			})
		}
	}

	if req.Height != nil {
		log.Printf("kiosk probe: ua=%q viewport=%dx%d rotate=%d height=%d", req.UserAgent, req.Viewport.W, req.Viewport.H, req.Rotate, *req.Height)
	} else {
		log.Printf("kiosk probe: ua=%q viewport=%dx%d rotate=%d", req.UserAgent, req.Viewport.W, req.Viewport.H, req.Rotate)
	}
	passed := 0
	for _, check := range req.Checks {
		status := "FAIL"
		if check.Pass {
			status = "PASS"
			passed++
		}
		log.Printf("kiosk probe: [%s] %s: %s", status, check.Name, check.Detail)
	}
	log.Printf("kiosk probe: %d/%d passed", passed, len(req.Checks))

	return c.NoContent(http.StatusNoContent)
}
