package main

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// jobsCountHandler is GET /api/inventory/jobs/count, the number on the Jobs
// row's badge (ADR 0293): the open jobs plus the services overdue or due soon
// that nobody has acknowledged, for items not in storage. It runs the same
// reads as the two lists, so what the badge counts is what the page lists.
//
// Incomplete is true when some item's schedule could not be read (its profile
// is missing or invalid), so its services were not judged and the count may be
// short. A failed read is an HTTP error, never a 0. The boat's day is the
// server's own (boat_today.go); a ?today= from the client is ignored.
func jobsCountHandler(c echo.Context) error {
	today, err := boatToday()
	if err != nil {
		return writeBoatDayError(c, err)
	}
	jobs, err := globalDocumentStore.ListOpenJobs(openJobFilter{Status: openJobOpen})
	if err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	views, _, scheduleErrors, err := maintenanceRuleViews(maintenanceRuleFilter{}, today)
	if err != nil {
		return writeDocumentError(c, err)
	}
	count := len(jobs)
	for _, v := range views {
		if (v.Status == string(maintenanceStatusOverdue) || v.Status == string(maintenanceStatusDueSoon)) && !v.Acknowledged {
			count++
		}
	}
	return c.JSON(http.StatusOK, map[string]any{"count": count, "incomplete": len(scheduleErrors) > 0})
}
