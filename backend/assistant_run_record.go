package main

import "encoding/json"

// mateRunCall is one tool call a run made and what came of it. err is why the
// call failed or never ran, "" when it ran; result is what it returned. A call
// that ran and returned nothing still succeeded.
type mateRunCall struct {
	call   openRouterToolCall
	result string
	err    string
}

// ok reports whether the call ran without error.
func (c mateRunCall) ok() bool { return c.err == "" }

// args decodes the call's arguments into v. False when they are not JSON of
// that shape.
func (c mateRunCall) args(v any) bool {
	return json.Unmarshal([]byte(c.call.Function.Arguments), v) == nil
}

// hasNumber reports whether the call's arguments hold a numeric value under
// key at any depth: propose_changes carries a depth in operations[].fields. A
// null or a non-number is not one.
func (c mateRunCall) hasNumber(key string) bool {
	var v any
	return c.args(&v) && holdsNumber(v, key)
}

func holdsNumber(v any, key string) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if _, isNum := e.(float64); isNum && k == key {
				return true
			}
			if holdsNumber(e, key) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if holdsNumber(e, key) {
				return true
			}
		}
	}
	return false
}

// mateRunRecord is every tool call a run has made so far, across every round,
// in the order made (ADR 0256). The draft checks read it to ask whether a tool
// ran and what it returned, instead of walking a slice of calls beside a slice
// of results. A nil record is a run that made no calls.
type mateRunRecord []mateRunCall

// addRound records one round's calls. toolMessages and done are the round's
// output, indexed like calls. A call the round cut short has no record in done
// and is kept with an error saying so.
func (r *mateRunRecord) addRound(calls []openRouterToolCall, toolMessages []openRouterMessage, done []assistantToolCall) {
	for i, c := range calls {
		rc := mateRunCall{call: c}
		if i >= len(toolMessages) || i >= len(done) || done[i].Name == "" {
			rc.err = "the call did not run"
		} else {
			rc.result, rc.err = string(toolMessages[i].Content), done[i].Error
		}
		*r = append(*r, rc)
	}
}

// named returns every call to the named tool, whether or not it ran.
func (r mateRunRecord) named(name string) []mateRunCall {
	var out []mateRunCall
	for _, c := range r {
		if c.call.Function.Name == name {
			out = append(out, c)
		}
	}
	return out
}

// succeeded returns the calls to the named tool that ran without error.
func (r mateRunRecord) succeeded(name string) []mateRunCall {
	var out []mateRunCall
	for _, c := range r.named(name) {
		if c.ok() {
			out = append(out, c)
		}
	}
	return out
}

// producedSince reports whether any call from index n on ran without error.
func (r mateRunRecord) producedSince(n int) bool {
	for _, c := range r[n:] {
		if c.ok() {
			return true
		}
	}
	return false
}
