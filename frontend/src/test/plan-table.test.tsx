import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";

import { PlanTable } from "@/components/plan-table";
import { usePlanAssessment } from "@/hooks/use-plan-assessment";
import { usePlanDayForecast } from "@/hooks/use-plan-day-forecast";
import { useWaveForecast } from "@/hooks/use-wave-forecast";
import { useWeatherForecast } from "@/hooks/use-weather-forecast";
import type { Plan, PlanAssessment, PlanLeg, PlanLegAssessment } from "@/lib/plans";

vi.mock("@/hooks/use-plan-assessment");
vi.mock("@/hooks/use-plan-day-forecast");
vi.mock("@/hooks/use-weather-forecast");
vi.mock("@/hooks/use-wave-forecast");
vi.mock("@/components/plan-leg-map", () => ({ PlanLegMap: () => <div data-testid="leg-map" /> }));

const zone = "Australia/Brisbane";
const stop = (name: string, arrive: string, nights: number, depart: string, extra = {}) => ({
  name, lat: -27, lon: 153, arrive, nights, depart, vias: [], departTime: "", timezone: zone, ...extra,
});

// A on the 14th, two nights at B (the 14th and 15th), C on the 16th.
const PLAN: Plan = {
  id: 1,
  name: "Home run",
  status: "active",
  firstDate: "2026-11-14",
  lastDate: "2026-11-16",
  stops: [
    stop("Alpha", "2026-11-14", 0, "2026-11-14", { departTime: "06:30" }),
    stop("Bravo", "2026-11-14", 2, "2026-11-16"),
    stop("Charlie", "2026-11-16", 0, "2026-11-16"),
  ],
};

const leg = (from: number): PlanLeg => ({
  from, to: from + 1, distanceNm: 30, courseDeg: 190, route: [[-27, 153], [-27.5, 153]], note: "", error: "", reason: "", errorKind: "", errorStop: "", errorTo: "", moved: [],
});

const legAssessment = (over: Partial<PlanLegAssessment>): PlanLegAssessment => ({
  leg: 0, date: "2026-11-14", verdict: null, limiting: "", note: "", noteKind: "", depart: null, departSuggested: false, arrive: null,
  hours: 5, speedKn: 6, currentKn: null, currentNote: "", notChecked: [], ...over,
});

const assessment = (over: Partial<PlanAssessment> = {}): PlanAssessment => ({
  asOf: "2026-11-13T20:30:00Z",
  forecastAsOf: "2026-11-13T19:12:00Z",
  problem: "",
  warningsError: "",
  legs: [legAssessment({}), legAssessment({ leg: 1, date: "2026-11-16" })],
  stops: [],
  ...over,
});

function show(a: PlanAssessment | null, opts: { error?: string | null } = {}) {
  vi.mocked(usePlanAssessment).mockReturnValue({
    assessment: a,
    loading: a === null && !opts.error,
    error: opts.error ?? null,
  });
  return render(
    <PlanTable
      plan={PLAN}
      legs={[leg(0), leg(1)]}
      legsLoading={false}
      legsError={null}
      canWrite
      selectedRowKey={null}
      addingNight={false}
      onSelect={() => {}}
      onAddNight={() => {}}
    />,
  );
}

const rows = () => screen.getAllByTestId("plan-row");

