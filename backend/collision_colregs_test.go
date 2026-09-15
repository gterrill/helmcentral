package main

import (
	"strings"
	"testing"
)

// baseAISEncounterInputs is a fully-populated, gate-passing set of inputs for
// a power-driven target crossing from our starboard bow at 040° relative --
// the example in the plan this classifier implements. Every test below
// starts here and overrides only the fields its scenario cares about, the
// same pattern collisionNotification/makeAlarm use elsewhere in this package.
func baseAISEncounterInputs() encounterInputs {
	return encounterInputs{
		Kind: encounterKindAIS,

		OwnHeadingDeg: 0,
		OwnHeadingOK:  true,
		OwnSOGKn:      6,
		OwnSOGOK:      true,
		OwnState:      "motoring",

		BearingToTargetDeg: 40,
		BearingToSelfDeg:   220,
		BearingOK:          true,

		TargetCOGDeg: 270,
		TargetCOGOK:  true,
		TargetSOGKn:  6,
		TargetSOGOK:  true,

		TCPASeconds: 300,
		TCPAOK:      true,

		TargetNavState: "motoring",
	}
}

func TestClassifyEncounterCrossingFromStarboardBothPower(t *testing.T) {
	in := baseAISEncounterInputs()

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Situation != "Crossing" {
		t.Errorf("situation: got %q, want Crossing", got.Situation)
	}
	if got.Role != encounterRoleGiveWay {
		t.Errorf("role: got %q, want give-way", got.Role)
	}
	if got.Rule != "15" {
		t.Errorf("rule: got %q, want 15", got.Rule)
	}
	// The exact sentence the plan specifies, word for word.
	want := "Crossing, she is on our starboard bow (040° rel). We give way (Rule 15): alter to starboard, pass astern."
	if got.Text != want {
		t.Errorf("text:\n got  %q\n want %q", got.Text, want)
	}
}

func TestClassifyEncounterCrossingFromPortBothPower(t *testing.T) {
	in := baseAISEncounterInputs()
	in.BearingToTargetDeg = 320 // she's on our port bow
	in.TargetCOGDeg = 90
	in.BearingToSelfDeg = 140

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Situation != "Crossing" {
		t.Errorf("situation: got %q, want Crossing", got.Situation)
	}
	if got.Role != encounterRoleStandOn {
		t.Errorf("role: got %q, want stand-on", got.Role)
	}
	if got.Rule != "15" {
		t.Errorf("rule: got %q, want 15", got.Rule)
	}
	if !strings.Contains(got.Text, "We stand on (Rule 15): hold course and speed; act if she doesn't.") {
		t.Errorf("text missing stand-on wording: %q", got.Text)
	}
	// Rule 17(c): a power-driven stand-on vessel doesn't turn to port for a
	// vessel on her own port side, and she is on ours here.
	if !strings.Contains(got.Text, "Rule 17(c)") {
		t.Errorf("text missing the 17(c) caveat for a power-driven stand-on with her to port: %q", got.Text)
	}
}

func TestClassifyEncounterHeadOnBothPower(t *testing.T) {
	in := baseAISEncounterInputs()
	in.BearingToTargetDeg = 2 // within 6 degrees of dead ahead
	in.TargetCOGDeg = 180     // coming straight at us
	in.BearingToSelfDeg = 182 // within 6 degrees of dead ahead of her too

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Situation != "Head-on" {
		t.Errorf("situation: got %q, want Head-on", got.Situation)
	}
	if got.Rule != "14" {
		t.Errorf("rule: got %q, want 14", got.Rule)
	}
	if !strings.Contains(got.Text, "Each alters to starboard (Rule 14)") {
		t.Errorf("text missing the mutual give-way wording: %q", got.Text)
	}
}

