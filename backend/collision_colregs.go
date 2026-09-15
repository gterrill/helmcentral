package main

import "fmt"

// This file turns a collision alarm's geometry and vessel types into the
// COLREGS "situation + role" line the card shows (ADR 0098): what kind of
// encounter this is, and what the rules ask of us in it. classifyEncounter
// has no snapshot access -- every input is a plain number or string the
// caller already read (signalKCollisionNotifications for AIS,
// radarCollisionNotifications for radar once ADR 0062 §8 ships it) -- which
// is what makes it fully table-testable (collision_colregs_test.go).
//
// It names a rule and a role. It does not compute a heading, and it is not a
// substitute for keeping a lookout (ADR 0098).

// encounterKind is what produced this contact: an AIS target, which carries
// a navigation status and her own reported course, or a radar/ARPA contact,
// which carries neither.
type encounterKind string

const (
	encounterKindAIS   encounterKind = "ais"
	encounterKindRadar encounterKind = "radar"
)

// encounterRole is which side of a COLREGS rule we are on. Rule 14's
// obligation falls on both vessels equally, which is neither give-way nor
// stand-on, so it gets its own value rather than being forced into one.
type encounterRole string

const (
	encounterRoleGiveWay    encounterRole = "give-way"
	encounterRoleStandOn    encounterRole = "stand-on"
	encounterRoleMutual     encounterRole = "mutual"      // Rule 14: both alter to starboard
	encounterRoleDontImpede encounterRole = "dont-impede" // Rule 18(d)
	encounterRoleNone       encounterRole = ""            // situation known, role isn't
)

// Rule 13(b)'s overtaking arc: a vessel more than 22.5 degrees abaft
// another's beam is overtaking her. 112.5 is 90+22.5 and 247.5 is 270-22.5,
// so the arc spans dead astern with 22.5 degrees of leeway either side of
// each beam.
const (
	overtakingArcLowDeg  = 112.5
	overtakingArcHighDeg = 247.5
)

// headOnToleranceDeg is Rule 14's "end on" read as within 6 degrees of dead
// ahead on both vessels' bearings of each other -- wide enough to absorb
// ordinary GPS/compass wander, narrow enough to still mean head-on rather
// than a fine crossing.
const headOnToleranceDeg = 6.0

// minEncounterSOGKn is the speed floor below which a reported course is not
// a course at all. ADR 0057 §6 and ADR 0062 §7b found the same failure on
// two different inputs: with a vessel stationary the relative velocity
// vector collapses to noise, CPA tends toward present range, and any
// geometry built from that is garbage wearing a bearing's clothes.
const minEncounterSOGKn = 1.0

// AIS navigation.state values this classifier keys off, spelled exactly as
// the SignalK schema's state enum (schemas/groups/navigation.json), not as
// COLREGS phrases them. "motoring" and "anchored" are confirmed against a
// live capture (testdata/signalk_notifications/README.md); the rest come
// from the schema alone, since no target this boat has heard from has
// transmitted them. Two are worth flagging because they read like COLREGS
// quotes and are not: the schema spells restricted manoeuvrability wrong
// ("manouverability", missing the second e), and constrained-by-draught
// short ("constrained by draft", no "her"). Both are matched verbatim --
// guessing at the "correct" spelling would just silently stop matching real
// traffic, which is the masking fallback the project forbids.
const (
	navStateNotUnderCommand     = "not under command"
	navStateRestrictedManoeuvre = "restricted manouverability"
	navStateFishing             = "fishing"
	navStateConstrainedByDraft  = "constrained by draft"
	navStateSailing             = "sailing"
	navStateMotoring            = "motoring"
)

