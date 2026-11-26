package main

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The comfort check (ADR 0234, amended 2026-11-26). get_plans gives each leg a
// comfort whose stretches carry a band. A draft sentence that names both stops of
// one leg of a plan and gives it a band the leg did not return is sent back.
//
// Nothing else about comfort is checked. The band words are the ones get_plans
// writes, capitalised: Comfortable, Lively, Rough. A band word the sentence
// negates ("not Rough", "won't be Lively", "less Lively", "far from Rough") is no
// claim; a negation does not reach past a comma, semicolon, colon or dash. A
// sentence naming one stop, or the stops of two legs, or of more than one leg
// across the plans read, is skipped. So is a leg that is done or skipped, has no
// comfort, or lost its stretches to shrink: its bands are not known in full.
const assistantComfortMismatchInstruction = "Your draft gives a leg a comfort band that get_plans did not return for that leg: quote the leg's own stretches and their bands as get_plans gave them, or remove the statement. A leg that was not judged, or whose bands are not learned yet, has no band to quote."

var draftBandWords = []string{"Comfortable", "Lively", "Rough"}

// comfortLeg is a leg of a plan as get_plans returned it: its stops, lower-cased,
// and the bands it returned. Only a checkable leg has its bands known in full.
type comfortLeg struct {
	from, to  string
	bands     map[string]bool
	checkable bool
}

// isWordRune is a letter or digit in any script.
func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// wordIndexes is where word occurs in s as a whole word: not inside a longer run
// of letters and digits, in any script. A name ending in a bracket still counts.
func wordIndexes(s, word string) []int {
	var out []int
	if word == "" {
		return nil
	}
	for from := 0; from <= len(s)-len(word); {
		i := strings.Index(s[from:], word)
		if i < 0 {
			break
		}
		i += from
		end := i + len(word)
		before, _ := utf8.DecodeLastRuneInString(s[:i])
		after, _ := utf8.DecodeRuneInString(s[end:])
		okBefore := i == 0 || !isWordRune(before) || !isWordRune([]rune(word)[0])
		okAfter := end == len(s) || !isWordRune(after) || !isWordRune(lastRune(word))
		if okBefore && okAfter {
			out = append(out, i)
		}
		from = i + utf8.RuneLen([]rune(word)[0])
	}
	return out
}

func lastRune(s string) rune {
	r, _ := utf8.DecodeLastRuneInString(s)
	return r
}

// comfortNegated reports whether the words just before a band word negate it.
func comfortNegated(before string) bool {
	// A negation does not reach back past punctuation.
	if i := strings.LastIndexAny(before, ",;:—–"); i >= 0 {
		before = before[i:]
	}
	f := strings.Fields(strings.ToLower(before))
	if len(f) > 3 {
		f = f[len(f)-3:]
	}
	for i, w := range f {
		w = strings.Trim(w, `.,;:!?"'()`)
		switch {
		case w == "not", w == "never", w == "less", w == "no",
			strings.HasSuffix(w, "n't"), strings.HasSuffix(w, "n’t"):
			return true
		case w == "from" && i > 0 && f[i-1] == "far":
			return true
		}
	}
	return false
}

// claimedBands is the band words a sentence gives, capitalised as get_plans writes
// them, markdown emphasis stripped, negated ones left out.
func claimedBands(sentence string) []string {
	s := strings.NewReplacer("*", "", "_", "").Replace(sentence)
	var out []string
	for _, b := range draftBandWords {
		for _, i := range wordIndexes(s, b) {
			if !comfortNegated(s[:i]) {
				out = append(out, b)
				break
			}
		}
	}
	return out
}

// plansLegs is the plan of a get_plans result, keyed by its id and name, and its
// legs. A leg is checkable only when it is still to be sailed and its comfort
// still carries the stretches: a done or skipped leg holds a logged rating, a leg
// with no comfort has no band, and a shrunk result names only the worst band.
func plansLegs(result string) (key string, legs []comfortLeg) {
	var r struct {
		Plan *struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
			Legs []struct {
				From     string `json:"from"`
				To       string `json:"to"`
				Progress string `json:"progress"`
				Comfort  *struct {
					Stretches []struct {
						Band string `json:"band"`
					} `json:"stretches"`
				} `json:"comfort"`
			} `json:"legs"`
		} `json:"plan"`
	}
	if json.Unmarshal([]byte(result), &r) != nil || r.Plan == nil {
		return "", nil
	}
	key = strconv.FormatInt(r.Plan.ID, 10) + "\x00" + r.Plan.Name
	for _, l := range r.Plan.Legs {
		leg := comfortLeg{
			from:  strings.ToLower(strings.TrimSpace(l.From)),
			to:    strings.ToLower(strings.TrimSpace(l.To)),
			bands: map[string]bool{},
		}
		if l.Comfort != nil && l.Progress == "" && len(l.Comfort.Stretches) > 0 {
			leg.checkable = true
			for _, s := range l.Comfort.Stretches {
				if s.Band != "" {
					leg.bands[s.Band] = true
				}
			}
		}
		legs = append(legs, leg)
	}
	return key, legs
}

