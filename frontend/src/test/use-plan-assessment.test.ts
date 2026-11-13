import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";

import { mapAssessment, usePlanAssessment } from "@/hooks/use-plan-assessment";
import { mapPlan } from "@/hooks/use-plans";
import { resetQueryStore } from "@/lib/api-query";
import { currentText, verdictText, type PlanLegAssessment } from "@/lib/plans";

const reply = (body: unknown) => ({ ok: true, status: 200, json: async () => body });

/** Moves the fake clock on inside act, so the updates a timer makes are flushed. */
const tick = (ms: number) =>
  act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });

const API_PLAN = {
  id: 7,
  name: "Home run",
  status: "active",
  first_date: "2026-11-14",
  last_date: "2026-11-15",
  stops: [
    { name: "A", lat: -25.5, lon: 152.9, arrive: "2026-11-14", nights: 0, depart: "2026-11-14", vias: [], depart_time: "06:30", timezone: "Australia/Brisbane" },
    { name: "B", lat: -26, lon: 153, arrive: "2026-11-14", nights: 1, depart: "2026-11-15", vias: [], depart_time: "", timezone: "Australia/Brisbane" },
    { name: "C", lat: -26.5, lon: 153.1, arrive: "2026-11-15", nights: 0, depart: "2026-11-15", vias: [], depart_time: "", timezone: "Australia/Brisbane" },
  ],
};

const API_ASSESSMENT = {
  as_of: "2026-11-13T20:30:00Z",
  forecast_as_of: "2026-11-13T19:12:00Z",
  problem: "",
  warnings_error: "",
  legs: [
    {
      leg: 0, date: "2026-11-14", verdict: "no_go", limiting: "gusts 32 kn ahead", note: "", note_kind: "",
      depart: "2026-11-14T06:30:00+10:00", depart_suggested: false, arrive: "2026-11-14T11:30:00+10:00",
      hours: 5, speed_kn: 6, current_kn: -0.8, current_note: "", not_checked: ["wave"],
    },
    {
      leg: 1, date: "2026-11-15", verdict: "", limiting: "", note: "No daylight window", note_kind: "no_window",
      depart: "", depart_suggested: false, arrive: "", hours: 5, speed_kn: 6, current_kn: null, current_note: "", not_checked: [],
    },
  ],
  stops: [{ stop: 1, nights: [{ date: "2026-11-14", warning: "Gale Warning", url: "https://example.test/w" }] }],
};

describe("mapAssessment", () => {
  it("reads the wire shape, an empty verdict and time becoming null", () => {
    const a = mapAssessment(API_ASSESSMENT as never);
    expect(a.asOf).toBe("2026-11-13T20:30:00Z");
    expect(a.forecastAsOf).toBe("2026-11-13T19:12:00Z");
    expect(a.legs[0]).toMatchObject({ verdict: "no_go", limiting: "gusts 32 kn ahead", departSuggested: false, currentKn: -0.8, hours: 5 });
    expect(a.legs[1]).toMatchObject({ verdict: null, depart: null, arrive: null, currentKn: null, note: "No daylight window", noteKind: "no_window" });
    expect(a.legs[0].noteKind).toBe("");
    expect(a.stops[0]).toEqual({ stop: 1, nights: [{ date: "2026-11-14", warning: "Gale Warning", url: "https://example.test/w" }] });
  });

  it("keeps a missing forecast time null, not an empty stamp", () => {
    expect(mapAssessment({ ...API_ASSESSMENT, forecast_as_of: "" } as never).forecastAsOf).toBeNull();
  });
});

