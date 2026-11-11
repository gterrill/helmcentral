package main

import (
	"slices"
	"strings"
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
	// EquipmentIDs are the inventory items the subject was linked to when it was
	// learned, so a change of link is noticed (wakeNormalIfInputsChanged).
	EquipmentIDs []string `json:"equipment_ids,omitempty"`
	// GatedMinutes are the minutes this subject was learned from, after the
	// gate and the alarm exclusions.
	GatedMinutes int                    `json:"gated_minutes"`
	Features     []normalFeatureVerdict `json:"features"`
	// Problem says why nothing could be learned for the subject.
	Problem string `json:"problem,omitempty"`
}

// Why a stretch is in Excluded or Labelled.
const (
	normalReasonAlarm          = "alarm"
	normalReasonAnsweredReal   = "answered real"
	normalReasonAnsweredNormal = "answered normal"
	normalReasonRepair         = "repair"
)

// normalExcluded is a stretch of one subject's history. In Excluded it was taken
// out of training (an alarm, a stretch answered real, a repair's fault range). In
// Labelled it was kept even where the reweighting pass would have dropped it
// (a stretch answered normal).
type normalExcluded struct {
	Subject string    `json:"subject"`
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
	Reason  string    `json:"reason"`
	Ref     string    `json:"ref,omitempty"`
	Title   string    `json:"title,omitempty"`
}

// What the latest training run did for a subsystem.
const (
	normalStateTrained = "trained"
	// normalStateSkipped is a boat with nothing of the kind set up. It is not a
	// failure, and the subsystem holds no model.
	normalStateSkipped = "skipped"
	// normalStateFailed keeps the model from before, if there was one.
	normalStateFailed = "failed"
)

// normalSubjectInputs is what one subject was trained from: the items it is made
// of and the readings it was offered.
type normalSubjectInputs struct {
	Subject      string   `json:"subject"`
	EquipmentIDs []string `json:"equipment_ids,omitempty"`
	Paths        []string `json:"paths,omitempty"`
}

// normalInputsOf is what a subsystem's subjects are made of now, in a form that
// can be compared with the inputs of its last attempt.
func normalInputsOf(subjects []normalSubject) []normalSubjectInputs {
	var out []normalSubjectInputs
	for _, s := range subjects {
		in := normalSubjectInputs{Subject: s.ID, EquipmentIDs: slices.Clone(s.EquipmentIDs)}
		slices.Sort(in.EquipmentIDs)
		for _, f := range s.Features {
			in.Paths = append(in.Paths, f.Path)
		}
		slices.Sort(in.Paths)
		out = append(out, in)
	}
	slices.SortFunc(out, func(a, b normalSubjectInputs) int { return strings.Compare(a.Subject, b.Subject) })
	return out
}

// normalSubsystemModel is one subsystem's saved record: its latest attempt, and
// the model from the last run that learned something.
type normalSubsystemModel struct {
	// State, AttemptedAt, Problem and Inputs are the latest attempt. Problem is why
	// it failed, or why it was skipped. Inputs are compared with the boat to see
	// whether training has anything new to learn from. A record saved before these
	// existed has no State and reads as trained.
	State       string                `json:"state,omitempty"`
	AttemptedAt time.Time             `json:"attempted_at,omitempty"`
	Problem     string                `json:"problem,omitempty"`
	Inputs      []normalSubjectInputs `json:"inputs,omitempty"`
	// ModelInputs are what the model below was learned from. A failed or skipped
	// attempt keeps the model and these, and the model is used only while they are
	// what the boat has.
	ModelInputs []normalSubjectInputs `json:"model_inputs,omitempty"`

	TrainedAt time.Time                     `json:"trained_at"`
	Window    normalWindow                  `json:"window"`
	Source    normalSource                  `json:"source"`
	Subjects  map[string]normalSubjectModel `json:"subjects"`
	Regimes   map[string]normalRegime       `json:"regimes"`
	Excluded  []normalExcluded              `json:"excluded"`
	// Labelled are the stretches the operator answered normal. They stay in
	// training, so they are not Excluded, whose entries all mean "taken out".
	Labelled []normalExcluded `json:"labelled"`
}

// usableFor says the model was learned from the subjects the boat has now. A model
// saved without its inputs is taken as it stands.
func (m normalSubsystemModel) usableFor(subjects []normalSubject) bool {
	return len(m.ModelInputs) == 0 || sameNormalInputs(normalInputsOf(subjects), m.ModelInputs)
}

// hasModel says the record holds something learned.
func (m normalSubsystemModel) hasModel() bool { return !m.TrainedAt.IsZero() }

type normalModelFile struct {
	Subsystems map[string]normalSubsystemModel `json:"subsystems"`
}

// normalNoModelYet is what the report, the tool and Settings say before the
// first run has learned anything.
const normalNoModelYet = "No learned normal yet."

// trainingProblem is why the latest attempt failed for a subsystem, or "" when it
// did not.
func (m normalModelFile) trainingProblem(subsystem string) string {
	if rec := m.Subsystems[subsystem]; rec.State == normalStateFailed {
		return rec.Problem
	}
	return ""
}

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
	m, _, err := readJSONFile[normalModelFile](path)
	if err != nil {
		return normalModelFile{}, err
	}
	if m.Subsystems == nil {
		m.Subsystems = map[string]normalSubsystemModel{}
	}
	return m, nil
}

func saveNormalModel(path string, m normalModelFile) error {
	return writeJSONFileAtomic(path, m)
}
