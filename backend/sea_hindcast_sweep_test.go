package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ── how a sweep plans and paces its requests, and what it says when it cannot (ADR 0299) ──

// spanOf is the first and last hour the spells' sea lies in.
func spanOf(spells []feltSpell) (first, last time.Time) {
	f, l, _ := hourRange(distinctSeaKeys(spells))
	return time.UnixMilli(f).UTC(), time.UnixMilli(l).UTC()
}

const archiveRefusal = "Open-Meteo wouldn't give the sea for this hour: Parameter 'start_hour' is out of allowed range from 2022-01-01 to 2026-11-30."

// Each passage is asked over its own hours and for its own cells, not as one
// window across every passage.
func TestSeaHindcastAsksEachPassageOverItsOwnHoursAndCells(t *testing.T) {
	store, _, older := seaPassage(t)
	logOf(t)
	_, newer := addSeaPassage(t, store, 60)
	fake := &fakeHindcast{}
	sum := runSeaHindcast(context.Background(), store, resolveTo(fake), passageEpoch.Add(5*24*time.Hour))
	if want := len(distinctSeaKeys(older)) + len(distinctSeaKeys(newer)); sum.Filled != want || sum.Failed != 0 {
		t.Fatalf("summary %+v, want %d filled", sum, want)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("two passages are two requests, got %d", len(fake.calls))
	}
	for i, spells := range [][]feltSpell{older, newer} {
		c := fake.calls[i]
		start, end := spanOf(spells)
		if !c.start.Equal(start) || !c.end.Equal(end) {
			t.Errorf("passage %d asked over %v to %v, its own hours are %v to %v", i, c.start, c.end, start, end)
		}
		cells := distinctCells(spells)
		if len(c.points) != len(cells) {
			t.Errorf("passage %d asked about %d positions, it lies in %d cells", i, len(c.points), len(cells))
		}
		for _, p := range c.points {
			if !cells[seaCell{LatC: centiDegrees(p.Lat), LonC: centiDegrees(p.Lon)}] {
				t.Errorf("passage %d asked about %v, %v, which is not one of its cells", i, p.Lat, p.Lon)
			}
		}
	}
}

// A refusal for the oldest hours (past the archive) settles those hours only; a
// newer passage's hours are asked in a request of their own and filled.
func TestARefusalForOnePassageSettlesOnlyItsOwnHours(t *testing.T) {
	store, _, older := seaPassage(t)
	logOf(t)
	_, newer := addSeaPassage(t, store, 60)
	limit := passageEpoch.Add(30 * time.Hour)
	fake := &fakeHindcast{answer: func(points []currentsPoint, start, end time.Time) waveHindcastBundle {
		if start.Before(limit) {
			return waveHindcastBundle{Refused: archiveRefusal}
		}
		return defaultHindcastAnswer(points, start, end)
	}}
	sum := runSeaHindcast(context.Background(), store, resolveTo(fake), passageEpoch.Add(5*24*time.Hour))
	if sum.NoValue != len(distinctSeaKeys(older)) || sum.Filled != len(distinctSeaKeys(newer)) || sum.Failed != 0 {
		t.Fatalf("summary %+v", sum)
	}
	for k, r := range storedSea(t, store, older) {
		if r.MissingReason != archiveRefusal {
			t.Errorf("old hour %+v: %+v", k, r)
		}
	}
	got := storedSea(t, store, newer)
	if len(got) != len(distinctSeaKeys(newer)) {
		t.Fatalf("newer rows %+v", got)
	}
	for k, r := range got {
		if r.MissingReason != "" || r.Swell == nil {
			t.Errorf("a refusal for the old passage settled a newer hour %+v: %+v", k, r)
		}
	}
}

// A hand-picked pair of hours two passages share is asked once, by the first.
func TestPlanSeaWindowsCutsEachPassageSeparately(t *testing.T) {
	h := func(hours, cell int) seaKey {
		return seaKey{HourMs: passageEpoch.Add(time.Duration(hours) * time.Hour).UnixMilli(), LatC: cell}
	}
	windows := planSeaWindows([][]seaKey{
		{h(0, 1), h(1, 1), h(200, 1)},
		{h(2, 9), h(3, 9)},
	})
	if len(windows) != 3 {
		t.Fatalf("windows %+v", windows)
	}
	if !windows[0].Start.Equal(passageEpoch) || !windows[0].End.Equal(passageEpoch.Add(time.Hour)) {
		t.Errorf("first window %+v", windows[0])
	}
	if !windows[1].Start.Equal(passageEpoch.Add(200*time.Hour)) || len(windows[1].Keys) != 1 {
		t.Errorf("second window %+v", windows[1])
	}
	if !windows[2].Start.Equal(passageEpoch.Add(2*time.Hour)) || !windows[2].End.Equal(passageEpoch.Add(3*time.Hour)) || windows[2].Keys[0].LatC != 9 {
		t.Errorf("a second passage is a window of its own over its own hours: %+v", windows[2])
	}
}

