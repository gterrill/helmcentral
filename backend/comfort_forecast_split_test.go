package main

import (
	"strings"
	"testing"
	"time"
)

// ── the forecast keeps the swell and wind-wave split and the wave model (ADR 0301) ──

func splitHour(tm time.Time, mutate func(*waveHourPoint)) (weatherForecastBundle, *waveForecastBundle) {
	wb := weatherForecastBundle{Hourly: []weatherHourPoint{{Time: tm, WindSpeedMS: 5}}}
	combined, per, dir := 1.2, 7.0, 200.0
	p := waveHourPoint{
		Time: tm, WaveHeightM: &combined, WavePeriodS: &per, WaveDirectionDeg: &dir,
		SwellWaveHeightM: 0.9, SwellWavePeriodS: 9, SwellWaveDirectionDeg: 190,
		WindWaveHeightM: 0.6, WindWavePeriodS: 4, WindWaveDirectionDeg: 220,
	}
	if mutate != nil {
		mutate(&p)
	}
	return wb, &waveForecastBundle{Hourly: []waveHourPoint{p}, Model: "ncep_gfswave025", ModelLabel: "GFS-Wave"}
}

func TestPassageHoursKeepTheSwellAndWindWaveAndTheModel(t *testing.T) {
	tm := pwDay(20, 6).UTC()
	wb, vb := splitHour(tm, nil)
	h := passageHoursFromBundles(wb, vb)[tm.Unix()]
	if h.Swell == nil || h.Swell.HeightM != 0.9 || h.Swell.PeriodS != 9 || h.Swell.FromDeg != 190 {
		t.Errorf("swell: %+v", h.Swell)
	}
	if h.WindWave == nil || h.WindWave.HeightM != 0.6 || h.WindWave.PeriodS != 4 || h.WindWave.FromDeg != 220 {
		t.Errorf("wind wave: %+v", h.WindWave)
	}
	if h.WaveModel != (waveModelRef{ID: "ncep_gfswave025", Label: "GFS-Wave"}) {
		t.Errorf("model: %+v", h.WaveModel)
	}
	if got := comfortGap(h); got != "" {
		t.Errorf("a full hour has no gap, got %q", got)
	}
}

func TestAWaveSystemWithNoPeriodIsNoSystem(t *testing.T) {
	tm := pwDay(20, 6).UTC()
	wb, vb := splitHour(tm, func(p *waveHourPoint) { p.WindWavePeriodS, p.WindWaveHeightM = 0, 0 })
	h := passageHoursFromBundles(wb, vb)[tm.Unix()]
	if h.WindWave != nil || h.Swell == nil {
		t.Errorf("swell %+v, wind wave %+v", h.Swell, h.WindWave)
	}
}

func TestAMissingSplitIsAReasonAndNeverCalm(t *testing.T) {
	tm := pwDay(20, 6).UTC()
	wb, vb := splitHour(tm, func(p *waveHourPoint) {
		p.SwellWaveHeightM, p.SwellWavePeriodS = 0, 0
		p.WindWaveHeightM, p.WindWavePeriodS = 0, 0
	})
	h := passageHoursFromBundles(wb, vb)[tm.Unix()]
	got := comfortGap(h)
	if !strings.Contains(got, "no swell and wind-wave split") {
		t.Fatalf("a 1.2 m combined sea with neither system must say the split is missing, got %q", got)
	}
}

func TestAFlatCombinedSeaWithNoSystemsIsNotAGap(t *testing.T) {
	tm := pwDay(20, 6).UTC()
	wb, vb := splitHour(tm, func(p *waveHourPoint) {
		flat := 0.05
		p.WaveHeightM = &flat
		p.SwellWaveHeightM, p.SwellWavePeriodS = 0, 0
		p.WindWaveHeightM, p.WindWavePeriodS = 0, 0
	})
	if got := comfortGap(passageHoursFromBundles(wb, vb)[tm.Unix()]); got != "" {
		t.Errorf("under 0.1 m the sea is flat, got %q", got)
	}
}

func TestNoModelLabelIsAReasonThatNamesTheUpdate(t *testing.T) {
	tm := pwDay(20, 6).UTC()
	wb, vb := splitHour(tm, nil)
	vb.ModelLabel = ""
	got := comfortGap(passageHoursFromBundles(wb, vb)[tm.Unix()])
	if !strings.Contains(got, "update the wave plugin") {
		t.Fatalf("an old plugin must say to update it, got %q", got)
	}
}

func TestAnHourWithNoWaveFigureAndNoSystemsSaysItHasNone(t *testing.T) {
	tm := pwDay(20, 6).UTC()
	wb, vb := splitHour(tm, func(p *waveHourPoint) {
		p.WaveHeightM, p.WavePeriodS, p.WaveDirectionDeg = nil, nil, nil
		p.SwellWaveHeightM, p.SwellWavePeriodS = 0, 0
		p.WindWaveHeightM, p.WindWavePeriodS = 0, 0
	})
	if got := comfortGap(passageHoursFromBundles(wb, vb)[tm.Unix()]); got != comfortReasonNoWave {
		t.Errorf("no wave at all is its own reason, got %q", got)
	}
}

// Training judges an hour from its systems whatever the combined height says, and
// so does the forecast.
func TestAnHourWithSystemsButNoCombinedHeightIsJudgedFromItsSystems(t *testing.T) {
	tm := pwDay(20, 6).UTC()
	wb, vb := splitHour(tm, func(p *waveHourPoint) { p.WaveHeightM, p.WavePeriodS, p.WaveDirectionDeg = nil, nil, nil })
	h := passageHoursFromBundles(wb, vb)[tm.Unix()]
	if got := comfortGap(h); got != "" {
		t.Errorf("swell and wind wave are present, so the hour can be judged; got %q", got)
	}
	row := seaHindcastRow{Swell: h.Swell, WindWave: h.WindWave}
	if row.noSplit() {
		t.Error("training and the forecast must agree: the hour has systems")
	}
}

func TestTheWaveModelIsKeptWhenThePluginDidNotFallBack(t *testing.T) {
	out := wasmFetchWavesOutput{Model: "ncep_gfswave025", ModelLabel: "GFS-Wave"}
	b, err := mapWasmFetchWavesOutput(out)
	if err != nil {
		t.Fatal(err)
	}
	if b.Model != "ncep_gfswave025" || b.ModelLabel != "GFS-Wave" {
		t.Errorf("model %q label %q", b.Model, b.ModelLabel)
	}
	if b.FallbackModel != "" {
		t.Errorf("no fallback was used, FallbackModel = %q", b.FallbackModel)
	}
	out.Model, out.ModelLabel, out.FallbackUsed = "best_match", "The Open-Meteo best-match blend", true
	b, err = mapWasmFetchWavesOutput(out)
	if err != nil {
		t.Fatal(err)
	}
	if b.Model != "best_match" || b.FallbackModel != "best_match" {
		t.Errorf("fallback: model %q fallback %q", b.Model, b.FallbackModel)
	}
}