// Rule 14 applies only when both are power-driven; a sail/power pair with
// the same nose-to-nose geometry is Rule 18 (categorical), not Rule 14.
func TestClassifyEncounterHeadOnGeometryButNotBothPowerIsRule18(t *testing.T) {
	in := baseAISEncounterInputs()
	in.BearingToTargetDeg = 2
	in.TargetCOGDeg = 180
	in.BearingToSelfDeg = 182
	in.TargetNavState = "sailing" // we're motoring, she's sailing

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Rule != "18" {
		t.Errorf("rule: got %q, want 18 (categorical beats the head-on geometry)", got.Rule)
	}
	if got.Role != encounterRoleGiveWay {
		t.Errorf("role: got %q, want give-way (power gives way to sail)", got.Role)
	}
}

func TestClassifyEncounterPowerGivesWayToSail(t *testing.T) {
	in := baseAISEncounterInputs()
	in.OwnState = "motoring"
	in.TargetNavState = "sailing"

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Role != encounterRoleGiveWay {
		t.Errorf("role: got %q, want give-way", got.Role)
	}
	if got.Rule != "18" {
		t.Errorf("rule: got %q, want 18", got.Rule)
	}
}

func TestClassifyEncounterSailStandsOnToPower(t *testing.T) {
	in := baseAISEncounterInputs()
	in.OwnState = "sailing"
	in.TargetNavState = "motoring"

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Role != encounterRoleStandOn {
		t.Errorf("role: got %q, want stand-on", got.Role)
	}
	if got.Rule != "18" {
		t.Errorf("rule: got %q, want 18", got.Rule)
	}
	// We're not power-driven here, so the 17(c) caveat must not appear.
	if strings.Contains(got.Text, "17(c)") {
		t.Errorf("17(c) only applies to a power-driven stand-on vessel: %q", got.Text)
	}
}

func TestClassifyEncounterFishingTargetRanksAboveUs(t *testing.T) {
	for _, ownState := range []string{"motoring", "sailing"} {
		in := baseAISEncounterInputs()
		in.OwnState = ownState
		in.TargetNavState = "fishing"

		got, ok := classifyEncounter(in)
		if !ok {
			t.Fatalf("[own=%s] expected a line", ownState)
		}
		if got.Role != encounterRoleGiveWay {
			t.Errorf("[own=%s] role: got %q, want give-way", ownState, got.Role)
		}
		if got.Rule != "18" {
			t.Errorf("[own=%s] rule: got %q, want 18", ownState, got.Rule)
		}
	}
}

func TestClassifyEncounterNotUnderCommandTargetRanksAboveUs(t *testing.T) {
	in := baseAISEncounterInputs()
	in.TargetNavState = "not under command"

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Role != encounterRoleGiveWay || got.Rule != "18" {
		t.Errorf("got role=%q rule=%q, want give-way/18", got.Role, got.Rule)
	}
}

// Spelled exactly as the SignalK schema enum does -- "manouverability", not
// "manoeuvrability" -- confirmed against schemas/groups/navigation.json.
func TestClassifyEncounterRestrictedManoeuvreTargetRanksAboveUs(t *testing.T) {
	in := baseAISEncounterInputs()
	in.TargetNavState = "restricted manouverability"

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Role != encounterRoleGiveWay || got.Rule != "18" {
		t.Errorf("got role=%q rule=%q, want give-way/18", got.Role, got.Rule)
	}
}

// The schema spells this "constrained by draft", not "constrained by her
// draught" (that's COLREGS Rule 3(h)'s wording, not the wire value) --
// confirmed against schemas/groups/navigation.json, see
// testdata/signalk_notifications/README.md.
func TestClassifyEncounterConstrainedByDraftGetsDontImpede(t *testing.T) {
	in := baseAISEncounterInputs()
	in.TargetNavState = "constrained by draft"

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Role != encounterRoleDontImpede {
		t.Errorf("role: got %q, want dont-impede", got.Role)
	}
	if got.Rule != "18(d)" {
		t.Errorf("rule: got %q, want 18(d)", got.Rule)
	}
	if !strings.Contains(got.Text, "impede") {
		t.Errorf("text should say not to impede her: %q", got.Text)
	}
}