// encounterInputs is everything classifyEncounter needs. The caller reads
// every field off a snapshot (AIS) or a radarTarget (radar); this file never
// touches either.
type encounterInputs struct {
	Kind encounterKind

	// Own vessel, read once per call from the self tree.
	OwnHeadingDeg float64
	OwnHeadingOK  bool

	OwnSOGKn float64
	OwnSOGOK bool

	// OwnState is our own navigation.state (signalk-autostate's own
	// vocabulary: "sailing", "motoring", "anchored", "moored", or anything
	// else). Only the first two give us a role to play; the rest give the
	// situation with no role line, because a vessel that isn't under way in
	// any COLREGS sense has no give-way/stand-on duty to report.
	OwnState string

	// OwnApparentWindAngleDeg is signed, negative to port, exactly the
	// SignalK convention (environment.wind.angleApparent's own meta says so
	// -- testdata/signalk_notifications/README.md). Positive is starboard,
	// which is what makes it double as our tack for Rule 12.
	OwnApparentWindAngleDeg float64
	OwnApparentWindAngleOK  bool

	// BearingToTargetDeg (true bearing from us to her) and BearingToSelfDeg
	// (true bearing from her to us) are both computed by the caller from the
	// two vessels' own positions via bearingDeg (poi_providers.go), never
	// trusted from a plugin's own bearing figure, whose direction has never
	// been verified (ADR 0057). One flag covers both: the caller only ever
	// has both or neither, since it needs both positions to compute either,
	// and the reciprocal is computed independently rather than assumed to be
	// the forward bearing plus 180 -- a great-circle reciprocal isn't that,
	// except by coincidence.
	BearingToTargetDeg float64
	BearingToSelfDeg   float64
	BearingOK          bool

	TargetCOGDeg float64
	TargetCOGOK  bool

	TargetSOGKn float64
	TargetSOGOK bool

	// TCPASeconds is read straight off the plugin's (AIS) or mayara's
	// (radar) own figure, never recomputed here -- ADR 0057 decision 2 and
	// ADR 0062 decision 2 already settled that those are trusted as given.
	// classifyEncounter only ever reads its sign.
	TCPASeconds float64
	TCPAOK      bool

	// TargetNavState is the AIS navigation.state value, verbatim off the
	// wire (e.g. "motoring", "restricted manouverability"). Empty for a
	// radar contact, which carries no such field.
	TargetNavState string

	// TargetShipTypeID is design.aisShipType.id, carried through for
	// completeness. It is never consulted for role: a sailboat's AIS ship
	// type (36 or 37) says nothing about whether her engine happens to be
	// running right now, and guessing from it would be exactly the
	// assumed-default the fallback policy forbids. Her navigation.state is
	// the only field this classifier trusts for that.
	TargetShipTypeID   int
	TargetShipTypeIDOK bool
}

// encounter is classifyEncounter's finished answer: the situation, the role
// COLREGS assigns us in it (when it can be determined), the rule that
// applies, and a complete sentence built here so the frontend has nothing
// left to derive.
type encounter struct {
	Situation string
	Role      encounterRole
	Rule      string
	Text      string
}

