import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";

import { resetQueryStore } from "@/lib/api-query";
import { useForecastWarnings } from "@/hooks/use-forecast-warnings";
import { useUpperAir } from "@/hooks/use-upper-air";
import { useWaveForecast } from "@/hooks/use-wave-forecast";
import { useWeatherForecast } from "@/hooks/use-weather-forecast";

// The Forecast page's stay view asks the same endpoints about a named place.
// Those are separate queries: the dashboard's own-ship ones, which feed the
// tiles, the clock's sunrise and sunset, Auto theme and the Rode Planner,
// must keep asking about the boat.

const PLACE = { lat: -27.2, lon: 153.1 };

function stubFetch() {
  const fetchMock = vi
    .fn()
    .mockResolvedValue({
      ok: true,
      json: async () => ({ days: [], bulletins: [] }),
    });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

const urls = (fetchMock: ReturnType<typeof vi.fn>) =>
  fetchMock.mock.calls.map((c) => String(c[0]));

describe("forecast hooks with a place", () => {
  beforeEach(() => {
    resetQueryStore();
    vi.clearAllMocks();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  const cases = [
    [
      "weather",
      "/api/weather-forecast",
      (place?: typeof PLACE, enabled?: boolean) =>
        useWeatherForecast(3600, { place, enabled }),
    ],
    [
      "wave",
      "/api/wave-forecast",
      (place?: typeof PLACE, enabled?: boolean) =>
        useWaveForecast(3600, { place, enabled }),
    ],
    [
      "upper air",
      "/api/upper-air",
      (place?: typeof PLACE, enabled?: boolean) =>
        useUpperAir(21600, { place, enabled }),
    ],
    [
      "warnings",
      "/api/forecast-warnings",
      (place?: typeof PLACE, enabled?: boolean) =>
        useForecastWarnings(3600, { place, enabled }),
    ],
  ] as const;

  for (const [name, path, useHook] of cases) {
    it(`${name}: asks about the place when given one`, async () => {
      const fetchMock = stubFetch();
      renderHook(() => useHook(PLACE));
      await act(async () => {
        await Promise.resolve();
      });
      expect(
        urls(fetchMock).some((u) => u.endsWith(`${path}?lat=-27.2&lon=153.1`)),
      ).toBe(true);
    });

    it(`${name}: asks about the boat, with no position, when given none`, async () => {
      const fetchMock = stubFetch();
      renderHook(() => useHook(undefined));
      await act(async () => {
        await Promise.resolve();
      });
      expect(urls(fetchMock).every((u) => u.endsWith(path))).toBe(true);
      expect(fetchMock).toHaveBeenCalledTimes(1);
    });

    it(`${name}: keeps the boat and the place as separate queries`, async () => {
      const fetchMock = stubFetch();
      renderHook(() => {
        useHook(undefined);
        useHook(PLACE);
      });
      await act(async () => {
        await Promise.resolve();
      });
      expect(fetchMock).toHaveBeenCalledTimes(2);
    });

    it(`${name}: does not fetch while disabled`, async () => {
      const fetchMock = stubFetch();
      renderHook(() => useHook(PLACE, false));
      await act(async () => {
        await Promise.resolve();
      });
      expect(fetchMock).not.toHaveBeenCalled();
    });
  }
});