describe("verdictText and currentText", () => {
  const leg = (over: Partial<PlanLegAssessment>): PlanLegAssessment => ({
    leg: 0, date: "2026-11-14", verdict: null, limiting: "", note: "", noteKind: "", depart: null, departSuggested: false, arrive: null,
    hours: null, speedKn: null, currentKn: null, currentNote: "", notChecked: [], ...over,
  });

  it("names the limiting factor on a marginal or no-go leg", () => {
    expect(verdictText(leg({ verdict: "no_go", limiting: "gusts 32 kn ahead" }))).toBe("No-Go: gusts 32 kn ahead");
    expect(verdictText(leg({ verdict: "marginal", limiting: "wind 15 kn ahead" }))).toBe("Marginal: wind 15 kn ahead");
  });

  it("says Go and No data plainly, and the note when there is no verdict", () => {
    expect(verdictText(leg({ verdict: "go", limiting: "wind 9 kn abeam" }))).toBe("Go");
    expect(verdictText(leg({ verdict: "no_data", note: "Forecast missing for part of the leg" }))).toBe("No data");
    expect(verdictText(leg({ note: "No daylight window" }))).toBe("No daylight window");
    expect(verdictText(leg({}))).toBeNull();
  });

  it("reads the current as helping or hindering, to a tenth of a knot", () => {
    expect(currentText(0.97)).toBe("1.0 kn helping");
    expect(currentText(-0.8)).toBe("0.8 kn hindering");
    expect(currentText(0.02)).toBe("0.0 kn");
  });
});

