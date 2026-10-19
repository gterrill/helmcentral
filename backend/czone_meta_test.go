package main

import "testing"

// SignalK's REST tree carries meta beside the children of a node. A meta map
// must never be read as a switch of its own.
func TestWalkSwitchTreeSkipsMeta(t *testing.T) {
	tree := map[string]any{
		"bank": map[string]any{
			"meta": map[string]any{"state": map[string]any{"value": true}},
			"1": map[string]any{
				"state": map[string]any{"value": true},
			},
		},
	}
	var out []czoneSwitch
	walkSwitchTree(tree, "", &out)
	if len(out) != 1 || out[0].ID != "bank.1" {
		t.Fatalf("switches = %+v, want only bank.1", out)
	}
}
