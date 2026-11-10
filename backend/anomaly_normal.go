package main

import (
	"fmt"
	"math"
	"sort"
	"time"

	"gonum.org/v1/gonum/mat"
	"gonum.org/v1/gonum/stat/distuv"
)

/*
The learned normal (ADR 0276): for each subject of a subsystem (an engine), the
mean and covariance of its valid readings in each operating regime (a 200 rpm
band). A minute is scored by its Mahalanobis distance within its regime, and the
reading that adds most to the distance explains it. This file is the generic
core. A subsystem is a feature list, a regime-labelling function and a gate; the
engines are the first (anomaly_normal_engines.go). Nothing here names an engine,
a path or a boat.
*/

const (
	// normalTrainWindow is how much history a training run reads. Long enough to
	// hold every band an engine is used in over a season, short enough that a
	// repaired sender or a new propeller is the normal within a quarter.
	normalTrainWindow = 90 * 24 * time.Hour
	// normalMinMinutes and normalMinRuns are what a regime needs before it is
	// judged: an hour of minutes from at least two separate runs, so one long
	// passage at one setting is not taken for the engine's normal.
	normalMinMinutes = 60
	normalMinRuns    = 2
	// normalRegimeExpiry is how long a band with no new data is carried forward.
	// Half a year: a boat that motors only in summer keeps its bands over winter.
	normalRegimeExpiry = 183 * 24 * time.Hour
	// normalStretchMinMinutes is the unusual minutes in a row that make a
	// stretch. Fewer is a blip a sender can make on its own.
	normalStretchMinMinutes = 3
	// An alarm for a subject takes its minutes out of training from this long
	// before it was raised to this long after it cleared: a fault is not normal,
	// and its start and its recovery are part of it.
	normalAlarmPadBefore = 10 * time.Minute
	normalAlarmPadAfter  = 5 * time.Minute
	// normalOpenAlarmSpan is how long an alarm that never cleared takes minutes out.
	normalOpenAlarmSpan = 24 * time.Hour

	// normalRidge is added to the correlation matrix's diagonal so two readings
	// that move together (rpm and boost) cannot make it singular.
	normalRidge = 0.1
	// normalReweight switches the one reweighting pass off when false. The pass
	// drops rows above chi-square 0.999 and refits, so an unlabelled fault in
	// history does not become normal.
	normalReweight = true
	// normalThresholdPercentile and normalThresholdChiSquare set a regime's
	// threshold: the larger of the percentile of its training distances and the
	// chi-square quantile for its number of readings. A starting value; the
	// month replay in anomaly_normal_replay_test.go is what bounds it.
	normalThresholdPercentile = 0.995
	normalThresholdChiSquare  = 0.99
	normalTrimChiSquare       = 0.999

	// Reasons a reading is left out, decided from the data (selectFeatures).
	normalMinPresence      = 0.8  // share of gated minutes that must carry the reading
	normalMaxOutOfLimits   = 0.05 // share of its minutes allowed outside physical limits
	normalMinDistinct      = 3    // fewer distinct values is a constant
	normalConstantShare    = 0.98 // one value in this share of minutes is a constant
	normalTwoSourceOverlap = 0.10 // another source on this share of the same minutes
)

// normalMinSD is the floor for a reading's standard deviation: the larger of
// the step seen in its data and the reading's MinChange. The observed step is
// measured on one-minute means, where a sender that steps in whole degrees
// averages to fractions of a step, so it cannot stand in for MinChange.
func normalMinSD(minChange, observedStep float64) float64 {
	return math.Max(minChange, observedStep)
}

// normalFeature is one reading a subject is judged on.
type normalFeature struct {
	Key    string // stable id, e.g. "oilPressure"
	Path   string
	SIUnit string
	// Explain says the reading may be named as the top reading. False for rpm,
	// which only says where on the engine's range the minute sits.
	Explain bool
	// Required says the subsystem's gate needs the reading, so the operator
	// cannot switch it off.
	Required bool
	// MinChange is the smallest change in the reading worth a look, in SI units.
	// A standard deviation is never taken below it, so a sender that sits on one
	// value and now and then steps by its own resolution cannot make a one-step
	// dip look many deviations out. 0 leaves the observed step alone.
	MinChange float64
}

