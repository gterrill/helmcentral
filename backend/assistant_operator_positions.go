package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Positions the skipper writes in a message, converted in code. A model that
// converts 021º40.271' S itself can land 0.4 nm off and then reuse the wrong
// figure for the rest of the conversation, so Helmcentral does the sum and
// hands over the decimals (ADR 0254).
//
// The reader is strict, on one rule: a position is a latitude half and a
// longitude half side by side, each with a hemisphere letter, a degree mark and
// a minute mark. Only that form needs the arithmetic a model gets wrong: a
// decimal needs at most a sign. And it is what tells a position from a wind, a
// course or a clearance in feet. Anything else is left out, and anything shaped
// like a half beside a position voids it, since no position is better than a
// wrong one handed over as fact.

// maxOperatorPositions caps how many positions go into one prompt.
const maxOperatorPositions = 8

// operatorPosition is one position as the skipper wrote it, with its signed
// decimal degrees rounded to six places.
type operatorPosition struct {
	Text string
	Lat  float64
	Lon  float64
}

const (
	opGap      = `[ \t]*`
	opDegMarks = "°º˚"
	opMinMarks = "'′’"
	opSecMarks = "\"”″"
	opHemi     = `[NSEWnsew]`
	opDecimal  = `\d+(?:\.\d+)?`
	// Degrees, minutes, seconds.
	opBody = `(\d+)` + opGap + `[` + opDegMarks + `]` + opGap + `(` + opDecimal + `)` + opGap + `[` + opMinMarks + `]` +
		`(?:` + opGap + `(` + opDecimal + `)` + opGap + `(?:[` + opSecMarks + `]|''|′′|’’))?`
	// The numbers and marks a half is made of.
	opFigures = `\d.` + opDegMarks + opMinMarks + opSecMarks + ` \t`
)

var (
	// 20°32.676' S, 20°32'40.5" S.
	opTrailing = regexp.MustCompile(`^` + opBody + opGap + `(` + opHemi + `)\b`)
	// S 21°40.271'.
	opLeading = regexp.MustCompile(`^(` + opHemi + `)` + opGap + opBody)
	// A letter closing what was read as a leading half, not opening another.
	opClosingLetter = regexp.MustCompile(`^` + opGap + opHemi + `\b` + opGap + `($|[^\d \t])`)
	// 150° E, 20.5 S, S 20.5, 40 16' S: not what the reader takes, but close
	// enough that a position beside it cannot be trusted to be paired rightly.
	opHalfLike = regexp.MustCompile(`^(?:[` + opFigures + `]+` + opHemi + `\b|` + opHemi + opGap + `\d[` + opFigures + `]*)`)
	// What may stand between two halves of one run.
	opSeparator = regexp.MustCompile(`^(?i:[\s,/:–—-]|and\b|lat(?:itude)?\b|lon(?:g|gitude)?\b)*$`)
)

// operatorHalf is one half of a position, or something shaped like one when
// ok is false: that still takes its place in a run, and voids it.
type operatorHalf struct {
	start, end int
	ok         bool
	lat        bool
	value      float64
}

// parseOperatorPositions returns every latitude and longitude pair in text, in
// the order written.
func parseOperatorPositions(text string) []operatorPosition {
	// Most messages have no mark at all, and every half needs one.
	if !strings.ContainsAny(text, opMinMarks) {
		return nil
	}
	var halves []operatorHalf
	for i := 0; i < len(text); {
		_, size := utf8.DecodeRuneInString(text[i:])
		next := i + size
		if startsOperatorHalf(text, i) {
			if h := readOperatorHalf(text, i); h.end > i {
				halves = append(halves, h)
				next = h.end
			}
		}
		i = next
	}

	// Halves with only a separator between them form a run, which is a list of
	// positions only if every half has its partner.
	var out []operatorPosition
	for k := 0; k < len(halves); {
		next := k + 1
		for next < len(halves) && operatorHalvesJoin(text, halves[next-1], halves[next]) {
			next++
		}
		out = append(out, pairOperatorHalves(text, halves[k:next])...)
		k = next
	}
	return out
}

// operatorHalvesJoin says whether two neighbouring halves are in one run. A blank
// line ends a run, and a new line keeps a heading or a depth on the next line
// from voiding a position it has nothing to do with.
func operatorHalvesJoin(text string, a, b operatorHalf) bool {
	gap := text[a.end:b.start]
	if !opSeparator.MatchString(gap) {
		return false
	}
	lines := strings.Count(gap, "\n")
	return lines == 0 || (lines == 1 && a.ok && b.ok)
}

// pairOperatorHalves reads a run two halves at a time, or returns nothing if any
// pair does not hold together: a stray or failed half leaves no telling which
// belong.
func pairOperatorHalves(text string, run []operatorHalf) []operatorPosition {
	if len(run)%2 != 0 {
		return nil
	}
	var out []operatorPosition
	for k := 0; k < len(run); k += 2 {
		a, b := run[k], run[k+1]
		if !a.ok || !b.ok || a.lat == b.lat {
			return nil
		}
		lat, lon := a.value, b.value
		if !a.lat {
			lat, lon = b.value, a.value
		}
		out = append(out, operatorPosition{Text: strings.Join(strings.Fields(text[a.start:b.end]), " "), Lat: lat, Lon: lon})
	}
	return out
}

