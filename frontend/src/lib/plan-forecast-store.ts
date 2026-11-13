import { createContext } from "react";

import type { useWaveForecast } from "@/hooks/use-wave-forecast";
import type { useWeatherForecast } from "@/hooks/use-weather-forecast";
import type { ForecastPlace } from "@/lib/forecast-place";

/** One place's weather and wave feeds, read once for every row at that place. */
export interface PlaceFeed {
  weather: ReturnType<typeof useWeatherForecast>;
  waves: ReturnType<typeof useWaveForecast>;
}

export const placeKey = (place: ForecastPlace) => `${place.lat},${place.lon}`;

/**
 * The feeds of the places a plan table shows, by place. The table reads each
 * distinct place's forecast once and writes it here; every row at that place
 * reads from it, so a row re-renders only when its own place's feed changes.
 */
export class PlanForecastStore {
  private feeds = new Map<string, PlaceFeed>();
  private listeners = new Map<string, Set<() => void>>();

  get = (key: string): PlaceFeed | undefined => this.feeds.get(key);

  set(key: string, feed: PlaceFeed): void {
    const held = this.feeds.get(key);
    if (held && held.weather === feed.weather && held.waves === feed.waves) return;
    this.feeds.set(key, feed);
    this.listeners.get(key)?.forEach((notify) => notify());
  }

  subscribe(key: string, notify: () => void): () => void {
    const set = this.listeners.get(key) ?? new Set<() => void>();
    set.add(notify);
    this.listeners.set(key, set);
    return () => set.delete(notify);
  }
}

export const PlanForecastContext = createContext<PlanForecastStore | null>(null);
