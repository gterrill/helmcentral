import { describe, expect, it } from "vitest";

import {
  buildPlanRows,
  legErrorText,
  legNoteText,
  draftToInput,
  previewArrivals,
  dayCoverage,
  meanForecastBearing,
  summariseOvernightWind,
  clearMovedLegs,
  findPlanRow,
  formatWaveRange,
  formatWindRange,
  legForecastPlace,
  summariseDayWind,
  windRelation,
  type PlanLeg,
  type PlanStop,
} from "@/lib/plans";

function stop(name: string, arrive: string, nights: number): PlanStop {
  const end = new Date(Date.UTC(+arrive.slice(0, 4), +arrive.slice(5, 7) - 1, +arrive.slice(8) + nights));
  return {
    name,
    lat: -26,
    lon: 153,
    arrive,
    nights,
    depart: end.toISOString().slice(0, 10),
    vias: [],
    departTime: "",
    arriveBy: "",
    timezone: "Australia/Brisbane",
  };
}

describe("buildPlanRows", () => {
  it("makes one row per day: lay days, then the passage on the day the boat sails", () => {
    // 14 Nov Great Sandy, 2 lay days; sails 16 Nov to Pelican Bay (1 night); sails 17 Nov to Mooloolaba (0).
    const rows = buildPlanRows({
      stops: [stop("A", "2026-11-14", 2), stop("B", "2026-11-16", 1), stop("C", "2026-11-17", 0)],
    });
    expect(rows.map((r) => [r.kind, r.dayKey, r.dayNumber])).toEqual([
      ["stop", "2026-11-14", 1],
      ["stop", "2026-11-15", 2],
      ["passage", "2026-11-16", 3],
      ["passage", "2026-11-17", 4],
    ]);
    expect(rows[2]).toMatchObject({ legIndex: 0 });
    expect(rows[3]).toMatchObject({ legIndex: 1 });
  });

  it("gives a stop's extra nights stop rows after the arrival day", () => {
    const rows = buildPlanRows({
      stops: [stop("A", "2026-11-14", 0), stop("B", "2026-11-14", 3), stop("C", "2026-11-17", 0)],
    });
    expect(rows.map((r) => [r.kind, r.dayKey])).toEqual([
      ["passage", "2026-11-14"],
      ["stop", "2026-11-15"],
      ["stop", "2026-11-16"],
      ["passage", "2026-11-17"],
    ]);
    expect(rows[1]).toMatchObject({ stopIndex: 1 });
  });

  it("shows a single stop for its nights, at least the arrival day", () => {
    // Three nights is four days: the arrival day through the day it leaves.
    expect(buildPlanRows({ stops: [stop("A", "2026-11-14", 3)] })).toHaveLength(4);
    expect(buildPlanRows({ stops: [stop("A", "2026-11-14", 0)] })).toHaveLength(1);
    expect(buildPlanRows({ stops: [] })).toEqual([]);
  });
});

describe("buildPlanRows final day", () => {
  it("includes the last stop's departure day, so the rows span first to last date", () => {
    const rows = buildPlanRows({
      stops: [stop("A", "2026-11-14", 1), stop("B", "2026-11-15", 2)],
    });
    expect(rows.map((r) => [r.kind, r.dayKey, r.dayNumber])).toEqual([
      ["stop", "2026-11-14", 1],
      ["passage", "2026-11-15", 2],
      ["stop", "2026-11-16", 3],
      ["stop", "2026-11-17", 4],
    ]);
  });
});

