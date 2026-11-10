package main

import (
	"errors"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/labstack/echo/v4"
)

// GET /api/anomaly/normal/status: what the learned normal holds, for the line
// under the one-minute bucket field in Settings, and each engine's readings
// with the switch (the ignored-sensor list) and the automatic verdict beside it.
// Every sentence is finished here, from the same wording the watch report uses.

type normalStatusReading struct {
	Key  string `json:"key"`
	Path string `json:"path"`
	Name string `json:"name"`
	// On is the switch: false while the reading is on the ignored-sensor list.
	On bool `json:"on"`
	// Required says the engine cannot be judged without the reading.
	Required bool   `json:"required"`
	Status   string `json:"status"`
}

type normalStatusEngine struct {
	ID       string                `json:"id"`
	Name     string                `json:"name"`
	Bands    int                   `json:"bands"`
	Problem  string                `json:"problem,omitempty"`
	Readings []normalStatusReading `json:"readings"`
}

type normalStatus struct {
	// Configured says InfluxDB is set up, so there is something to learn from.
	Configured bool `json:"configured"`
	// Summary says when and from where the normal was learned.
	Summary string `json:"summary,omitempty"`
	// Note is the finished sentence for what is wrong, if anything.
	Note    string               `json:"note,omitempty"`
	Engines []normalStatusEngine `json:"engines"`
}

// buildNormalStatus is the status from the saved model and the settings.
func buildNormalStatus(file normalModelFile, fileErr error, srcErr error, ignored map[string]bool, loc *time.Location) normalStatus {
	st := normalStatus{Configured: srcErr == nil, Engines: []normalStatusEngine{}}
	if fileErr != nil {
		st.Note = unusualProblemNote(unusualUnreadable, firstErrorLine(fileErr))
		return st
	}
	model, have := file.Subsystems[normalSubsystemEngines]
	failed := ""
	if file.LastAttempt != nil && !file.LastAttempt.OK {
		failed = file.LastAttempt.Problem
	}
	switch {
	case errors.Is(srcErr, errNormalNoInflux):
		st.Note = unusualProblemNote(unusualNoInflux, firstErrorLine(srcErr))
	case srcErr != nil:
		st.Note = unusualProblemNote(unusualError, firstErrorLine(srcErr))
	case !have && failed != "":
		st.Note = unusualProblemNote(unusualNoModel, failed)
	case !have:
		st.Note = unusualProblemNote(unusualNeverTrained, "")
	case failed != "":
		st.Note = unusualTrainingNote(failed)
	}
	if !have {
		return st
	}

	bands := model.bandsBySubject()
	for _, id := range slices.Sorted(maps.Keys(model.Subjects)) {
		sm := model.Subjects[id]
		e := normalStatusEngine{ID: id, Name: sm.Name, Bands: bands[id], Problem: sm.Problem, Readings: []normalStatusReading{}}
		for _, v := range sm.Features {
			r := normalStatusReading{Key: v.Key, Path: v.Path, Name: normalFeatureName(v), On: !ignored[v.Path], Required: v.Required}
			switch {
			case !r.On:
				r.Status = "switched off"
			case !v.Used && v.SwitchedOff:
				r.Status = "switched on, used from the next update"
			default:
				r.Status = v.Status()
			}
			e.Readings = append(e.Readings, r)
		}
		st.Engines = append(st.Engines, e)
	}
	if loc == nil {
		loc = time.UTC
	}
	// Each engine's band count is on its heading, so it is not repeated here.
	st.Summary = "Learned " + boatDayClock(model.TrainedAt, loc) + " from " + normalSourceText(model.Source) + "."
	return st
}

func normalStatusHandler(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	file, fileErr := loadNormalModel(anomalyNormalPath())
	_, srcErr := resolveNormalSource(settingsPath, false)
	return c.JSON(http.StatusOK, buildNormalStatus(file, fileErr, srcErr, ignoredSensorSet(), boatTimeLastFix(false).Loc))
}

func anomalyNormalRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/anomaly/normal/status", tierRead, normalStatusHandler},
	}
}