func TestClassifyEncounterUnknownTargetTypeGivesSituationOnly(t *testing.T) {
	in := baseAISEncounterInputs()
	in.TargetNavState = "aground" // a real schema value, just not one this classifier keys off

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Situation != "Crossing" {
		t.Errorf("situation: got %q, want Crossing", got.Situation)
	}
	if got.Role != encounterRoleNone {
		t.Errorf("role: got %q, want none", got.Role)
	}
	if !strings.Contains(got.Text, "Her type isn't transmitted, so the role can't be set") {
		t.Errorf("text missing the unknown-type explanation: %q", got.Text)
	}
}

func TestClassifyEncounterOurTypeNotUnderwayGivesSituationOnlyNoElaboration(t *testing.T) {
	for _, ownState := range []string{"anchored", "moored", "", "not under command"} {
		in := baseAISEncounterInputs()
		in.OwnState = ownState

		got, ok := classifyEncounter(in)
		if !ok {
			t.Fatalf("[own=%q] expected a line", ownState)
		}
		if got.Role != encounterRoleNone {
			t.Errorf("[own=%q] role: got %q, want none", ownState, got.Role)
		}
		want := "Crossing, she is on our starboard bow (040° rel)."
		if got.Text != want {
			t.Errorf("[own=%q] text:\n got  %q\n want %q", ownState, got.Text, want)
		}
	}
}

// ── Rule 13, overtaking ─────────────────────────────────────────────────────

func TestClassifyEncounterSheOvertakesUs(t *testing.T) {
	in := baseAISEncounterInputs()
	in.BearingToTargetDeg = 200 // well abaft our beam
	in.TargetCOGDeg = 350       // roughly paralleling us, catching up from astern
	in.BearingToSelfDeg = 10

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Situation != "Overtaking" {
		t.Errorf("situation: got %q, want Overtaking", got.Situation)
	}
	if got.Role != encounterRoleStandOn {
		t.Errorf("role: got %q, want stand-on (the overtaking vessel gives way)", got.Role)
	}
	if got.Rule != "13" {
		t.Errorf("rule: got %q, want 13", got.Rule)
	}
}

func TestClassifyEncounterWeOvertakeHer(t *testing.T) {
	in := baseAISEncounterInputs()
	in.BearingToTargetDeg = 15 // nearly dead ahead: we're catching up on her
	in.TargetCOGDeg = 0        // same heading as us
	in.BearingToSelfDeg = 180  // we're astern of her

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Situation != "Overtaking" {
		t.Errorf("situation: got %q, want Overtaking", got.Situation)
	}
	if got.Role != encounterRoleGiveWay {
		t.Errorf("role: got %q, want give-way (we're the one overtaking)", got.Role)
	}
	if got.Rule != "13" {
		t.Errorf("rule: got %q, want 13", got.Rule)
	}
}

// Rule 13 is checked before Rule 18: the overtaking vessel gives way
// whatever the vessel types, so a power vessel overtaking a sailing vessel
// still gives way under Rule 13, not Rule 18.
func TestClassifyEncounterPowerOvertakingSailIsStillRule13(t *testing.T) {
	in := baseAISEncounterInputs()
	in.OwnState = "motoring"
	in.TargetNavState = "sailing"
	in.BearingToTargetDeg = 15
	in.TargetCOGDeg = 0
	in.BearingToSelfDeg = 180

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Rule != "13" {
		t.Errorf("rule: got %q, want 13 (overtaking wins over the type hierarchy)", got.Rule)
	}
	if got.Role != encounterRoleGiveWay {
		t.Errorf("role: got %q, want give-way", got.Role)
	}
}

// ── Rule 12, both under sail ────────────────────────────────────────────────

func TestClassifyEncounterBothSailingPortTackGivesWay(t *testing.T) {
	in := baseAISEncounterInputs()
	in.OwnState = "sailing"
	in.TargetNavState = "sailing"
	in.OwnApparentWindAngleDeg = -30 // negative: wind from port, port tack
	in.OwnApparentWindAngleOK = true

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Role != encounterRoleGiveWay {
		t.Errorf("role: got %q, want give-way", got.Role)
	}
	if got.Rule != "12" {
		t.Errorf("rule: got %q, want 12", got.Rule)
	}
}

