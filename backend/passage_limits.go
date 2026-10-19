package main

import (
	"fmt"
	"log"
	"math"

	"gopkg.in/yaml.v3"
)

// Passage limits are the operator's own go/no-go thresholds for a passage,
// kept under assistant.passage_limits. Nothing ships with a value: every
// threshold is nil until the operator sets it, and the passage assessor
// refuses a sector whose no-go wind is blank rather than assume a number
// (ADR 0191, amended by ADR 0194).
//
// Only no-go is entered. Marginal, and every gust limit, is worked out from
// it (ADR 0194).
//
// Sectors are the true wind angle off the course: ahead 0-60 degrees, abeam
// 60-120, astern 120-180.

// The heuristics below are derived from the operator's own entries, not from
// a standard. Their marginal/no-go pairs averaged 0.7, and their gust no-go
// figures (25, 30, 40) sat 10 kn above the matching wind no-go (15, 22, 35)
// to within a few knots.
const (
	// passageMarginalFraction is marginal as a share of no-go, for wind, gusts
	// and waves.
	passageMarginalFraction = 0.7
	// passageGustAboveWindKn is how far above a sector's no-go wind its gust
	// no-go sits.
	passageGustAboveWindKn = 10.0
)

// marginalOf is the marginal threshold for a no-go. Rounded to the
// thousandth so 0.7 of 15 is 10.5, not 10.499999999999998.
func marginalOf(noGo float64) float64 {
	return math.Round(noGo*passageMarginalFraction*1000) / 1000
}

// passageSectorLimits is one sector's no-go figures. A nil figure is unset,
// which is not zero.
type passageSectorLimits struct {
	WindKnNoGo *float64 `yaml:"wind_kn_no_go,omitempty" json:"wind_kn_no_go"`
	WaveMNoGo  *float64 `yaml:"wave_m_no_go,omitempty" json:"wave_m_no_go"`
}

type passageLimits struct {
	Ahead  passageSectorLimits `yaml:"ahead,omitempty" json:"ahead"`
	Abeam  passageSectorLimits `yaml:"abeam,omitempty" json:"abeam"`
	Astern passageSectorLimits `yaml:"astern,omitempty" json:"astern"`
	// WindAgainstTideOffsetKn is how many knots to take off every wind limit
	// while the tidal stream runs against the wind, through the strongest part
	// of the flood or ebb. It only applies to legs with a known flood
	// direction. Nil is not checked.
	WindAgainstTideOffsetKn *float64 `yaml:"wind_against_tide_offset_kn,omitempty" json:"wind_against_tide_offset_kn"`
	DaylightArrival         bool     `yaml:"daylight_arrival,omitempty" json:"daylight_arrival"`
}

// legacyPassageLimits is the shape v0.49.0 stored: marginal and no-go for wind,
// gust and waves per sector, and a wind-against-tide band. Read only to carry
// the no-go figures over.
type legacyPassageLimitBand struct {
	NoGo *float64 `yaml:"no_go"`
}
type legacyPassageSector struct {
	WindKn legacyPassageLimitBand `yaml:"wind_kn"`
	WaveM  legacyPassageLimitBand `yaml:"wave_m"`
	// GustKn is read so a bad stored value is still an error.
	GustKn legacyPassageLimitBand `yaml:"gust_kn"`
}
type legacyPassageLimits struct {
	Ahead           legacyPassageSector `yaml:"ahead"`
	Abeam           legacyPassageSector `yaml:"abeam"`
	Astern          legacyPassageSector `yaml:"astern"`
	WindAgainstTide map[string]any      `yaml:"wind_against_tide"`
	DaylightArrival bool                `yaml:"daylight_arrival"`
}

// isLegacyPassageLimits reports whether the stored block still has the v0.49.0
// shape: a sector with wind_kn, gust_kn or wave_m, or a wind_against_tide band.
func isLegacyPassageLimits(m map[string]any) bool {
	if _, ok := m["wind_against_tide"]; ok {
		return true
	}
	for _, sec := range []string{"ahead", "abeam", "astern"} {
		sm, _ := m[sec].(map[string]any)
		for _, k := range []string{"wind_kn", "gust_kn", "wave_m"} {
			if _, ok := sm[k]; ok {
				return true
			}
		}
	}
	return false
}

