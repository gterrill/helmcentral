package main

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Passage limits are the operator's own go/no-go thresholds for a passage,
// kept under assistant.passage_limits. Nothing ships with a value: every
// threshold is nil until the operator sets it, and the passage assessor
// refuses a sector whose limits are blank rather than assume a number
// (ADR 0191).
//
// Sectors are the true wind angle off the course: ahead 0-60 degrees, abeam
// 60-120, astern 120-180.

// passageLimitBand is one measure's marginal and no-go thresholds. A nil end
// is unset, which is not zero.
type passageLimitBand struct {
	Marginal *float64 `yaml:"marginal,omitempty" json:"marginal"`
	NoGo     *float64 `yaml:"no_go,omitempty" json:"no_go"`
}

type passageSectorLimits struct {
	WindKn passageLimitBand `yaml:"wind_kn,omitempty" json:"wind_kn"`
	GustKn passageLimitBand `yaml:"gust_kn,omitempty" json:"gust_kn"`
	WaveM  passageLimitBand `yaml:"wave_m,omitempty" json:"wave_m"`
}

// passageWindAgainstTide is the wind band that applies while the tidal stream
// runs against the wind, through the strongest part of the flood or ebb. It
// only applies to legs with a known flood direction.
type passageWindAgainstTide struct {
	WindKn passageLimitBand `yaml:"wind_kn,omitempty" json:"wind_kn"`
}

type passageLimits struct {
	Ahead           passageSectorLimits    `yaml:"ahead,omitempty" json:"ahead"`
	Abeam           passageSectorLimits    `yaml:"abeam,omitempty" json:"abeam"`
	Astern          passageSectorLimits    `yaml:"astern,omitempty" json:"astern"`
	WindAgainstTide passageWindAgainstTide `yaml:"wind_against_tide,omitempty" json:"wind_against_tide"`
	DaylightArrival bool                   `yaml:"daylight_arrival,omitempty" json:"daylight_arrival"`
}

// passageLimitsFromSettings decodes the stored block. A block that cannot be
// decoded is an error, never empty limits: the settings page would otherwise
// show blanks and save them back over the operator's values.
func passageLimitsFromSettings(raw any) (passageLimits, error) {
	var out passageLimits
	if raw == nil {
		return out, nil
	}
	b, err := yaml.Marshal(raw)
	if err != nil {
		return passageLimits{}, fmt.Errorf("assistant.passage_limits: %w", err)
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

func validateBand(label string, b passageLimitBand) string {
	if b.Marginal != nil && *b.Marginal < 0 {
		return label + ": marginal must be 0 or more"
	}
	if b.NoGo != nil && *b.NoGo < 0 {
		return label + ": no-go must be 0 or more"
	}
	if b.Marginal != nil && b.NoGo != nil && *b.Marginal >= *b.NoGo {
		return label + ": marginal must be below no-go"
	}
	return ""
}

// validatePassageLimits names the first bad field in plain words.
func validatePassageLimits(l passageLimits) *settingsValidationError {
	sectors := []struct {
		name, key string
		s         passageSectorLimits
	}{{"ahead", "ahead", l.Ahead}, {"abeam", "abeam", l.Abeam}, {"astern", "astern", l.Astern}}
	for _, sec := range sectors {
		bands := []struct {
			name, key string
			b         passageLimitBand
		}{{"wind", "wind_kn", sec.s.WindKn}, {"gust", "gust_kn", sec.s.GustKn}, {"wave height", "wave_m", sec.s.WaveM}}
		for _, band := range bands {
			if msg := validateBand(fmt.Sprintf("Passage limits, %s %s", sec.name, band.name), band.b); msg != "" {
				return &settingsValidationError{Field: "assistant.passage_limits." + sec.key + "." + band.key, Message: msg}
			}
		}
	}
	if msg := validateBand("Passage limits, wind against tide", l.WindAgainstTide.WindKn); msg != "" {
		return &settingsValidationError{Field: "assistant.passage_limits.wind_against_tide.wind_kn", Message: msg}
	}
	return nil
}
