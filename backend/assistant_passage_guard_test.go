package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type mateCase struct {
	User      string `json:"user"`
	BadAnswer string `json:"bad_answer"`
	Why       string `json:"why"`
}

func loadMateCase(t *testing.T, name string) mateCase {
	t.Helper()
	data, err := os.ReadFile("testdata/mate_cases/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var c mateCase
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if c.BadAnswer == "" || c.Why == "" {
		t.Fatalf("case %s is incomplete", name)
	}
	return c
}

func TestPassageGuardTrigger(t *testing.T) {
	c := loadMateCase(t, "southbound-northerly-no-passage-check")
	cases := []struct {
		name      string
		text      string
		limitsSet bool
		want      string
	}{
		{"snapshot answer", c.BadAnswer, true, "passage limits claim"},
		{"snapshot answer, limits unset", c.BadAnswer, false, "relative wind"},
		{"set them", "Set them in Settings → Mate → Passage limits and I'll score it.", true, "passage limits claim"},
		{"havent set", "You haven't set passage limits yet.", true, "passage limits claim"},
		{"not set", "Your passage limits are not set.", true, "passage limits claim"},
		{"limits quoted back", "Your passage limits are set at 15 kn ahead and Monday is inside them.", true, ""},
		{"head sea", "A northerly gives you a head sea.", true, "relative wind"},
		{"on the beam", "A brisk wind on the beam all day.", true, "relative wind"},
		{"following sea", "Expect a following sea.", true, "relative wind"},
		{"batteries", "The house bank is at 92% and the alternators are idle.", true, ""},
		{"quarter of the way", "You are a quarter of the way there.", true, ""},
		{"motor astern", "Motor astern to set the hook.", true, ""},
		{"dinghy astern", "The dinghy is astern.", true, ""},
		{"target on beam", "The target is on your beam.", true, ""},
		{"buoy on bow", "Keep the buoy on the bow.", true, ""},
		{"northerly astern", "You'll have a northerly astern.", true, "relative wind"},
		{"following winds list intro", "The following winds are forecast:\n- Monday 10 kn", true, ""},
		{"following seas list intro", "The following seas are expected: 1 m on Monday.", true, ""},
		{"a following sea", "You get a following sea all the way.", true, "relative wind"},
		{"following seas", "You'll have following seas.", true, "relative wind"},
	}
	for _, tc := range cases {
		limits := passageLimits{}
		if tc.limitsSet {
			limits = pwLimitsAll()
		}
		if got, _ := pwToolDeps(limits).checkDraft(tc.text, nil, nil); got != tc.want {
			t.Errorf("%s: trigger = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// guardRun drives the real loop with real tool deps. rounds is the scripted
// model: each entry is a final answer, or "TOOL" for a call to
// assess_passage_window.
func guardRun(t *testing.T, limits passageLimits, rounds ...string) (assistantReply, *queuedChatDoer, *[]recordedEvent) {
	t.Helper()
	d := pwToolDepsWithCoastline(limits)
	doer := &queuedChatDoer{}
	for _, r := range rounds {
		switch r {
		case "TOOL":
			doer.responses = append(doer.responses, toolCallResponse(t, "call_1", mateToolAssessPassageWindow.name, pwToolArgs, usage(1, 1, 0)))
		case "WIND_COURSE":
			doer.responses = append(doer.responses, toolCallResponse(t, "call_1", mateToolGetWindForecast.name, `{"lat":-19,"lon":147,"course_deg":152}`, usage(1, 1, 0)))
		case "WIND_NOCOURSE":
			doer.responses = append(doer.responses, toolCallResponse(t, "call_1", mateToolGetWindForecast.name, `{"lat":-19,"lon":147}`, usage(1, 1, 0)))
		default:
			doer.responses = append(doer.responses, finalResponse(t, r, "m", usage(1, 1, 0)))
		}
		doer.errs = append(doer.errs, nil)
	}
	emit, events := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: d, emit: emit}
	reply, err := runner.run(context.Background(), "system", "", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return reply, doer, events
}

const guardGood = "Monday is the better day."

func TestPassageGuard_BadDraftIsSentBackOnceAndTheSecondAnswerWins(t *testing.T) {
	c := loadMateCase(t, "southbound-northerly-no-passage-check")
	reply, doer, events := guardRun(t, pwLimitsAll(), c.BadAnswer, guardGood)
	if len(doer.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(doer.requests))
	}
	last := doer.requests[1].Messages
	if got := string(last[len(last)-1].Content); !strings.HasPrefix(got, "[Helmcentral check") {
		t.Fatalf("the second request must end with the corrective message, got %q", got)
	}
	if got := string(last[len(last)-2].Content); got != c.BadAnswer {
		t.Fatalf("the draft must precede the correction, got %q", got)
	}
	if reply.Content != guardGood {
		t.Fatalf("reply = %q, want the second answer", reply.Content)
	}
	// Whatever of the draft streamed must be retracted before the new answer.
	sawDelta, retracted := false, false
	for _, e := range *events {
		if e.event == "delta" && !retracted {
			sawDelta = true
		}
		if e.event == "retract" {
			retracted = true
		}
	}
	if sawDelta && !retracted {
		t.Fatal("the draft streamed to the operator and was not retracted")
	}
}

func TestPassageGuard_NoExtraRoundWhenTheToolRan(t *testing.T) {
	c := loadMateCase(t, "southbound-northerly-no-passage-check")
	_, doer, _ := guardRun(t, pwLimitsAll(), "TOOL", c.BadAnswer)
	if len(doer.requests) != 2 {
		t.Fatalf("requests = %d, want 2 (tool round, answer)", len(doer.requests))
	}
}

func TestPassageGuard_NoExtraRoundWhenForecastGaveTheAngle(t *testing.T) {
	_, doer, _ := guardRun(t, pwLimitsAll(), "WIND_COURSE", "A head sea on the bow all day.")
	if len(doer.requests) != 2 {
		t.Fatalf("requests = %d, want 2 (forecast round, answer)", len(doer.requests))
	}
}

func TestPassageGuard_ForecastWithoutCourseDoesNotGiveTheAngle(t *testing.T) {
	_, doer, _ := guardRun(t, pwLimitsAll(), "WIND_NOCOURSE", "A head sea on the bow all day.", guardGood)
	if len(doer.requests) != 3 {
		t.Fatalf("requests = %d, want 3 (forecast round, draft, corrected answer)", len(doer.requests))
	}
}

func TestPassageGuard_LimitsClaimNotExcusedByForecastWithCourse(t *testing.T) {
	_, doer, _ := guardRun(t, pwLimitsAll(), "WIND_COURSE", "Set passage limits in Settings.", guardGood)
	if len(doer.requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(doer.requests))
	}
}

func TestPassageGuard_LimitsUnsetMakesTheClaimTrue(t *testing.T) {
	_, doer, _ := guardRun(t, passageLimits{}, "Your passage limits are not set. Set passage limits in Settings.")
	if len(doer.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(doer.requests))
	}
}

func TestPassageGuard_FiresOnlyOnceAndLogsTheSecondMiss(t *testing.T) {
	var buf strings.Builder
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })
	c := loadMateCase(t, "southbound-northerly-no-passage-check")
	reply, doer, _ := guardRun(t, pwLimitsAll(), c.BadAnswer, c.BadAnswer)
	if len(doer.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(doer.requests))
	}
	if reply.Content != c.BadAnswer {
		t.Fatalf("the second answer must be accepted")
	}
	if !strings.Contains(buf.String(), "passage limits claim") || !strings.Contains(buf.String(), "still") {
		t.Fatalf("the accepted miss must be logged with its trigger, got %q", buf.String())
	}
}

