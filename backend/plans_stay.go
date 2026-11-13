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
	// Active is whether the stay still counts: its dates are the place's own,
	// so it is gone the day after departure.
	Active bool `json:"-"`
}

// GetPlannedStay returns the stop of the current plan: the stop the boat is at
// tonight (on a passage day, the destination), the first stop of an upcoming plan, or, when every plan
// is past, the last stop of the one that ended most recently (which
// Active then reads as over). ok is false when there are no plans.
func (s *documentStore) GetPlannedStay(now time.Time) (plannedStay, bool, error) {
	plans, err := s.ListPlans()
	if err != nil {
		return plannedStay{}, false, err
	}
	if len(plans) == 0 {
		return plannedStay{}, false, nil
	}
	decorated := make([]plan, 0, len(plans))
	for _, p := range plans {
		d, err := decoratePlan(p, now)
		if err != nil {
			return plannedStay{}, false, fmt.Errorf("get planned stay: %w", err)
		}
		decorated = append(decorated, d)
	}
	idx, _ := selectCurrentPlan(decorated)
	p := decorated[idx]
	// The stop the boat spends tonight at: the first whose departure is after
	// today. On a passage day that is the destination, not the stop being left
	// (and a stop passed through the same day is skipped). After the last
	// departure, the last stop. A stop's dates are its own place's, so each is
	// compared with today in that stop's zone.
	var stop planStop
	var zone placeZone
	for i, st := range p.Stops {
		z, err := stayZoneAt(st.Lat, st.Lon)
		if err != nil {
			return plannedStay{}, false, fmt.Errorf("get planned stay: the stop's time zone is not known: %w", err)
		}
		stop, zone = st, z
		if i == len(p.Stops)-1 || dayAt(z, now) < st.Depart {
			break
		}
	}
	return plannedStay{
		Name:      stop.Name,
		Lat:       stop.Lat,
		Lon:       stop.Lon,
		Arrive:    stop.Arrive,
		Depart:    stop.Depart,
		UpdatedAt: p.UpdatedAt,
		Timezone:  zone.Name,
		Active:    stop.Depart >= dayAt(zone, now),
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
	stay, ok, err := store.GetPlannedStay(d.now())
	if err != nil {
		return "", fmt.Errorf("get_planned_stay: %w", err)
	}
	var res assistantPlannedStayResult
	switch {
	case !ok:
		res.Note = "No plan is saved."
	case !stay.Active:
		res.Note = "The last plan ended before today, so none is planned."
	default:
		res.Stay = &stay
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
