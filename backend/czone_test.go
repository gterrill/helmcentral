package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// switchesFixtureServer serves a fixed JSON body for every request, standing
// in for SignalK's electrical/switches subtree.
func switchesFixtureServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
}

// liveVesselSwitchesPayload is captured verbatim from the live vessel. SignalK
// nests circuits under "bank" (singular) alongside sibling "venus-*"/"gx" leaf
// entries — not under "banks" (plural). meta is nested INSIDE each value node
// (state.meta, order.meta) rather than being a sibling of "state" — an
// earlier, uncorrected assumption here read circuit["meta"] directly and
// silently found nothing, which is why gx.gxInternalRelay1's real
// displayName ("GX internal relay 1") and venus-0/gx's real supportsPut:true
// were both missed until this was fixed.
const liveVesselSwitchesPayload = `{
  "venus-0": {
    "state": { "meta": { "supportsPut": true }, "value": 0, "$source": "venus.com.victronenergy.system.0", "timestamp": "2026-08-23T04:22:45.291Z" }
  },
  "bank": {
    "0": {
      "2": {
        "state": { "meta": {}, "value": 0, "$source": "YachtDevices.224", "timestamp": "2026-08-23T04:23:12.341Z", "pgn": 127501,
          "values": { "YachtDevices.227": { "value": 0, "pgn": 127501, "timestamp": "2026-08-23T04:23:09.435Z" } } },
        "order": { "meta": {}, "value": 2, "$source": "YachtDevices.224", "timestamp": "2026-08-23T04:23:12.341Z", "pgn": 127501 }
      },
      "3": {
        "state": { "meta": {}, "value": 0, "$source": "YachtDevices.224", "timestamp": "2026-08-23T04:23:12.341Z", "pgn": 127501 },
        "order": { "meta": {}, "value": 3, "$source": "YachtDevices.224", "timestamp": "2026-08-23T04:23:12.341Z", "pgn": 127501 }
      }
    }
  },
  "gx": {
    "gxInternalRelay1": {
      "state": { "meta": { "supportsPut": true, "units": "bool", "displayName": "GX internal relay 1" }, "value": false, "$source": "venus.com.victronenergy.system.0", "timestamp": "2026-08-23T04:22:45.289Z" }
    }
  }
}`

