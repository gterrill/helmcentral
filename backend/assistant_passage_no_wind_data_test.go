package main

import (
	"context"
	"testing"
	"time"
)

// An hour the provider has no wind for decodes to speed 0 and direction 0. The
// passage check must see no hour there, not a calm from the north.
func noWindDataForecast() forecastSource {
	var hours []weatherHourPoint
	for t := pwDay(19, 0); !t.After(pwDay(23, 23)); t = t.Add(time.Hour) {
		hp := weatherHourPoint{Time: t.UTC()}
		if t.Before(pwDay(22, 19)) {
			hp.WindSpeedMS, hp.WindGustMS, hp.WindDirectionDeg = kn(5), kn(6), 180
		}
		hours = append(hours, hp)
	}
	wb := weatherForecastBundle{Hourly: hours}
	return forecastSource{Provider: "wx", Fetch: func(context.Context, float64, float64) (passageForecast, error) {
		return passageForecast{Hours: passageHoursFromBundles(wb, nil), FetchedAt: pwNow}, nil
	}}
}

func TestPassageHoursFromBundles_HourWithNoWindDataIsAbsent(t *testing.T) {
	tm := pwDay(20, 6).UTC()
	north := tm.Add(time.Hour)
	calm := tm.Add(2 * time.Hour)
	out := passageHoursFromBundles(weatherForecastBundle{Hourly: []weatherHourPoint{
		{Time: tm},
		{Time: north, WindSpeedMS: 6.1, WindDirectionDeg: 0},
		{Time: calm, WindSpeedMS: 0, WindDirectionDeg: 90},
	}}, nil)
	if h, ok := out[tm.Unix()]; ok {
		t.Errorf("speed 0 and direction 0 must be absent, got %+v", h)
	}
	if h, ok := out[north.Unix()]; !ok || h.WindMS != 6.1 || h.WindDirDeg != 0 {
		t.Errorf("wind from due north must stay, got %+v (present %v)", h, ok)
	}
	if _, ok := out[calm.Unix()]; !ok {
		t.Errorf("no wind with a direction is a reading and must stay")
	}
}

func TestAssessItinerary_LegIntoHoursWithNoWindDataIsNoData(t *testing.T) {
	fc := noWindDataForecast()
	early := pwArgs([]string{"2026-10-22"}, "06:00", pwNorthLeg())
	if c := pwCell0(t, pwAssess(t, early, pwLimitsAll(), fc)); c.State != "go" {
		t.Fatalf("a leg inside the wind data must be judged, got %+v", c)
	}
	late := pwArgs([]string{"2026-10-22"}, "20:00", pwNorthLeg())
	if c := pwCell0(t, pwAssess(t, late, pwLimitsAll(), fc)); c.State != "no_data" {
		t.Fatalf("a leg into hours with no wind data must be no_data, got %+v", c)
	}
}
