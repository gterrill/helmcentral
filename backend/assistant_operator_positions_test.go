package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseOperatorPositions_TheIncidentMessage(t *testing.T) {
	got := parseOperatorPositions("Whites Bay (021º40.271' S / 150º17.341' E)")
	if len(got) != 1 {
		t.Fatalf("expected one position, got %+v", got)
	}
	if got[0].Lat != -21.671183 || got[0].Lon != 150.289017 {
		t.Fatalf("expected -21.671183, 150.289017, got %v, %v", got[0].Lat, got[0].Lon)
	}
	if got[0].Text != "021º40.271' S / 150º17.341' E" {
		t.Fatalf("expected the text as written, got %q", got[0].Text)
	}
}

func TestParseOperatorPositions_Formats(t *testing.T) {
	cases := []struct {
		name, in string
		lat, lon float64
	}{
		{"degrees and decimal minutes", "20°32.676' S 149°07.136' E", -20.5446, 149.118933},
		{"smart quote minutes", "20°32.676’ S 149°07.136’ E", -20.5446, 149.118933},
		{"prime minutes", "20˚32.676′ S 149˚07.136′ E", -20.5446, 149.118933},
		{"degrees minutes seconds", `20°32'40.5" S 149°07'08" E`, -20.544583, 149.118889},
		{"double prime seconds", "20°32′40.5″ S 149°07′08″ E", -20.544583, 149.118889},
		{"joined with a hyphen", "20°32.676' S - 149°07.136' E", -20.5446, 149.118933},
		{"labelled", "Lat 21°40.271' S, Lon 150°17.341' E", -21.671183, 150.289017},
		{"labelled in full", "latitude: 21°40.271' S longitude: 150°17.341' E", -21.671183, 150.289017},
		{"joined with and", "21º 40.271' S and 150º 17.341' E", -21.671183, 150.289017},
		{"joined with a dash", "20°32.676' S – 149°07.136' E", -20.5446, 149.118933},
		{"split over two lines", "20°32.676' S\n149°07.136' E", -20.5446, 149.118933},
		{"hemisphere leading", "S 21°40.271' E 150°17.341'", -21.671183, 150.289017},
		{"no gap can split the degrees", "13°2.5' S 149°07.136' E", -13.041667, 149.118933},
		{"slash", "20°32.676' S/149°07.136' E", -20.5446, 149.118933},
		{"comma", "20°32.676' S,149°07.136' E", -20.5446, 149.118933},
		{"longitude first", "149°07.136' E 20°32.676' S", -20.5446, 149.118933},
		{"north and west", "40°42.768' N 74°00.360' W", 40.7128, -74.006},
		{"mixed letter placement", "S 21°40.271' 150°17.341' E", -21.671183, 150.289017},
		{"a wind on the next line", "21°40.271' S 150°17.341' E\nS 15 kn forecast", -21.671183, 150.289017},
		{"a wind beside it", "21°40.271' S 150°17.341' E, 15 N wind", -21.671183, 150.289017},
		{"a distance on the next line", "Whites Bay 21°40.271' S 150°17.341' E\n3 E of the point", -21.671183, 150.289017},
		{"a heading on the next line", "21°40.271' S 150°17.341' E\nHeading 150° E", -21.671183, 150.289017},
		{"lowercase letters", "021º40.271' s / 150º17.341' e", -21.671183, 150.289017},
		{"curly quotes on seconds", "20°32’40.5” S 149°07’08” E", -20.544583, 149.118889},
		{"a speed after the leading form", "S 21°40.271' E 150°17.341' 20 knots", -21.671183, 150.289017},
		{"a waypoint number on the line before", "WP 12\n21°40.271' S 150°17.341' E", -21.671183, 150.289017},
		{"lowercase leading", "s 21°40.271' e 150°17.341'", -21.671183, 150.289017},
		{"in a sentence", "Anchor at 20°32.676' S 149°07.136' E tonight, then on to Hook.", -20.5446, 149.118933},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseOperatorPositions(tc.in)
			if len(got) != 1 {
				t.Fatalf("%q: expected one position, got %+v", tc.in, got)
			}
			if got[0].Lat != tc.lat || got[0].Lon != tc.lon {
				t.Fatalf("%q: expected %v, %v, got %v, %v", tc.in, tc.lat, tc.lon, got[0].Lat, got[0].Lon)
			}
		})
	}
}