// classifyEncounter turns one target's geometry and type into a COLREGS
// encounter line, or reports false when no line should show at all --
// rather than a wrong one built on a stationary vector or a guessed vessel
// type (ADR 0057 §6, ADR 0062 §7b, and the fallback policy generally).
func classifyEncounter(in encounterInputs) (encounter, bool) {
	if !in.TargetSOGOK || in.TargetSOGKn < minEncounterSOGKn {
		return encounter{}, false
	}
	if !in.OwnSOGOK || in.OwnSOGKn < minEncounterSOGKn {
		return encounter{}, false
	}
	if !in.OwnHeadingOK || !in.BearingOK || !in.TargetCOGOK {
		return encounter{}, false
	}
	if !in.TCPAOK || in.TCPASeconds < 0 {
		return encounter{}, false
	}

	rb := relativeBearingDeg(in.OwnHeadingDeg, in.BearingToTargetDeg)     // our relative bearing of her, 0-360
	rbt := relativeBearingDeg(in.TargetCOGDeg, in.BearingToSelfDeg)       // her relative bearing of us, 0-360
	foldedRB := relativeAngleDeg(in.OwnHeadingDeg, in.BearingToTargetDeg) // 0-180 magnitude, reused for the descriptive label
	foldedRBt := relativeAngleDeg(in.TargetCOGDeg, in.BearingToSelfDeg)

	sheOvertakesUs := float64(rb) > overtakingArcLowDeg && float64(rb) < overtakingArcHighDeg
	weOvertakeHer := float64(rbt) > overtakingArcLowDeg && float64(rbt) < overtakingArcHighDeg
	headOn := float64(foldedRB) <= headOnToleranceDeg && float64(foldedRBt) <= headOnToleranceDeg

	var situation string
	switch {
	case sheOvertakesUs || weOvertakeHer:
		situation = "Overtaking"
	case headOn:
		situation = "Head-on"
	default:
		situation = "Crossing"
	}

	sideIsStarboard := rb < 180
	side := "port"
	if sideIsStarboard {
		side = "starboard"
	}
	positionClause := fmt.Sprintf("she is on our %s %s (%03d° rel)", side, bearingLabel(foldedRB), rb)

	if in.Kind == encounterKindRadar {
		return classifyRadarEncounter(situation, positionClause, weOvertakeHer, foldedRB), true
	}

	// Rule 13 first: the overtaking vessel gives way whatever the vessel
	// types, so this is decided before either vessel's type is even looked
	// at.
	if sheOvertakesUs || weOvertakeHer {
		return classifyOvertaking(situation, positionClause, sheOvertakesUs), true
	}

	ourType, ourTypeOK := ownVesselTypeFromState(in.OwnState)
	if !ourTypeOK {
		// Anchored, moored, or anything else signalk-autostate reports: we
		// have no COLREGS role to play right now, so the card says only
		// what geometry already established.
		return encounter{Situation: situation, Role: encounterRoleNone, Text: situationOnlySentence(situation, positionClause)}, true
	}

	herType := targetTypeFromNavState(in.TargetNavState)
	switch herType {
	case targetTypePriority:
		text := sentenceWithBody(situation, positionClause, "We give way (Rule 18): keep clear of her.")
		return encounter{Situation: situation, Role: encounterRoleGiveWay, Rule: "18", Text: text}, true

	case targetTypeConstrainedByDraft:
		text := sentenceWithBody(situation, positionClause, "Don't impede her (Rule 18(d)): she's constrained by her draught.")
		return encounter{Situation: situation, Role: encounterRoleDontImpede, Rule: "18(d)", Text: text}, true

	case targetTypeUnknown:
		text := sentenceWithBody(situation, positionClause, "Her type isn't transmitted, so the role can't be set.")
		return encounter{Situation: situation, Role: encounterRoleNone, Text: text}, true

	case targetTypeSail:
		if ourType == ownTypeSail {
			return classifyBothSailing(situation, positionClause, rb, in.OwnApparentWindAngleDeg, in.OwnApparentWindAngleOK), true
		}
		// We're power-driven, she's under sail: Rule 18 puts the give-way
		// duty on us regardless of the crossing/head-on geometry.
		text := sentenceWithBody(situation, positionClause, "We give way (Rule 18): keep clear of her, she's under sail.")
		return encounter{Situation: situation, Role: encounterRoleGiveWay, Rule: "18", Text: text}, true

	default: // targetTypePower
		if ourType == ownTypePower {
			return classifyBothPower(situation, positionClause, sideIsStarboard), true
		}
		// We're under sail, she's power-driven: Rule 18 puts the give-way
		// duty on her.
		text := sentenceWithBody(situation, positionClause, "We stand on (Rule 18): hold course and speed; act if she doesn't.")
		return encounter{Situation: situation, Role: encounterRoleStandOn, Rule: "18", Text: text}, true
	}
}

