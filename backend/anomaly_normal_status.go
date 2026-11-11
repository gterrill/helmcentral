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
// under the one-minute bucket field in Settings, and each engine's and the house
// bank's readings with the switch (the ignored-sensor list) and the automatic
// verdict beside it.
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
	// Paths are all the paths the switch puts on or off the ignored list together,
	// for a reading that is the sum of several. Path is the first.
	Paths []string `json:"paths,omitempty"`
}

// normalStatusSubject is one engine or the house bank with its readings.
type normalStatusSubject struct {
	ID       string                `json:"id"`
	Name     string                `json:"name"`
	Bands    int                   `json:"bands"`
	Problem  string                `json:"problem,omitempty"`
	Readings []normalStatusReading `json:"readings"`
}

// normalStatusSubsystem is a group of subjects under its heading. Note says why
// it holds none when the boat has nothing of the kind set up.
type normalStatusSubsystem struct {
	ID       string                `json:"id"`
	Title    string                `json:"title"`
	Note     string                `json:"note,omitempty"`
	Subjects []normalStatusSubject `json:"subjects"`
}

type normalStatus struct {
	// Configured says InfluxDB is set up, so there is something to learn from.
	Configured bool `json:"configured"`
	// Summary says when and from where the normal was learned.
	Summary string `json:"summary,omitempty"`
	// Note is the finished sentence for what is wrong, if anything.
	Note       string                  `json:"note,omitempty"`
	Subsystems []normalStatusSubsystem `json:"subsystems"`
}

