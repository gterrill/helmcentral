package main

import (
	"math"
	"math/rand"
	"testing"
)

// correlatedRows makes n rows of two readings where b follows a closely: a
// stands in for engine load and b for fuel rate.
func correlatedRows(n int, seed int64) [][]float64 {
	rng := rand.New(rand.NewSource(seed))
	rows := make([][]float64, n)
	for i := range rows {
		a := 10 + 4*rng.Float64()
		rows[i] = []float64{a, 2*a + 0.4*rng.NormFloat64()}
	}
	return rows
}

func columnMeanSD(rows [][]float64, col int) (mean, sd float64) {
	for _, r := range rows {
		mean += r[col]
	}
	mean /= float64(len(rows))
	for _, r := range rows {
		sd += (r[col] - mean) * (r[col] - mean)
	}
	return mean, math.Sqrt(sd / float64(len(rows)-1))
}

// D2 must equal the textbook figure worked out by hand for two readings:
// z'(R+lambda*I)^-1 z with R's off-diagonal the sample correlation.
func TestFitRegime_ScoreMatchesADirectCalculation(t *testing.T) {
	rows := correlatedRows(400, 1)
	stats, err := fitRegime(rows, normalFitOptions{Ridge: 0.1})
	if err != nil {
		t.Fatal(err)
	}
	m0, s0 := columnMeanSD(rows, 0)
	m1, s1 := columnMeanSD(rows, 1)
	var cov float64
	for _, r := range rows {
		cov += (r[0] - m0) * (r[1] - m1)
	}
	rho := cov / float64(len(rows)-1) / (s0 * s1)

	scorer, err := stats.scorer()
	if err != nil {
		t.Fatal(err)
	}
	x := []float64{12, 27}
	got, _, err := scorer.score(x)
	if err != nil {
		t.Fatal(err)
	}
	z0, z1 := (x[0]-m0)/s0, (x[1]-m1)/s1
	a := 1 + 0.1
	det := a*a - rho*rho
	want := (a*z0*z0 - 2*rho*z0*z1 + a*z1*z1) / det
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("D2 = %v, want %v", got, want)
	}
}

func TestFitRegime_ContributionsSumToD2(t *testing.T) {
	stats, err := fitRegime(correlatedRows(300, 2), normalFitOptions{Ridge: 0.1})
	if err != nil {
		t.Fatal(err)
	}
	scorer, _ := stats.scorer()
	d2, contrib, err := scorer.score([]float64{13, 22})
	if err != nil {
		t.Fatal(err)
	}
	var sum float64
	for _, c := range contrib {
		sum += c
	}
	if math.Abs(sum-d2) > 1e-9 {
		t.Fatalf("contributions sum to %v, D2 is %v", sum, d2)
	}
}

// A reading that moves in steps of 0.5 and sat still for 999 of 1,000 minutes
// has a standard deviation far below what the sender can show. It is floored
// at the sender's resolution so a one-step wobble is not a 30-sigma event.
func TestFitRegime_StandardDeviationIsFlooredAtTheObservedResolution(t *testing.T) {
	rows := make([][]float64, 1000)
	for i := range rows {
		rows[i] = []float64{10, float64(i%7) * 1.5}
	}
	rows[500][0] = 10.5
	stats, err := fitRegime(rows, normalFitOptions{Ridge: 0.1})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(stats.SD[0]-0.5) > 1e-9 {
		t.Fatalf("sd floor = %v, want 0.5 (the smallest step seen)", stats.SD[0])
	}
}

func TestFitRegime_RidgeKeepsAPerfectlyCorrelatedPairInvertible(t *testing.T) {
	rows := make([][]float64, 100)
	for i := range rows {
		v := float64(i%10) + 0.1*float64(i)
		rows[i] = []float64{v, 3 * v}
	}
	stats, err := fitRegime(rows, normalFitOptions{Ridge: 0.1})
	if err != nil {
		t.Fatalf("a singular correlation matrix must still fit: %v", err)
	}
	scorer, err := stats.scorer()
	if err != nil {
		t.Fatal(err)
	}
	d2, _, err := scorer.score(rows[3])
	if err != nil || math.IsNaN(d2) || math.IsInf(d2, 0) {
		t.Fatalf("D2 = %v, %v", d2, err)
	}
}