func TestPassageGuard_UnrelatedAnswerGetsNoExtraRound(t *testing.T) {
	_, doer, _ := guardRun(t, pwLimitsAll(), "The house bank is at 92%.")
	if len(doer.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(doer.requests))
	}
}

func TestPassageGuard_OffWhenTheToolIsNotOffered(t *testing.T) {
	doer := &queuedChatDoer{
		responses: []*http.Response{finalResponse(t, "A head sea on the bow.", "m", usage(1, 1, 0))},
		errs:      []error{nil},
	}
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: &noPassageTools{}, emit: emit}
	if _, err := runner.run(context.Background(), "system", "", nil); err != nil {
		t.Fatal(err)
	}
	if len(doer.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(doer.requests))
	}
}

type noPassageTools struct{}

func (noPassageTools) definitions() []openRouterTool { return nil }
func (noPassageTools) execute(ctx context.Context, name string, args json.RawMessage) (string, assistantToolOutcome, error) {
	return "{}", assistantToolOutcome{}, nil
}

func TestPassageLimitsStatusLine(t *testing.T) {
	if got := passageLimitsStatusLine(pwLimitsAll(), nil); got != "Passage limits: set (ahead, abeam, astern have a no-go wind)" {
		t.Errorf("all set: %q", got)
	}
	if got := passageLimitsStatusLine(passageLimits{}, nil); got != "Passage limits: not set" {
		t.Errorf("unset: %q", got)
	}
	got := passageLimitsStatusLine(passageLimits{}, errors.New("settings are unreadable"))
	if !strings.Contains(got, "could not be read") || !strings.Contains(got, "settings are unreadable") {
		t.Errorf("error must be surfaced plainly: %q", got)
	}
}

