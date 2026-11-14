import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";

import { usePlanAssessment } from "@/hooks/use-plan-assessment";
import { usePlanChangeMarks } from "@/hooks/use-plan-change-marks";
import { mapPlan } from "@/hooks/use-plans";
import { resetQueryStore } from "@/lib/api-query";
import type { PlanLegAssessment } from "@/lib/plans";

const reply = (body: unknown, ok = true, status = 200) => ({ ok, status, json: async () => body });

const leg = (over: Partial<PlanLegAssessment>): PlanLegAssessment => ({
  leg: 0, date: "2026-11-14", key: "A → B|2026-11-14", progress: "", stillInPort: false, verdict: "marginal", limiting: "", note: "", noteKind: "",
  depart: null, departSuggested: false, arrive: null, hours: null, speedKn: null, currentKn: null, currentNote: "", notChecked: [],
  windChanges: { changes: [], notChecked: null }, ...over,
});

const LEGS = [leg({}), leg({ leg: 1, key: "B → C|2026-11-15", verdict: "go" })];

function stubFetch(seen: unknown = { legs: [{ key: "A → B|2026-11-14", verdict: "go" }, { key: "B → C|2026-11-15", verdict: "go" }] }) {
  const fetchMock = vi.fn(async (_url: string, init?: RequestInit) =>
    init?.method === "PUT" ? reply({ saved: true }) : reply(seen),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

const calls = (fetchMock: ReturnType<typeof stubFetch>) =>
  fetchMock.mock.calls.map(([url, init]) => `${init?.method ?? "GET"} ${String(url).replace(/^.*\/api/, "/api")}`);

describe("usePlanChangeMarks", () => {
  beforeEach(() => resetQueryStore());
  afterEach(() => vi.unstubAllGlobals());

  it("reads what was seen last, marks the legs that differ, then saves what is shown now", async () => {
    const fetchMock = stubFetch();
    const { result } = renderHook(() => usePlanChangeMarks(7, LEGS, true));
    await waitFor(() => expect(result.current.marks.size).toBe(1));
    expect(result.current.marks.get("A → B|2026-11-14")).toEqual({ was: "go", now: "marginal" });
    await waitFor(() => expect(calls(fetchMock)).toEqual(["GET /api/plans/7/seen", "PUT /api/plans/7/seen"]));
    const put = fetchMock.mock.calls[1][1] as RequestInit;
    expect(JSON.parse(String(put.body))).toEqual({
      legs: [{ key: "A → B|2026-11-14", verdict: "marginal" }, { key: "B → C|2026-11-15", verdict: "go" }],
    });
  });

  it("compares against what was seen when the visit began, and saves only once", async () => {
    const fetchMock = stubFetch();
    const { result, rerender } = renderHook(({ legs }) => usePlanChangeMarks(7, legs, true), { initialProps: { legs: LEGS } });
    await waitFor(() => expect(result.current.marks.size).toBe(1));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    // The forecast refreshes and the verdict goes back to Go: that is no change from
    // the last visit. The snapshot saved just now never becomes the comparison.
    rerender({ legs: [leg({ verdict: "go" }), LEGS[1]] });
    await waitFor(() => expect(result.current.marks.size).toBe(0));
    rerender({ legs: [leg({ verdict: "no_go" }), LEGS[1]] });
    await waitFor(() => expect(result.current.marks.get("A → B|2026-11-14")).toEqual({ was: "go", now: "no_go" }));
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("saves nothing when the snapshot is the one already on the box", async () => {
    const fetchMock = stubFetch({
      legs: [{ key: "A → B|2026-11-14", verdict: "marginal" }, { key: "B → C|2026-11-15", verdict: "go" }],
    });
    const { result } = renderHook(() => usePlanChangeMarks(7, LEGS, true));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    await new Promise((r) => setTimeout(r, 30));
    expect(result.current.marks.size).toBe(0);
    expect(calls(fetchMock)).toEqual(["GET /api/plans/7/seen"]);
  });

  it("reads what was seen at once but waits for a verdict before it saves anything", async () => {
    const fetchMock = stubFetch();
    const { rerender } = renderHook(({ legs }) => usePlanChangeMarks(7, legs, true), {
      initialProps: { legs: null as PlanLegAssessment[] | null },
    });
    rerender({ legs: [leg({ verdict: null, note: "No forecast yet" })] });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    await new Promise((r) => setTimeout(r, 30));
    expect(calls(fetchMock)).toEqual(["GET /api/plans/7/seen"]);
    rerender({ legs: LEGS });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  });

  it("marks nothing the first time a plan is seen, and then saves", async () => {
    const fetchMock = stubFetch({ legs: [] });
    const { result } = renderHook(() => usePlanChangeMarks(7, LEGS, true));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(result.current.marks.size).toBe(0);
  });

  it("reads but never saves for a viewer who cannot write", async () => {
    const fetchMock = stubFetch();
    const { result } = renderHook(() => usePlanChangeMarks(7, LEGS, false));
    await waitFor(() => expect(result.current.marks.size).toBe(1));
    await new Promise((r) => setTimeout(r, 30));
    expect(calls(fetchMock)).toEqual(["GET /api/plans/7/seen"]);
  });

  it("says so, and does not overwrite what was seen, when it could not be read", async () => {
    const fetchMock = vi.fn().mockResolvedValue(reply({ error: "boom" }, false, 500));
    vi.stubGlobal("fetch", fetchMock);
    const { result } = renderHook(() => usePlanChangeMarks(7, LEGS, true));
    await waitFor(() => expect(result.current.error).toMatch(/boom/));
    expect(result.current.marks.size).toBe(0);
    expect(calls(fetchMock as never)).toEqual(["GET /api/plans/7/seen"]);
  });

  it("on a remount waits for this mount's own read before it marks or saves anything", async () => {
    const first = { legs: [{ key: "A → B|2026-11-14", verdict: "go" }, { key: "B → C|2026-11-15", verdict: "go" }] };
    // What the first visit saved: the verdicts it showed.
    const second = { legs: [{ key: "A → B|2026-11-14", verdict: "marginal" }, { key: "B → C|2026-11-15", verdict: "go" }] };
    let gets = 0;
    let release: () => void = () => {};
    const held = new Promise<void>((r) => (release = r));
    const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === "PUT") return reply({ saved: true });
      gets += 1;
      if (gets === 1) return reply(first);
      await held;
      return reply(second);
    });
    vi.stubGlobal("fetch", fetchMock);
    const one = renderHook(() => usePlanChangeMarks(7, LEGS, true));
    await waitFor(() => expect(calls(fetchMock as never)).toEqual(["GET /api/plans/7/seen", "PUT /api/plans/7/seen"]));
    one.unmount();

    // Back on the page inside the cache window: the cached answer is the old one.
    const two = renderHook(() => usePlanChangeMarks(7, LEGS, true));
    await waitFor(() => expect(gets).toBe(2));
    await new Promise((r) => setTimeout(r, 30));
    expect(two.result.current.marks.size).toBe(0);
    expect(calls(fetchMock as never)).toEqual(["GET /api/plans/7/seen", "PUT /api/plans/7/seen", "GET /api/plans/7/seen"]);

    release();
    await new Promise((r) => setTimeout(r, 30));
    // The fresh read says the box already holds what is shown: no marks, no save.
    expect(two.result.current.marks.size).toBe(0);
    expect(calls(fetchMock as never)).toEqual(["GET /api/plans/7/seen", "PUT /api/plans/7/seen", "GET /api/plans/7/seen"]);
  });

  it("saves only the verdicts of an assessment fetched in this mount, never the cached ones from the last visit", async () => {
    const PLAN = mapPlan({
      id: 7, name: "Home run", status: "active", first_date: "2026-11-14", last_date: "2026-11-15",
      stops: [
        { name: "A", lat: -25.5, lon: 152.9, arrive: "2026-11-14", nights: 0, depart: "2026-11-14", vias: [], depart_time: "06:30", timezone: "Australia/Brisbane" },
        { name: "B", lat: -26, lon: 153, arrive: "2026-11-14", nights: 1, depart: "2026-11-15", vias: [], depart_time: "", timezone: "Australia/Brisbane" },
        { name: "C", lat: -26.5, lon: 153.1, arrive: "2026-11-15", nights: 0, depart: "2026-11-15", vias: [], depart_time: "", timezone: "Australia/Brisbane" },
      ],
    } as never);
    const wireLeg = (i: number, key: string, verdict: string) => ({
      leg: i, date: "2026-11-14", key, progress: "", still_in_port: false, verdict, limiting: "", note: "", note_kind: "",
      depart: "", depart_suggested: false, arrive: "", hours: 5, speed_kn: 6, current_kn: null, current_note: "", not_checked: [],
      wind_changes: { changes: [], not_checked: "" },
    });
    const assessment = (verdictA: string) => ({
      as_of: "", forecast_as_of: "", problem: "", warnings_error: "", night_winds: [], stops: [],
      legs: [wireLeg(0, "A → B|2026-11-14", verdictA), wireLeg(1, "B → C|2026-11-15", "go")],
    });
    // What the box holds from before: A was Go. The cached assessment says Marginal,
    // the fresh one says No-Go.
    const seen = { legs: [{ key: "A → B|2026-11-14", verdict: "go" }, { key: "B → C|2026-11-15", verdict: "go" }] };
    let release: () => void = () => {};
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") return reply({ saved: true });
      if (String(url).endsWith("/seen")) return reply(seen);
      return reply(assessment("marginal"));
    });
    vi.stubGlobal("fetch", fetchMock);
    const visit = () =>
      renderHook(() => {
        const { assessment: a, fresh } = usePlanAssessment(PLAN);
        return usePlanChangeMarks(7, fresh ? (a?.legs ?? null) : null, true);
      });
    const puts = () => fetchMock.mock.calls.filter(([, init]) => init?.method === "PUT").map(([, init]) => JSON.parse(String((init as RequestInit).body)));

    const one = visit();
    await waitFor(() => expect(puts()).toHaveLength(1));
    one.unmount();
    fetchMock.mockClear();

    // Back inside the cache window: the cached assessment renders while the fresh one
    // is held. The cached verdicts must not be saved as the new snapshot.
    const held2 = new Promise<void>((r) => (release = r));
    fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") return reply({ saved: true });
      if (String(url).endsWith("/seen")) return reply(seen);
      await held2;
      return reply(assessment("no_go"));
    });
    const two = visit();
    await new Promise((r) => setTimeout(r, 50));
    expect(puts()).toEqual([]);
    expect(two.result.current.marks.size).toBe(0);
    release();
    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(puts()[0]).toEqual({
      legs: [{ key: "A → B|2026-11-14", verdict: "no_go" }, { key: "B → C|2026-11-15", verdict: "go" }],
    });
  });

  it("starts again for another plan, with none of the first plan's marks", async () => {
    stubFetch();
    const { result, rerender } = renderHook(({ id }) => usePlanChangeMarks(id, LEGS, true), { initialProps: { id: 7 } });
    await waitFor(() => expect(result.current.marks.size).toBe(1));
    rerender({ id: 8 });
    expect(result.current.marks.size).toBe(0);
  });
});