// Fuel rate high for the load is off the correlation although each reading is
// ordinary on its own. The joint score finds it and the top contribution names
// fuel rate.
func TestFitRegime_ABreakInCorrelationScoresAboveThresholdAndBlamesTheRightReading(t *testing.T) {
	stats, err := fitRegime(correlatedRows(2000, 3), normalFitOptions{Ridge: 0.1, Reweight: true})
	if err != nil {
		t.Fatal(err)
	}
	// Load is the middle of its range and fuel is 5 above its own middle: about
	// two of its marginal spreads, but 12 conditional spreads off for that load.
	x := []float64{stats.Mean[0], stats.Mean[1] + 5}
	for i, v := range x {
		if math.Abs(v-stats.Mean[i]) > 2.5*stats.SD[i] {
			t.Fatalf("reading %d is %v sd out: the case must be ordinary on its own", i, (v-stats.Mean[i])/stats.SD[i])
		}
	}
	scorer, _ := stats.scorer()
	d2, contrib, err := scorer.score(x)
	if err != nil {
		t.Fatal(err)
	}
	if d2 <= stats.Threshold {
		t.Fatalf("D2 %v must exceed the threshold %v", d2, stats.Threshold)
	}
	if contrib[1] <= contrib[0] {
		t.Fatalf("fuel (contribution %v) must outweigh load (%v)", contrib[1], contrib[0])
	}
	if stats.Mean[1] >= x[1] {
		t.Fatalf("the reading must sit above its usual value")
	}
}

func TestFitRegime_ThresholdIsTheLargerOfPercentileAndChiSquare(t *testing.T) {
	stats, err := fitRegime(correlatedRows(1000, 4), normalFitOptions{Ridge: 0.1})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Threshold < 9.21 { // chi-square 0.99 for 2 degrees of freedom
		t.Fatalf("threshold %v is under the chi-square 0.99 floor for two readings", stats.Threshold)
	}
}

// One unlabelled fault in history must not become normal: a reweighting pass
// drops rows far above chi-square 0.999 and refits.
func TestFitRegime_ReweightingKeepsAnUnlabelledFaultOutOfNormal(t *testing.T) {
	rows := correlatedRows(1000, 5)
	for i := 0; i < 25; i++ {
		rows[i] = []float64{12, 12*2 + 6} // fuel far above for the load
	}
	with, err := fitRegime(rows, normalFitOptions{Ridge: 0.1, Reweight: true})
	if err != nil {
		t.Fatal(err)
	}
	without, err := fitRegime(rows, normalFitOptions{Ridge: 0.1, Reweight: false})
	if err != nil {
		t.Fatal(err)
	}
	if with.Trimmed < 20 {
		t.Fatalf("trimmed %d rows, want the fault's 25 or nearly", with.Trimmed)
	}
	scorer, _ := with.scorer()
	d2With, _, _ := scorer.score([]float64{12, 30})
	scorerOff, _ := without.scorer()
	d2Without, _, _ := scorerOff.score([]float64{12, 30})
	if d2With <= with.Threshold {
		t.Fatalf("with reweighting the fault is still unusual: D2 %v threshold %v", d2With, with.Threshold)
	}
	if d2Without >= d2With {
		t.Fatalf("without reweighting the fault should have been partly learned: %v vs %v", d2Without, d2With)
	}
}

func TestRegimeShift_IsTheNewMeanSeenThroughTheOldCovariance(t *testing.T) {
	old, err := fitRegime(correlatedRows(500, 6), normalFitOptions{Ridge: 0.1})
	if err != nil {
		t.Fatal(err)
	}
	same, ok := regimeShift(old, old.Mean)
	if !ok || same > 1e-9 {
		t.Fatalf("no move: shift = %v %v", same, ok)
	}
	moved := []float64{old.Mean[0] + old.SD[0], old.Mean[1] + 2*old.SD[1]}
	got, ok := regimeShift(old, moved)
	if !ok || got <= 0 {
		t.Fatalf("moved mean: shift = %v %v", got, ok)
	}
	if _, ok := regimeShift(old, []float64{1}); ok {
		t.Fatal("a different number of readings cannot be compared")
	}
}

func TestFitRegime_RefusesTooFewRowsForTheReadings(t *testing.T) {
	if _, err := fitRegime([][]float64{{1, 2}, {2, 3}}, normalFitOptions{Ridge: 0.1}); err == nil {
		t.Fatal("two rows cannot fit two readings")
	}
}
