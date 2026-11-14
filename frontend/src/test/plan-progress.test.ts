import { describe, expect, it } from "vitest";

import {
  changeMarkText,
  changeMarks,
  currentVerdicts,
  parseNoteKind,
  parseVerdict,
  slackText,
  slipText,
  VERDICT_LABEL,
  VERDICT_ORDER,
  VERDICTS,
  type PlanLegAssessment,
  type PlanSlip,
} from "@/lib/plans";

const leg = (over: Partial<PlanLegAssessment>): PlanLegAssessment => ({
  leg: 0, date: "2026-11-14", key: "A → B|2026-11-14", progress: "", stillInPort: false, verdict: null, limiting: "", note: "", noteKind: "",
  depart: null, departSuggested: false, arrive: null, hours: null, speedKn: null, currentKn: null, currentNote: "", notChecked: [], ...over,
});

const stops = ["Mooloolaba", "Manly", "Boat Works"].map((name) => ({ name }));

const slip = (over: Partial<PlanSlip>): PlanSlip => ({
  days: 1, lateByDays: 0, leg: 1, date: "2026-11-18", verdict: "go", limiting: "", note: "", noteKind: "", ...over,
});

describe("verdict and note kind parsing", () => {
  it("reads the four verdicts and nothing else", () => {
    expect(["go", "marginal", "no_go", "no_data"].map(parseVerdict)).toEqual(["go", "marginal", "no_go", "no_data"]);
    expect(parseVerdict("")).toBeNull();
    expect(parseVerdict("great")).toBeNull();
  });

  it("reads a note kind it does not know as other, and done and skipped are no longer kinds", () => {
    expect(parseNoteKind("")).toBe("");
    expect(parseNoteKind("gap")).toBe("gap");
    expect(parseNoteKind("done")).toBe("other");
    expect(parseNoteKind("from_a_newer_server")).toBe("other");
  });

  it("ranks a no-go worst and names each level once", () => {
    expect(VERDICT_ORDER.no_go).toBeGreaterThan(VERDICT_ORDER.no_data);
    expect(VERDICT_ORDER.no_data).toBeGreaterThan(VERDICT_ORDER.marginal);
    expect(VERDICT_ORDER.marginal).toBeGreaterThan(VERDICT_ORDER.go);
    expect(VERDICTS.map((v) => VERDICT_LABEL[v])).toEqual(["Go", "Marginal", "No-Go", "No data"]);
  });
});

describe("slackText", () => {
  it("says the slack in days, singular for one", () => {
    expect(slackText({ days: 1, stop: 2 })).toEqual({ text: "Slack 1 day", late: false });
    expect(slackText({ days: 3, stop: 2 })).toEqual({ text: "Slack 3 days", late: false });
    expect(slackText({ days: 0, stop: 2 })).toEqual({ text: "Slack 0 days", late: false });
  });

  it("says Late by when the plan is already past the arrive-by", () => {
    expect(slackText({ days: -1, stop: 2 })).toEqual({ text: "Late by 1 day", late: true });
    expect(slackText({ days: -4, stop: 2 })).toEqual({ text: "Late by 4 days", late: true });
  });
});

describe("slipText", () => {
  it("words a Go slip with the leg and its day", () => {
    expect(slipText(slip({}), { stops })).toBe("Slip 1 day: Manly → Boat Works Wed, Go.");
  });

  it("words a no-go slip from its limiting factor and says how late the plan is", () => {
    const text = slipText(
      slip({ days: 2, leg: 0, date: "2026-11-19", verdict: "no_go", limiting: "wind 18 kn ahead", lateByDays: 1 }),
      { stops },
    );
    expect(text).toBe("Slip 2 days: Mooloolaba → Manly Thu meets wind 18 kn ahead, No-Go (late by 1 day).");
  });

  it("words a marginal slip and a no-data slip", () => {
    expect(slipText(slip({ verdict: "marginal", limiting: "gusts 24 kn abeam" }), { stops })).toBe(
      "Slip 1 day: Manly → Boat Works Wed meets gusts 24 kn abeam, Marginal.",
    );
    expect(slipText(slip({ verdict: "no_data" }), { stops })).toBe("Slip 1 day: Manly → Boat Works Wed, No data.");
  });

  it("says No forecast yet for a leg past the forecast, and a note for any other reason there is no verdict", () => {
    expect(slipText(slip({ verdict: null, note: "No forecast yet", noteKind: "no_forecast" }), { stops })).toBe(
      "Slip 1 day: Manly → Boat Works Wed: No forecast yet.",
    );
    expect(slipText(slip({ verdict: null, note: "Not routed", noteKind: "not_routed", lateByDays: 2, days: 3 }), { stops })).toBe(
      "Slip 3 days: Manly → Boat Works Wed: Not routed (late by 2 days).",
    );
  });
});

describe("change marks", () => {
  it("marks a leg whose verdict level differs from the one last seen", () => {
    const marks = changeMarks(
      [{ key: "A → B|2026-11-14", verdict: "go" }],
      [leg({ verdict: "marginal", limiting: "wind 15 kn ahead" })],
    );
    expect(marks.get("A → B|2026-11-14")).toEqual({ was: "go", now: "marginal" });
    expect(changeMarkText(marks.get("A → B|2026-11-14")!)).toBe("was Go, now Marginal");
  });

  it("names every level in the operator's words", () => {
    expect(changeMarkText({ was: "no_go", now: "go" })).toBe("was No-Go, now Go");
    expect(changeMarkText({ was: "go", now: "no_data" })).toBe("was Go, now No data");
  });

  it("marks nothing when the level is the same, even if the limiting factor moved", () => {
    const marks = changeMarks(
      [{ key: "A → B|2026-11-14", verdict: "no_go" }],
      [leg({ verdict: "no_go", limiting: "gusts 40 kn ahead" })],
    );
    expect(marks.size).toBe(0);
  });

  it("marks nothing for a leg with no earlier verdict or no verdict now", () => {
    expect(changeMarks([], [leg({ verdict: "go" })]).size).toBe(0);
    expect(changeMarks([{ key: "A → B|2026-11-14", verdict: "go" }], [leg({ verdict: null, note: "No forecast yet" })]).size).toBe(0);
  });

  it("lists the verdicts to save, only for legs that have one", () => {
    expect(
      currentVerdicts([
        leg({ verdict: "go" }),
        leg({ leg: 1, key: "B → C|2026-11-15", verdict: null }),
        leg({ leg: 2, key: "C → D|2026-11-16", verdict: "no_data" }),
      ]),
    ).toEqual([
      { key: "A → B|2026-11-14", verdict: "go" },
      { key: "C → D|2026-11-16", verdict: "no_data" },
    ]);
  });
});
