import { describe, expect, it } from "vitest";

import { dayForecast } from "@/hooks/use-plan-day-forecast";
import type { PlaceFeed } from "@/lib/plan-forecast-store";

function feed(waves: { loading: boolean; error: string | null }): PlaceFeed {
  return {
    weather: { forecast: [], loading: false, error: null },
    waves: { days: [], ...waves },
  } as unknown as PlaceFeed;
}

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