// lowerSameLen lower-cases s without changing any byte offset: a rune whose lower
// case is a different length stays as it was.
func lowerSameLen(s string) string {
	return strings.Map(func(r rune) rune {
		if l := unicode.ToLower(r); utf8.RuneLen(l) == utf8.RuneLen(r) {
			return l
		}
		return r
	}, s)
}

// stopMatch is where a plan stop is named in a sentence.
type stopMatch struct {
	name  string
	start int
}

// maskStops blanks every plan stop name out of the sentence, longest name first,
// so a stop called "Rough Point" gives no band word and "Russell Wharf" is not read
// as "Russell". It returns the blanked sentence, offsets unchanged, and the matches.
func maskStops(sentence string, names []string) (string, []stopMatch) {
	sorted := append([]string(nil), names...)
	sort.SliceStable(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	lower := []byte(lowerSameLen(sentence))
	masked := []byte(sentence)
	var found []stopMatch
	for _, n := range sorted {
		for _, i := range wordIndexes(string(lower), n) {
			found = append(found, stopMatch{n, i})
			for k := i; k < i+len(n); k++ {
				lower[k], masked[k] = ' ', ' '
			}
		}
	}
	return string(masked), found
}

// chargedText is the part of the masked sentence charged to the leg: from the
// start of the ';' clause holding the first of its stops to the end of the ';'
// clause holding the last, so "A to B is Lively; the next leg is Comfortable"
// charges only the first clause. A colon, comma or dash does not end the
// statement: "A to B isn't bad: Rough after 2 PM" charges Rough.
func chargedText(masked string, leg comfortLeg, found []stopMatch) string {
	lo, hi := len(masked), -1
	for _, m := range found {
		if m.name == leg.from || m.name == leg.to {
			lo, hi = min(lo, m.start), max(hi, m.start)
		}
	}
	if hi < 0 {
		return ""
	}
	start := strings.LastIndex(masked[:lo], ";") + 1
	end := len(masked)
	if i := strings.Index(masked[hi:], ";"); i >= 0 {
		end = hi + i
	}
	return masked[start:end]
}

// checkComfortDraft sends back a sentence that names both stops of exactly one leg
// of the plans get_plans returned this run and gives it a band the leg did not
// return. A plan read again replaces its earlier reading; different plans are kept
// apart. Two legs with the same stops, across plans or within one, make the
// sentence ambiguous and it is not checked. It is off when get_plans is not on
// offer.
func (d assistantToolDeps) checkComfortDraft(sentences []string, run mateRunRecord) (trigger, instruction string) {
	if !mateToolAvailable(d, mateToolGetPlans.name) {
		return "", ""
	}
	byPlan := map[string][]comfortLeg{}
	var order []string
	for _, c := range run.succeeded(mateToolGetPlans.name) {
		key, legs := plansLegs(c.result)
		if legs == nil {
			continue
		}
		if _, seen := byPlan[key]; !seen {
			order = append(order, key)
		}
		byPlan[key] = legs
	}
	var legs []comfortLeg
	seenName := map[string]bool{}
	var names []string
	for _, key := range order {
		for _, l := range byPlan[key] {
			legs = append(legs, l)
			for _, n := range []string{l.from, l.to} {
				if n != "" && !seenName[n] {
					seenName[n] = true
					names = append(names, n)
				}
			}
		}
	}
	if len(legs) == 0 {
		return "", ""
	}
	for _, sentence := range sentences {
		// Emphasis markers become blanks, so offsets hold.
		plain := strings.NewReplacer("*", " ", "_", " ").Replace(sentence)
		masked, found := maskStops(plain, names)
		named := map[string]bool{}
		for _, m := range found {
			named[m.name] = true
		}
		var hit []comfortLeg
		for _, l := range legs {
			if l.from != "" && l.to != "" && named[l.from] && named[l.to] {
				hit = append(hit, l)
			}
		}
		if len(hit) != 1 || !hit[0].checkable {
			continue
		}
		for _, b := range claimedBands(chargedText(masked, hit[0], found)) {
			if !hit[0].bands[b] {
				return "comfort band mismatch", assistantComfortMismatchInstruction
			}
		}
	}
	return "", ""
}
