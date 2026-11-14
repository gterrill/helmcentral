package main

import (
	"testing"
)

// A get_plans result that carries the plan's own verdicts is the passage check
// for those legs (ADR 0289), so the passage guard does not send the draft back
// to assess_passage_window for them.

func getPlansCall() []openRouterToolCall {
	var c openRouterToolCall
	c.Function.Name = mateToolGetPlans.name
	c.Function.Arguments = `{}`
	return []openRouterToolCall{c}
}

const (
	plansWithVerdicts = `{"plans":[{"id":1,"name":"Home run"}],"plan":{"id":1,"legs":[{"from":"A","to":"B","verdict":"no_go","limiting":"Wind 25 kn from N, ahead"}]}}`
	plansNoVerdicts   = `{"plans":[{"id":1,"name":"Home run"}],"plan":{"id":1,"legs":[{"from":"A","to":"B","note":"No forecast yet"}]}}`
	plansBriefOnly    = `{"plans":[{"id":1,"name":"Home run"}]}`
)

func TestPassageGuard_APlanWithVerdictsIsThePassageCheck(t *testing.T) {
	d := pwToolDeps(pwLimitsAll())
	cases := []struct {
		name   string
		draft  string
		result string
		want   string
	}{
		{"relative wind, plan verdicts", "A head sea on the bow all day.", plansWithVerdicts, ""},
		{"limits claim, plan verdicts", "Set passage limits in Settings.", plansWithVerdicts, ""},
		{"relative wind, a leg with only a note", "A head sea on the bow all day.", plansNoVerdicts, "relative wind"},
		{"relative wind, the brief list alone", "A head sea on the bow all day.", plansBriefOnly, "relative wind"},
		{"relative wind, the call failed", "A head sea on the bow all day.", "", "relative wind"},
		{"limits claim, the call failed", "Set passage limits in Settings.", "", "passage limits claim"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := getPlansCall()
			if got, _ := d.checkDraft(tc.draft, runOf(calls, []string{tc.result})); got != tc.want {
				t.Errorf("trigger = %q, want %q", got, tc.want)
			}
		})
	}
}