func TestParseOperatorPositions_TwoInOneMessage(t *testing.T) {
	got := parseOperatorPositions("From 20°32.676' S 149°07.136' E to 21°40.271' S / 150°17.341' E")
	if len(got) != 2 {
		t.Fatalf("expected two positions, got %+v", got)
	}
	if got[1].Lat != -21.671183 {
		t.Fatalf("unexpected second position %+v", got[1])
	}
}

func TestParseOperatorPositions_Refuses(t *testing.T) {
	cases := []struct{ name, in string }{
		{"minutes of 60", "20°60.000' S 149°07.136' E"},
		{"minutes over 60", "20°75.5' S 149°07.136' E"},
		{"seconds of 60", `20°32'60" S 149°07'08" E`},
		{"seconds in the longitude", `20°32'10" S 149°07'61" E`},
		{"latitude over 90", "91°00.000' S 149°07.136' E"},
		{"latitude past 90 by minutes", "90°00.5' S 149°07.136' E"},
		{"longitude over 180", "20°32.676' S 181°07.136' E"},
		{"lone latitude", "20°32.676' S"},
		{"lone longitude", "Bound for 149°07.136' E"},
		{"two latitudes", "20°32.676' S 21°40.271' S"},
		{"two longitudes", "149°07.136' E 150°17.341' E"},
		{"north and south", "20°32.676' N 21°40.271' S"},
		{"signed plain decimals", "-21.67, 150.28"},
		{"plain decimals no letter", "21.67 150.28"},
		{"bare integer degrees", "20 S 149 E"},
		{"bare integer degrees and minutes", "20 32 S 149 07 E"},
		{"decimal degrees with minutes", "20.5 32.676 S 149 07.136 E"},
		{"minus and letter", "-20°32.676' S 149°07.136' E"},
		{"whole degrees", "20° S 149° E"},
		{"variation", "Variation 12° E, 5° S"},
		{"short decimals", "21.67 S 150.28 E"},
		{"wind", "wind S 12.5 E 15.5 knots"},
		{"tide", "Tide: E 1.5 S 2.0"},
		{"spaced seconds with no mark", "21° 40' 16.2 S 150.2890 E"},
		{"spaced seconds after decimal minutes", "21°40.2' 5.5 S, 150.1 E"},
		{"a number before it", "Mark 3 21°40.271' S 150°17.341' E"},
		{"no degree mark", "20 32.676' S 149 07.136' E"},
		{"feet", "Mooring 5 10' E 6 20' S"},
		{"feet with a letter first", "N 5 10' E 6 20'"},
		{"a label taken for the letter", "Mark E 12°30.000' N 61°30.000' W"},
		{"a label the same as the letter", "Route W 12°30.000' N 61°30.000' W"},
		{"halves in two paragraphs", "Bound for 149°07.136' E\n\n20°32.676' S was last night"},
		{"minutes as degrees after deg dot", "21 deg. 40 16' S, 150°17.341' E"},
		{"minutes as degrees after d", "21d 40 16' S, 150°17.341' E"},
		{"minutes as degrees over a line", "21\n40 16' S, 150°17.341' E"},
		{"a heading before it", "Heading 150° E, 21°40.271' S 150°17.341' E"},
		{"a failed half joins two positions", "20°75.5' S 149°07.136' E, 21°40.271' S 150°17' E, 150°60' E"},
		{"a bad half each end of a list", "20° 32 S 149°07.136' E, 21°40.271' S 150° 17 E"},
		{"decimals each end of a list", "20.5 S 149°07.136' E, 21°40.271' S 150.289 E"},
		{"minutes read as degrees", "21 40 16.2' S 150°17.341' E"},
		{"minutes read as degrees after a mark", "21° 40 16.2' S 150°17.341' E"},
		{"minutes read as degrees after a word", "21 deg 40 16' S, 150°17.341' E"},
		{"a bearing with no minute mark", "Bearing 150° 5.5 E, 020° 7.5 S"},
		{"minutes with no mark", "21º 40.271 S and 150º 17.341 E"},
		{"a number cut short", "S 21°40.271' E 150°17.34.5"},
		{"no marks at all", "20 32.676 S, 149 07.136 E"},
		{"decimal degrees", "21.6712 S 150.2890 E"},
		{"decimal degrees with a mark", "21.6712° S, 150.2890° E"},
		{"decimal degrees leading", "S 21.6712 E 150.2890"},
		{"digits run together", "2132.5' S 14907.1' E"},
		{"a stray half in a list", "20°32.676' S 149°07.136' E, 21°40.271' S"},
		{"a word between", "20°32.676' S then 149°07.136' E"},
		{"a number before decimal degrees", "Mark 3 21.6712 S 150.2890 E"},
		{"a stray longitude before the pair", "Bound 150°30' E, 21°40.271' S 150°17.341' E"},
		{"a stray decimal before the pair", "Course 12.5 E, 21.67 S 150.28 E"},
		{"mixed notation", "21°40.271' S 150.289 E"},
		{"a speed taken for minutes", "S 21°40' E 150° 12 knots"},
		{"seconds with no mark", "20°32'40.5 S, 8.2 E"},
		{"bare degrees minutes seconds", "20 32 40.5 S 8 2.5 E"},
		{"lowercase with no symbols", "20 32.676 s, 149 07.136 e"},
		{"lowercase times", "5 12.5 s 3 4.5 e"},
		{"a possessive", "boat's 20°30' e 21° s"},
		{"lowercase decimal degrees", "12.5 s 8.5 e"},
		{"lowercase decimal degrees leading", "s 12.5 e 8.5"},
		{"letter run into the number", "20°32.676'S149°07.136'E"},
		{"seconds without a minute symbol", `20 32 40.5" S 149 07 08" E`},
		{"wind", "wind 12 knots SE, gusting 20 E"},
		{"time and day", "Leaving at 7 30 S Sat 14 E"},
		{"date", "3 Nov 2026 S 5 E"},
		{"course", "course 150 S 14 E"},
		{"letters inside words", "21.5 SE 150.3 NE"},
		{"empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseOperatorPositions(tc.in); len(got) != 0 {
				t.Fatalf("%q: expected no position, got %+v", tc.in, got)
			}
		})
	}
}

