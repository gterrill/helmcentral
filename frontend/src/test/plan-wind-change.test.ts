import { describe, expect, it } from "vitest";

import { planWindChangeLine, planWindChangesTitle } from "@/lib/plan-wind-change";
import type { PlanWindChange } from "@/lib/plans";

// The server works out each row's changes, their sectors against the leg's
// course and which are prominent (ADR 0287). The page only words them.

function change(over: Partial<PlanWindChange> = {}): PlanWindChange {
  return {
    label: "7 AM", from: "NNW", to: "SE", speedMinKn: 8, speedMaxKn: 18,
    lowerConfidence: false, prominent: false, fromSector: "astern", sector: "ahead", ...over,
  };
}

describe("planWindChangeLine", () => {
  it("words the change and the relation of each side to the course", () => {
    expect(planWindChangeLine(change())).toEqual({ text: "NNW→SE 7 AM", relation: "astern → ahead" });
  });

  it("says a wind that stays on one side stays there", () => {
    expect(planWindChangeLine(change({ fromSector: "ahead", sector: "ahead" })).relation).toBe("stays ahead");
  });

  it("has no relation for a night at a stop, which has no course", () => {
    expect(planWindChangeLine(change({ fromSector: null, sector: null })).relation).toBeNull();
  });
});

describe("planWindChangesTitle", () => {
  it("lists every change with the wind after it, and says which are less certain", () => {
    const title = planWindChangesTitle([
      change(),
      change({ label: "9 PM", from: "SE", to: "S", speedMinKn: 10, speedMaxKn: 10, fromSector: "ahead", sector: "abeam", lowerConfidence: true }),
    ]);
    expect(title.split("\n")).toEqual([
      "NNW→SE 7 AM, astern → ahead, 8–18 kn after",
      "SE→S 9 PM, ahead → abeam, 10 kn after (less certain this far out)",
    ]);
  });

  it("leaves the relation out for a night", () => {
    expect(planWindChangesTitle([change({ fromSector: null, sector: null })])).toBe("NNW→SE 7 AM, 8–18 kn after");
  });
});