// Starboard tack with the target to our windward: even not knowing her
// tack, rule 12(a)(ii) can only put the give-way duty on whichever of us is
// windward, and she is, so standing on here is safe regardless of her tack.
func TestClassifyEncounterBothSailingStarboardTackWindwardStandsOn(t *testing.T) {
	in := baseAISEncounterInputs()
	in.OwnState = "sailing"
	in.TargetNavState = "sailing"
	in.OwnApparentWindAngleDeg = 30 // positive: wind from starboard, starboard tack
	in.OwnApparentWindAngleOK = true
	in.BearingToTargetDeg = 60 // on our starboard (windward) side
	in.TargetCOGDeg = 180
	in.BearingToSelfDeg = 260 // her relative bearing of us (80°) stays clear of the overtaking arc

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Role != encounterRoleStandOn {
		t.Errorf("role: got %q, want stand-on", got.Role)
	}
	if got.Rule != "12" {
		t.Errorf("rule: got %q, want 12", got.Rule)
	}
}

// Starboard tack with the target to our leeward: if she also turns out to
// be on starboard tack, rule 12(a)(ii) would make US the windward, give-way
// vessel -- and her tack is not an AIS field, so that can't be ruled out.
// The card says only what geometry already established.
func TestClassifyEncounterBothSailingStarboardTackLeewardGivesSituationOnly(t *testing.T) {
	in := baseAISEncounterInputs()
	in.OwnState = "sailing"
	in.TargetNavState = "sailing"
	in.OwnApparentWindAngleDeg = 30 // starboard tack
	in.OwnApparentWindAngleOK = true
	in.BearingToTargetDeg = 300 // on our port (leeward) side
	in.TargetCOGDeg = 180
	in.BearingToSelfDeg = 80 // her relative bearing of us (260°) stays clear of the overtaking arc

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Role != encounterRoleNone {
		t.Errorf("role: got %q, want none (can't confirm her tack)", got.Role)
	}
}

func TestClassifyEncounterBothSailingNoWindDataGivesSituationOnly(t *testing.T) {
	in := baseAISEncounterInputs()
	in.OwnState = "sailing"
	in.TargetNavState = "sailing"
	in.OwnApparentWindAngleOK = false

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Role != encounterRoleNone {
		t.Errorf("role: got %q, want none (our own tack is unknown too)", got.Role)
	}
}

// ── no-line conditions ──────────────────────────────────────────────────────

func TestClassifyEncounterNoLineConditions(t *testing.T) {
	cases := map[string]func(*encounterInputs){
		"target SOG below 1kt":    func(in *encounterInputs) { in.TargetSOGKn = 0.9 },
		"target SOG missing":      func(in *encounterInputs) { in.TargetSOGOK = false },
		"own SOG below 1kt":       func(in *encounterInputs) { in.OwnSOGKn = 0.9 },
		"own SOG missing":         func(in *encounterInputs) { in.OwnSOGOK = false },
		"own heading missing":     func(in *encounterInputs) { in.OwnHeadingOK = false },
		"bearing missing":         func(in *encounterInputs) { in.BearingOK = false },
		"target COG missing":      func(in *encounterInputs) { in.TargetCOGOK = false },
		"TCPA missing":            func(in *encounterInputs) { in.TCPAOK = false },
		"TCPA negative (opening)": func(in *encounterInputs) { in.TCPASeconds = -1 },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := baseAISEncounterInputs()
			mutate(&in)
			if _, ok := classifyEncounter(in); ok {
				t.Errorf("%s: expected no line", name)
			}
		})
	}
}

// Pins the boundary: SOG at exactly the 1kt floor is not below it.
func TestClassifyEncounterSOGAtTheFloorStillProducesALine(t *testing.T) {
	in := baseAISEncounterInputs()
	in.OwnSOGKn = 1
	in.TargetSOGKn = 1

	if _, ok := classifyEncounter(in); !ok {
		t.Error("expected a line at exactly the 1kt floor")
	}
}