// TestFetchSignalKSwitches_SurfacesBankVenusAndGxSwitches is the red test for
// this round: the generic recursive walk must surface bank circuits *and*
// the sibling venus-*/gx.* nodes from one real payload, with wire IDs that
// are exactly the dotted SignalK sub-path ("bank.0.2", not "banks.0.2") —
// that's what makes the PUT-side dot-to-slash conversion hit a real path.
func TestFetchSignalKSwitches_SurfacesBankVenusAndGxSwitches(t *testing.T) {
	srv := switchesFixtureServer(t, liveVesselSwitchesPayload)
	defer srv.Close()

	switches, err := fetchSignalKSwitches(srv.URL, "/signalk/v1/api/vessels/self/electrical/switches")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Root keys sort alphabetically ("bank" < "gx" < "venus-0"; none of them
	// parse as numeric IDs), so bank's two circuits walk first, then gx, then
	// the venus-0 leaf switch.
	assertSwitchIDOrder(t, switches, []string{"bank.0.2", "bank.0.3", "gx.gxInternalRelay1", "venus-0"})

	byID := make(map[string]czoneSwitch, len(switches))
	for _, sw := range switches {
		byID[sw.ID] = sw
	}

	if got := byID["bank.0.2"].DisplayName; got != "Bank 0 Circuit 2" {
		t.Errorf("bank.0.2 DisplayName: got %q, want %q", got, "Bank 0 Circuit 2")
	}
	if got := byID["bank.0.3"].DisplayName; got != "Bank 0 Circuit 3" {
		t.Errorf("bank.0.3 DisplayName: got %q, want %q", got, "Bank 0 Circuit 3")
	}
	if got := byID["venus-0"].State; got != 0 {
		t.Errorf("venus-0 State: got %d, want 0", got)
	}
	if got := byID["gx.gxInternalRelay1"].State; got != 0 {
		t.Errorf("gx.gxInternalRelay1 State: got %d, want 0 (off)", got)
	}

	// gx.gxInternalRelay1's real meta lives at state.meta, not circuit.meta —
	// this pins the correct nesting against the actual live-vessel capture.
	if got := byID["gx.gxInternalRelay1"].DisplayName; got != "GX internal relay 1" {
		t.Errorf("gx.gxInternalRelay1 DisplayName: got %q, want %q (from state.meta.displayName)", got, "GX internal relay 1")
	}
	if got := byID["gx.gxInternalRelay1"].Writable; !got {
		t.Errorf("gx.gxInternalRelay1 Writable: got %v, want true (state.meta.supportsPut is true)", got)
	}
	if got := byID["venus-0"].Writable; !got {
		t.Errorf("venus-0 Writable: got %v, want true (state.meta.supportsPut is true)", got)
	}
	// bank.0.2's state.meta is {} in the real capture — no displayName, no
	// supportsPut — so it correctly falls back to the generated name and false.
	if got := byID["bank.0.2"].Writable; got {
		t.Errorf("bank.0.2 Writable: got %v, want false (state.meta is {} in the real capture)", got)
	}
}

// TestFetchSignalKSwitches_IgnoresOrderSibling is a regression guard: each
// bank circuit carries an "order" leaf alongside "state" (used for UI
// ordering). parseSwitchState only reads "state", so "order" must never be
// counted as its own switch — the fixture has 4 real switches (2 bank
// circuits, gx.gxInternalRelay1, venus-0), not 8.
func TestFetchSignalKSwitches_IgnoresOrderSibling(t *testing.T) {
	srv := switchesFixtureServer(t, liveVesselSwitchesPayload)
	defer srv.Close()

	switches, err := fetchSignalKSwitches(srv.URL, "/signalk/v1/api/vessels/self/electrical/switches")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(switches) != 4 {
		t.Fatalf("switch count: got %d, want 4 (order must not be emitted as a circuit): %+v", len(switches), switches)
	}
}

// TestFetchSignalKSwitches_NoRecognisableSwitchNodeReturnsError guards the
// Fallback Policy with a generic walk: there is no longer a single required
// top-level key ("bank"), so the fail-fast condition is "the walk found no
// node anywhere with a readable state" rather than "the bank key is missing".
// This must still surface as an explicit error naming the top-level keys
// present, never as a silent empty-but-successful result.
func TestFetchSignalKSwitches_NoRecognisableSwitchNodeReturnsError(t *testing.T) {
	payload := `{
		"venus-0": {"notAState": "hello"},
		"gx": {}
	}`
	srv := switchesFixtureServer(t, payload)
	defer srv.Close()

	switches, err := fetchSignalKSwitches(srv.URL, "/signalk/v1/api/vessels/self/electrical/switches")
	if err == nil {
		t.Fatalf("expected an error when no node in the payload has a readable state, got switches=%+v, err=nil", switches)
	}
	if switches != nil {
		t.Fatalf("expected a nil slice alongside the error, got %+v", switches)
	}
	if !strings.Contains(err.Error(), "gx") || !strings.Contains(err.Error(), "venus-0") {
		t.Fatalf("error should list the top-level keys that were present (gx, venus-0), got: %v", err)
	}
}

// TestFetchSignalKSwitches_PrefersMetaDisplayName asserts a bank circuit's
// meta.displayName, when present, wins over the generated "Bank N Circuit M"
// fallback name. meta is nested INSIDE the state value node (state.meta),
// matching the real server (confirmed against the live vessel: node keys
// under a switch are just ["state"], and state.meta carries displayName /
// supportsPut) — not a sibling of "state" at the circuit level.
func TestFetchSignalKSwitches_PrefersMetaDisplayName(t *testing.T) {
	payload := `{
		"bank": {
			"0": {
				"1": {
					"state": {"value": 1, "meta": {"displayName": "Nav Lights"}}
				}
			}
		}
	}`
	srv := switchesFixtureServer(t, payload)
	defer srv.Close()

	switches, err := fetchSignalKSwitches(srv.URL, "/signalk/v1/api/vessels/self/electrical/switches")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(switches) != 1 {
		t.Fatalf("switch count: got %d, want 1 (%+v)", len(switches), switches)
	}
	if switches[0].DisplayName != "Nav Lights" {
		t.Fatalf("DisplayName: got %q, want %q (state.meta.displayName must win over the generated name)", switches[0].DisplayName, "Nav Lights")
	}
}

// TestFetchSignalKSwitches_IgnoresMetaWhenNotNestedInsideState is the guard
// against regressing back to the wrong assumption: a payload with meta as a
// SIBLING of state (the shape this codebase mistakenly assumed before) must
// NOT produce a display name or a writable flag from it. Pins the nesting so
// this cannot silently regress again.
func TestFetchSignalKSwitches_IgnoresMetaWhenNotNestedInsideState(t *testing.T) {
	payload := `{
		"bank": {
			"0": {
				"4": {
					"state": {"value": 1},
					"meta": {"displayName": "Should Not Appear", "supportsPut": true}
				}
			}
		}
	}`
	srv := switchesFixtureServer(t, payload)
	defer srv.Close()

	switches, err := fetchSignalKSwitches(srv.URL, "/signalk/v1/api/vessels/self/electrical/switches")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(switches) != 1 {
		t.Fatalf("switch count: got %d, want 1 (%+v)", len(switches), switches)
	}
	if got := switches[0].DisplayName; got != "Bank 0 Circuit 4" {
		t.Errorf("DisplayName: got %q, want %q (sibling meta must be ignored, not just the generated fallback skipped)", got, "Bank 0 Circuit 4")
	}
	if switches[0].Writable {
		t.Errorf("Writable: got true, want false (sibling meta.supportsPut must be ignored)")
	}
}

// TestFetchSignalKSwitches_FallsBackToLastPathSegmentForDisplayName covers a
// non-bank shape with no meta.displayName: the "Bank N Circuit M" fallback
// only makes sense for bank circuits, so anything else must fall back to its
// own last path segment rather than a invented decorative name.
func TestFetchSignalKSwitches_FallsBackToLastPathSegmentForDisplayName(t *testing.T) {
	payload := `{
		"venus-1": {"state": {"value": 0}}
	}`
	srv := switchesFixtureServer(t, payload)
	defer srv.Close()

	switches, err := fetchSignalKSwitches(srv.URL, "/signalk/v1/api/vessels/self/electrical/switches")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(switches) != 1 {
		t.Fatalf("switch count: got %d, want 1 (%+v)", len(switches), switches)
	}
	if switches[0].DisplayName != "venus-1" {
		t.Fatalf("DisplayName: got %q, want %q (non-bank shapes fall back to the last path segment)", switches[0].DisplayName, "venus-1")
	}
}

// TestFetchSignalKSwitches_WritableReflectsSupportsPut covers both directions
// of state.meta.supportsPut: true for a controllable Venus relay, false for a
// CZone bank circuit that is a status indicator only (PGN 127501), not a
// controllable output — the live vessel's actual split (Finding 2).
func TestFetchSignalKSwitches_WritableReflectsSupportsPut(t *testing.T) {
	payload := `{
		"venus-0": {
			"state": {"value": 1, "meta": {"supportsPut": true}}
		},
		"bank": {
			"0": {
				"2": {
					"state": {"value": 0, "meta": {"supportsPut": false}}
				}
			}
		}
	}`
	srv := switchesFixtureServer(t, payload)
	defer srv.Close()

	switches, err := fetchSignalKSwitches(srv.URL, "/signalk/v1/api/vessels/self/electrical/switches")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	byID := make(map[string]czoneSwitch, len(switches))
	for _, sw := range switches {
		byID[sw.ID] = sw
	}

	if got := byID["venus-0"].Writable; !got {
		t.Errorf("venus-0 Writable: got %v, want true (meta.supportsPut is true)", got)
	}
	if got := byID["bank.0.2"].Writable; got {
		t.Errorf("bank.0.2 Writable: got %v, want false (meta.supportsPut is false)", got)
	}
}

// TestFetchSignalKSwitches_WritableDefaultsFalseWhenSupportsPutAbsent covers
// both "no meta at all" and "state.meta present but no supportsPut key":
// never claim a control works when the metadata does not say so.
func TestFetchSignalKSwitches_WritableDefaultsFalseWhenSupportsPutAbsent(t *testing.T) {
	payload := `{
		"venus-1": {"state": {"value": 0}},
		"gx": {
			"gxInternalRelay2": {
				"state": {"value": 0, "meta": {"displayName": "GX internal relay 2"}}
			}
		}
	}`
	srv := switchesFixtureServer(t, payload)
	defer srv.Close()

	switches, err := fetchSignalKSwitches(srv.URL, "/signalk/v1/api/vessels/self/electrical/switches")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(switches) != 2 {
		t.Fatalf("switch count: got %d, want 2 (%+v)", len(switches), switches)
	}
	for _, sw := range switches {
		if sw.Writable {
			t.Errorf("%s Writable: got true, want false (no meta.supportsPut present)", sw.ID)
		}
	}
}

// switchIDs extracts the ID field in returned order, for asserting sequence.
func switchIDs(switches []czoneSwitch) []string {
	ids := make([]string, len(switches))
	for i, sw := range switches {
		ids[i] = sw.ID
	}
	return ids
}

func assertSwitchIDOrder(t *testing.T, got []czoneSwitch, want []string) {
	t.Helper()
	gotIDs := switchIDs(got)
	if len(gotIDs) != len(want) {
		t.Fatalf("switch order: got %v, want %v", gotIDs, want)
	}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Fatalf("switch order: got %v, want %v", gotIDs, want)
		}
	}
}