// bearingLabel names a folded relative-bearing magnitude (0-180, from
// relativeAngleDeg) the way a lookout actually says it: on the bow, abeam,
// or on the quarter. This is its own three-band scale rather than
// relativeAngleLabel's five (assistant_geometry.go) -- that one was built
// for a wind/wave direction's finer head/bow/beam/quarter/following
// gradient, and its 45-degree "head" band would call the plan's own worked
// example (040 degrees relative) a head bearing rather than a bow one.
func bearingLabel(foldedDeg int) string {
	switch {
	case foldedDeg <= 45:
		return "bow"
	case foldedDeg <= 135:
		return "beam"
	default:
		return "quarter"
	}
}

// situationOnlySentence is the whole card line when no role can be set: the
// geometry, and nothing else.
func situationOnlySentence(situation, positionClause string) string {
	return fmt.Sprintf("%s, %s.", situation, positionClause)
}

// sentenceWithBody appends the rule/role sentence after the situation
// clause -- the two-sentence form every role-bearing branch below uses.
func sentenceWithBody(situation, positionClause, body string) string {
	return fmt.Sprintf("%s, %s. %s", situation, positionClause, body)
}

// classifyOvertaking handles Rule 13, once the caller has already
// established that one vessel is overtaking the other.
func classifyOvertaking(situation, positionClause string, sheOvertakesUs bool) encounter {
	if sheOvertakesUs {
		text := sentenceWithBody(situation, positionClause, "We stand on (Rule 13): hold course and speed; she must keep clear until past and clear.")
		return encounter{Situation: situation, Role: encounterRoleStandOn, Rule: "13", Text: text}
	}
	text := sentenceWithBody(situation, positionClause, "We give way (Rule 13): keep clear until past and clear.")
	return encounter{Situation: situation, Role: encounterRoleGiveWay, Rule: "13", Text: text}
}

// classifyBothPower handles Rules 14 and 15, reached only once both
// vessels' types have resolved to power-driven.
func classifyBothPower(situation, positionClause string, sideIsStarboard bool) encounter {
	if situation == "Head-on" {
		text := sentenceWithBody(situation, positionClause, "Each alters to starboard (Rule 14).")
		return encounter{Situation: situation, Role: encounterRoleMutual, Rule: "14", Text: text}
	}

	if sideIsStarboard {
		// Rule 15: the vessel with the other on her own starboard side gives way.
		text := sentenceWithBody(situation, positionClause, "We give way (Rule 15): alter to starboard, pass astern.")
		return encounter{Situation: situation, Role: encounterRoleGiveWay, Rule: "15", Text: text}
	}

	// She's on our port side, so we stand on -- and Rule 17(c) is exactly
	// this case: a power-driven stand-on vessel should not alter to port for
	// a vessel on her own port side.
	text := sentenceWithBody(situation, positionClause, "We stand on (Rule 15): hold course and speed; act if she doesn't. Rule 17(c): don't alter to port for her.")
	return encounter{Situation: situation, Role: encounterRoleStandOn, Rule: "15", Text: text}
}

// classifyBothSailing handles Rule 12, reached only once both vessels'
// types have resolved to sail.
//
// Our tack comes from the sign of our own apparent wind angle. Her tack does
// not: no AIS field carries a target's wind, so it can never be read, only
// inferred, and inferring it from her AIS ship type or her course would be
// exactly the assumed default the fallback policy forbids.
//
// What CAN be read without her tack is which of the two of us is further
// upwind -- that's a fact about where the two vessels sit relative to the
// wind axis, not about either hull's heading, so our own wind angle plus her
// relative bearing settles it on its own.
//
// On port tack, rule 12(a)(i) gives way whenever tacks differ, and even if
// they don't, 12(a)(ii) would only ask us to give way if we were the
// windward one -- so port tack always gives way, safely.
//
// On starboard tack, the case that has to be ruled out is the one where
// rule 12(a)(ii) could flip the duty onto us: same tack, and us the windward
// (give-way) vessel. That only happens if she is to LEEWARD of us -- if she
// is to windward, she'd carry the give-way duty on the same tack, so
// standing on is safe either way. Leeward is where her unknown tack still
// matters, so that's where the line stops claiming a role it can't stand
// behind.
func classifyBothSailing(situation, positionClause string, rb int, ownWindAngleDeg float64, ownWindAngleOK bool) encounter {
	if !ownWindAngleOK {
		return encounter{Situation: situation, Role: encounterRoleNone, Text: situationOnlySentence(situation, positionClause)}
	}

	ourStarboardTack := ownWindAngleDeg >= 0
	if !ourStarboardTack {
		text := sentenceWithBody(situation, positionClause, "We give way (Rule 12): we're on port tack.")
		return encounter{Situation: situation, Role: encounterRoleGiveWay, Rule: "12", Text: text}
	}

	sheIsStarboardSide := rb < 180
	windwardOfUs := sheIsStarboardSide == ourStarboardTack
	if windwardOfUs {
		text := sentenceWithBody(situation, positionClause, "We stand on (Rule 12): hold course and speed.")
		return encounter{Situation: situation, Role: encounterRoleStandOn, Rule: "12", Text: text}
	}

	return encounter{Situation: situation, Role: encounterRoleNone, Text: situationOnlySentence(situation, positionClause)}
}