// TCPA of exactly zero is the instant of closest approach, not yet opening.
func TestClassifyEncounterTCPAAtZeroStillProducesALine(t *testing.T) {
	in := baseAISEncounterInputs()
	in.TCPASeconds = 0

	if _, ok := classifyEncounter(in); !ok {
		t.Error("expected a line at TCPA=0")
	}
}

// ── radar branch (ADR 0062 §7b/§8: built and tested, not wired to an alarm yet) ──

func TestClassifyEncounterRadarForwardOfBeam(t *testing.T) {
	in := baseAISEncounterInputs()
	in.Kind = encounterKindRadar
	in.TargetNavState = "" // radar carries no nav status
	in.BearingToTargetDeg = 30
	in.TargetCOGDeg = 200
	in.BearingToSelfDeg = 90

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Role != encounterRoleNone {
		t.Errorf("role: got %q, want none (rule 19 assigns no give-way/stand-on role)", got.Role)
	}
	if got.Rule != "19(d)" {
		t.Errorf("rule: got %q, want 19(d)", got.Rule)
	}
	if !strings.HasPrefix(got.Text, "Crossing, she is on our starboard bow (030° rel). If not in sight") {
		t.Errorf("text: %q", got.Text)
	}
	if !strings.Contains(got.Text, "avoid altering course to port") {
		t.Errorf("forward of the beam should caution against a port turn: %q", got.Text)
	}
}

func TestClassifyEncounterRadarAbeamOrAbaft(t *testing.T) {
	in := baseAISEncounterInputs()
	in.Kind = encounterKindRadar
	in.TargetNavState = ""
	in.BearingToTargetDeg = 100 // abeam, and outside the overtaking arc
	in.TargetCOGDeg = 200
	in.BearingToSelfDeg = 90

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Rule != "19(d)" {
		t.Errorf("rule: got %q, want 19(d)", got.Rule)
	}
	if !strings.Contains(got.Text, "avoid altering course towards her") {
		t.Errorf("abeam or abaft should caution against turning toward her: %q", got.Text)
	}
	if strings.Contains(got.Text, "to port") {
		t.Errorf("the forward-of-beam caution must not appear abeam/abaft: %q", got.Text)
	}
}

// Rule 19(d)(i) exempts a vessel we are overtaking from the "forward of the
// beam, avoid altering to port" caution.
func TestClassifyEncounterRadarOvertakingExemptsThePortCaution(t *testing.T) {
	in := baseAISEncounterInputs()
	in.Kind = encounterKindRadar
	in.TargetNavState = ""
	in.BearingToTargetDeg = 15 // forward of the beam
	in.TargetCOGDeg = 0
	in.BearingToSelfDeg = 180 // we're overtaking her from astern of her

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if got.Situation != "Overtaking" {
		t.Errorf("situation: got %q, want Overtaking", got.Situation)
	}
	if strings.Contains(got.Text, "avoid altering course to port") {
		t.Errorf("19(d)(i) exempts a vessel being overtaken: %q", got.Text)
	}
	if !strings.Contains(got.Text, "19(d)(i)") {
		t.Errorf("text should still cite 19(d)(i) for the exemption: %q", got.Text)
	}
}

// ── 0/360 wraparound ─────────────────────────────────────────────────────────

func TestClassifyEncounterWraparoundAcrossNorth(t *testing.T) {
	in := baseAISEncounterInputs()
	in.OwnHeadingDeg = 350
	in.BearingToTargetDeg = 30 // relative bearing wraps: (30-350+360) = 40
	in.TargetCOGDeg = 260
	in.BearingToSelfDeg = 210

	got, ok := classifyEncounter(in)
	if !ok {
		t.Fatal("expected a line")
	}
	if !strings.Contains(got.Text, "(040° rel)") {
		t.Errorf("text should show the wrapped relative bearing of 040: %q", got.Text)
	}
	if got.Situation != "Crossing" {
		t.Errorf("situation: got %q, want Crossing", got.Situation)
	}
}
