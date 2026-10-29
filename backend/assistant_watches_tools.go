package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Mate's tools for watches (ADR 0227): list what the boat has, read what the
// runs found, and run one now. Creating, changing and deleting a watch go
// through propose_changes (the watch record type), so the operator's Apply tap
// is the consent.

type assistantWatchEntry struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Schedule         string          `json:"schedule"`
	ScheduleSentence string          `json:"schedule_sentence"`
	Sections         []string        `json:"sections"`
	Thresholds       watchThresholds `json:"thresholds"`
	LookOutFor       string          `json:"look_out_for"`
	Enabled          bool            `json:"enabled"`
	// Notify is where each report is sent and Send is when: "findings" for a
	// report only when it has a finding the last one sent did not, or "every".
	Notify          []string `json:"notify"`
	Send            string   `json:"send"`
	LastRun         string   `json:"last_run,omitempty"`
	LastRunFindings *int     `json:"last_run_findings,omitempty"`
	Version         string   `json:"version"`
}

type assistantListWatchesResult struct {
	Watches []assistantWatchEntry `json:"watches"`
	Total   int                   `json:"total"`
	Limit   int                   `json:"limit"`
	Note    string                `json:"note"`
}

func (d assistantToolDeps) executeListWatches(ctx context.Context, _ json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if d.documents == nil || d.documents() == nil {
		return "", errors.New("list_watches: the watches are not available")
	}
	result := assistantListWatchesResult{Watches: []assistantWatchEntry{}, Limit: maxScheduledWatches,
		Note: "These are the scheduled watches. Findings and Mate's reviews are on the Watches page."}
	err := d.documents().Read(func(q sqlQueryer) error {
		list, err := listSavedWatchesQ(q)
		if err != nil {
			return err
		}
		for _, w := range list {
			if w.Kind != watchKindScheduled {
				continue
			}
			e := assistantWatchEntry{
				ID: w.ID, Name: w.Name, Schedule: w.Schedule, ScheduleSentence: watchSentenceOrCron(w), Sections: w.Sections,
				Thresholds: w.Thresholds, LookOutFor: w.LookOutFor, Enabled: w.Enabled, Notify: w.Notify, Send: w.Send, Version: savedWatchVersion(w),
			}
			if w.LastRunAt != nil {
				e.LastRun = w.LastRunAt.UTC().Format(time.RFC3339)
			}
			last, err := lastRunSummaryQ(q, w.ID)
			if err != nil {
				return err
			}
			if last != nil {
				n := last.FindingCount
				e.LastRunFindings = &n
			}
			result.Watches = append(result.Watches, e)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("list_watches: %w", err)
	}
	result.Total = len(result.Watches)
	return capToolResultJSON(&result, func() bool { return false })
}

var mateToolListWatches = mateTool{
	name: "list_watches",
	function: openRouterFunctionDef{
		Description: "List the scheduled watches the boat has: id, name, schedule (cron, with its sentence), report sections, " +
			"thresholds, what to look out for, enabled, where its reports are sent (notify) and when (send), when it last ran and how many findings that run had. Call it before proposing a watch, " +
			"so you change the one that already does the job instead of adding a second, and when the operator asks what is being checked regularly. " +
			"At most 20 scheduled watches can exist.",
		Parameters: json.RawMessage(`{"type": "object", "properties": {}}`),
	},
	execute: assistantToolDeps.executeListWatches,
	describe: func(args json.RawMessage) string {
		return "Checking the watches…"
	},
}

const (
	assistantWatchRunsDefault = 3
	assistantWatchRunsMax     = 10
)

type assistantWatchRunsArgs struct {
	WatchID string `json:"watch_id"`
	RunID   string `json:"run_id"`
	Limit   int    `json:"limit"`
}

type assistantWatchRunsResult struct {
	Watch     string     `json:"watch,omitempty"`
	Runs      []watchRun `json:"runs"`
	Truncated bool       `json:"truncated,omitempty"`
}

func (d assistantToolDeps) executeGetWatchRuns(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if d.documents == nil || d.documents() == nil {
		return "", errors.New("get_watch_runs: the watches are not available")
	}
	var args assistantWatchRunsArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("get_watch_runs: parse arguments: %w", err)
	}
	if args.WatchID == "" && args.RunID == "" {
		return "", errors.New("get_watch_runs: give a watch_id (from list_watches) or a run_id")
	}
	limit := args.Limit
	if limit <= 0 {
		limit = assistantWatchRunsDefault
	}
	if limit > assistantWatchRunsMax {
		limit = assistantWatchRunsMax
	}
	var result assistantWatchRunsResult
	err := d.documents().Read(func(q sqlQueryer) error {
		if args.RunID != "" {
			run, err := watchRunAnyByIDQ(q, args.RunID)
			if err != nil {
				return err
			}
			if args.WatchID != "" && run.WatchID != args.WatchID {
				return errWatchRunNotFound
			}
			w, err := savedWatchByIDQ(q, run.WatchID)
			if err == nil {
				result.Watch = w.Name
			}
			result.Runs = []watchRun{run}
			return nil
		}
		w, err := savedWatchByIDQ(q, args.WatchID)
		if err != nil {
			return err
		}
		result.Watch = w.Name
		result.Runs, err = watchRunsQ(q, w.ID, limit, nil)
		return err
	})
	if errors.Is(err, errWatchNotFound) || errors.Is(err, errWatchRunNotFound) {
		return "", fmt.Errorf("get_watch_runs: %w; call list_watches for the ids", err)
	}
	if err != nil {
		return "", fmt.Errorf("get_watch_runs: %w", err)
	}
	return capToolResultJSON(&result, func() bool {
		if len(result.Runs) <= 1 {
			return false
		}
		result.Runs = result.Runs[:len(result.Runs)-1]
		result.Truncated = true
		return true
	})
}

var mateToolGetWatchRuns = mateTool{
	name: "get_watch_runs",
	function: openRouterFunctionDef{
		Description: "Read a watch's latest runs, newest first: when each ran, the report (figures by section), the findings the rules " +
			"reached and Mate's own review, whether it was skipped and why, what the review cost, and which channels the run was sent to (notified). Give watch_id for the latest runs " +
			"(limit 1 to 10, default 3), or run_id to read that one run, as when the operator asks about the run on their screen.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"watch_id": {"type": "string", "description": "The watch's id from list_watches."},
				"run_id": {"type": "string", "description": "One run's id, as in the screen context when the operator is looking at a run."},
				"limit": {"type": "integer", "minimum": 1, "maximum": 10, "description": "How many runs, newest first. Default 3."}
			}
		}`),
	},
	execute: assistantToolDeps.executeGetWatchRuns,
	describe: func(args json.RawMessage) string {
		return "Reading the watch's runs…"
	},
}

func (d assistantToolDeps) executeRunWatchNow(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if d.documents == nil || d.documents() == nil {
		return "", errors.New("run_watch_now: the watches are not available")
	}
	host := activeWatchHost
	if host == nil {
		return "", errors.New("run_watch_now: the watch scheduler is not running")
	}
	var args struct {
		WatchID string `json:"watch_id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || args.WatchID == "" {
		return "", errors.New("run_watch_now: give the watch_id from list_watches")
	}
	var w savedWatch
	if err := d.documents().Read(func(q sqlQueryer) error {
		var err error
		w, err = savedWatchByIDQ(q, args.WatchID)
		return err
	}); err != nil {
		if errors.Is(err, errWatchNotFound) {
			return "", fmt.Errorf("run_watch_now: %w; call list_watches for the ids", err)
		}
		return "", fmt.Errorf("run_watch_now: %w", err)
	}
	run, err := host.RunNow(ctx, w)
	if err != nil {
		return "", fmt.Errorf("run_watch_now: %w", err)
	}
	return capToolResultJSON(&assistantWatchRunsResult{Watch: w.Name, Runs: []watchRun{*run}}, func() bool { return false })
}

var mateToolRunWatchNow = mateTool{
	name: "run_watch_now",
	function: openRouterFunctionDef{
		Description: "Run a scheduled watch now and return the run: its report, the findings the rules reached and Mate's review of it. " +
			"Use it when the operator asks for a watch's report right away. The run is saved on the Watches page like any other. " +
			"It reads the boat and changes nothing.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {"watch_id": {"type": "string", "description": "The watch's id from list_watches."}},
			"required": ["watch_id"]
		}`),
	},
	execute: assistantToolDeps.executeRunWatchNow,
	describe: func(args json.RawMessage) string {
		return "Running the watch…"
	},
}
