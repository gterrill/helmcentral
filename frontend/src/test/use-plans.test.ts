import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";

import { searchPlaces } from "@/hooks/use-place-search";
import { mapLeg, mapPlan, planToApi, usePlanLegs, usePlans } from "@/hooks/use-plans";
import { resetQueryStore } from "@/lib/api-query";

const reply = (body: unknown, ok = true, status = 200) => ({ ok, status, json: async () => body });

const API_PLAN = {
  id: 7,
  name: "Home run",
  status: "active",
  first_date: "2026-11-14",
  last_date: "2026-11-15",
  stops: [
    { name: "A", lat: -25.5, lon: 152.9, arrive: "2026-11-14", nights: 1, depart: "2026-11-15", vias: null, depart_time: "06:30", timezone: "Australia/Brisbane" },
    { name: "B", lat: -26, lon: 153, arrive: "2026-11-15", nights: 0, depart: "2026-11-15", vias: [], depart_time: "", arrive_by: "2026-11-16T17:00", timezone: "Australia/Brisbane" },
  ],
};

describe("plan mapping", () => {
  it("reads the wire shape, taking no vias as an empty list", () => {
    const plan = mapPlan(API_PLAN as never);
    expect(plan).toMatchObject({ id: 7, firstDate: "2026-11-14", lastDate: "2026-11-15", status: "active" });
    expect(plan.stops[0]).toMatchObject({ vias: [], departTime: "06:30", depart: "2026-11-15" });
  });

  it("reads an arrive-by, and a stop with none as the empty string", () => {
    const plan = mapPlan(API_PLAN as never);
    expect(plan.stops[1].arriveBy).toBe("2026-11-16T17:00");
    expect(plan.stops[0].arriveBy).toBe("");
  });

  it("sends the editor's fields in the wire shape and no server-owned ones", () => {
    const body = planToApi({
      name: "Run",
      stops: [{ name: "A", lat: 1, lon: 2, arrive: "2026-11-14", nights: 1, vias: [], departTime: "06:30", arriveBy: "2026-11-20T17:00" }],
    });
    expect(body.stops[0]).toEqual({ name: "A", lat: 1, lon: 2, arrive: "2026-11-14", nights: 1, vias: [], depart_time: "06:30", arrive_by: "2026-11-20T17:00" });
  });

  it("gives a leg that could not be routed no distance and no course, only its error", () => {
    const leg = mapLeg({ from: 0, to: 1, distance_nm: 0, course_deg: 0, route: [], error: "start is on land" });
    expect(leg).toMatchObject({ distanceNm: null, courseDeg: null, error: "start is on land" });
    expect(mapLeg({ from: 0, to: 1, distance_nm: 12.5, course_deg: 90, route: [[1, 2]] })).toMatchObject({ distanceNm: 12.5, courseDeg: 90 });
  });
});

describe("usePlans", () => {
  beforeEach(() => resetQueryStore());
  afterEach(() => vi.unstubAllGlobals());

  it("reads the plans and which is current", async () => {
    const fetchMock = vi.fn().mockResolvedValue(reply({ plans: [API_PLAN], current_id: 7 }));
    vi.stubGlobal("fetch", fetchMock);
    const { result } = renderHook(() => usePlans());
    await waitFor(() => expect(result.current.plans).toHaveLength(1));
    expect(result.current.currentId).toBe(7);
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/api\/plans$/);
  });
});

describe("usePlanLegs", () => {
  beforeEach(() => resetQueryStore());
  afterEach(() => vi.unstubAllGlobals());

  it("asks again when a stop is renamed, because the leg messages carry the names", async () => {
    const fetchMock = vi.fn().mockResolvedValue(reply({ legs: [] }));
    vi.stubGlobal("fetch", fetchMock);
    const plan = mapPlan(API_PLAN as never);
    const { rerender } = renderHook(({ p }) => usePlanLegs(p), { initialProps: { p: plan } });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    const renamed = { ...plan, stops: [{ ...plan.stops[0], name: "A renamed" }, plan.stops[1]] };
    rerender({ p: renamed });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  });

  it("does not ask again when only the nights change", async () => {
    const fetchMock = vi.fn().mockResolvedValue(reply({ legs: [] }));
    vi.stubGlobal("fetch", fetchMock);
    const plan = mapPlan(API_PLAN as never);
    const { rerender } = renderHook(({ p }) => usePlanLegs(p), { initialProps: { p: plan } });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    rerender({ p: { ...plan, stops: [{ ...plan.stops[0], nights: 2 }, plan.stops[1]] } });
    await new Promise((r) => setTimeout(r, 30));
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});

describe("searchPlaces", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("maps candidates, leaving absent distances null and dropping malformed ones", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      reply({
        results: [
          { name: "Mooloolaba", kind: "marina", lat: -26.68, lon: 153.12, straight_line_nm: 40.2, straight_line_bearing_deg: 10 },
          { name: "Bare", lat: 1, lon: 2 },
          { lat: 1, lon: 2 },
        ],
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const { candidates } = await searchPlaces(" Mooloolaba ");
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/api\/place-search\?q=Mooloolaba$/);
    expect(candidates).toEqual([
      { name: "Mooloolaba", kind: "marina", lat: -26.68, lon: 153.12, distanceNm: 40.2, bearingDeg: 10 },
      { name: "Bare", kind: "", lat: 1, lon: 2, distanceNm: null, bearingDeg: null },
    ]);
  });
});