func TestBuildAssistantSystemPrompt_NeverConvertAPositionRule(t *testing.T) {
	stable, _ := assistantSystemPromptParts(basePromptContext())
	if !strings.Contains(stable, "Never convert a position the skipper gives in degrees and minutes") {
		t.Errorf("the stable prompt must tell the model not to convert the skipper's position")
	}
}

func TestParseOperatorPositions_AListSplitIntoPairs(t *testing.T) {
	got := parseOperatorPositions("20°32.676' S 149°07.136' E\n21°40.271' S 150°17.341' E")
	if len(got) != 2 || got[0].Lat != -20.5446 || got[1].Lon != 150.289017 {
		t.Fatalf("expected two positions, got %+v", got)
	}
}

func TestOperatorPositionsFromMessages_WithinOneMessageInTheOrderWritten(t *testing.T) {
	got := operatorPositionsFromMessages([]assistantMessage{
		{Role: "user", Content: "Use 21°40.271' S 150°17.341' E, not 20°32.676' S 149°07.136' E"},
	})
	if len(got) != 2 || got[0].Lat != -21.671183 {
		t.Fatalf("expected the order written, got %+v", got)
	}
}

func TestShelterInstruction_PutsTheSkippersPositionFirst(t *testing.T) {
	if !strings.Contains(assistantAnchorageShelterInstruction, "a position the skipper gave") {
		t.Errorf("the send-back must not pull Mate off the skipper's figure: %q", assistantAnchorageShelterInstruction)
	}
}

func TestBuildAssistantSystemPrompt_TheSkippersPositionComesBeforeFindPlaces(t *testing.T) {
	stable, _ := assistantSystemPromptParts(basePromptContext())
	if !strings.Contains(stable, "a position the skipper gave for that place comes first") {
		t.Errorf("the stable prompt must put the skipper's position before find_places")
	}
}

func TestOperatorPositionsNote_SaysWhatOutranksWhat(t *testing.T) {
	note := operatorPositionsNote(parseOperatorPositions("20°32.676' S\n149°07.136' E"))
	for _, want := range []string{
		"Newest message first",
		"its words say which the skipper means",
		"replaces an earlier one",
		"in any other form also replaces",
		"outranks a find_places result or an earlier answer of yours for the same place",
		"- 20°32.676' S 149°07.136' E is lat -20.544600, lon 149.118933.",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("the note misses %q: %q", want, note)
		}
	}
}

