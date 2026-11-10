package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// The saved learned normal: data/anomaly-normal.json. One file, keyed
// subsystem, then regime, so a new subsystem adds a key and reshapes nothing.
// Each regime carries its own list of readings, because a band can be fitted on
// fewer readings than the subject now uses (anomaly_normal_train.go).

func anomalyNormalPath() string {
	return cacheFilePath("ANOMALY_NORMAL_FILE", "data/anomaly-normal.json")
}

// normalModelMu serialises the trainer's read, change and write of the file.
var normalModelMu sync.Mutex

type normalWindow struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// normalAttempt is the last training run, kept whether or not it succeeded, so
// a failing run is visible beside the good model it left alone.
type normalAttempt struct {
	At      time.Time `json:"at"`
	OK      bool      `json:"ok"`
	Problem string    `json:"problem,omitempty"`
	// Source is what the run read from, so a restart can tell the settings are
	// unchanged since.
	Source normalSource `json:"source,omitempty"`
}

type normalShift struct {
	FromLearnedAt time.Time `json:"from_learned_at"`
	MeanDistance  float64   `json:"mean_distance"`
}

// normalRegime is one subject's one operating regime. The learned figures sit
// in the embedded normalRegimeStats and are flattened into the JSON.
type normalRegime struct {
	Subject         string       `json:"subject"`
	Label           string       `json:"label"`
	LearnedAt       time.Time    `json:"learned_at"`
	Window          normalWindow `json:"window"`
	CarriedForward  bool         `json:"carried_forward"`
	Minutes         int          `json:"minutes"`
	Runs            int          `json:"runs"`
	ExcludedMinutes int          `json:"excluded_minutes,omitempty"`
	Judged          bool         `json:"judged"`
	Reason          string       `json:"reason,omitempty"`
	// Features are the readings, in order, that Mean, SD and Corr describe.
	Features []string `json:"features,omitempty"`
	// Joining are readings the subject now uses that this regime does not yet
	// include, and JoiningMinutes how many complete minutes it has with them.
	Joining        []string `json:"joining,omitempty"`
	JoiningMinutes int      `json:"joining_minutes,omitempty"`
	normalRegimeStats
	Shift *normalShift `json:"shift,omitempty"`
}

type normalSubjectModel struct {
	Name string `json:"name"`
	// GatedMinutes are the minutes this subject was learned from, after the
	// gate and the alarm exclusions.
	GatedMinutes int                    `json:"gated_minutes"`
	Features     []normalFeatureVerdict `json:"features"`
	// Problem says why nothing could be learned for the subject.
	Problem string `json:"problem,omitempty"`
}

// normalExcluded is a stretch of one subject's history taken out of training.
// PR 2 adds reasons ("labelled normal", "fault") with the same shape.
type normalExcluded struct {
	Subject string    `json:"subject"`
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
	Reason  string    `json:"reason"`
	Ref     string    `json:"ref,omitempty"`
	Title   string    `json:"title,omitempty"`
}

type normalSubsystemModel struct {
	TrainedAt time.Time                     `json:"trained_at"`
	Window    normalWindow                  `json:"window"`
	Source    normalSource                  `json:"source"`
	Subjects  map[string]normalSubjectModel `json:"subjects"`
	Regimes   map[string]normalRegime       `json:"regimes"`
	Excluded  []normalExcluded              `json:"excluded"`
}

type normalModelFile struct {
	LastAttempt *normalAttempt                  `json:"last_attempt,omitempty"`
	Subsystems  map[string]normalSubsystemModel `json:"subsystems"`
}

// normalNoModelYet is what the report, the tool and Settings say before the
// first run has learned anything.
const normalNoModelYet = "No learned normal yet."

// bandsBySubject counts the judged regimes of each subject, by subject id.
func (m normalSubsystemModel) bandsBySubject() map[string]int {
	out := map[string]int{}
	for _, r := range m.Regimes {
		if r.Judged {
			out[r.Subject]++
		}
	}
	return out
}

// judgedRegimes counts the regimes a subsystem can score against.
func (m normalSubsystemModel) judgedRegimes() int {
	n := 0
	for _, c := range m.bandsBySubject() {
		n += c
	}
	return n
}

// loadNormalModel reads the file. A missing file is a boat that has not trained
// yet and loads empty. A file that exists and does not parse is corrupt, which
// is an error the report and the tool say, and never an empty model.
func loadNormalModel(path string) (normalModelFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return normalModelFile{Subsystems: map[string]normalSubsystemModel{}}, nil
		}
		return normalModelFile{}, fmt.Errorf("reading %s: %w", path, err)
	}
	var m normalModelFile
	if err := json.Unmarshal(raw, &m); err != nil {
		return normalModelFile{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	if m.Subsystems == nil {
		m.Subsystems = map[string]normalSubsystemModel{}
	}
	return m, nil
}

func saveNormalModel(path string, m normalModelFile) error {
	return writeJSONFileAtomic(path, m)
}
