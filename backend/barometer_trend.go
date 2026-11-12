package main

import (
	"math"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

const barometerTrendBucket = 5 * time.Minute

type barometerTrendPoint struct {
	Time       time.Time `json:"time"`
	PressurePa float64   `json:"pressure_pa"`
}

// barometerTrendResponse is the /api/barometer-trend body. Everything is in
// pascals; the dashboard converts to the operator's pressure unit. A boat
// with no barometer, or one that has stopped reporting, gets an empty list
// and nulls -- never a stand-in value.
type barometerTrendResponse struct {
	Points    []barometerTrendPoint `json:"points"`
	CurrentPa *float64              `json:"current_pa"`
	// ChangePa is last minus first over the recorded history in the window,
	// and ChangeSpanS is how many seconds that history covers (up to three
	// hours), so the tile can say "40m" rather than call it a 3h change.
	ChangePa    *float64 `json:"change_pa"`
	ChangeSpanS *float64 `json:"change_span_s"`
}

func barometerTrend(c echo.Context) error {
	return c.JSON(http.StatusOK, buildBarometerTrend(time.Now().UTC()))
}

// buildBarometerTrend buckets the last pressureTrendWindow of barometerHistory
// into 5-minute means. The history is only recorded while the reading is
// fresh (tracks.go), so a stale newest sample means the sensor has gone quiet
// and the answer is empty. The 3h change is pressureChange, the same
// gate environment.pressureChange3h uses.
func buildBarometerTrend(now time.Time) barometerTrendResponse {
	empty := barometerTrendResponse{Points: []barometerTrendPoint{}}

	samples := barometerHistory.since(now.Add(-pressureTrendWindow))
	if len(samples) == 0 || now.Sub(samples[len(samples)-1].Timestamp) > barometerMaxAge {
		return empty
	}

	buckets := bucketMeans(samples, barometerTrendBucket, 10)
	points := make([]barometerTrendPoint, len(buckets))
	for i, b := range buckets {
		points[i] = barometerTrendPoint{Time: b.Time, PressurePa: b.Mean}
	}

	current := samples[len(samples)-1].Value
	out := barometerTrendResponse{Points: points, CurrentPa: &current}
	if change, span, ok := pressureChange(samples); ok {
		spanS := math.Round(span)
		out.ChangePa = &change
		out.ChangeSpanS = &spanS
	}
	return out
}
