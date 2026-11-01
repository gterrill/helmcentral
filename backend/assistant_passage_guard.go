package main

import (
	"encoding/json"
	"log"
	"regexp"
	"strings"
	"sync"
)

// The passage-check guard (ADR 0234). On the boat, a southbound question got
// an answer that called a northerly on a 152 degree course a head sea and told
// the operator to set passage limits that were already set, with no call to
// assess_passage_window at all. The prompt already says to call it and was
// ignored, so the run loop checks the draft: an answer that judges the wind
// against the course, or talks about passage limits, without the check having
// run is sent back once. The run loop only knows the generic assistantDraftChecker
// seam; this file is what assistantToolDeps answers it with.
//
// A second trigger (ADR 0243) covers anchorages. Asked about a passage to the
// south end of Thomas Island, whose bay faces south, with a northerly forecast,
// Mate said the tip was exposed to the north and sent the operator to the
// north side. It had not called anchorage_details, which reads the charted
// shoreline and would have said the opposite. A draft that judges an
// anchorage's shelter or exposure without that call, or a get_wind_forecast
// that came back with shelter labels, is sent back the same way. The labels are
// the main fix: get_wind_forecast attaches the shelter itself.
//
// A third trigger covers the water under it. Mate has no depth for an anchorage
// of its own: the only sources are a figure the operator gave in the
// conversation and the depth of a saved place, and anchorage_details reports
// which it used. A draft that states a depth in metres, feet or fathoms for an
// anchorage with neither is sent back.

// The corrective messages sent back with a draft, one per trigger. They go in
// as a user turn labelled as Helmcentral's own check: no other part of the
// loop puts a system message mid-conversation, and some providers reject one
// after an assistant turn or hoist it to the top. Only the limits claim may
// say the limits are set, because that trigger fires only when they are.
const assistantCheckLabel = "[Helmcentral check, not from the operator] "

const (
	assistantPassageLimitsClaimInstruction  = "This answer talks about passage limits without the check having run. The operator's passage limits are set. If the question is whether or when to go, call assess_passage_window for the legs and days under discussion and base the answer on its result. Otherwise remove the statements about the limits."
	assistantPassageRelativeWindInstruction = "This answer judges the wind or sea against the course without the check having run. If the question is whether or when to go, call assess_passage_window for the legs and days under discussion and base any ahead, abeam or astern judgement on its sectors. Otherwise take the angle only from get_wind_forecast's rel_wind and rel_wave labels, called with course_deg, or remove those statements."
	assistantAnchorageDepthInstruction      = "This answer states a water depth for an anchorage that has no source. " + ruleDepth + " If the operator gave the figure, call anchorage_details with it as depth_m and base the answer on its result. Otherwise remove the depth."
	assistantAnchorageShelterInstruction    = "This answer judges shelter or exposure to the wind without the shoreline check having run. If the statements are about an anchorage, call anchorage_details with its position (from find_places, or the boat's position if it is where you are now) and when the boat arrives, and base the judgement on its sheltered, partly sheltered and exposed arcs compared against the forecast wind direction. Otherwise remove the statements about shelter and exposure."
)

