package main

import (
	"regexp"
	"strings"
)

// This file is the no-Mate half of note classification (plan §9's "no-Mate
// path" table): classifyNoteType turns a note's body into one of six
// note_type values using nothing but local text heuristics - no network
// call, no OpenRouter spend, no dependency on the operator ever configuring
// Mate at all. Every capability this feature offers has to work without it,
// and "what kind of note is this" is the first one an operator sees (it
// drives the icon/colour every row renders with - notes-panel.tsx, a later
// phase).

// Note type sentinels, named rather than left as bare string literals
// scattered through this file and notes_handlers.go: a typo in one of these
// is a compile error instead of a silently-never-matching case, and they
// are exactly the five non-empty values documents_store.go's note_type
// CHECK constraint allows.
const (
	noteTypeContact   = "contact"
	noteTypeQuirk     = "quirk"
	noteTypeSpec      = "spec"
	noteTypeProcedure = "procedure"
	noteTypeRecipe    = "recipe"
	noteTypeNote      = "note"
)

// validNoteTypes is the set an explicit operator override (POST/PATCH's
// "type" field, notes_handlers.go) may name - every value classifyNoteType
// can produce, and nothing else. The empty string (note_type's own
// DEFAULT, meaning "not yet classified") is deliberately excluded: an
// operator can pick any of the six visible categories, but this API has no
// "clear the classification back to unset" action.
var validNoteTypes = map[string]bool{
	noteTypeContact:   true,
	noteTypeQuirk:     true,
	noteTypeSpec:      true,
	noteTypeProcedure: true,
	noteTypeRecipe:    true,
	noteTypeNote:      true,
}

func validNoteType(s string) bool { return validNoteTypes[s] }

// Structural signals: a Markdown task-list item ("- [ ] " / "- [x] ",
// starred bullets too) or a numbered step ("1. ", "2) ") are the strongest
// evidence a note is a procedure - stronger than any keyword, because a
// skipper writing a checklist doesn't need to say the word "procedure" for
// it to obviously be one.
var (
	taskListItemRe = regexp.MustCompile(`(?m)^\s*[-*]\s*\[[ xX]\]`)
	numberedStepRe = regexp.MustCompile(`(?m)^\s*\d+[.)]\s+\S`)
)

// Contact's own structural signals: a phone number or an email address.
// Deliberately loose (a phone pattern matches most digit-and-separator runs
// of plausible length) because catching a false positive costs nothing here
// - contact is checked last, after every more specific category, so a
// phone-shaped number inside a procedure or a spec never reaches this far
// (see classifyNoteType's own doc comment on why the order matters).
var (
	phoneRe = regexp.MustCompile(`\b(?:\+?\d[\d ().-]{6,}\d)\b`)
	emailRe = regexp.MustCompile(`[[:alnum:]._%+\-]+@[[:alnum:].\-]+\.[[:alpha:]]{2,}`)
)

// Keyword sets, checked case-insensitively against the whole body via
// containsAny. Each is deliberately a little generous - a false positive
// inside one category just means the classifier's "auto" guess is one tap
// away from being corrected (note_type_source='operator' overrides,
// notes_handlers.go), which is a far cheaper mistake than the note landing
// in the wrong place with nothing to fix it.
// "chop" and "stir" are deliberately left out even though both are ordinary
// kitchen verbs: "chop" is everyday boating language for wave state ("heavy
// chop off the bar") and "stir" shows up in idiom far too often outside a
// galley - both would misfire on notes that have nothing to do with food.
var recipeKeywords = []string{
	"recipe", "ingredient", "tbsp", "tablespoon", "tsp", "teaspoon",
	"preheat", "simmer", "whisk", "marinate", "saute", "sauté",
	"cup ", "cups ", "oven", "bake", "boil", "roast",
	"grill", "garnish", "dough", "batter",
}

var specKeywords = []string{
	"rpm", "psi", "voltage", "amperage", "capacity", "dimension",
	"litre", "liter", "gallon", "displacement", "specification",
	"kw ", "hp ", " bar ", "torque", "wattage", "amp-hour", "ah ",
}

var procedureKeywords = []string{
	"procedure", "checklist", "step by step", "step-by-step",
	"shut down", "shutdown", "shut-down", "start up", "startup",
	"start-up", "before leaving", "getting underway",
}

var quirkKeywords = []string{
	"quirk", "gotcha", "watch out", "careful", "don't forget",
	"do not forget", "tends to", "known issue", "workaround", "flaky",
}

var contactKeywords = []string{"phone", "mobile", "cell", "contact", "reach him", "reach her", "email"}

// containsAny reports whether lower (already-lowercased text) contains any
// of words as a plain substring.
func containsAny(lower string, words []string) bool {
	for _, w := range words {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// classifyNoteType assigns a deterministic note_type from body alone - pure
// text heuristics, no network call, no Mate (plan §9). Its checking order
// encodes two precedence traps named explicitly in the plan's Verification
// section, and getting the order wrong is the whole way to fail this
// function even though every individual keyword list looks reasonable in
// isolation:
//
//  1. A procedure that happens to mention a phone number ("call the yard at
//     555-0142 if the genset won't restart") must classify as procedure,
//     not contact. Procedure's structural signals (checklist markers,
//     numbered steps) and keyword set are therefore checked well before
//     contact's phone/email pattern - contact is checked last of all five,
//     because a phone number or email address is the least specific signal
//     here: it can appear inside a note of any other type without making
//     the note ABOUT contacting someone.
//  2. A recipe's quantities ("2 cups flour", "500g sugar", "bake at 180°C")
//     are exactly the kind of unit-bearing numeric text spec's keyword set
//     exists to catch. Recipe is therefore checked ahead of spec - it has
//     the more specific vocabulary (nobody bakes an alternator) and the
//     most to lose by being shadowed.
//
// An empty body, or one matching none of the five categories, falls
// through to "note" - the least specific type there is, and always a safe
// default (never an error: an unclassifiable note is still a note).
func classifyNoteType(body string) string {
	lower := strings.ToLower(body)

	switch {
	case containsAny(lower, recipeKeywords):
		return noteTypeRecipe
	case taskListItemRe.MatchString(body), numberedStepRe.MatchString(body), containsAny(lower, procedureKeywords):
		return noteTypeProcedure
	case containsAny(lower, quirkKeywords):
		return noteTypeQuirk
	case containsAny(lower, specKeywords):
		return noteTypeSpec
	case phoneRe.MatchString(body), emailRe.MatchString(body), containsAny(lower, contactKeywords):
		return noteTypeContact
	default:
		return noteTypeNote
	}
}