func TestPassageLimitsStatusIsInTheLivePartNotTheStable(t *testing.T) {
	pc := basePromptContext()
	pc.PassageLimits = "Passage limits: not set"
	stable, live := assistantSystemPromptParts(pc)
	if !strings.Contains(live, "Passage limits: not set") {
		t.Error("the live part must carry the passage limits line")
	}
	if strings.Contains(stable, "Passage limits: not set") {
		t.Error("the stable (cached) part must not carry it")
	}
	_, live = assistantSystemPromptParts(basePromptContext())
	if strings.Contains(live, "Passage limits:") {
		t.Error("an uncollected status must leave the line out")
	}
}

func TestPassageGuardInstructionPerTrigger(t *testing.T) {
	_, claim := pwToolDeps(pwLimitsAll()).checkDraft("Set passage limits in Settings.", nil, nil)
	if !strings.Contains(claim, "limits are set") {
		t.Errorf("claim instruction should say the limits are set: %q", claim)
	}
	trig, wind := pwToolDeps(passageLimits{}).checkDraft("A northerly gives you a head sea.", nil, nil)
	if trig != "relative wind" {
		t.Fatalf("trigger = %q", trig)
	}
	if strings.Contains(wind, "limits are set") || strings.Contains(strings.ToLower(wind), "passage limits") {
		t.Errorf("relative-wind instruction must not mention the limits: %q", wind)
	}
}

func TestPassageGuard_BudgetAcceptanceIsLogged(t *testing.T) {
	var buf strings.Builder
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })
	d := pwToolDeps(passageLimits{})
	// A bad draft that finishes streaming after the tool phase has ended: no
	// round is left to send it back in. (A used-up round ceiling withdraws the
	// checker with the tools, so only time reaches this.)
	shortBudget(t, 50*time.Millisecond, 5*time.Second)
	doer := &streamingAnswerDoer{first: "A northerly gives you ", rest: "a head sea.", delay: 200 * time.Millisecond}
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: d, emit: emit}
	if _, err := runner.run(context.Background(), "system", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "accepted as is") || !strings.Contains(buf.String(), "budget used up") {
		t.Fatalf("budget acceptance must be logged, got %q", buf.String())
	}
}