describe("usePlanAssessment", () => {
  beforeEach(() => resetQueryStore());
  afterEach(() => vi.unstubAllGlobals());

  it("reads the plan's assessment", async () => {
    const fetchMock = vi.fn().mockResolvedValue(reply(API_ASSESSMENT));
    vi.stubGlobal("fetch", fetchMock);
    const { result } = renderHook(() => usePlanAssessment(mapPlan(API_PLAN as never)));
    await waitFor(() => expect(result.current.assessment).not.toBeNull());
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/api\/plans\/7\/assessment$/);
    expect(result.current.assessment?.legs).toHaveLength(2);
  });

  it("asks again when the operator changes a departure time, because the verdict is at that time", async () => {
    const fetchMock = vi.fn().mockResolvedValue(reply(API_ASSESSMENT));
    vi.stubGlobal("fetch", fetchMock);
    const plan = mapPlan(API_PLAN as never);
    const { rerender } = renderHook(({ p }) => usePlanAssessment(p), { initialProps: { p: plan } });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    rerender({ p: { ...plan, stops: [{ ...plan.stops[0], departTime: "08:00" }, ...plan.stops.slice(1)] } });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2), { timeout: 2000 });
  });

  it("waits half a second for the plan to stop changing before it asks again", async () => {
    vi.useFakeTimers();
    try {
      const fetchMock = vi.fn().mockResolvedValue(reply(API_ASSESSMENT));
      vi.stubGlobal("fetch", fetchMock);
      const plan = mapPlan(API_PLAN as never);
      const edited = (time: string) => ({ ...plan, stops: [{ ...plan.stops[0], departTime: time }, ...plan.stops.slice(1)] });
      const { rerender } = renderHook(({ p }) => usePlanAssessment(p), { initialProps: { p: plan } });
      await tick(10);
      expect(fetchMock).toHaveBeenCalledTimes(1);

      // Three edits in quick succession are one question, asked after the last.
      rerender({ p: edited("07:00") });
      await tick(200);
      rerender({ p: edited("07:30") });
      await tick(200);
      rerender({ p: edited("08:00") });
      await tick(400);
      expect(fetchMock).toHaveBeenCalledTimes(1);
      await tick(200);
      expect(fetchMock).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps showing the last assessment while the next one loads", async () => {
    vi.useFakeTimers();
    try {
      const fetchMock = vi.fn().mockResolvedValueOnce(reply(API_ASSESSMENT)).mockReturnValue(new Promise(() => {}));
      vi.stubGlobal("fetch", fetchMock);
      const plan = mapPlan(API_PLAN as never);
      const { result, rerender } = renderHook(({ p }) => usePlanAssessment(p), { initialProps: { p: plan } });
      await tick(10);
      expect(result.current.assessment?.legs).toHaveLength(2);
      rerender({ p: { ...plan, stops: [{ ...plan.stops[0], departTime: "08:00" }, ...plan.stops.slice(1)] } });
      await tick(700);
      expect(fetchMock).toHaveBeenCalledTimes(2);
      expect(result.current.assessment?.legs).toHaveLength(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it("marks the carried-over assessment as updating from the edit until the new one lands", async () => {
    vi.useFakeTimers();
    try {
      let answer: (v: unknown) => void = () => {};
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce(reply(API_ASSESSMENT))
        .mockReturnValue(new Promise((r) => (answer = r)));
      vi.stubGlobal("fetch", fetchMock);
      const plan = mapPlan(API_PLAN as never);
      const { result, rerender } = renderHook(({ p }) => usePlanAssessment(p), { initialProps: { p: plan } });
      await tick(10);
      expect(result.current.updating).toBe(false);
      rerender({ p: { ...plan, stops: [{ ...plan.stops[0], departTime: "08:00" }, ...plan.stops.slice(1)] } });
      // At once, while the plan is still being edited, the old verdicts are not current.
      expect(result.current.updating).toBe(true);
      await tick(700);
      expect(fetchMock).toHaveBeenCalledTimes(2);
      expect(result.current.updating).toBe(true);
      expect(result.current.assessment?.legs).toHaveLength(2);
      await act(async () => {
        answer(reply(API_ASSESSMENT));
        await vi.advanceTimersByTimeAsync(10);
      });
      expect(result.current.updating).toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });

  it("carries a leg over only while its from and to stops are the same, so no verdict lands on another row", async () => {
    vi.useFakeTimers();
    try {
      const fetchMock = vi.fn().mockResolvedValueOnce(reply(API_ASSESSMENT)).mockReturnValue(new Promise(() => {}));
      vi.stubGlobal("fetch", fetchMock);
      const plan = mapPlan(API_PLAN as never);
      const { result, rerender } = renderHook(({ p }) => usePlanAssessment(p), { initialProps: { p: plan } });
      await tick(10);
      expect(result.current.assessment?.legs).toHaveLength(2);
      // A stop added at the end leaves the two legs as they were.
      const d = { ...plan.stops[2], name: "D", arrive: "2026-11-16", departTime: "" };
      rerender({ p: { ...plan, stops: [...plan.stops, d] } });
      await tick(700);
      expect(result.current.assessment?.legs.map((l) => l.leg)).toEqual([0, 1]);
      // A stop inserted after A moves every leg: A -> B is now A -> X.
      const x = { ...plan.stops[1], name: "X" };
      rerender({ p: { ...plan, stops: [plan.stops[0], x, ...plan.stops.slice(1)] } });
      await tick(700);
      expect(result.current.assessment?.legs).toEqual([]);
      expect(result.current.assessment?.stops).toEqual([]);
    } finally {
      vi.useRealTimers();
    }
  });

  it("does not show one plan's assessment for another", async () => {
    vi.useFakeTimers();
    try {
      const fetchMock = vi.fn().mockResolvedValueOnce(reply(API_ASSESSMENT)).mockReturnValue(new Promise(() => {}));
      vi.stubGlobal("fetch", fetchMock);
      const plan = mapPlan(API_PLAN as never);
      const { result, rerender } = renderHook(({ p }) => usePlanAssessment(p), { initialProps: { p: plan } });
      await tick(10);
      expect(result.current.assessment).not.toBeNull();
      rerender({ p: { ...plan, id: 8 } });
      await tick(10);
      expect(String(fetchMock.mock.calls[1][0])).toMatch(/\/api\/plans\/8\/assessment$/);
      expect(result.current.assessment).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it("asks again on the forecast refresh interval", async () => {
    vi.useFakeTimers();
    try {
      const fetchMock = vi.fn().mockResolvedValue(reply(API_ASSESSMENT));
      vi.stubGlobal("fetch", fetchMock);
      renderHook(() => usePlanAssessment(mapPlan(API_PLAN as never)));
      await vi.advanceTimersByTimeAsync(10);
      expect(fetchMock).toHaveBeenCalledTimes(1);
      await vi.advanceTimersByTimeAsync(600_000 + 10);
      expect(fetchMock.mock.calls.length).toBeGreaterThanOrEqual(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it("does not ask for a plan of one stop", async () => {
    const fetchMock = vi.fn().mockResolvedValue(reply(API_ASSESSMENT));
    vi.stubGlobal("fetch", fetchMock);
    const plan = mapPlan({ ...API_PLAN, stops: API_PLAN.stops.slice(0, 1) } as never);
    renderHook(() => usePlanAssessment(plan));
    await new Promise((r) => setTimeout(r, 30));
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
