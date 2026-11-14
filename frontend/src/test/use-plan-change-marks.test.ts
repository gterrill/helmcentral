import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";

import { usePlanChangeMarks } from "@/hooks/use-plan-change-marks";
import { resetQueryStore } from "@/lib/api-query";
import type { PlanLegAssessment } from "@/lib/plans";

const reply = (body: unknown, ok = true, status = 200) => ({ ok, status, json: async () => body });

const leg = (over: Partial<PlanLegAssessment>): PlanLegAssessment => ({
  leg: 0, date: "2026-11-14", key: "A → B|2026-11-14", progress: "", stillInPort: false, verdict: "marginal", limiting: "", note: "", noteKind: "",
  depart: null, departSuggested: false, arrive: null, hours: null, speedKn: null, currentKn: null, currentNote: "", notChecked: [], ...over,
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

  it("starts again for another plan, with none of the first plan's marks", async () => {
    stubFetch();
    const { result, rerender } = renderHook(({ id }) => usePlanChangeMarks(id, LEGS, true), { initialProps: { id: 7 } });
    await waitFor(() => expect(result.current.marks.size).toBe(1));
    rerender({ id: 8 });
    expect(result.current.marks.size).toBe(0);
  });
});
