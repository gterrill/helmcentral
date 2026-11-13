import { describe, expect, it, vi } from "vitest";

import { PlanForecastStore, type PlaceFeed } from "@/lib/plan-forecast-store";

const feed = (): PlaceFeed => ({ weather: {}, waves: {} }) as unknown as PlaceFeed;

describe("PlanForecastStore", () => {
  it("holds no listener set for a place once its last row unsubscribes", () => {
    const store = new PlanForecastStore();
    const off = store.subscribe("a", () => {});
    off();
    expect(store.listenerKeys()).toEqual([]);
  });

  it("keeps the set while another row still listens", () => {
    const store = new PlanForecastStore();
    const notify = vi.fn();
    const offA = store.subscribe("a", () => {});
    store.subscribe("a", notify);
    offA();
    store.set("a", feed());
    expect(notify).toHaveBeenCalledTimes(1);
    expect(store.listenerKeys()).toEqual(["a"]);
  });

  it("drops a place's feed, tells its rows, and hands nothing back for a reused key", () => {
    const store = new PlanForecastStore();
    const notify = vi.fn();
    store.subscribe("a", notify);
    store.set("a", feed());
    notify.mockClear();
    store.remove("a");
    expect(store.get("a")).toBeUndefined();
    expect(notify).toHaveBeenCalledTimes(1);
  });
});