func TestOperatorPositionsFromMessages_UserRowsNewestFirstDeduplicatedAndCapped(t *testing.T) {
	msgs := []assistantMessage{
		{Role: "user", Content: "Start at 20°32.676' S 149°07.136' E"},
		{Role: "assistant", Content: "Taking 19°00.000' S 148°00.000' E as the start."},
		{Role: "user", Content: "No, 21°40.271' S / 150°17.341' E"},
		{Role: "watch", Content: "17°00.000' S 147°00.000' E"},
		{Role: "user", Content: "Again: 20°32.676' S 149°07.136' E"},
	}
	got := operatorPositionsFromMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("expected two positions, got %+v", got)
	}
	if got[0].Text != "20°32.676' S 149°07.136' E" || got[0].Lat != -20.5446 {
		t.Fatalf("expected the latest message's position first, got %+v", got[0])
	}
	if got[1].Lat != -21.671183 {
		t.Fatalf("expected the earlier position second, got %+v", got[1])
	}

	var many []assistantMessage
	for i := 10; i < 22; i++ {
		many = append(many, assistantMessage{Role: "user", Content: fmt.Sprintf("Mark %d°10.000' S 149°00.000' E", i)})
	}
	capped := operatorPositionsFromMessages(many)
	if len(capped) != maxOperatorPositions {
		t.Fatalf("expected %d positions, got %d", maxOperatorPositions, len(capped))
	}
	if capped[0].Lat > -21.1 || capped[0].Lat < -21.2 {
		t.Fatalf("expected the newest message first, got %+v", capped[0])
	}
}

func TestAssistantSystemPrompt_OperatorPositionsAreLiveAndOptional(t *testing.T) {
	pc := basePromptContext()
	stableBefore, liveBefore := assistantSystemPromptParts(pc)
	if strings.Contains(liveBefore, "Positions the skipper gave") {
		t.Fatal("no positions note expected when none were given")
	}

	pc.OperatorPositions = parseOperatorPositions("Whites Bay (021º40.271' S / 150º17.341' E)")
	stable, live := assistantSystemPromptParts(pc)
	if stable != stableBefore {
		t.Fatal("a position must not change the stable, cached part of the prompt")
	}
	for _, want := range []string{
		"Positions the skipper gave, converted by Helmcentral",
		"do not convert a position yourself",
		"021º40.271' S / 150º17.341' E is lat -21.671183, lon 150.289017",
		"outrank",
		"find_places",
	} {
		if !strings.Contains(live, want) {
			t.Fatalf("expected %q in the live prompt, got:\n%s", want, live)
		}
	}

	pc.OperatorPositions = nil
	_, liveAfter := assistantSystemPromptParts(pc)
	if liveAfter != liveBefore {
		t.Fatal("with no positions the live prompt must be byte-for-byte what it was")
	}
}

func TestBeginAssistantTurn_PositionInAnEarlierSkipperMessageReachesTheLaterTurn(t *testing.T) {
	store, conv := watchFollowUpSetup(t, true)

	var mu sync.Mutex
	var gotLive string
	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, opts assistantRunnerOptions, emit assistantEmitter) assistantRunnerFace {
		return assistantRunnerFunc(func(ctx context.Context, systemStable, systemLive string, history []openRouterMessage) (assistantReply, error) {
			mu.Lock()
			gotLive = systemLive
			mu.Unlock()
			return assistantReply{Content: "Fine.", Model: "openai/gpt-4o"}, nil
		})
	})

	for _, m := range []assistantMessage{
		{Role: "user", Content: "Whites Bay (021º40.271' S / 150º17.341' E) looks good"},
		{Role: "assistant", Content: "Then I will use 19°00.000' S 148°00.000' E."},
		{Role: "user", Content: "How is the shelter there?"},
	} {
		m.ConversationID = conv.ID
		if _, err := store.AppendMessage(m); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	messages, err := store.ListMessages(conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}

	runCtx, run, ok := acquireAssistantRunForWatch(conv.ID, time.Second)
	if !ok {
		t.Fatal("expected to acquire the run")
	}
	params := assistantTurnParams{apiKey: "sk-test", model: "openai/gpt-4o", settingsPath: os.Getenv("SETTINGS_FILE")}
	if err := beginAssistantTurn(runCtx, run, conv.ID, messages, params); err != nil {
		globalAssistantRuns.remove(conv.ID, run)
		t.Fatalf("beginAssistantTurn: %v", err)
	}
	waitForConditionT(t, 2*time.Second, func() bool {
		msgs, _ := store.ListMessages(conv.ID)
		return len(msgs) == 4
	})
	waitForConditionT(t, time.Second, func() bool { _, busy := globalAssistantRuns.get(conv.ID); return !busy })

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(gotLive, "lat -21.671183, lon 150.289017") {
		t.Fatalf("expected the earlier skipper position in this turn's prompt, got:\n%s", gotLive)
	}
	if strings.Contains(gotLive, "148.000000") || strings.Contains(gotLive, "-19.000000") {
		t.Fatalf("a position in an assistant message must not be handed over, got:\n%s", gotLive)
	}
}
