package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// list_routines (ADR 0227): the routines the boat has, so Mate edits one
// rather than proposing a second.

type assistantRoutineEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Schedule  string `json:"schedule"`
	Cadence   string `json:"cadence"`
	DailyTime string `json:"daily_time,omitempty"`
	Focus     string `json:"focus"`
	Enabled   bool   `json:"enabled"`
	LastRun   string `json:"last_run,omitempty"`
	LastNote  string `json:"last_note,omitempty"`
	Version   string `json:"version"`
}

type assistantListRoutinesResult struct {
	Routines []assistantRoutineEntry `json:"routines"`
	Total    int                     `json:"total"`
	Limit    int                     `json:"limit"`
}

func (d assistantToolDeps) executeListRoutines(ctx context.Context, _ json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if d.documents == nil || d.documents() == nil {
		return "", errors.New("list_routines: the routines are not available")
	}
	var stored []mateRoutine
	if err := d.documents().Read(func(q sqlQueryer) error {
		var err error
		stored, err = listMateRoutinesQ(q)
		return err
	}); err != nil {
		return "", fmt.Errorf("list_routines: %w", err)
	}
	result := assistantListRoutinesResult{Routines: []assistantRoutineEntry{}, Limit: maxMateRoutines}
	for _, r := range stored {
		e := assistantRoutineEntry{
			ID: r.ID, Name: r.Name, Schedule: mateRoutineSchedule(r.Cadence, r.DailyTime), Cadence: r.Cadence,
			DailyTime: r.DailyTime, Focus: r.Focus, Enabled: r.Enabled, LastNote: r.LastNote, Version: mateRoutineVersion(r),
		}
		if r.LastRunAt != nil {
			e.LastRun = r.LastRunAt.UTC().Format(time.RFC3339)
		}
		result.Routines = append(result.Routines, e)
	}
	result.Total = len(result.Routines)
	return capToolResultJSON(&result, func() bool { return false })
}

var mateToolListRoutines = mateTool{
	name: "list_routines",
	function: openRouterFunctionDef{
		Description: "List the routines the boat has: id, name, schedule (every hour, or daily at a time on the boat's clock), focus, " +
			"enabled, when it last ran and the first line of what it found (or why it was skipped). Call it before proposing a routine, " +
			"so you change the one that already does the job instead of adding a second, and when the operator asks what Mate checks on its own. " +
			"At most 8 routines can exist.",
		Parameters: json.RawMessage(`{"type": "object", "properties": {}}`),
	},
	execute: assistantToolDeps.executeListRoutines,
	describe: func(args json.RawMessage) string {
		return "Checking the routines…"
	},
}
