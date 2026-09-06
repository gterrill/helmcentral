package main

import (
	"math"
	"time"
)

// sunTimes computes sunrise and sunset for the civil day date represents, in
// date's own Location, at the given latitude/longitude (degrees, north and
// east positive). It follows NOAA's General Solar Position Calculations
// (https://gml.noaa.gov/grad/solcalc/solareqns.PDF): the Spencer (1971)
// Fourier series for the equation of time and solar declination from a
// fractional-year angle, then the hour angle at a solar zenith of 90.833
// degrees (the standard horizon correction: 90 degrees plus ~34 arcminutes
// of atmospheric refraction plus the sun's ~16 arcminute apparent radius).
//
// The calculation is anchored on local noon of the given civil day rather
// than iterating to convergence: the equation of time and declination move
// slowly enough within a single day that this is accurate to well under the
// plan's +-3 minute tolerance, and anchoring on local noon is what fixes
// which UTC calendar day the day-of-year figure and the returned instants
// are computed against.
//
// Returned instants are UTC. ok is false when the hour-angle cosine falls
// outside [-1, 1] - the sun does not cross the horizon that day (polar day
// if it never sets, polar night if it never rises), and there is nothing to
// report either way.
func sunTimes(date time.Time, lat, lon float64) (sunrise, sunset time.Time, ok bool) {
	loc := date.Location()
	localNoon := time.Date(date.Year(), date.Month(), date.Day(), 12, 0, 0, 0, loc)
	noonUTC := localNoon.UTC()

	dayOfYear := float64(noonUTC.YearDay())
	hourOfDay := float64(noonUTC.Hour()) + float64(noonUTC.Minute())/60 + float64(noonUTC.Second())/3600

	// Fractional year angle (radians), per NOAA's convention of measuring it
	// from Jan 1 at 00:00 (hence dayOfYear-1), with a small correction for
	// time-of-day.
	gamma := (2 * math.Pi / 365) * (dayOfYear - 1 + (hourOfDay-12)/24)

	// Equation of time, in minutes: the gap between apparent (sundial) and
	// mean solar time, caused by the Earth's elliptical orbit and axial
	// tilt.
	eqTimeMin := 229.18 * (0.000075 +
		0.001868*math.Cos(gamma) -
		0.032077*math.Sin(gamma) -
		0.014615*math.Cos(2*gamma) -
		0.040849*math.Sin(2*gamma))

	// Solar declination, in radians.
	decl := 0.006918 -
		0.399912*math.Cos(gamma) +
		0.070257*math.Sin(gamma) -
		0.006758*math.Cos(2*gamma) +
		0.000907*math.Sin(2*gamma) -
		0.002697*math.Cos(3*gamma) +
		0.00148*math.Sin(3*gamma)

	latRad := lat * math.Pi / 180
	const zenithDeg = 90.833
	zenithRad := zenithDeg * math.Pi / 180

	cosHA := math.Cos(zenithRad)/(math.Cos(latRad)*math.Cos(decl)) - math.Tan(latRad)*math.Tan(decl)
	if cosHA < -1 || cosHA > 1 {
		return time.Time{}, time.Time{}, false
	}
	haDeg := math.Acos(cosHA) * 180 / math.Pi

	// Local mean solar time runs ahead of UTC by longitude/15 hours (east
	// positive, per the standard geographic convention); apparent solar
	// noon additionally shifts by the equation of time. haDeg/15 converts
	// the hour angle (degrees of Earth rotation) to hours either side of
	// solar noon.
	solarNoonUTCHours := 12 - (lon/15 + eqTimeMin/60)
	sunriseHours := solarNoonUTCHours - haDeg/15
	sunsetHours := solarNoonUTCHours + haDeg/15

	// midnightUTC is the reference origin the two offsets above are hours
	// past - the UTC calendar day that local noon of the requested civil
	// day falls on. time.Time.Add handles an offset that rolls past either
	// midnight boundary (e.g. a sunrise before 00:00 UTC lands on the
	// previous UTC day) without any special-casing here.
	midnightUTC := time.Date(noonUTC.Year(), noonUTC.Month(), noonUTC.Day(), 0, 0, 0, 0, time.UTC)
	sunrise = midnightUTC.Add(time.Duration(sunriseHours * float64(time.Hour)))
	sunset = midnightUTC.Add(time.Duration(sunsetHours * float64(time.Hour)))
	return sunrise, sunset, true
}
