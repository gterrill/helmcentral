package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// The boat's calendar day (ADR 0230). Maintenance due dates and Mate's "today"
// are calendar days read on the boat's wall, so the server works them out from
// the boat's zone and the screen and Mate cannot disagree. A client never
// supplies the day: a device in another zone would move every due date.

var errBoatDayUnknown = errors.New("the boat has not reported a position yet, so today's date on the boat is not known")

// boatZoneUnreadable is a boat day that is unknown because the zone could not
// be worked out (a saved last-fix zone that is damaged or names a zone this
// build doesn't know), not because there has been no fix. It is still
// errBoatDayUnknown to errors.Is, and its words name the cause rather than the
// position.
type boatZoneUnreadable struct{ cause error }

func (e *boatZoneUnreadable) Error() string {
	return "the boat's time zone can't be worked out (" + firstErrorLine(e.cause) + "), so today's date on the boat is not known"
}
func (e *boatZoneUnreadable) Is(target error) bool { return target == errBoatDayUnknown }
func (e *boatZoneUnreadable) Unwrap() error        { return e.cause }

// boatCalendarDay is the calendar day (midnight UTC, the shape the status engine
// compares) that `now` falls on in the boat's zone. With no position fix ever
// seen there is no zone to read it in, and the day is not guessed.
func boatCalendarDay(bt boatTime, now time.Time) (time.Time, error) {
	if bt.NoFix {
		if bt.Err != nil {
			return time.Time{}, &boatZoneUnreadable{bt.Err}
		}
		return time.Time{}, errBoatDayUnknown
	}
	return civilDateUTC(now.In(bt.Loc)), nil
}

// boatDayAt is the boat's day from the scheduler's zone reading
// (watchBoatLocation), with the same reasons boatCalendarDay gives: a damaged
// or unknown saved zone is boatZoneUnreadable, anything else is no fix yet.
func boatDayAt(now time.Time, loc *time.Location, locErr error) (time.Time, error) {
	if locErr != nil {
		var bz *boatZoneProblem
		if errors.As(locErr, &bz) {
			return time.Time{}, &boatZoneUnreadable{bz.cause}
		}
		return time.Time{}, errBoatDayUnknown
	}
	if loc == nil {
		return time.Time{}, errBoatDayUnknown
	}
	return civilDateUTC(now.In(loc)), nil
}

// boatToday is the boat's day now, the day every endpoint that judges a due
// date works against. A var so a test can fix it.
var boatToday = func() (time.Time, error) {
	state, err := fetchSignalKVesselState()
	return boatCalendarDay(boatTimeForState(state, err), time.Now())
}

// writeBoatDayError answers a request that needs the boat's day when it is not
// known: 503, with the reason.
func writeBoatDayError(c echo.Context, err error) error {
	return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
}
