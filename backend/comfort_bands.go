package main

import (
	"fmt"
	"slices"
	"strings"
)

// Comfort bands (ADR 0298): the felt-motion edges between Comfortable, Lively
// and Rough, learned from the operator's own ratings. The module knows only
// rated felt figures. Whatever stores them, and the words that go beside a
// figure outside the rated range, belong to the caller.

// ratingOrder is the operator's three ratings, from gentlest to roughest.
var ratingOrder = []string{ratingComfortable, ratingLively, ratingShouldntHaveGone}

const (
	bandComfortable = "comfortable"
	bandLively      = "lively"
	bandRough       = "rough"

	bandsMinRated = 3
)

// ratedFelt is one rated felt figure, in degrees of sway.
type ratedFelt struct {
	Rating  string
	FeltDeg float64
}

// bandOutside says whether a figure lies beyond everything that was rated.
type bandOutside int

const (
	insideRated bandOutside = iota
	belowRated
	aboveRated
)

// comfortBands are the felt-motion edges learned from one boat's ratings.
type comfortBands struct {
	Learned bool `json:"learned"`
	// RatedCount is how many rated figures the edges came from, or the count so
	// far when they are not learned.
	RatedCount int `json:"rated_count"`
	// Reason says why the bands are not learned yet; Summary says what they were
	// learned from and where the edges lie. One of the two is set.
	Reason  string `json:"reason,omitempty"`
	Summary string `json:"summary,omitempty"`
	// LivelyFrom is where Comfortable ends and Lively begins, in degrees. Nil
	// when no Comfortable and Lively figure were both rated.
	LivelyFrom *float64 `json:"lively_from_deg"`
	// RoughFrom is where Lively ends and Rough begins. Nil until a figure is
	// rated Shouldn't have gone.
	RoughFrom *float64 `json:"rough_from_deg"`

	minRated, maxRated float64
}

// ratingStats summarises the figures rated one way.
type ratingStats struct {
	n              int
	low, high, med float64
}

func ratingStatsOf(felts []float64) ratingStats {
	if len(felts) == 0 {
		return ratingStats{}
	}
	return ratingStats{n: len(felts), low: slices.Min(felts), high: slices.Max(felts), med: median(felts)}
}

// edgeBetween sits midway between the higher class's lowest figure and the
// lower class's highest. Where the two overlap it sits midway between their
// medians.
func edgeBetween(lower, higher ratingStats) *float64 {
	if lower.n == 0 || higher.n == 0 {
		return nil
	}
	var v float64
	if higher.low < lower.high {
		v = (lower.med + higher.med) / 2
	} else {
		v = (higher.low + lower.high) / 2
	}
	return &v
}

func ratedNoun(n int) string {
	if n == 1 {
		return "passage"
	}
	return "passages"
}

// learnComfortBands learns the edges from the rated figures. An edge between
// two neighbouring classes exists only when both have a figure; the bands need
// three rated figures and an edge. A figure with a rating outside ratingOrder
// is left out.
func learnComfortBands(rated []ratedFelt) comfortBands {
	by := make(map[string][]float64, len(ratingOrder))
	for _, name := range ratingOrder {
		by[name] = nil
	}
	var b comfortBands
	for _, r := range rated {
		figures, known := by[r.Rating]
		if !known {
			continue
		}
		by[r.Rating] = append(figures, r.FeltDeg)
		if b.RatedCount == 0 || r.FeltDeg < b.minRated {
			b.minRated = r.FeltDeg
		}
		if b.RatedCount == 0 || r.FeltDeg > b.maxRated {
			b.maxRated = r.FeltDeg
		}
		b.RatedCount++
	}
	classes := 0
	for _, figures := range by {
		if len(figures) > 0 {
			classes++
		}
	}
	switch {
	case b.RatedCount < bandsMinRated:
		b.Reason = fmt.Sprintf("Not learned yet: %d rated %s with a felt figure, and it needs at least %d.", b.RatedCount, ratedNoun(b.RatedCount), bandsMinRated)
		return b
	case classes < 2:
		b.Reason = "Not learned yet: every rated passage has the same rating, and it needs at least two different ratings."
		return b
	}
	comfortable, lively, rough := ratingStatsOf(by[ratingComfortable]), ratingStatsOf(by[ratingLively]), ratingStatsOf(by[ratingShouldntHaveGone])
	b.LivelyFrom = edgeBetween(comfortable, lively)
	b.RoughFrom = edgeBetween(lively, rough)
	if b.LivelyFrom != nil && b.RoughFrom != nil && *b.LivelyFrom >= *b.RoughFrom {
		// Overlapping ratings can put the Lively edge above the Rough one. Nothing
		// would ever be Lively, so draw neither.
		b.LivelyFrom, b.RoughFrom = nil, nil
		b.Reason = "Not learned yet: your ratings overlap too much to draw the bands. Comfortable, Lively and Shouldn't have gone passages need to sit in order from gentlest to roughest."
		return b
	}
	if b.LivelyFrom == nil && b.RoughFrom == nil {
		b.Reason = "Not learned yet: it needs ratings in two neighbouring classes, Comfortable and Lively or Lively and Shouldn't have gone."
		return b
	}
	b.Learned = true
	var edges []string
	if b.LivelyFrom != nil {
		edges = append(edges, fmt.Sprintf("Lively from %.1f°", *b.LivelyFrom))
	}
	if b.RoughFrom != nil {
		edges = append(edges, fmt.Sprintf("Rough from %.1f°", *b.RoughFrom))
	}
	b.Summary = fmt.Sprintf("Learned from %d rated %s: %s.", b.RatedCount, ratedNoun(b.RatedCount), strings.Join(edges, ", "))
	return b
}

// classify names the band a figure falls in, and says when it lies outside
// everything that was rated. Unlearned bands name nothing.
func (b comfortBands) classify(deg float64) (band string, outside bandOutside) {
	if !b.Learned {
		return "", insideRated
	}
	if b.LivelyFrom != nil && deg < *b.LivelyFrom {
		return bandComfortable, insideRated
	}
	if b.RoughFrom != nil && deg >= *b.RoughFrom {
		return bandRough, insideRated
	}
	if b.LivelyFrom == nil && deg < b.minRated {
		return bandLively, belowRated
	}
	if b.RoughFrom == nil && deg > b.maxRated {
		return bandLively, aboveRated
	}
	return bandLively, insideRated
}