// joinNormalMinutes builds a subject's minutes from each reading's minute
// means (values, by Key). A minute exists when it has every Required reading,
// and carries each reading that need accepts. A minute missing a Required
// reading is not a minute and nothing is filled in. The result is sorted.
func joinNormalMinutes(features []normalFeature, values map[string]map[time.Time]float64, need func(key string) bool) []normalMinute {
	seen := map[time.Time]bool{}
	for _, f := range features {
		if f.Required {
			for at := range values[f.Key] {
				seen[at] = true
			}
		}
	}
	var minutes []normalMinute
	for at := range seen {
		m := normalMinute{At: at, Values: map[string]float64{}}
		complete := true
		for _, f := range features {
			if !need(f.Key) {
				continue
			}
			if v, ok := values[f.Key][at]; ok {
				m.Values[f.Key] = v
			} else if f.Required {
				complete = false
				break
			}
		}
		if complete {
			minutes = append(minutes, m)
		}
	}
	sort.Slice(minutes, func(i, j int) bool { return minutes[i].At.Before(minutes[j].At) })
	return minutes
}

type normalSubject struct {
	ID, Name string
	Features []normalFeature
}

// normalMinute is one minute of one subject: each feature's mean, by Key.
type normalMinute struct {
	At     time.Time
	Values map[string]float64
}

// normalSubsystem is what the core needs to know about a kind of subject.
type normalSubsystem struct {
	Name     string
	Subjects func(vesselView) []normalSubject
	// Regime names the operating regime of a minute. ok is false when the minute
	// cannot be placed (no rpm).
	Regime func(s normalSubject, m normalMinute) (key, label string, ok bool)
	// Gate says which minutes of one subject (sorted ascending) may be judged
	// and learned from. A reading missing from a minute fails the gate.
	Gate func(minutes []normalMinute) []bool
	// Run numbers the subject's separate runs (an engine's starts), one number per
	// minute, so a regime can ask for minutes from more than one run.
	Run func(minutes []normalMinute) []int
	// AlarmOwns says which subject an alarm's path belongs to.
	AlarmOwns func(alarmPath string) (subjectID string, ok bool)
	// AlarmReading says an alarm is about one of the subsystem's readings, and
	// which (key, "" for a reading the subsystem does not use). An alarm that is
	// not about a single reading, such as a detector's, is not one. Only an alarm
	// about a reading the model uses takes minutes out of training.
	AlarmReading func(alarmPath string) (key string, isReading bool)
	// Active says a minute is one the subject was running in, before the gate asks
	// it to be steady and warm. A reading is judged constant over these.
	Active func(m normalMinute) bool
	// PathMatters says a SignalK path is one of the subsystem's readings, so a
	// change to whether it is ignored changes what is learned.
	PathMatters func(path string) bool
}

// normalFitOptions are the knobs of one fit.
type normalFitOptions struct {
	Ridge    float64
	Reweight bool
	// MinSD, when it has an entry per reading, floors each standard deviation at
	// the reading's resolution seen across the whole subject. A reading that
	// holds still inside one band has no resolution of its own to take.
	MinSD []float64
}

// normalRegimeStats is a regime's learned normal: standardising constants,
// the correlation matrix and the threshold.
type normalRegimeStats struct {
	Mean       []float64   `json:"mean"`
	SD         []float64   `json:"sd"`
	Corr       [][]float64 `json:"corr"`
	Ridge      float64     `json:"ridge"`
	Threshold  float64     `json:"threshold"`
	TrainD2P50 float64     `json:"train_d2_p50"`
	// Trimmed is how many rows the reweighting pass left out of the fit.
	Trimmed int `json:"trimmed_minutes,omitempty"`
}