// The anchorage-shelter check (ADR 0243). Mate called the south tip of Thomas
// Island exposed to a northerly and sent the operator to the north side, with
// the bay in fact facing south, and never called anchorage_shelter.
const thomasIslandAnswer = "Thomas Island South (the southern tip) is exposed to the NE-N that's forecast tonight and tomorrow. If anchoring, look for a nook on the north side of Thomas Island rather than the south tip, or be ready for an open roadstead to the north wind. This is general knowledge of the coastline - check the chart before committing."

// shelterDeps is the passage fixture with an Offline coastline wired in, the
// gate the shelter check needs. The reader is never called by a draft check.
func shelterDeps() assistantToolDeps {
	d := pwToolDeps(pwLimitsAll())
	d.offlineCoastline = enabledReaderStub
	return d
}

// enabledReaderStub is a coastline reader that is switched on and has no
// store: enough for a draft check, which never reads shorelines.
func enabledReaderStub() *offlineCoastlineReader {
	return &offlineCoastlineReader{Enabled: func() (bool, error) { return true, nil }}
}

func pwToolDepsWithCoastline(l passageLimits) assistantToolDeps {
	d := pwToolDeps(l)
	d.offlineCoastline = enabledReaderStub
	return d
}

// okResults is a non-empty result for every call: each one ran.
func okResults(calls []openRouterToolCall) []string {
	out := make([]string, len(calls))
	for i := range out {
		out[i] = `{"summary":"ok"}`
	}
	return out
}

func shelterCall(t *testing.T) []openRouterToolCall {
	t.Helper()
	var c openRouterToolCall
	c.Function.Name = mateToolAnchorageShelter.name
	c.Function.Arguments = `{"lat":-20.5446,"lon":149.118933}`
	return []openRouterToolCall{c}
}

func assessCall() []openRouterToolCall {
	var c openRouterToolCall
	c.Function.Name = mateToolAssessPassageWindow.name
	c.Function.Arguments = pwToolArgs
	return []openRouterToolCall{c}
}