// TestFetchSignalKSwitches_SortsCircuitsNumericallyNotLexicographically is the
// red test for the reported symptom: sort.Strings on circuit IDs "2".."12"
// lexicographically puts "10","11","12" before "2" — the live API returned
// 10,11,12,2,3,...,9 instead of 2,3,...,12. Each circuit's order.value here
// agrees with its ID, so this isolates the numeric-vs-lexicographic bug.
func TestFetchSignalKSwitches_SortsCircuitsNumericallyNotLexicographically(t *testing.T) {
	payload := `{
		"bank": {
			"0": {
				"2":  {"state": {"value": 0}, "order": {"value": 2}},
				"3":  {"state": {"value": 0}, "order": {"value": 3}},
				"10": {"state": {"value": 0}, "order": {"value": 10}}
			}
		}
	}`
	srv := switchesFixtureServer(t, payload)
	defer srv.Close()

	switches, err := fetchSignalKSwitches(srv.URL, "/signalk/v1/api/vessels/self/electrical/switches")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertSwitchIDOrder(t, switches, []string{"bank.0.2", "bank.0.3", "bank.0.10"})
}

// TestFetchSignalKSwitches_HonoursOrderOverCircuitIDWhenTheyDisagree is the
// important case: circuit "2" is configured (via order.value) to display
// after circuit "5". On a real CZone install with Third Party Mode enabled,
// the installer's configured panel order can legitimately diverge from the
// bus index, so the returned sequence must follow order.value, not the ID —
// sorting numerically by ID would pass every other test here but get this
// one backwards.
func TestFetchSignalKSwitches_HonoursOrderOverCircuitIDWhenTheyDisagree(t *testing.T) {
	payload := `{
		"bank": {
			"0": {
				"2": {"state": {"value": 0}, "order": {"value": 5}},
				"5": {"state": {"value": 0}, "order": {"value": 2}}
			}
		}
	}`
	srv := switchesFixtureServer(t, payload)
	defer srv.Close()

	switches, err := fetchSignalKSwitches(srv.URL, "/signalk/v1/api/vessels/self/electrical/switches")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertSwitchIDOrder(t, switches, []string{"bank.0.5", "bank.0.2"})
}