// significant rounds v to nine significant digits, so float noise from an
// average (310.79999999999967 and 310.8) is one value.
func significant(v float64) float64 {
	if v == 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	exp := math.Floor(math.Log10(math.Abs(v)))
	scale := math.Pow(10, 8-exp)
	return math.Round(v*scale) / scale
}

// smallestStep is the smallest gap between two distinct values of a column
// (0 when there is only one value): the reading's resolution as observed.
func smallestStep(values []float64) float64 {
	q := make([]float64, len(values))
	for i, v := range values {
		q[i] = significant(v)
	}
	sort.Float64s(q)
	step := 0.0
	for i := 1; i < len(q); i++ {
		if gap := q[i] - q[i-1]; gap > 0 && (step == 0 || gap < step) {
			step = gap
		}
	}
	return step
}

func columnOf(rows [][]float64, j int) []float64 {
	out := make([]float64, len(rows))
	for i, r := range rows {
		out[i] = r[j]
	}
	return out
}

// fitRegime learns the mean, standard deviation and correlation of the rows'
// readings, and the regime's threshold. With opts.Reweight it fits twice: the
// second time without rows that sit far above the first fit's chi-square 0.999.
func fitRegime(rows [][]float64, opts normalFitOptions) (normalRegimeStats, error) {
	if len(rows) == 0 {
		return normalRegimeStats{}, fmt.Errorf("no rows to fit")
	}
	d := len(rows[0])
	if d == 0 || len(rows) <= d {
		return normalRegimeStats{}, fmt.Errorf("%d rows are too few to fit %d readings", len(rows), d)
	}
	first, err := fitOnce(rows, opts)
	if err != nil {
		return normalRegimeStats{}, err
	}
	kept := rows
	if opts.Reweight {
		scorer, err := first.scorer()
		if err != nil {
			return normalRegimeStats{}, err
		}
		cut := distuv.ChiSquared{K: float64(d)}.Quantile(normalTrimChiSquare)
		kept = make([][]float64, 0, len(rows))
		for _, r := range rows {
			d2, _, err := scorer.score(r)
			if err != nil {
				return normalRegimeStats{}, err
			}
			if d2 <= cut {
				kept = append(kept, r)
			}
		}
		if len(kept) <= d {
			return normalRegimeStats{}, fmt.Errorf("only %d of %d rows are left after reweighting, too few to fit %d readings", len(kept), len(rows), d)
		}
	}
	final, err := fitOnce(kept, opts)
	if err != nil {
		return normalRegimeStats{}, err
	}
	final.Trimmed = len(rows) - len(kept)
	scorer, err := final.scorer()
	if err != nil {
		return normalRegimeStats{}, err
	}
	d2s := make([]float64, len(kept))
	for i, r := range kept {
		if d2s[i], _, err = scorer.score(r); err != nil {
			return normalRegimeStats{}, err
		}
	}
	sort.Float64s(d2s)
	rank := int(math.Ceil(normalThresholdPercentile*float64(len(d2s)))) - 1
	if rank < 0 {
		rank = 0
	}
	final.Threshold = math.Max(d2s[rank], distuv.ChiSquared{K: float64(d)}.Quantile(normalThresholdChiSquare))
	final.TrainD2P50 = d2s[len(d2s)/2]
	return final, nil
}