// classifyRadarEncounter handles a radar/ARPA contact: no AIS, no nav
// status, no type on either side of the pairing, so none of Rules 12-18's
// give-way/stand-on framework applies. Rule 19(d) is the rule that governs a
// vessel detected only by radar, and it assigns no role at all -- just two
// cautions about which way not to turn.
func classifyRadarEncounter(situation, positionClause string, weOvertakeHer bool, foldedRB int) encounter {
	var advisory string
	switch {
	case weOvertakeHer:
		// Rule 19(d)(i) exempts a vessel being overtaken from the
		// forward-of-the-beam caution.
		advisory = "the alter-to-port caution doesn't bind for a vessel you're overtaking (Rule 19(d)(i))"
	case foldedRB <= 90:
		advisory = "avoid altering course to port for her (Rule 19(d)(i))"
	default:
		advisory = "avoid altering course towards her (Rule 19(d)(ii))"
	}

	text := fmt.Sprintf("%s, %s. If not in sight, %s.", situation, positionClause, advisory)
	return encounter{Situation: situation, Role: encounterRoleNone, Rule: "19(d)", Text: text}
}

// ── vessel typing ────────────────────────────────────────────────────────

type ownVesselType string

const (
	ownTypeSail  ownVesselType = "sail"
	ownTypePower ownVesselType = "power"
)

// ownVesselTypeFromState reads our own COLREGS type off navigation.state.
// Anchored, moored, or anything else signalk-autostate reports leaves us
// with no role to play, reported as !ok rather than guessed at.
func ownVesselTypeFromState(state string) (ownVesselType, bool) {
	switch state {
	case navStateSailing:
		return ownTypeSail, true
	case navStateMotoring:
		return ownTypePower, true
	default:
		return "", false
	}
}

type targetVesselType string

const (
	targetTypeSail               targetVesselType = "sail"
	targetTypePower              targetVesselType = "power"
	targetTypePriority           targetVesselType = "priority" // not under command, restricted manoeuvre, fishing -- Rule 18 ranks her above us
	targetTypeConstrainedByDraft targetVesselType = "constrained-by-draft"
	targetTypeUnknown            targetVesselType = "unknown"
)

// targetTypeFromNavState reads a target's COLREGS type off her AIS
// navigation.state. Anything not in this list -- including a status this
// classifier simply doesn't recognise, and an empty string when the field
// never arrived -- is targetTypeUnknown. It is never guessed from her AIS
// ship type: a sailboat's ship type (36/37) says nothing about whether her
// engine is running.
func targetTypeFromNavState(state string) targetVesselType {
	switch state {
	case navStateNotUnderCommand, navStateRestrictedManoeuvre, navStateFishing:
		return targetTypePriority
	case navStateConstrainedByDraft:
		return targetTypeConstrainedByDraft
	case navStateSailing:
		return targetTypeSail
	case navStateMotoring:
		return targetTypePower
	default:
		return targetTypeUnknown
	}
}