describe("plan row keys", () => {
  const plan = { stops: [stop("A", "2026-11-14", 2), stop("B", "2026-11-16", 2), stop("C", "2026-11-18", 0)] };
  it("names a row by what it is, not by its date", () => {
    expect(buildPlanRows(plan).map((r) => r.key)).toEqual(["stop-0-0", "stop-0-1", "leg-0", "stop-1-0", "leg-1"]);
  });
  it("finds a row again by its key after the dates move", () => {
    // A night added at A moves every later date; the key still lands on A's second lay day.
    const longer = { stops: [stop("A", "2026-11-14", 3), stop("B", "2026-11-17", 2), stop("C", "2026-11-19", 0)] };
    expect(findPlanRow(longer, "stop-0-1")).toMatchObject({ kind: "stop", stopIndex: 0, dayKey: "2026-11-15" });
    expect(findPlanRow(longer, "leg-0")).toMatchObject({ kind: "passage", legIndex: 0, dayKey: "2026-11-17" });
  });
  it("is null when the row is gone", () => {
    expect(findPlanRow({ stops: [stop("A", "2026-11-14", 1)] }, "leg-0")).toBeNull();
    expect(findPlanRow(plan, "stop-0-5")).toBeNull();
  });
});

describe("clearMovedLegs", () => {
  const via = { name: "V", lat: 1, lon: 2 };
  const d = (id: string) => ({ id, vias: [via], departTime: "06:00" });
  it("clears the vias and time of a stop whose next stop changed", () => {
    const before = [d("a"), d("b"), d("c")];
    const after = clearMovedLegs(before, [before[0], before[2], before[1]]);
    expect(after[0]).toMatchObject({ id: "a", vias: [], departTime: "" });
    expect(after[1]).toMatchObject({ id: "c", vias: [], departTime: "" });
    expect(after[2]).toMatchObject({ id: "b" });
  });
  it("clears the stop before a removed one and keeps legs that did not change", () => {
    const before = [d("a"), d("b"), d("c"), d("d")];
    const after = clearMovedLegs(before, [before[0], before[1], before[3]]);
    expect(after[0].vias).toEqual([via]);
    expect(after[1]).toMatchObject({ id: "b", vias: [], departTime: "" });
  });
});

describe("windRelation", () => {
  it("reads a wind from the direction of travel as ahead", () => {
    expect(windRelation(90, 90)).toBe("ahead");
    expect(windRelation(90, 130)).toBe("ahead");
    expect(windRelation(350, 20)).toBe("ahead");
  });
  it("reads a wind from behind as astern", () => {
    expect(windRelation(90, 270)).toBe("astern");
    expect(windRelation(10, 200)).toBe("astern");
  });
  it("reads a wind from the side as abeam", () => {
    expect(windRelation(90, 0)).toBe("abeam");
    expect(windRelation(90, 180)).toBe("abeam");
  });
  // The server's passageSector (assistant_passage_window.go): ahead under 60
  // degrees off the bow, abeam from 60 up to but not including 120, astern from 120.
  it("draws the sector edges where the server does, on both sides", () => {
    expect(windRelation(0, 59)).toBe("ahead");
    expect(windRelation(0, 60)).toBe("abeam");
    expect(windRelation(0, 119)).toBe("abeam");
    expect(windRelation(0, 120)).toBe("astern");
    expect(windRelation(0, 240)).toBe("astern");
    expect(windRelation(0, 300)).toBe("abeam");
    expect(windRelation(0, 301)).toBe("ahead");
  });
});

describe("meanForecastBearing", () => {
  it("averages across north", () => {
    expect(meanForecastBearing([350, 10])).toBe(0);
  });
  it("is null with nothing usable, and ignores the -1 sentinel", () => {
    expect(meanForecastBearing([])).toBeNull();
    expect(meanForecastBearing([-1])).toBeNull();
    expect(meanForecastBearing([-1, 90])).toBe(90);
  });
});