// A rate limit stops the sweep where it is, and the next sweep resumes.
func TestARateLimitStopsTheSweepAndTheNextOneCarriesOn(t *testing.T) {
	store, _, older := seaPassage(t)
	buf := logOf(t)
	_, newer := addSeaPassage(t, store, 60)
	now := passageEpoch.Add(5 * 24 * time.Hour)
	limited := &fakeHindcast{err: fmt.Errorf("%w: open-meteo marine hindcast API rate limited the request (429)", errWaveHindcastRateLimited)}
	sum := runSeaHindcast(context.Background(), store, resolveTo(limited), now)
	if len(limited.calls) != 1 {
		t.Fatalf("the sweep must stop at the first rate limit, asked %d times", len(limited.calls))
	}
	if sum.Failed != len(distinctSeaKeys(older)) || sum.Filled != 0 {
		t.Errorf("summary %+v", sum)
	}
	if got := storedSea(t, store, older); len(got) != 0 {
		t.Errorf("a rate limit stores nothing: %+v", got)
	}
	if out := buf.String(); !strings.Contains(out, "rate limited") || !strings.Contains(out, "next") {
		t.Errorf("the log should say it was rate limited and that the next sweep resumes: %s", out)
	}
	good := &fakeHindcast{}
	sum = runSeaHindcast(context.Background(), store, resolveTo(good), now)
	if want := len(distinctSeaKeys(older)) + len(distinctSeaKeys(newer)); sum.Filled != want || len(good.calls) != 2 {
		t.Errorf("the next sweep resumes: %+v with %d calls", sum, len(good.calls))
	}
}

// Each sweep asks for at most a fixed budget of locations, so a large backfill
// spreads over several hours.
func TestASweepStopsAtItsLocationBudgetAndTheNextOneCarriesOn(t *testing.T) {
	if seaHindcastLocationBudget < maxHindcastPoints {
		t.Fatalf("the budget %d must hold at least one full request of %d", seaHindcastLocationBudget, maxHindcastPoints)
	}
	store, spells := seaMovingPassage(t, 80, 1.2)
	buf := logOf(t)
	cells := distinctCells(spells)
	if len(cells) <= maxHindcastPoints {
		t.Fatalf("the test needs more than %d cells, got %d", maxHindcastPoints, len(cells))
	}
	now := passageEpoch.Add(120 * time.Hour)
	fake := &fakeHindcast{}
	sum := runSeaHindcastWithin(context.Background(), store, resolveTo(fake), now, maxHindcastPoints)
	if len(fake.calls) != 1 || len(fake.calls[0].points) != maxHindcastPoints {
		t.Fatalf("a budget of %d locations is one request of that many, got %d calls", maxHindcastPoints, len(fake.calls))
	}
	if sum.Filled == 0 || sum.Filled >= len(distinctSeaKeys(spells)) || sum.Failed != 0 {
		t.Errorf("summary %+v, want some but not all of %d filled", sum, len(distinctSeaKeys(spells)))
	}
	if !strings.Contains(buf.String(), "budget") {
		t.Errorf("the log should say the sweep stopped at its budget: %s", buf.String())
	}
	rest := &fakeHindcast{}
	runSeaHindcast(context.Background(), store, resolveTo(rest), now)
	if len(rest.calls) != 1 || len(rest.calls[0].points) >= len(cells) {
		t.Errorf("the next sweep asks only for what is left: %+v", rest.calls)
	}
	if got := storedSea(t, store, spells); len(got) != len(distinctSeaKeys(spells)) {
		t.Errorf("stored %d of %d hours", len(got), len(distinctSeaKeys(spells)))
	}
}

