import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";

import { resetQueryStore } from "@/lib/api-query";
import { usePlannedStay } from "@/hooks/use-planned-stay";
import { searchPlaces } from "@/hooks/use-place-search";
import { usePlaceTideStation } from "@/hooks/use-place-tide-station";

const STAY = {
  name: "Coral Coast Marina",
  lat: -27.2,
  lon: 153.1,
  arrive: "2026-10-27",
  depart: "2026-10-31",
};

function reply(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body };
}

describe("usePlannedStay", () => {
  beforeEach(() => {
    resetQueryStore();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("reads the stay with no date from the device; the server judges expiry at the place", async () => {
    const fetchMock = vi.fn().mockResolvedValue(reply({ stay: STAY }));
    vi.stubGlobal("fetch", fetchMock);
    const { result } = renderHook(() => usePlannedStay());
    await waitFor(() => expect(result.current.stay).toEqual(STAY));
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/api\/planned-stay$/);
  });

  it("has no stay when the server answers null", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(reply({ stay: null })));
    const { result } = renderHook(() => usePlannedStay());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.stay).toBeNull();
  });

  it("saves with a PUT and shows the saved stay at once", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(reply({ stay: null }))
      .mockResolvedValueOnce(reply({ stay: STAY }));
    vi.stubGlobal("fetch", fetchMock);
    const { result } = renderHook(() => usePlannedStay());
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await result.current.save(STAY);
    });
    const [, init] = fetchMock.mock.calls[1];
    expect(init.method).toBe("PUT");
    expect(JSON.parse(init.body)).toEqual(STAY);
    expect(result.current.stay).toEqual(STAY);
  });

  it("clears with a DELETE", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(reply({ stay: STAY }))
      .mockResolvedValueOnce(reply({ stay: null }));
    vi.stubGlobal("fetch", fetchMock);
    const { result } = renderHook(() => usePlannedStay());
    await waitFor(() => expect(result.current.stay).toEqual(STAY));

    await act(async () => {
      await result.current.clear();
    });
    expect(fetchMock.mock.calls[1][1].method).toBe("DELETE");
    expect(result.current.stay).toBeNull();
  });

  it("passes the server's refusal on as it was worded", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(reply({ stay: null }))
      .mockResolvedValueOnce(
        reply(
          { field: "depart", message: "depart cannot be before arrive" },
          false,
          400,
        ),
      );
    vi.stubGlobal("fetch", fetchMock);
    const { result } = renderHook(() => usePlannedStay());
    await waitFor(() => expect(result.current.loading).toBe(false));
    await expect(result.current.save(STAY)).rejects.toThrow(
      "depart cannot be before arrive",
    );
  });
});

describe("searchPlaces", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("maps candidates with their distance and bearing", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        reply({
          results: [
            {
              name: "Coral Coast Marina",
              kind: "marina",
              lat: -27.2,
              lon: 153.1,
              straight_line_nm: 12.3,
              straight_line_bearing_deg: 45,
            },
            { kind: "junk" },
          ],
        }),
      );
    vi.stubGlobal("fetch", fetchMock);
    const found = await searchPlaces(" Coral Coast ");
    expect(String(fetchMock.mock.calls[0][0])).toContain(
      "/api/place-search?q=Coral%20Coast",
    );
    expect(found.candidates).toEqual([
      {
        name: "Coral Coast Marina",
        kind: "marina",
        lat: -27.2,
        lon: 153.1,
        distanceNm: 12.3,
        bearingDeg: 45,
      },
    ]);
  });

  it("keeps a missing distance or bearing absent instead of reading 0", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          reply({
            results: [
              { name: "Bona Bay", kind: "bay", lat: -20.1, lon: 149.1 },
            ],
          }),
        ),
    );
    const found = await searchPlaces("Bona Bay");
    expect(found.candidates[0].distanceNm).toBeNull();
    expect(found.candidates[0].bearingDeg).toBeNull();
  });

  it("does not pass on the note written for Mate", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          reply({
            results: [],
            note: "vessel position unknown; ask the operator for a region",
          }),
        ),
    );
    const found = await searchPlaces("x");
    expect(found).toEqual({ candidates: [] });
  });
});

describe("usePlaceTideStation", () => {
  beforeEach(() => {
    resetQueryStore();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("measures the distance to a station that has coordinates", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          reply({
            station_id: "BRI",
            name: "Brisbane Bar",
            lat: -27.37,
            lon: 153.17,
          }),
        ),
    );
    const { result } = renderHook(() =>
      usePlaceTideStation("bom", { lat: -27.2, lon: 153.1 }),
    );
    await waitFor(() => expect(result.current.nearest).not.toBeNull());
    expect(result.current.nearest?.distanceNm).toBeGreaterThan(5);
  });

  it("never measures against 0,0 when the station has no coordinates", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(reply({ station_id: "BRI", name: "Brisbane Bar" })),
    );
    const { result } = renderHook(() =>
      usePlaceTideStation("bom", { lat: -27.2, lon: 153.1 }),
    );
    await waitFor(() => expect(result.current.nearest).not.toBeNull());
    expect(result.current.nearest?.station.name).toBe("Brisbane Bar");
    expect(result.current.nearest?.distanceNm).toBeNull();
  });
});
