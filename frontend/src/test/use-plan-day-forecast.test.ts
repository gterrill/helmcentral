import { describe, expect, it } from "vitest";

import { dayForecast } from "@/hooks/use-plan-day-forecast";
import type { PlaceFeed } from "@/lib/plan-forecast-store";

function feed(waves: { loading: boolean; error: string | null }): PlaceFeed {
  return {
    weather: { forecast: [], loading: false, error: null },
    waves: { days: [], ...waves },
  } as unknown as PlaceFeed;
}

describe("dayForecast overnight window", () => {
  const hours = (speed: number, hourOfDay: number[]) =>
    hourOfDay.map((h) => ({ hourOfDay: h, windSpeed: speed, windGust: speed + 4, windDirectionDeg: 90 }));
  const weatherFeed = (): PlaceFeed =>
    ({
      weather: {
        forecast: [
          { dayKey: "2026-11-15", windSpeed: 20, windGust: 25, windDirection: "E", hourlyWind: [...hours(20, [9, 14]), ...hours(10, [19, 22])] },
          { dayKey: "2026-11-16", windSpeed: 22, windGust: 27, windDirection: "E", hourlyWind: [...hours(14, [2, 4]), ...hours(30, [10, 15])] },
        ],
        loading: false,
        error: null,
      },
      waves: { days: [], loading: false, error: null },
    }) as unknown as PlaceFeed;

  it("reads a night from 18:00 to 06:00 the next day", () => {
    const day = dayForecast(weatherFeed(), "2026-11-15", "overnight");
    expect(day.wind).toMatchObject({ minKts: 10, maxKts: 14 });
    expect(day.windDirection).toBe("E");
  });
  it("reads the whole day for a passage row", () => {
    expect(dayForecast(weatherFeed(), "2026-11-15").wind).toMatchObject({ minKts: 10, maxKts: 20 });
  });
  it("uses the evening alone on the forecast's last day", () => {
    expect(dayForecast(weatherFeed(), "2026-11-16", "overnight").wind).toBeNull();
  });
});

describe("dayForecast waves state", () => {
  it("says the waves are still loading rather than showing no reading", () => {
    const day = dayForecast(feed({ loading: true, error: null }), "2026-11-15");
    expect(day.wavesLoading).toBe(true);
    expect(day.wavesError).toBeNull();
  });

  it("carries a failed wave fetch instead of an empty cell", () => {
    const day = dayForecast(feed({ loading: false, error: "Failed to load wave forecast data" }), "2026-11-15");
    expect(day.wavesLoading).toBe(false);
    expect(day.wavesError).toBe("Failed to load wave forecast data");
  });
});
