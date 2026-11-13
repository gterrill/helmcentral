package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// plannedStay is the stop of the current plan the boat is at or heading for,
// as Mate, the vessel status and the watch report read it. It is derived from
// the plans on every read; nothing stores it. Arrive and Depart are calendar
// days at the place (YYYY-MM-DD). PR 3 of the passage plans replaces this
// with the plans themselves.
type plannedStay struct {
	Name      string     `json:"name"`
	Lat       float64    `json:"lat"`
	Lon       float64    `json:"lon"`
	Arrive    string     `json:"arrive"`
	Depart    string     `json:"depart"`
	UpdatedAt *time.Time `json:"updated_at"`
	// Timezone is the IANA zone at the place (ADR 0230), worked out on every
	// read.
	Timezone string `json:"timezone"`
}

// stayZoneAt finds the zone at a stop's position. A variable so a test can
// make the lookup fail.
var stayZoneAt = zoneAt

// plannedStayNow is the clock plans are judged by; tests replace it.
var plannedStayNow = time.Now

// withZone sets Timezone from the stay's position. A position with no zone is
// an error, not UTC.
func (s plannedStay) withZone() (plannedStay, error) {
	z, err := stayZoneAt(s.Lat, s.Lon)
	if err != nil {
		return s, err
	}
	s.Timezone = z.Name
	return s, nil
}

// plannedStayActive reports whether the stay still counts at now. Its dates
// are the place's own, so "today" is the date at the place: it is gone the day
// after departure. A place whose zone cannot be found is an error.
func plannedStayActive(s plannedStay, now time.Time) (bool, error) {
	zone, err := stayZoneAt(s.Lat, s.Lon)
	if err != nil {
		return false, err
	}
	return s.Depart >= now.In(zone.Loc).Format(planDateLayout), nil
}

// GetPlannedStay returns the stop of the current plan: the stop the boat is at
// tonight (on a passage day, the destination), the first stop of an upcoming plan, or, when every plan
// is past, the last stop of the one that ended most recently (which
// plannedStayActive then reads as over). ok is false when there are no plans.
func (s *documentStore) GetPlannedStay() (plannedStay, bool, error) {
	plans, err := s.ListPlans()
	if err != nil {
		return plannedStay{}, false, err
	}
	if len(plans) == 0 {
		return plannedStay{}, false, nil
	}
	now := plannedStayNow()
	idx, ok, err := selectCurrentPlan(plans, now)
	if err != nil {
		return plannedStay{}, false, fmt.Errorf("get planned stay: %w", err)
	}
	if !ok {
		for i, p := range plans {
			_, last := planDates(p)
			if _, best := planDates(plans[idx]); last > best {
				idx = i
			}
		}
	}
	p := plans[idx]
	today, err := planToday(p, now)
	if err != nil {
		return plannedStay{}, false, fmt.Errorf("get planned stay: %w", err)
	}
	// The stop the boat spends tonight at: the first whose departure is after
	// today. On a passage day that is the destination, not the stop being left
	// (and a stop passed through the same day is skipped). After the last
	// departure, the last stop.
	stop := p.Stops[len(p.Stops)-1]
	for _, st := range p.Stops {
		if today < addPlanDays(st.Arrive, st.Nights) {
			stop = st
			break
		}
	}
	return plannedStay{
		Name:      stop.Name,
		Lat:       stop.Lat,
		Lon:       stop.Lon,
		Arrive:    stop.Arrive,
		Depart:    addPlanDays(stop.Arrive, stop.Nights),
		UpdatedAt: p.UpdatedAt,
	}, true, nil
}

type assistantPlannedStayResult struct {
	Stay *plannedStay `json:"stay"`
	Note string       `json:"note,omitempty"`
}

func (d assistantToolDeps) executeGetPlannedStay(ctx context.Context, _ json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store, err := d.recordStore("get_planned_stay")
	if err != nil {
		return "", err
	}
	stay, ok, err := store.GetPlannedStay()
	if err != nil {
		return "", fmt.Errorf("get_planned_stay: %w", err)
	}
	var res assistantPlannedStayResult
	active := false
	if ok {
		var zerr error
		if active, zerr = plannedStayActive(stay, d.now()); zerr != nil {
			return "", fmt.Errorf("get_planned_stay: the stop's time zone is not known: %w", zerr)
		}
	}
	switch {
	case !ok:
		res.Note = "No plan is saved."
	case !active:
		res.Note = "The last plan ended before today, so none is planned."
	default:
		withZone, zerr := stay.withZone()
		if zerr != nil {
			return "", fmt.Errorf("get_planned_stay: the stop's time zone is not known: %w", zerr)
		}
		res.Stay = &withZone
		res.Note = "The operator's current plan on the Forecast page is at or heading for this stop."
	}
	return capToolResultJSON(&res, nil)
}

var mateToolGetPlannedStay = mateTool{
	name:      "get_planned_stay",
	effect:    effectReads,
	audiences: audienceReviewsAndMCP,
	function: openRouterFunctionDef{
		Description: "Read the stop the operator's current passage plan has the boat at or heading for tonight (on a passage day, the destination where it spends the night): a place (name, lat, lon) " +
			"and the dates they arrive and depart, which the Forecast page shows the weather for. Returns no stay when no plan is saved or " +
			"the last one has ended. Use it when the operator talks about \"the marina\" or \"our stay\" or asks about weather or tides there; " +
			"pass its lat and lon to get_wind_forecast and get_tides. " + rulePlannedStay,
		Parameters: json.RawMessage(`{"type": "object", "properties": {}}`),
	},
	execute: plainTool(assistantToolDeps.executeGetPlannedStay),
	describe: func(args json.RawMessage) string {
		return "Reading the planned stay…"
	},
}