var (
	// passageLimitsClaimRE matches an answer that tells the operator to set
	// passage limits, or says they are not set.
	passageLimitsClaimRE = regexp.MustCompile(`(?i)(\bset\b[^.\n]{0,60}passage limits|passage limits[^.\n]{0,40}\b(not set|aren'?t set|isn'?t set|unset|not been set|haven'?t been set)|\b(haven'?t|have not|don'?t have|do not have|without)\s+(set\s+|yet\s+|any\s+|your\s+)*passage limits)`)

	// passageRelativeWindRE matches wind or sea described relative to the
	// boat's course.
	passageRelativeWindRE = regexp.MustCompile(`(?i)(\bbow[- ]quarter|\bhead[- ]quarter|\b(stern|following) quarter|\bon (the|your) (bow|beam|quarter|nose|stern)\b|\b(head|beam|quartering|stern)\s+(sea|seas|wind|winds|swell)\b|\b(a|have|get|got|with|expect|expecting)\s+following\s+(sea|seas|wind|winds|swell)\b|\bheadwind|\bbeam reach|\bbroad reach|\bdead run\b|\bastern\b)`)

	// passageWindOrSeaRE matches a word that names wind or sea. A relative
	// term alone ("astern", "on the beam") is ordinary boat-handling or
	// target talk, so a sentence counts only when it also names wind or sea.
	passageWindOrSeaRE = regexp.MustCompile(`(?i)(wind|breeze|\bseas?\b|swell|\bwaves?\b|chop|gust|\w+erly\b|nor'?easter|sou'?wester)`)

	// anchorageShelterRE matches a shelter or exposure judgement. Bare
	// "sheltered" is left out: "the sheltered marina" is a description, not a
	// judgement against a wind.
	anchorageShelterRE = regexp.MustCompile(`(?i)(\bexposed (to|in|from)\b|\b(sheltered|untenable) in\b|\bshelter in\b|\bgood shelter\b|\bexposure to\b|\bopen to\b|\b(in|into|under) the lee of\b|\bsheltered from\b|\bshelter from\b|\bshelter against\b|\bprotected from\b|\bprotection from\b|\blee shore\b|\broadstead\b|\b(well|poorly|badly|fully|partly)[- ](sheltered|protected)\b|\bunsheltered\b)`)

	// anchorageDirectionRE matches a compass point or wind named in words, so
	// a sentence counts only when the shelter word is set against a wind or a
	// direction. The abbreviations are case-sensitive.
	anchorageDirectionRE = regexp.MustCompile(`(?i:\b(north|south|east|west)\w*|\bwinds?\b|\bbreezes?\b|\btrades?\b|\bswells?\b|\bblow(s|ing|n)?\b|nor'?easter|sou'?wester)|\b(N|NNE|NE|ENE|E|ESE|SE|SSE|S|SSW|SW|WSW|W|WNW|NW|NNW)\b`)

	// anchorageDepthFigureRE matches a depth-sized figure with a unit.
	anchorageDepthFigureRE = regexp.MustCompile(`(?i)\b\d+(?:[.,]\d+)?\s*(?:m|metres?|meters?|ft|feet|foot|fathoms?|fm)\b`)

	// anchorageDepthWordRE matches the words that make a figure a water depth.
	// A bare "charted" is left out: "a mooring charted at 12 m" is a vessel size.
	anchorageDepthWordRE = regexp.MustCompile(`(?i)\b(depths?|deep(?:er|est)?|shoal(?:s|ed|er|est)?|shallow(?:er|est)?|soundings?|fathoms?|of water|water depth|under (?:the|your|her) keel|below (?:the )?keel)\b`)

	// anchorageAnchorInRE matches the common phrasing that needs no depth word:
	// "anchored in 5 m", "anchor in 12 feet", "drop the anchor in 3 fathoms".
	// The figure must follow "in" directly, so "anchored in the bay, 5 m from
	// the reef" and a mooring size do not match.
	anchorageAnchorInRE = regexp.MustCompile(`(?i)\banchor(?:ed|ing)?\s+in\s+(?:about\s+|around\s+|roughly\s+|some\s+)?\d+(?:[.,]\d+)?\s*(?:m|metres?|meters?|ft|feet|foot|fathoms?|fm)\b`)

	// anchorageWordRE matches the places a depth is being given for.
	anchorageWordRE = regexp.MustCompile(`(?i)\b(anchor\w*|bay|cove|bight|inlet|moorings?|roadstead|holding|swing\w*)\b`)

	// passageSentenceSplitRE splits a draft at sentence ends and newlines,
	// leaving decimals such as "1.4 m" whole.
	passageSentenceSplitRE = regexp.MustCompile(`[.!?]+(\s|$)|\n`)
)

