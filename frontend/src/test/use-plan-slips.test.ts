import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";

import { mapSlips, usePlanSlips } from "@/hooks/use-plan-slips";
import { resetQueryStore } from "@/lib/api-query";

const reply = (body: unknown) => ({ ok: true, status: 200, json: async () => body });

const API_SLIPS = {
  problem: "",
  capped: false,
  slips: [
    { days: 1, late_by_days: 0, leg: 1, date: "2026-11-16", verdict: "go", limiting: "", note: "", note_kind: "" },
    { days: 2, late_by_days: 1, leg: 0, date: "2026-11-16", verdict: "", limiting: "", note: "No forecast yet", note_kind: "no_forecast" },
  ],
};

describe("mapSlips", () => {
  it("reads the wire shape, an empty verdict becoming null", () => {
    const s = mapSlips(API_SLIPS as never);
    expect(s).toMatchObject({ problem: "", capped: false });
    expect(s.slips[0]).toEqual({ days: 1, lateByDays: 0, leg: 1, date: "2026-11-16", verdict: "go", limiting: "", note: "", noteKind: "" });
    expect(s.slips[1]).toMatchObject({ days: 2, lateByDays: 1, verdict: null, noteKind: "no_forecast" });
  });

  it("reads a note kind it does not know as other, as the assessment does", () => {
    const s = mapSlips({ ...API_SLIPS, slips: [{ ...API_SLIPS.slips[1], note_kind: "from_a_newer_server" }] } as never);
    expect(s.slips[0].noteKind).toBe("other");
  });
});

describe("usePlanSlips", () => {
  beforeEach(() => resetQueryStore());
  afterEach(() => vi.unstubAllGlobals());

  it("asks for the slips of the plan, apart from the assessment", async () => {
    const fetchMock = vi.fn().mockResolvedValue(reply(API_SLIPS));
    vi.stubGlobal("fetch", fetchMock);
    const { result } = renderHook(() => usePlanSlips(7, "shape"));
    await waitFor(() => expect(result.current.slips?.slips).toHaveLength(2));
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/api\/plans\/7\/slips$/);
  });

  it("asks again for another settled shape of the plan", async () => {
    const fetchMock = vi.fn().mockResolvedValue(reply(API_SLIPS));
    vi.stubGlobal("fetch", fetchMock);
    const { result, rerender } = renderHook(({ shape }) => usePlanSlips(7, shape), { initialProps: { shape: "one" } });
    await waitFor(() => expect(result.current.slips).not.toBeNull());
    rerender({ shape: "two" });
    expect(result.current.slips).toBeNull();
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  });

  it("says why when the slips could not be priced", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 500, json: async () => ({ error: "router down" }) }));
    const { result } = renderHook(() => usePlanSlips(7, "shape"));
    await waitFor(() => expect(result.current.error).toMatch(/router down/));
    expect(result.current.slips).toBeNull();
  });
});