// TestFetchSignalKSwitches_FallsBackToNumericIDWhenOrderAbsent covers
// circuits with no "order" leaf at all: they must still sort numerically by
// ID (2, 3, 10), not lexicographically (10, 2, 3).
func TestFetchSignalKSwitches_FallsBackToNumericIDWhenOrderAbsent(t *testing.T) {
	payload := `{
		"bank": {
			"0": {
				"2":  {"state": {"value": 0}},
				"3":  {"state": {"value": 0}},
				"10": {"state": {"value": 0}}
			}
		}
	}`
	srv := switchesFixtureServer(t, payload)
	defer srv.Close()

	switches, err := fetchSignalKSwitches(srv.URL, "/signalk/v1/api/vessels/self/electrical/switches")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertSwitchIDOrder(t, switches, []string{"bank.0.2", "bank.0.3", "bank.0.10"})
}

// TestFetchSignalKSwitches_SortsBankIDsNumerically covers the identical
// lexicographic bug one level up: sort.Strings(bankIDs) would put bank "10"
// before bank "2" once a vessel has more than one CZone bank.
func TestFetchSignalKSwitches_SortsBankIDsNumerically(t *testing.T) {
	payload := `{
		"bank": {
			"10": {
				"1": {"state": {"value": 0}, "order": {"value": 1}}
			},
			"2": {
				"1": {"state": {"value": 0}, "order": {"value": 1}}
			}
		}
	}`
	srv := switchesFixtureServer(t, payload)
	defer srv.Close()

	switches, err := fetchSignalKSwitches(srv.URL, "/signalk/v1/api/vessels/self/electrical/switches")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertSwitchIDOrder(t, switches, []string{"bank.2.1", "bank.10.1"})
}