// buildNormalStatus is the status from the saved model and the settings.
func buildNormalStatus(file normalModelFile, fileErr error, srcErr error, ignored map[string]bool, vessel *vesselView, loc *time.Location) normalStatus {
	st := normalStatus{Configured: srcErr == nil, Subsystems: []normalStatusSubsystem{}}
	// vessel is nil when it could not be read, and the records are then all there is.
	if fileErr != nil {
		st.Note = unusualProblemNote(unusualUnreadable, firstErrorLine(fileErr))
		return st
	}
	have := false
	for _, model := range file.Subsystems {
		have = have || model.hasModel()
	}
	switch {
	case errors.Is(srcErr, errNormalNoInflux):
		st.Note = unusualProblemNote(unusualNoInflux, firstErrorLine(srcErr))
	case srcErr != nil:
		st.Note = unusualProblemNote(unusualError, firstErrorLine(srcErr))
	case len(file.Subsystems) == 0:
		st.Note = unusualProblemNote(unusualNeverTrained, "")
	}
	// With nothing learned and a reason that stops everything, that reason is all.
	if !have && (srcErr != nil || len(file.Subsystems) == 0) {
		return st
	}

	var learned time.Time
	var source normalSource
	for _, sub := range normalSubsystems() {
		model, recorded := file.Subsystems[sub.Name]
		if !recorded {
			// Something of this kind is set up and nothing has been attempted for it yet.
			if vessel != nil && len(sub.Subjects(*vessel)) > 0 {
				st.Subsystems = append(st.Subsystems, normalStatusSubsystem{ID: sub.Name, Title: sub.Title, Note: normalNoModelYet, Subjects: []normalStatusSubject{}})
			}
			continue
		}
		// Each subsystem says its own state under its own heading.
		group := normalStatusSubsystem{ID: sub.Name, Title: sub.Title, Subjects: []normalStatusSubject{}}
		switch {
		case model.State == normalStateSkipped:
			group.Note = endedSentence(model.Problem)
			// The boat now decides: with something of this kind set up, it is only
			// waiting to be learned.
			if vessel != nil && len(sub.Subjects(*vessel)) > 0 {
				group.Note = normalNoModelYet
			}
		case model.State == normalStateFailed && model.hasModel():
			group.Note = unusualTrainingNote(model.Problem)
		case model.State == normalStateFailed:
			group.Note = unusualProblemNote(unusualNoModel, model.Problem)
		}
		// A kept model is shown only while it is the model of what is set up now. With
		// nothing set up the reason says so, and with something else it is waiting to
		// be learned; the model stays in the file for the next run.
		if vessel != nil && model.hasModel() {
			if subjects := sub.Subjects(*vessel); len(subjects) == 0 {
				group.Note = endedSentence(sub.NoSubjects)
				model = normalSubsystemModel{}
			} else if !model.usableFor(subjects) {
				group.Note = normalNoModelYet
				if model.State == normalStateFailed && model.Problem != "" {
					group.Note = unusualProblemNote(unusualNoModel, model.Problem)
				}
				model = normalSubsystemModel{}
			}
		}
		if model.hasModel() && (learned.IsZero() || model.TrainedAt.Before(learned)) {
			learned, source = model.TrainedAt, model.Source
		}
		bands := model.bandsBySubject()
		for _, id := range slices.Sorted(maps.Keys(model.Subjects)) {
			sm := model.Subjects[id]
			e := normalStatusSubject{ID: id, Name: sm.Name, Bands: bands[id], Problem: sm.Problem, Readings: []normalStatusReading{}}
			for _, v := range statusRows(sm.Features) {
				if v.Placing || v.Component {
					continue
				}
				r := normalStatusReading{Key: v.Key, Path: v.Path, Name: normalFeatureName(v), On: !ignored[v.Path], Required: v.Required}
				if len(v.Paths) > 0 {
					// One switch for the parts: on only while none of them is switched off.
					r.Path, r.Paths = v.Paths[0], v.Paths
					for _, p := range v.Paths {
						r.On = r.On && !ignored[p]
					}
				}
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
			group.Subjects = append(group.Subjects, e)
		}
		st.Subsystems = append(st.Subsystems, group)
	}
	if loc == nil {
		loc = time.UTC
	}
	// Each subject's band count is on its heading, so it is not repeated here.
	if !learned.IsZero() {
		st.Summary = "Learned " + boatDayClock(learned, loc) + " from " + normalSourceText(source) + "."
	}
	return st
}

func normalStatusHandler(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	file, fileErr := loadNormalModel(anomalyNormalPath())
	_, srcErr := resolveNormalSource(settingsPath, false)
	var vessel *vesselView
	if v, err := normalPathVessel(); err == nil {
		vessel = &v
	}
	return c.JSON(http.StatusOK, buildNormalStatus(file, fileErr, srcErr, ignoredSensorSet(), vessel, boatTimeLastFix(false).Loc))
}

func anomalyNormalRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/anomaly/normal/status", tierRead, normalStatusHandler},
		{http.MethodPut, "/api/anomaly/normal/answers", tierWrite, func(c echo.Context) error {
			return normalAnswerHandler(productionNormalAnswerDeps(getEnv("SETTINGS_FILE", "../settings.yaml")))(c)
		}},
	}
}

// statusRows are the verdicts Settings shows, one for each reading. Of several
// paths for the same reading, the row is the one the model uses, else the one the
// operator switched off, else the first, so the switch acts on one path.
func statusRows(verdicts []normalFeatureVerdict) []normalFeatureVerdict {
	var out []normalFeatureVerdict
	for i, v := range verdicts {
		if v.Group == "" {
			out = append(out, v)
			continue
		}
		var members []int
		for j, w := range verdicts {
			if w.Group == v.Group {
				members = append(members, j)
			}
		}
		shown := members[0]
		for _, j := range members {
			if verdicts[j].Used {
				shown = j
				break
			}
			if verdicts[j].SwitchedOff && !verdicts[shown].Used && !verdicts[shown].SwitchedOff {
				shown = j
			}
		}
		if i == shown {
			// One switch for every path the reading is published under.
			v.Paths = nil
			for _, j := range members {
				v.Paths = append(v.Paths, verdicts[j].Path)
			}
			out = append(out, v)
		}
	}
	return out
}