func (l legacyPassageLimits) convert() passageLimits {
	if len(l.WindAgainstTide) > 0 {
		// A band has no offset equivalent; the operator sets one afresh.
		log.Printf("assistant.passage_limits: dropped the old wind against tide band; set \"Reduce wind limits by\" in Settings if wanted")
	}
	sector := func(s legacyPassageSector) passageSectorLimits {
		return passageSectorLimits{WindKnNoGo: s.WindKn.NoGo, WaveMNoGo: s.WaveM.NoGo}
	}
	return passageLimits{Ahead: sector(l.Ahead), Abeam: sector(l.Abeam), Astern: sector(l.Astern), DaylightArrival: l.DaylightArrival}
}

// passageLimitsFromSettings decodes the stored block. A block that cannot be
// decoded is an error, never empty limits: the settings page would otherwise
// show blanks and save them back over the operator's values. The one
// exception is the v0.49.0 shape, whose no-go wind and wave figures carry
// over (marginal and gust figures are dropped); the next save writes the new
// shape.
func passageLimitsFromSettings(raw any) (passageLimits, error) {
	var out passageLimits
	if raw == nil {
		return out, nil
	}
	b, err := yaml.Marshal(raw)
	if err != nil {
		return passageLimits{}, fmt.Errorf("assistant.passage_limits: %w", err)
	}
	var probe map[string]any
	if err := yaml.Unmarshal(b, &probe); err != nil {
		return passageLimits{}, fmt.Errorf("assistant.passage_limits is unreadable: %w", err)
	}
	if isLegacyPassageLimits(probe) {
		var old legacyPassageLimits
		if err := yaml.Unmarshal(b, &old); err != nil {
			return passageLimits{}, fmt.Errorf("assistant.passage_limits is unreadable: %w", err)
		}
		return old.convert(), nil
	}
	if err := yaml.Unmarshal(b, &out); err != nil {
		return passageLimits{}, fmt.Errorf("assistant.passage_limits is unreadable: %w", err)
	}
	return out, nil
}

// passageLimitsToSettings is the map form written into settings.yaml.
func passageLimitsToSettings(l passageLimits) (map[string]any, error) {
	out := map[string]any{}
	b, err := yaml.Marshal(l)
	if err != nil {
		return nil, fmt.Errorf("assistant.passage_limits: %w", err)
	}
	if err := yaml.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("assistant.passage_limits: %w", err)
	}
	return out, nil
}

func validateNoGo(label string, v *float64) string {
	if v != nil && *v < 0 {
		return label + " must be 0 or more"
	}
	return ""
}

// validatePassageLimits names the first bad field in plain words.
func validatePassageLimits(l passageLimits) *settingsValidationError {
	sectors := []struct {
		name string
		s    passageSectorLimits
	}{{"ahead", l.Ahead}, {"abeam", l.Abeam}, {"astern", l.Astern}}
	for _, sec := range sectors {
		if msg := validateNoGo(fmt.Sprintf("Passage limits, %s no-go wind", sec.name), sec.s.WindKnNoGo); msg != "" {
			return &settingsValidationError{Field: "assistant.passage_limits." + sec.name + ".wind_kn_no_go", Message: msg}
		}
		if msg := validateNoGo(fmt.Sprintf("Passage limits, %s no-go wave height", sec.name), sec.s.WaveMNoGo); msg != "" {
			return &settingsValidationError{Field: "assistant.passage_limits." + sec.name + ".wave_m_no_go", Message: msg}
		}
	}
	if off := l.WindAgainstTideOffsetKn; off != nil {
		const field = "assistant.passage_limits.wind_against_tide_offset_kn"
		if *off <= 0 {
			return &settingsValidationError{Field: field, Message: "Passage limits, wind against tide: the reduction must be more than 0"}
		}
		for _, sec := range sectors {
			if sec.s.WindKnNoGo != nil && *off >= *sec.s.WindKnNoGo {
				return &settingsValidationError{Field: field, Message: fmt.Sprintf("Passage limits, wind against tide: the reduction must be less than the %s no-go wind (%g kn)", sec.name, *sec.s.WindKnNoGo)}
			}
		}
	}
	return nil
}