describe("PlanTable verdicts", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-11-13T20:00:00Z"));
    vi.mocked(usePlanDayForecast).mockReturnValue({
      loading: false, error: null, coverage: "inside",
      wind: { minKts: 12, maxKts: 18, gustKts: 26, fromDeg: 190 }, windDirection: "S",
      waveMinM: 0.8, waveMaxM: 1.4, wavesLoading: false, wavesError: null,
    } as never);
    vi.mocked(useWeatherForecast).mockReturnValue({ loading: false } as never);
    vi.mocked(useWaveForecast).mockReturnValue({ loading: false } as never);
  });
  afterEach(() => vi.useRealTimers());

  it("heads the table with Current and Verdict, and no Lightning column", () => {
    show(assessment());
    const heads = screen.getAllByRole("columnheader").map((h) => h.textContent);
    expect(heads).toContain("Current");
    expect(heads[heads.length - 1]).toBe("Verdict");
    expect(heads).not.toContain("Lightning");
  });

  it("names the limiting factor on a no-go and marks the departure the operator did not set", () => {
    show(
      assessment({
        legs: [
          legAssessment({
            verdict: "no_go", limiting: "gusts 32 kn ahead", depart: "2026-11-14T06:40:00+10:00", departSuggested: true,
            arrive: "2026-11-14T11:40:00+10:00", currentKn: -0.8,
          }),
          legAssessment({ leg: 1, date: "2026-11-16" }),
        ],
      }),
    );
    const passage = rows()[0];
    expect(passage).toHaveTextContent("No-Go: gusts 32 kn ahead");
    expect(passage).toHaveTextContent("Suggested 6.40 AM → 11.40 AM");
    expect(passage).toHaveTextContent("0.8 kn hindering");
  });

  it("shows the operator's own time as plain, not as a suggestion", () => {
    show(
      assessment({
        legs: [
          legAssessment({ verdict: "go", depart: "2026-11-14T06:30:00+10:00", arrive: "2026-11-14T11:30:00+10:00", currentKn: 1.2 }),
          legAssessment({ leg: 1, date: "2026-11-16" }),
        ],
      }),
    );
    expect(rows()[0]).toHaveTextContent("6.30 AM → 11.30 AM");
    expect(rows()[0]).not.toHaveTextContent("Suggested");
    expect(rows()[0]).toHaveTextContent("Go");
    expect(rows()[0]).toHaveTextContent("1.2 kn helping");
  });

  it("says No daylight window with no time and no verdict", () => {
    show(assessment({ legs: [legAssessment({ note: "No daylight window" }), legAssessment({ leg: 1, date: "2026-11-16" })] }));
    expect(rows()[0]).toHaveTextContent("No daylight window");
    expect(rows()[0]).not.toHaveTextContent("Go");
    expect(rows()[0]).not.toHaveTextContent("→ 11");
  });

  it("leaves the current a dash with its reason as the tooltip, never a zero", () => {
    show(assessment({ legs: [legAssessment({ verdict: "go", currentNote: "the model has no current near shore" }), legAssessment({ leg: 1, date: "2026-11-16" })] }));
    const cell = screen.getAllByRole("cell").find((c) => c.querySelector('[title="the model has no current near shore"]'));
    expect(cell).toBeDefined();
    expect(cell).toHaveTextContent("—");
    expect(rows()[0]).not.toHaveTextContent("0.0 kn");
  });

  it("says when the verdicts were made and when the forecast was fetched", () => {
    show(assessment());
    expect(screen.getByTestId("plan-assessment-asof")).toHaveTextContent("Verdicts as of 6.30 AM");
    expect(screen.getByTestId("plan-assessment-asof")).toHaveTextContent("forecast fetched 5.12 AM");
  });

  it("says the problem that stopped the verdicts, once", () => {
    show(assessment({ problem: "Passage limits aren't set: Settings → Mate → Passage limits" }));
    expect(screen.getAllByRole("alert").filter((a) => a.textContent?.includes("Passage limits aren't set"))).toHaveLength(1);
  });

  it("reads a leg's note by its kind, not by its words", () => {
    // The kind says the note is the plan's one problem, whatever the note says.
    show(
      assessment({
        problem: "No ETA: no recorded passages yet",
        legs: [
          legAssessment({ note: "Something else entirely", noteKind: "problem" }),
          legAssessment({ leg: 1, date: "2026-11-16", note: "No ETA: no recorded passages yet", noteKind: "other" }),
        ],
      }),
    );
    expect(rows()[0]).toHaveTextContent("Not checked");
    expect(rows()[0]).not.toHaveTextContent("Something else entirely");
    // The same words under another kind are the leg's own note.
    expect(rows().find((r) => r.textContent?.includes("Charlie"))).toHaveTextContent("No ETA: no recorded passages yet");
  });

  it("leaves the verdict a dash for a day past the forecast, which the wind columns already say", () => {
    vi.mocked(usePlanDayForecast).mockReturnValue({ loading: false, error: null, coverage: "beyond", wind: null, waveMinM: null, waveMaxM: null } as never);
    show(assessment({ legs: [legAssessment({ note: "No forecast yet", noteKind: "no_forecast" }), legAssessment({ leg: 1, date: "2026-11-16" })] }));
    expect((rows()[0].textContent ?? "").match(/No forecast yet/g)).toHaveLength(1);
  });

  it("gives the reason under No data for a hole in the forecast", () => {
    show(
      assessment({
        legs: [
          legAssessment({ verdict: "no_data", note: "Forecast missing for part of the leg", noteKind: "gap" }),
          legAssessment({ leg: 1, date: "2026-11-16" }),
        ],
      }),
    );
    expect(rows()[0]).toHaveTextContent("No data");
    expect(rows()[0]).toHaveTextContent("Forecast missing for part of the leg");
  });

  it("shows … in the verdict-dependent cells while the check runs, with nothing dimmed or stale", () => {
    show(null);
    const first = rows()[0];
    expect(first).toHaveTextContent("…");
    expect(first).not.toHaveClass("opacity-60");
    expect(screen.queryByTestId("plan-assessment-asof")).toBeNull();
    expect(document.body).not.toHaveTextContent("Updating");
    // The Current and Verdict cells of a leg are waiting, not empty.
    const cells = Array.from(first.querySelectorAll('[role="cell"]'));
    const current = cells.find((c) => c.textContent?.startsWith("Current"));
    const verdict = cells.find((c) => c.textContent?.startsWith("Verdict"));
    expect(current).toHaveTextContent("…");
    expect(verdict).toHaveTextContent("…");
  });

  it("looks a leg's arrival-night warning up under the day it arrives, not the day it leaves", () => {
    // Leaves Alpha at 8 PM on the 14th, reaches Bravo at 1 AM on the 15th: the
    // server keys Bravo's nights by its arrival date.
    show(
      assessment({
        legs: [
          legAssessment({
            verdict: "go", depart: "2026-11-14T20:00:00+10:00", arrive: "2026-11-15T01:00:00+10:00",
          }),
          legAssessment({ leg: 1, date: "2026-11-16" }),
        ],
        stops: [{ stop: 1, nights: [{ date: "2026-11-15", warning: "Gale Warning", url: "https://example.test/b" }] }],
      }),
    );
    expect(rows()[0]).toHaveTextContent("Bravo tonight: Gale Warning");
  });

  it("does not put an arrival-night warning for the departure day on a leg that arrives after midnight", () => {
    show(
      assessment({
        legs: [
          legAssessment({
            verdict: "go", depart: "2026-11-14T20:00:00+10:00", arrive: "2026-11-15T01:00:00+10:00",
          }),
          legAssessment({ leg: 1, date: "2026-11-16" }),
        ],
        stops: [{ stop: 1, nights: [{ date: "2026-11-14", warning: "Strong Wind Warning", url: "" }] }],
      }),
    );
    expect(rows()[0]).not.toHaveTextContent("Strong Wind Warning");
  });

  it("names what could not be checked in the words the Mate card uses", () => {
    show(assessment({ legs: [legAssessment({ verdict: "go", notChecked: ["gust", "wave", "wave_direction", "daylight"] }), legAssessment({ leg: 1, date: "2026-11-16" })] }));
    expect(rows()[0]).toHaveTextContent("Not checked: gusts, wave height, wave direction, daylight arrival");
  });

  it("says when the check itself failed, and still shows the table", () => {
    show(null, { error: "502 bad gateway" });
    expect(screen.getByText(/could not be checked against the forecast/i)).toBeInTheDocument();
    expect(rows()).toHaveLength(3);
  });

  it("puts a night's warning on the stop row and the arrival night's under the passage verdict", () => {
    show(
      assessment({
        legs: [legAssessment({ verdict: "go" }), legAssessment({ leg: 1, date: "2026-11-16" })],
        stops: [
          {
            stop: 1,
            nights: [
              { date: "2026-11-14", warning: "Strong Wind Warning", url: "https://example.test/a" },
              { date: "2026-11-15", warning: "Gale Warning", url: "https://example.test/b" },
            ],
          },
        ],
      }),
    );
    const stopRow = rows().find((r) => r.getAttribute("data-kind") === "stop")!;
    expect(stopRow).toHaveTextContent("Gale Warning");
    expect(stopRow.querySelector("a")).toHaveAttribute("href", "https://example.test/b");
    expect(rows()[0]).toHaveTextContent("Strong Wind Warning");
    expect(rows()[0]).toHaveTextContent("Bravo");
  });

  it("leaves a stop row's verdict column empty with no warning", () => {
    show(assessment());
    const stopRow = rows().find((r) => r.getAttribute("data-kind") === "stop")!;
    expect(stopRow).not.toHaveTextContent(/warning/i);
    expect(stopRow).not.toHaveTextContent(/Go|No-Go|Marginal/);
  });

  it("says warnings could not be read, instead of reading as none", () => {
    show(assessment({ warningsError: 'forecast warnings provider "bom" unavailable' }));
    expect(screen.getByText(/Warnings not checked/)).toBeInTheDocument();
  });
});