describe("summariseOvernightWind", () => {
  const hr = (hourOfDay: number, windSpeed: number, windDirectionDeg = 90, windGust = windSpeed + 5) => ({
    hourOfDay,
    windSpeed,
    windGust,
    windDirectionDeg,
  });
  it("reads 18:00 on the night's day to 06:00 the next morning, and nothing of the daytime", () => {
    const evening = [hr(9, 30), hr(17, 28), hr(18, 10), hr(23, 12)];
    const morning = [hr(0, 14), hr(5, 16), hr(6, 25), hr(12, 30)];
    expect(summariseOvernightWind(evening, morning)).toEqual({ minKts: 10, maxKts: 16, gustKts: 21, fromDeg: 90 });
  });
  it("uses the evening alone when the next day is past the forecast, without inventing a morning", () => {
    expect(summariseOvernightWind([hr(9, 30), hr(20, 11), hr(22, 13)], undefined)).toEqual({
      minKts: 11,
      maxKts: 13,
      gustKts: 18,
      fromDeg: 90,
    });
  });
  it("has no wind rather than the day's figures when no overnight hour exists", () => {
    expect(summariseOvernightWind([hr(9, 30)], [hr(12, 30)])).toBeNull();
    expect(summariseOvernightWind([], undefined)).toBeNull();
  });
  it("skips hours the forecast has no reading for", () => {
    expect(summariseOvernightWind([hr(19, -1), hr(20, 8)], [])).toMatchObject({ minKts: 8, maxKts: 8 });
  });
});

describe("summariseDayWind", () => {
  it("takes the range and highest gust from the hours", () => {
    const w = summariseDayWind(
      [
        { windSpeed: 12, windGust: 18, windDirectionDeg: 80 },
        { windSpeed: 20, windGust: 27, windDirectionDeg: 100 },
      ],
      { windSpeed: 15, windGust: 22 },
    );
    expect(w).toEqual({ minKts: 12, maxKts: 20, gustKts: 27, fromDeg: 90 });
  });
  it("reports no wind rather than -1 when the forecast has none", () => {
    expect(summariseDayWind([{ windSpeed: -1, windGust: -1, windDirectionDeg: -1 }], { windSpeed: -1, windGust: -1 })).toBeNull();
  });
  it("uses the day's own figures, and no direction, without hours", () => {
    expect(summariseDayWind([], { windSpeed: 15, windGust: 22 })).toEqual({
      minKts: 15,
      maxKts: 15,
      gustKts: 22,
      fromDeg: null,
    });
  });
});

describe("summariseDayWind gust sentinel", () => {
  it("has no gust when the forecast sent -1", () => {
    const w = summariseDayWind([{ windSpeed: 10, windGust: -1, windDirectionDeg: 90 }], { windSpeed: 10, windGust: -1 });
    expect(w?.gustKts).toBeNull();
    expect(summariseDayWind([], { windSpeed: 15, windGust: -1 })?.gustKts).toBeNull();
  });
});

describe("formatting", () => {
  it("writes ranges once when they are one number", () => {
    expect(formatWindRange(12.4, 18.2)).toBe("12–18 kn");
    expect(formatWindRange(15, 15.2)).toBe("15 kn");
    expect(formatWaveRange(1.2, 1.84)).toBe("1.2–1.8 m");
    expect(formatWaveRange(0.5, 0.52)).toBe("0.5 m");
  });
});

describe("legForecastPlace", () => {
  it("reads the middle of the route, rounded", () => {
    const leg = {
      route: [
        [-26, 153],
        [-26.123, 153.456],
        [-27, 154],
      ],
    } as PlanLeg;
    expect(legForecastPlace(leg)).toEqual({ lat: -26.12, lon: 153.46 });
  });
  it("has no place, and never a straight-line midpoint, when the leg is not routed", () => {
    expect(legForecastPlace(undefined)).toBeNull();
    expect(legForecastPlace({ route: [], error: "start is on land" } as unknown as PlanLeg)).toBeNull();
  });
});

describe("dayCoverage", () => {
  const keys = ["2026-11-14", "2026-11-15", "2026-11-16"];
  it("places a day against the forecast window", () => {
    expect(dayCoverage(keys, "2026-11-15")).toBe("inside");
    expect(dayCoverage(keys, "2026-11-17")).toBe("beyond");
    expect(dayCoverage(keys, "2026-11-13")).toBe("before");
  });
  it("knows nothing without a forecast", () => {
    expect(dayCoverage([], "2026-11-15")).toBe("unknown");
  });
});