// anySentenceMatchesAll reports whether any one sentence matches every
// expression. The expressions are tried in the order given, so the cheapest
// goes first.
func anySentenceMatchesAll(sentences []string, res ...*regexp.Regexp) bool {
	for _, sentence := range sentences {
		matched := true
		for _, re := range res {
			if !re.MatchString(sentence) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// judgesRelativeWind reports whether any sentence both describes something
// relative to the boat and names wind or sea.
func judgesRelativeWind(sentences []string) bool {
	return anySentenceMatchesAll(sentences, passageRelativeWindRE, passageWindOrSeaRE)
}

// statesAnchorageDepth reports whether any sentence gives a depth figure for
// an anchorage: a figure with a length unit, a depth word and anchorage
// wording together, or "anchor in <figure>" on its own. A vessel size, a tide range or a distance does not carry a
// depth word, and a depth word with no figure is not a figure.
func statesAnchorageDepth(sentences []string) bool {
	return anySentenceMatchesAll(sentences, anchorageDepthFigureRE, anchorageDepthWordRE, anchorageWordRE) ||
		anySentenceMatchesAll(sentences, anchorageAnchorInRE)
}

// judgesAnchorageShelter reports whether any sentence both judges shelter or
// exposure and names a wind or a direction.
func judgesAnchorageShelter(sentences []string) bool {
	return anySentenceMatchesAll(sentences, anchorageShelterRE, anchorageDirectionRE)
}

// checkDraft makes assistantToolDeps an assistantDraftChecker. It names what
// in a draft answer needs the passage check, or returns "" when the draft is
// fine as it stands:
//
//   - "passage limits claim": the draft talks of setting limits that are set,
//     and assess_passage_window was not called this run (it is the only
//     source that says the limits are unset).
//   - "relative wind": the draft judges wind or sea against the course, and
//     neither assess_passage_window nor get_wind_forecast with a course_deg
//     was called this run.
//
// The passage triggers are off when assess_passage_window is not on offer.
// Unreadable limits skip the claim check (it would be a guess) and log why.
//
// A third trigger stands apart from those two:
//
//   - "anchorage shelter": the draft judges an anchorage's shelter or exposure
//     against a wind or direction, and anchorage_details was not called this
//     run, and no get_wind_forecast result carried shelter_summary. It is on when the tool could run here (the install has an Offline
//     coastline reader; whether the coastline is switched on is not read per
//     draft), whether or not assess_passage_window ran.
//
// A draft that hits more than one trigger gets one message carrying each
// correction, since the run loop sends a draft back only once.
func (d assistantToolDeps) checkDraft(draft string, calls []openRouterToolCall, results []string) (trigger, instruction string) {
	var triggers, instructions []string
	sentences := passageSentenceSplitRE.Split(draft, -1)
	// Whether anchorage_details can run here reads settings, so it is worked
	// out at most once per reply, and only for a draft that reaches it.
	armed := sync.OnceValue(d.anchorageToolArmed)
	if t, i := d.checkPassageDraft(draft, sentences, calls); t != "" {
		triggers, instructions = append(triggers, t), append(instructions, i)
	}
	if t, i := d.checkShelterDraft(sentences, calls, results, armed); t != "" {
		triggers, instructions = append(triggers, t), append(instructions, i)
	}
	if t, i := d.checkDepthDraft(sentences, calls, results, armed); t != "" {
		triggers, instructions = append(triggers, t), append(instructions, i)
	}
	if len(triggers) == 0 {
		return "", ""
	}
	if len(triggers) == 1 {
		return triggers[0], assistantCheckLabel + instructions[0]
	}
	return strings.Join(triggers, " and "), assistantCheckLabel + strings.Join(instructions, " Also: ")
}

// forecastDeliveredShelter reports whether a get_wind_forecast result carries
// shelter labels. Only a forecast that read the coastline sets shelter_summary:
// a failed call, shelter_unavailable and open water all leave it out.
func forecastDeliveredShelter(result string) bool {
	var r struct {
		ShelterSummary string `json:"shelter_summary"`
	}
	return json.Unmarshal([]byte(result), &r) == nil && r.ShelterSummary != ""
}

// shelterArmed reports whether the coastline is switched on. A coastline the
// operator turned off cannot be read, so sending a draft back to read it would
// only send Mate to a tool that says it is off. An unreadable setting disarms
// the check too, and is logged.
func (d assistantToolDeps) shelterArmed() bool {
	r := d.offlineCoastline()
	if r == nil || r.Enabled == nil {
		return false
	}
	on, err := r.Enabled()
	if err != nil {
		log.Printf("assistant: anchorage shelter check skipped: could not read the Offline coastline setting: %v", err)
		return false
	}
	return on
}

// anchorageToolArmed says whether anchorage_details can run here, so a draft
// can be sent to it: the tool is on offer and the Offline coastline it needs is
// on.
func (d assistantToolDeps) anchorageToolArmed() bool {
	return mateToolAvailable(d, mateToolAnchorageDetails.name) && d.offlineCoastline != nil && d.shelterArmed()
}

// depthSourced reports whether this run produced a depth to state: an
// anchorage_details result whose depth section is known (a saved place's or the
// operator's), or a tool call that carried a depth_m argument and ran, which is
// the operator's figure being passed on or saved. The checker cannot see the
// conversation, so it trusts the argument; the tool's description restricts it.
func depthSourced(calls []openRouterToolCall, results []string) bool {
	for i, c := range calls {
		if i >= len(results) || results[i] == "" {
			continue // the call errored or did not run
		}
		if strings.Contains(string(c.Function.Arguments), `"depth_m"`) {
			return true
		}
		if c.Function.Name != mateToolAnchorageDetails.name {
			continue
		}
		var r struct {
			Depth struct {
				Known bool `json:"known"`
			} `json:"depth"`
		}
		if json.Unmarshal([]byte(results[i]), &r) == nil && r.Depth.Known {
			return true
		}
	}
	return false
}

// checkDepthDraft is the "anchorage depth" trigger.
func (d assistantToolDeps) checkDepthDraft(sentences []string, calls []openRouterToolCall, results []string, armed func() bool) (trigger, instruction string) {
	if !statesAnchorageDepth(sentences) || !armed() || depthSourced(calls, results) {
		return "", ""
	}
	return "anchorage depth", assistantAnchorageDepthInstruction
}

// checkShelterDraft is the "anchorage shelter" trigger.
func (d assistantToolDeps) checkShelterDraft(sentences []string, calls []openRouterToolCall, results []string, armed func() bool) (trigger, instruction string) {
	if !judgesAnchorageShelter(sentences) || !armed() {
		return "", ""
	}
	for i, c := range calls {
		switch c.Function.Name {
		case mateToolAnchorageDetails.name:
			// A call whose result is empty errored or did not run.
			if i < len(results) && results[i] != "" {
				return "", ""
			}
		case mateToolGetWindForecast.name:
			if i < len(results) && forecastDeliveredShelter(results[i]) {
				return "", ""
			}
		}
	}
	return "anchorage shelter", assistantAnchorageShelterInstruction
}

// checkPassageDraft is the two passage triggers.
func (d assistantToolDeps) checkPassageDraft(draft string, sentences []string, calls []openRouterToolCall) (trigger, instruction string) {
	if !mateToolAvailable(d, mateToolAssessPassageWindow.name) {
		return "", ""
	}
	assessed, angled := false, false
	for _, c := range calls {
		switch c.Function.Name {
		case mateToolAssessPassageWindow.name:
			assessed = true
		case mateToolGetWindForecast.name:
			var args struct {
				CourseDeg *float64 `json:"course_deg"`
			}
			if json.Unmarshal([]byte(c.Function.Arguments), &args) == nil && args.CourseDeg != nil {
				angled = true
			}
		}
	}
	switch {
	case assessed:
		return "", ""
	case passageLimitsClaimRE.MatchString(draft) && d.limitsAreSet():
		return "passage limits claim", assistantPassageLimitsClaimInstruction
	case !angled && judgesRelativeWind(sentences):
		return "relative wind", assistantPassageRelativeWindInstruction
	}
	return "", ""
}

// limitsAreSet reports whether any sector has a no-go wind, the test
// assess_passage_window itself applies before it will judge anything, read
// fresh. False when the limits cannot be read, and the reason is logged.
func (d assistantToolDeps) limitsAreSet() bool {
	if d.passageLimits == nil {
		return false
	}
	l, err := d.passageLimits()
	if err != nil {
		log.Printf("assistant: passage-check guard: passage limits unreadable, claim check skipped: %v", err)
		return false
	}
	return l.anyWindNoGo()
}
