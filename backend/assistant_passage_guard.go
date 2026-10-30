package main

import (
	"encoding/json"
	"log"
	"regexp"
)

// The passage-check guard (ADR 0234). On the boat, a southbound question got
// an answer that called a northerly on a 152 degree course a head sea and told
// the operator to set passage limits that were already set, with no call to
// assess_passage_window at all. The prompt already says to call it and was
// ignored, so the run loop checks the draft: an answer that judges the wind
// against the course, or talks about passage limits, without the check having
// run is sent back once. The run loop only knows the generic assistantDraftChecker
// seam; this file is what assistantToolDeps answers it with.

// The corrective messages sent back with a draft, one per trigger. They go in
// as a user turn labelled as Helmcentral's own check: no other part of the
// loop puts a system message mid-conversation, and some providers reject one
// after an assistant turn or hoist it to the top. Only the limits claim may
// say the limits are set, because that trigger fires only when they are.
const (
	assistantPassageLimitsClaimInstruction  = "[Helmcentral check, not from the operator] This answer talks about passage limits without the check having run. The operator's passage limits are set. If the question is whether or when to go, call assess_passage_window for the legs and days under discussion and base the answer on its result. Otherwise remove the statements about the limits."
	assistantPassageRelativeWindInstruction = "[Helmcentral check, not from the operator] This answer judges the wind or sea against the course without the check having run. If the question is whether or when to go, call assess_passage_window for the legs and days under discussion and base any ahead, abeam or astern judgement on its sectors. Otherwise take the angle only from get_wind_forecast's rel_wind and rel_wave labels, called with course_deg, or remove those statements."
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

	// passageSentenceSplitRE splits a draft at sentence ends and newlines,
	// leaving decimals such as "1.4 m" whole.
	passageSentenceSplitRE = regexp.MustCompile(`[.!?]+(\s|$)|\n`)
)

// judgesRelativeWind reports whether any sentence of the draft both describes
// something relative to the boat and names wind or sea.
func judgesRelativeWind(draft string) bool {
	for _, sentence := range passageSentenceSplitRE.Split(draft, -1) {
		if passageRelativeWindRE.MatchString(sentence) && passageWindOrSeaRE.MatchString(sentence) {
			return true
		}
	}
	return false
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
// Off when assess_passage_window is not on offer. Unreadable limits skip the
// claim check (it would be a guess) and log why.
func (d assistantToolDeps) checkDraft(draft string, calls []openRouterToolCall) (trigger, instruction string) {
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
	case !angled && judgesRelativeWind(draft):
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