// fitOnce is one fit without the threshold.
func fitOnce(rows [][]float64, opts normalFitOptions) (normalRegimeStats, error) {
	n, d := len(rows), len(rows[0])
	mean := make([]float64, d)
	rawSD := make([]float64, d)
	for j := 0; j < d; j++ {
		for _, r := range rows {
			mean[j] += r[j]
		}
		mean[j] /= float64(n)
		var ss float64
		for _, r := range rows {
			ss += (r[j] - mean[j]) * (r[j] - mean[j])
		}
		rawSD[j] = math.Sqrt(ss / float64(n-1))
	}
	sd := make([]float64, d)
	for j := 0; j < d; j++ {
		floor := smallestStep(columnOf(rows, j))
		if len(opts.MinSD) == d && opts.MinSD[j] > floor {
			floor = opts.MinSD[j]
		}
		sd[j] = math.Max(rawSD[j], floor)
		if !(sd[j] > 0) {
			return normalRegimeStats{}, fmt.Errorf("reading %d did not change, so it has no spread to judge by", j)
		}
	}
	corr := make([][]float64, d)
	for i := range corr {
		corr[i] = make([]float64, d)
		corr[i][i] = 1
	}
	for i := 0; i < d; i++ {
		for j := i + 1; j < d; j++ {
			if !(rawSD[i] > 0) || !(rawSD[j] > 0) {
				continue
			}
			var c float64
			for _, r := range rows {
				c += (r[i] - mean[i]) * (r[j] - mean[j])
			}
			rho := c / float64(n-1) / (rawSD[i] * rawSD[j])
			corr[i][j], corr[j][i] = rho, rho
		}
	}
	return normalRegimeStats{Mean: mean, SD: sd, Corr: corr, Ridge: opts.Ridge}, nil
}

// normalScorer scores minutes against one regime.
type normalScorer struct {
	stats normalRegimeStats
	inv   *mat.SymDense
}

// scorer builds the inverse of the ridged correlation matrix once for a regime.
func (s normalRegimeStats) scorer() (*normalScorer, error) {
	d := len(s.Mean)
	if d == 0 || len(s.SD) != d || len(s.Corr) != d {
		return nil, fmt.Errorf("the regime's learned figures do not agree on its %d readings", d)
	}
	sym := mat.NewSymDense(d, nil)
	for i := 0; i < d; i++ {
		if len(s.Corr[i]) != d {
			return nil, fmt.Errorf("the regime's correlation matrix is not %d by %d", d, d)
		}
		for j := i; j < d; j++ {
			v := s.Corr[i][j]
			if i == j {
				v += s.Ridge
			}
			sym.SetSym(i, j, v)
		}
	}
	var chol mat.Cholesky
	if ok := chol.Factorize(sym); !ok {
		return nil, fmt.Errorf("the regime's correlation matrix is not positive definite")
	}
	inv := mat.NewSymDense(d, nil)
	if err := chol.InverseTo(inv); err != nil {
		return nil, fmt.Errorf("inverting the regime's correlation matrix: %w", err)
	}
	return &normalScorer{stats: s, inv: inv}, nil
}

// score returns D2 for one row of readings and each reading's contribution to
// it, which sum to D2.
func (sc *normalScorer) score(x []float64) (float64, []float64, error) {
	d := len(sc.stats.Mean)
	if len(x) != d {
		return 0, nil, fmt.Errorf("%d readings given, the regime has %d", len(x), d)
	}
	z := make([]float64, d)
	for j := range z {
		z[j] = (x[j] - sc.stats.Mean[j]) / sc.stats.SD[j]
	}
	contrib := make([]float64, d)
	var d2 float64
	for i := 0; i < d; i++ {
		var v float64
		for j := 0; j < d; j++ {
			v += sc.inv.At(i, j) * z[j]
		}
		contrib[i] = z[i] * v
		d2 += contrib[i]
	}
	return d2, contrib, nil
}

// regimeShift is the distance of a new mean from the old one, seen through the
// old regime's covariance. ok is false when the two do not have the same readings.
func regimeShift(old normalRegimeStats, newMean []float64) (float64, bool) {
	if len(newMean) != len(old.Mean) {
		return 0, false
	}
	sc, err := old.scorer()
	if err != nil {
		return 0, false
	}
	d2, _, err := sc.score(newMean)
	if err != nil || d2 < 0 {
		return 0, false
	}
	return math.Sqrt(d2), true
}
