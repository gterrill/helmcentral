package main

import (
	"fmt"
	"time"
)

// planNextStop is the stop of the current plan the boat is at or heading for,
// as the vessel status and the watch report read it. It is derived from the
// plans on every read; nothing stores it. Arrive and Depart are calendar days at
// the place (YYYY-MM-DD). Mate reads the plans themselves with get_plans
// (ADR 0289).
type planNextStop struct {
	Name   string
	Lat    float64
	Lon    float64
	Arrive string
	Depart string
	// Timezone is the IANA zone at the place (ADR 0230), worked out on every
	// read.
	Timezone string
	// Active is whether the stop still counts: its dates are the place's own,
	// so it is gone the day after departure.
	Active bool
}

// GetPlanNextStop returns the stop of the current plan: the stop the boat is at
// tonight (on a passage day, the destination), the first stop of an upcoming plan, or, when every plan
// is past, the last stop of the one that ended most recently (which
// Active then reads as over). ok is false when there are no plans.
func (s *documentStore) GetPlanNextStop(now time.Time) (planNextStop, bool, error) {
	plans, err := s.ListPlans()
	if err != nil {
		return planNextStop{}, false, err
	}
	if len(plans) == 0 {
		return planNextStop{}, false, nil
	}
	decorated := make([]plan, 0, len(plans))
	for _, p := range plans {
		d, err := decoratePlan(p, now)
		if err != nil {
			return planNextStop{}, false, fmt.Errorf("get the plan's next stop: %w", err)
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
		z, err := planStopZone(p, i)
		if err != nil {
			return planNextStop{}, false, fmt.Errorf("get the plan's next stop: %w", err)
		}
		stop, zone = st, z
		if i == len(p.Stops)-1 || dayAt(z, now) < st.Depart {
			break
		}
	}
	return planNextStop{
		Name:     stop.Name,
		Lat:      stop.Lat,
		Lon:      stop.Lon,
		Arrive:   stop.Arrive,
		Depart:   stop.Depart,
		Timezone: zone.Name,
		Active:   stop.Depart >= dayAt(zone, now),
	}, true, nil
}
