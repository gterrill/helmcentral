import { describe, expect, it } from "vitest";

import {
  addDaysISO,
  daysBetweenISO,
  distanceNm,
  formatStayRange,
  formatWeekday,
  isFarFromPlace,
  isStayDay,
  pendingStayDays,
} from "@/lib/stay";

const range = (arrive: string, depart: string) => ({ arrive, depart });

describe("formatWeekday", () => {
  it("names the weekday of a calendar day whatever zone the device is in", () => {
    expect(formatWeekday("2026-10-24")).toBe("Sat");
    expect(formatWeekday("2026-11-01")).toBe("Sun");
  });
});

describe("stay dates", () => {
  it("shifts calendar days across month and year ends", () => {
    expect(addDaysISO("2026-10-30", 3)).toBe("2026-11-02");
    expect(addDaysISO("2026-12-31", 1)).toBe("2027-01-01");
    expect(daysBetweenISO("2026-10-24", "2026-11-02")).toBe(9);
  });

  it("writes the range the way a booking reads", () => {
    expect(formatStayRange("2026-10-27", "2026-10-31")).toBe("27–31 Oct");
    expect(formatStayRange("2026-10-30", "2026-11-02")).toBe("30 Oct – 2 Nov");
    expect(formatStayRange("2026-10-27", "2026-10-27")).toBe("27 Oct");
  });

  it("knows a stay day, arrival and departure included", () => {
    const stay = range("2026-10-27", "2026-10-31");
    expect(isStayDay("2026-10-26", stay)).toBe(false);
    expect(isStayDay("2026-10-27", stay)).toBe(true);
    expect(isStayDay("2026-10-31", stay)).toBe(true);
    expect(isStayDay("2026-11-01", stay)).toBe(false);
  });
});

describe("pendingStayDays", () => {
  const window10 = Array.from({ length: 10 }, (_, i) =>
    addDaysISO("2026-10-24", i),
  ); // 24 Oct .. 2 Nov

  it("lists nothing when the stay is inside the forecast", () => {
    expect(
      pendingStayDays(window10, range("2026-10-27", "2026-10-31")),
    ).toEqual([]);
  });

  it("lists the stay days past the last forecast day, with when each enters the window", () => {
    const pending = pendingStayDays(
      window10,
      range("2026-10-31", "2026-11-05"),
    );
    expect(pending.map((p) => p.dayKey)).toEqual([
      "2026-11-03",
      "2026-11-04",
      "2026-11-05",
    ]);
    // 3 Nov becomes the tenth day when the window starts on 25 Oct.
    expect(pending.map((p) => p.availableFrom)).toEqual([
      "2026-10-25",
      "2026-10-26",
      "2026-10-27",
    ]);
  });

  it("lists the whole stay when it starts beyond the forecast", () => {
    const pending = pendingStayDays(
      window10,
      range("2026-11-10", "2026-11-11"),
    );
    expect(pending.map((p) => p.dayKey)).toEqual(["2026-11-10", "2026-11-11"]);
  });

  it("has nothing to say with no forecast at all", () => {
    expect(pendingStayDays([], range("2026-10-27", "2026-10-31"))).toEqual([]);
  });
});

describe("tide station distance", () => {
  it("flags a station farther than twenty nautical miles", () => {
    expect(isFarFromPlace(20)).toBe(false);
    expect(isFarFromPlace(20.1)).toBe(true);
  });

  it("measures in nautical miles", () => {
    // One degree of latitude is sixty nautical miles.
    expect(distanceNm(0, 0, 1, 0)).toBeCloseTo(60, 0);
  });
});