// TestValidSwitchIDRegexp_AcceptsRealWireIDs covers the ID shapes the generic
// walk actually produces, including the hyphenated venus-*/gx shapes that
// required loosening the regexp.
func TestValidSwitchIDRegexp_AcceptsRealWireIDs(t *testing.T) {
	valid := []string{"bank.0.2", "bank.10.11", "venus-0", "venus-1", "gx.gxInternalRelay1", "gx.gxInternalRelay2"}
	for _, id := range valid {
		if !validSwitchIDRegexp.MatchString(id) {
			t.Errorf("expected %q to be accepted as a switch id", id)
		}
	}
}

// TestValidSwitchIDRegexp_RejectsTraversalAfterAllowingHyphens is the safety
// net for loosening the regexp to permit hyphens (needed for "venus-0"):
// confirms the wider charset did not also open a path-traversal hole. Path
// segments must stay non-empty and alphanumeric-plus-hyphen, so a bare ".."
// segment remains structurally impossible.
func TestValidSwitchIDRegexp_RejectsTraversalAfterAllowingHyphens(t *testing.T) {
	invalid := []string{
		"../etc/passwd",
		"a/b",
		"..",
		".bank",
		"bank.",
		"a..b",
		"",
		"bank.0.2/../../etc/passwd",
	}
	for _, id := range invalid {
		if validSwitchIDRegexp.MatchString(id) {
			t.Errorf("expected %q to be rejected as a switch id", id)
		}
	}
}