// With an old plugin the sweep can store nothing, so the spells and the passage
// line say which plugin to install instead of "hasn't been fetched yet".
func TestAnOldPluginIsNamedOnTheSpellsAndThePassageLine(t *testing.T) {
	store, rec, spells := seaPassage(t)
	logOf(t)
	prev := globalPassageStore
	globalPassageStore = store
	t.Cleanup(func() { globalPassageStore = prev })
	withHelmHeight(t, nil)

	runSeaHindcast(context.Background(), store, resolveTo(&fakeHindcast{unsupported: true}), seaNow)
	lookup := store.seaLookupFor(spells)
	var outs []feltSpellOut
	for _, sp := range spells {
		reason := lookup.forSpell(sp).Reason
		if !strings.Contains(reason, "install the updated fake-waves plugin") || strings.Contains(reason, "hasn't been fetched yet") {
			t.Errorf("spell reason %q", reason)
		}
		outs = append(outs, spellOut(sp.Start, sp.End, lookup.forSpell(sp)))
	}
	if r := seaSummaryOf(outs).Reason; !strings.Contains(r, "install the updated fake-waves plugin") || strings.Contains(r, "this hour") {
		t.Errorf("passage line %q", r)
	}
	_, body := getJSON(t, getPassageHandler, "/api/passages/x", rec.ID)
	felt := body["passage"].(map[string]any)["felt"].(map[string]any)
	if r, _ := felt["sea"].(map[string]any)["reason"].(string); !strings.Contains(r, "install the updated fake-waves plugin") {
		t.Errorf("the page's passage line: %q", r)
	}

	// No wave plugin at all says so too.
	runSeaHindcast(context.Background(), store, func() (waveProvider, string, error) {
		return nil, "", fmt.Errorf("unknown wave provider configured: %q: %w", "open-meteo-marine", errNoWaveProviderInstalled)
	}, seaNow)
	if r := store.seaLookupFor(spells).forSpell(spells[0]).Reason; !strings.Contains(r, "no wave provider") || strings.Contains(r, "hasn't been fetched yet") {
		t.Errorf("no plugin: %q", r)
	}

	// A plugin that can answer again clears it: the hours fill and nothing is said.
	runSeaHindcast(context.Background(), store, resolveTo(&fakeHindcast{}), seaNow)
	if r := store.seaLookupFor(spells).forSpell(spells[0]); r.Reason != "" || r.Swell == nil {
		t.Errorf("after the plugin is updated: %+v", r)
	}
}

// A transient failure is not a plugin problem: the spells still say the hour
// hasn't been fetched yet.
func TestATransientErrorLeavesTheSpellsSayingNotFetchedYet(t *testing.T) {
	store, _, spells := seaPassage(t)
	logOf(t)
	runSeaHindcast(context.Background(), store, resolveTo(&fakeHindcast{err: errors.New("returned 503")}), seaNow)
	if r := store.seaLookupFor(spells).forSpell(spells[0]).Reason; r != seaReasonNotFetched {
		t.Errorf("reason %q", r)
	}
}

// The passage line never carries a spell's raw reason or its "this hour".
func TestPassageSeaLineNeverCarriesASpellsRawReason(t *testing.T) {
	refused := func(raw string) spellSea {
		return spellSea{Reason: "Open-Meteo wouldn't give the sea for this hour: " + raw}
	}
	const want = "Open-Meteo wouldn't give the sea for this passage's hours."
	one := seaSummaryOf([]feltSpellOut{
		spellOut(1000, 1900, refused("Parameter 'start_hour' is out of allowed range from 2022-01-01 to 2026-11-30.")),
		spellOut(2000, 2900, refused("Parameter 'start_hour' is out of allowed range from 2022-01-01 to 2026-11-30.")),
	})
	if one.Reason != want {
		t.Errorf("every spell refused: %q", one.Reason)
	}
	// Spells refused for different raw reasons are still one answer for the passage.
	two := seaSummaryOf([]feltSpellOut{
		spellOut(1000, 1900, refused("one thing.")),
		spellOut(2000, 2900, refused("another.")),
	})
	if two.Reason != want {
		t.Errorf("refused for different reasons: %q", two.Reason)
	}
	// A reason the plugin worded for a spell is not copied onto the passage.
	noValue := seaSummaryOf([]feltSpellOut{
		spellOut(1000, 1900, spellSea{Reason: "GFS-Wave has no value at this cell."}),
		spellOut(2000, 2900, spellSea{Reason: "GFS-Wave has no value at this cell."}),
	})
	if strings.Contains(noValue.Reason, "GFS-Wave") || strings.Contains(noValue.Reason, "this hour") || noValue.Reason == "" {
		t.Errorf("a plugin's own reason: %q", noValue.Reason)
	}
}
