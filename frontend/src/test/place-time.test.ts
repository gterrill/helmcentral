import { describe, expect, it } from "vitest";

import {
  placeClockDiffers,
  placeDayStart,
  placeOffsetLabel,
  placeTimeZoneName,
  placeUtcOffsetHours,
} from "@/lib/place-time";
import { vesselZone } from "@/lib/passages";

describe("place time", () => {
  it("reckons the offset from longitude the way the server does", () => {
    expect(placeUtcOffsetHours(153.1)).toBe(10);
    expect(placeUtcOffsetHours(-122)).toBe(-8);
    expect(placeUtcOffsetHours(0.4)).toBe(0);
    expect(placeUtcOffsetHours(180)).toBe(12);
    expect(placeUtcOffsetHours(999)).toBe(0);
  });

  it("rounds a half-way longitude away from zero, as the server's math.Round does", () => {
    expect(placeUtcOffsetHours(-112.5)).toBe(-8);
    expect(placeUtcOffsetHours(112.5)).toBe(8);
    expect(placeUtcOffsetHours(-7.5)).toBe(-1);
    expect(placeUtcOffsetHours(-0.4)).toBe(0);
    expect(vesselZone(-112.5)).toEqual({ offsetHours: -8, label: "UTC-8" });
  });

  it("names the zone with the POSIX inverted sign", () => {
    expect(placeTimeZoneName(150)).toBe("Etc/GMT-10");
    expect(placeTimeZoneName(-75)).toBe("Etc/GMT+5");
    expect(placeTimeZoneName(3)).toBe("UTC");
    expect(placeOffsetLabel(150)).toBe("UTC+10");
    expect(placeOffsetLabel(-75)).toBe("UTC-5");
    expect(placeOffsetLabel(3)).toBe("UTC");
  });

  it("starts a day at local midnight at the place", () => {
    expect(placeDayStart("2026-10-27", 150).toISOString()).toBe(
      "2026-10-26T14:00:00.000Z",
    );
    expect(placeDayStart("2026-10-27", -75).toISOString()).toBe(
      "2026-10-27T05:00:00.000Z",
    );
  });

  it("says when the place keeps a different clock from the boat", () => {
    expect(placeClockDiffers(153, 153.5)).toBe(false);
    expect(placeClockDiffers(174, 153)).toBe(true);
    expect(placeClockDiffers(174, null)).toBe(false);
  });
});