func TestShelterGuardTrigger(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		calls []openRouterToolCall
		want  string
	}{
		{"thomas island answer", thomasIslandAnswer, nil, "anchorage shelter"},
		{"thomas island, shelter tool ran", thomasIslandAnswer, shelterCall(t), ""},
		{"passage check ran, no shelter tool", thomasIslandAnswer, assessCall(), "anchorage shelter"},
		{"both ran", thomasIslandAnswer, append(assessCall(), shelterCall(t)...), ""},
		{"sheltered from", "The bay is sheltered from the southeast trades.", nil, "anchorage shelter"},
		{"protected from", "Well protected from a northerly.", nil, "anchorage shelter"},
		{"lee shore", "With the wind NE that is a lee shore.", nil, "anchorage shelter"},
		{"open roadstead", "It is an open roadstead in a northerly.", nil, "anchorage shelter"},
		{"no shelter claim", "Anchorage depth is 6 m on sand, 1.4 nm from the pass.", nil, ""},
		{"sheltered marina, no direction", "Hamilton Island is a sheltered marina with fuel.", nil, ""},
		{"head north to marina", "Head north to the sheltered marina and take a berth.", nil, ""},
		{"exposed, no wind or direction", "The pontoon is exposed to the weather.", nil, ""},
		{"open to", "The bay is open to the north.", nil, "anchorage shelter"},
		{"in the lee of", "Tuck into the lee of the island in a northerly.", nil, "anchorage shelter"},
		{"hyphenated well-sheltered", "Well-sheltered in a southeasterly.", nil, "anchorage shelter"},
		{"exposed in", "The north side is exposed in a blow.", nil, "anchorage shelter"},
		{"reef exposed at low water", "The reef north of the anchorage is exposed at low water.", nil, ""},
		{"chain locker exposed", "Keep the windlass clear, the chain locker is exposed.", nil, ""},
		{"open to the public", "The marina is open to the public.", nil, ""},
		{"exposed from", "Exposed from NE through SE.", nil, "anchorage shelter"},
		{"shelter in", "You'll find shelter in a northerly.", nil, "anchorage shelter"},
		{"sheltered in", "Sheltered in anything from the north.", nil, "anchorage shelter"},
		{"untenable in", "It is untenable in a southerly.", nil, "anchorage shelter"},
		{"good shelter", "Good shelter from the east wind here.", nil, "anchorage shelter"},
		{"forecast only", "A northerly of 15 kn tonight, easing tomorrow.", nil, ""},
	}
	for _, tc := range cases {
		if got, _ := shelterDeps().checkDraft(tc.text, tc.calls, okResults(tc.calls)); got != tc.want {
			t.Errorf("%s: trigger = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestShelterGuard_TwoTriggersSentTogether(t *testing.T) {
	text := "A northerly gives you a head sea, and the bay is sheltered from the north."
	trig, ins := pwToolDepsWithCoastline(passageLimits{}).checkDraft(text, nil, nil)
	if !strings.Contains(trig, "relative wind") || !strings.Contains(trig, "anchorage shelter") {
		t.Fatalf("trigger = %q", trig)
	}
	if !strings.Contains(ins, "assess_passage_window") || !strings.Contains(ins, "anchorage_shelter") {
		t.Errorf("instruction should carry both corrections: %q", ins)
	}
}

func TestShelterGuard_SentBackOnceAndSecondAnswerWins(t *testing.T) {
	reply, doer, _ := guardRun(t, pwLimitsAll(), thomasIslandAnswer, guardGood)
	if len(doer.requests) != 2 || reply.Content != guardGood {
		t.Fatalf("requests = %d, content = %q", len(doer.requests), reply.Content)
	}
}

func TestShelterGuard_OffWithoutOfflineCoastline(t *testing.T) {
	d := shelterDeps()
	d.offlineCoastline = nil
	if got, _ := d.checkDraft(thomasIslandAnswer, nil, nil); got != "" {
		t.Errorf("no Offline coastline on this install: trigger = %q, want none", got)
	}
}

func TestShelterGuardInstructionCoversBoatPositionAndOpenWater(t *testing.T) {
	_, ins := shelterDeps().checkDraft(thomasIslandAnswer, nil, nil)
	for _, want := range []string{"find_places", "boat's position", "Otherwise remove"} {
		if !strings.Contains(ins, want) {
			t.Errorf("instruction lacks %q: %q", want, ins)
		}
	}
}

func TestShelterGuard_AnErroredShelterCallDoesNotSatisfyIt(t *testing.T) {
	d := shelterDeps()
	if got, _ := d.checkDraft(thomasIslandAnswer, shelterCall(t), []string{""}); got != "anchorage shelter" {
		t.Errorf("a call with no result (it errored) must not satisfy the guard, trigger = %q", got)
	}
	if got, _ := d.checkDraft(thomasIslandAnswer, shelterCall(t), []string{`{"summary":"ok"}`}); got != "" {
		t.Errorf("a call that returned satisfies it, trigger = %q", got)
	}
}

func TestShelterGuard_DisarmedWhileTheCoastlineIsOffOrUnreadable(t *testing.T) {
	var buf strings.Builder
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })
	d := shelterDeps()
	d.offlineCoastline = func() *offlineCoastlineReader {
		return &offlineCoastlineReader{Enabled: func() (bool, error) { return false, nil }}
	}
	if got, _ := d.checkDraft(thomasIslandAnswer, nil, nil); got != "" {
		t.Errorf("coastline switched off: trigger = %q, want none", got)
	}
	d.offlineCoastline = func() *offlineCoastlineReader {
		return &offlineCoastlineReader{Enabled: func() (bool, error) { return false, errors.New("settings unreadable") }}
	}
	if got, _ := d.checkDraft(thomasIslandAnswer, nil, nil); got != "" {
		t.Errorf("coastline setting unreadable: trigger = %q, want none", got)
	}
	if !strings.Contains(buf.String(), "settings unreadable") {
		t.Errorf("the unreadable setting must be logged, got %q", buf.String())
	}
}