describe("the editor's draft", () => {
  it("previews each arrival as the one before plus its nights", () => {
    expect(previewArrivals("2026-11-14", [2, 1, 0])).toEqual(["2026-11-14", "2026-11-16", "2026-11-17"]);
  });
  it("sends no vias or departure time for the last stop", () => {
    const via = { name: "V", lat: -26, lon: 153 };
    const input = draftToInput(" Run ", "2026-11-14", [
      { name: "A", lat: 1, lon: 2, nights: 1, vias: [via], departTime: "06:30", arriveBy: "" },
      { name: "B ", lat: 3, lon: 4, nights: 0, vias: [via], departTime: "07:00", arriveBy: "2026-11-20T17:00" },
    ]);
    expect(input.name).toBe("Run");
    expect(input.stops[0]).toMatchObject({ arrive: "2026-11-14", vias: [via], departTime: "06:30" });
    // An arrive-by belongs to any stop, the last included.
    expect(input.stops[1]).toMatchObject({ vias: [], departTime: "", arriveBy: "2026-11-20T17:00" });
    // The server dates every stop after the first.
    expect(input.stops[1]).toMatchObject({ name: "B", vias: [], departTime: "" });
    expect(input.stops[1].arrive).toBeUndefined();
  });
});

describe("leg wording", () => {
  const base = { from: 0, to: 1, distanceNm: null, courseDeg: null, route: [], note: "", reason: "", errorKind: "", errorStop: "", errorTo: "", moved: [] };

  it("says a stop on land needs moving, without coordinates", () => {
    const text = legErrorText({
      ...base,
      error: "no passage distance for Great Sandy Straits Marina to Pelican Bay: the start (-25.2930, 152.9070) is on land and the water in front of it could not be found",
      errorKind: "on_land",
      errorStop: "Great Sandy Straits Marina",
    });
    expect(text.summary).toBe("Can't route: Great Sandy Straits Marina is on land here. Change the stop to a spot on the water.");
    expect(text.detail).toContain("-25.2930");
  });

  it("says closed-off water plainly with both stop names, no coordinates", () => {
    const text = legErrorText({
      ...base,
      error: "no passage distance for Manly Boat Harbour to The Boat Works: no water route found from -27.4550, 153.1880 to -27.8615, 153.3520 within 50 nm of the straight line: the water between them is closed off by charted shoreline",
      reason: "no water route found from -27.4550, 153.1880 to -27.8615, 153.3520 within 50 nm of the straight line: the water between them is closed off by charted shoreline",
      errorKind: "closed_off",
      errorStop: "Manly Boat Harbour",
      errorTo: "The Boat Works",
    });
    expect(text.summary).toBe(
      "Can't route: the water between Manly Boat Harbour and The Boat Works is closed off on the chart. Move a stop or add a via place.",
    );
    expect(text.detail).toContain("-27.4550");
  });

  it("shortens any other router error and keeps the full message as detail", () => {
    const text = legErrorText({
      ...base,
      error: "no passage distance for A to B: the plan's legs took longer than 1m0s to route",
      reason: "the plan's legs took longer than 1m0s to route",
    });
    expect(text.summary).toBe("Can't route this leg: the plan's legs took longer than 1m0s to route");
    expect(text.detail).toContain("no passage distance for A to B");
  });

  it("says how far inside the charted shoreline a stop is, in one decimal", () => {
    expect(legNoteText({ ...base, moved: [{ stop: "Mooloolaba Marina", nm: 0.22 }] })).toBe(
      "Mooloolaba Marina is 0.2 nm inside the charted shoreline; the distance starts at the nearest water.",
    );
    expect(legNoteText({ ...base, moved: [] })).toBe("");
  });
});