// startsOperatorHalf says whether position i can begin a half: a digit or a
// hemisphere letter, not in the middle of a word or number, and not after a
// sign, a mark or an apostrophe, which would read the tail of something else.
func startsOperatorHalf(text string, i int) bool {
	if !isOperatorDigit(rune(text[i])) && !strings.ContainsRune("NSEWnsew", rune(text[i])) {
		return false
	}
	prev, _ := utf8.DecodeLastRuneInString(text[:i])
	return !unicode.IsLetter(prev) && !isOperatorDigit(prev) &&
		!strings.ContainsRune(".-+−"+opDegMarks+opMinMarks+opSecMarks, prev)
}

// isOperatorDigit is the ASCII digit the expressions above read.
func isOperatorDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// readOperatorHalf reads what is at i. Its end is i when the text there has no
// shape of a half, and it is not ok when the shape failed the exact checks.
func readOperatorHalf(text string, i int) operatorHalf {
	rest := text[i:]
	var whole, hemi string
	var parts []string
	if isOperatorDigit(rune(rest[0])) {
		if m := opTrailing.FindStringSubmatch(rest); m != nil {
			whole, hemi, parts = m[0], m[4], m[1:4]
		}
	} else if m := opLeading.FindStringSubmatch(rest); m != nil {
		whole, hemi, parts = m[0], m[1], m[2:5]
		// A leading half has no letter to close it, so it must not run into a
		// following word or number. A letter closing it means the letter it was
		// read with was a label: Mark E 12°30' N 61°30' W.
		tail := rest[len(whole):]
		after, _ := utf8.DecodeRuneInString(tail)
		cutShort := len(tail) > 1 && tail[0] == '.' && isOperatorDigit(rune(tail[1]))
		if unicode.IsLetter(after) || isOperatorDigit(after) || cutShort || opClosingLetter.MatchString(tail) {
			return operatorHalf{start: i, end: i + len(whole)}
		}
	}
	if whole == "" {
		like := opHalfLike.FindString(rest)
		// Plain numbers beside a letter are a wind or a distance: 15 N, S 15.
		if !strings.ContainsAny(like, "."+opDegMarks+opMinMarks+opSecMarks) {
			like = ""
		}
		return operatorHalf{start: i, end: i + len(like)}
	}
	end := i + len(whole)

	upper := strings.ToUpper(hemi)
	lat := upper == "N" || upper == "S"
	value, ok := operatorHalfValue(parts, lat)
	if !ok {
		return operatorHalf{start: i, end: end}
	}
	value = roundTo(value, 6)
	if (upper == "S" || upper == "W") && value != 0 {
		value = -value
	}
	return operatorHalf{start: i, end: end, ok: true, lat: lat, value: value}
}

// operatorHalfValue turns the captured parts (degrees, minutes, seconds) into
// unsigned decimal degrees, or reports that they are not an exact reading.
func operatorHalfValue(p []string, lat bool) (float64, bool) {
	minText, secText := p[1], p[2]
	if secText != "" && strings.Contains(minText, ".") {
		return 0, false
	}
	deg, _ := strconv.ParseFloat(p[0], 64)
	min, _ := strconv.ParseFloat(minText, 64)
	var sec float64
	if secText != "" {
		sec, _ = strconv.ParseFloat(secText, 64)
	}
	if min >= 60 || sec >= 60 {
		return 0, false
	}
	value := deg + min/60 + sec/3600
	limit := 180.0
	if lat {
		limit = 90
	}
	return value, value <= limit
}

// operatorPositionsFromMessages collects the positions the skipper wrote, newest
// message first and each in the order written, each position once, at most
// maxOperatorPositions. Only the skipper's rows are read: an answer of Mate's is
// not the skipper's word.
func operatorPositionsFromMessages(messages []assistantMessage) []operatorPosition {
	var out []operatorPosition
	seen := map[[2]float64]bool{}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		for _, p := range parseOperatorPositions(messages[i].Content) {
			key := [2]float64{p.Lat, p.Lon}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, p)
			if len(out) == maxOperatorPositions {
				return out
			}
		}
	}
	return out
}

// operatorPositionsNote is the live-prompt paragraph for positions the skipper
// gave, or "" when there are none.
func operatorPositionsNote(positions []operatorPosition) string {
	if len(positions) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Positions the skipper gave, converted by Helmcentral. Newest message first; where one message gives " +
		"several, its words say which the skipper means. When the skipper corrects a position, the newer one replaces " +
		"an earlier one, and a later correction in any other form also replaces the one listed. Use these decimals " +
		"exactly in tool calls whenever you use that position, and do not convert a position yourself. The skipper's " +
		"figure outranks a find_places result or an earlier answer of yours for the same place. It says nothing about " +
		"a different place.\n")
	for _, p := range positions {
		fmt.Fprintf(&b, "- %s is lat %.6f, lon %.6f.\n", p.Text, p.Lat, p.Lon)
	}
	b.WriteString("\n")
	return b.String()
}
