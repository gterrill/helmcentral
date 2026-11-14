import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import { PlanTable } from "@/components/plan-table";
import { usePlanAssessment } from "@/hooks/use-plan-assessment";
import { usePlanChangeMarks } from "@/hooks/use-plan-change-marks";
import { usePlanDayForecast } from "@/hooks/use-plan-day-forecast";
import { usePlanSlips } from "@/hooks/use-plan-slips";
import { useWaveForecast } from "@/hooks/use-wave-forecast";
import { useWeatherForecast } from "@/hooks/use-weather-forecast";
import type { Plan, PlanAssessment, PlanLeg, PlanLegAssessment } from "@/lib/plans";

vi.mock("@/hooks/use-plan-assessment");
vi.mock("@/hooks/use-plan-day-forecast");
vi.mock("@/hooks/use-plan-slips");
vi.mock("@/hooks/use-plan-change-marks");
vi.mock("@/hooks/use-weather-forecast");
vi.mock("@/hooks/use-wave-forecast");
vi.mock("@/components/plan-leg-map", () => ({ PlanLegMap: () => <div data-testid="leg-map" /> }));

const zone = "Australia/Brisbane";
const stop = (name: string, arrive: string, nights: number, depart: string, extra = {}) => ({
  name, lat: -27, lon: 153, arrive, nights, depart, vias: [], departTime: "", arriveBy: "", timezone: zone, ...extra,
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
  leg: 0, date: "2026-11-14", key: "k0", progress: "", stillInPort: false, verdict: null, limiting: "", note: "", noteKind: "", depart: null, departSuggested: false, arrive: null,
  hours: 5, speedKn: 6, currentKn: null, currentNote: "", notChecked: [], ...over,
});

const assessment = (over: Partial<PlanAssessment> = {}): PlanAssessment => ({
  asOf: "2026-11-13T20:30:00Z",
  forecastAsOf: "2026-11-13T19:12:00Z",
  problem: "",
  warningsError: "",
  progressError: "",
  slack: null,
  legs: [legAssessment({}), legAssessment({ leg: 1, date: "2026-11-16", key: "k1" })],
  stops: [],
  ...over,
});

function show(
  a: PlanAssessment | null,
  opts: { error?: string | null; canWrite?: boolean; onAddNight?: (i: number) => void } = {},
) {
  vi.mocked(usePlanAssessment).mockReturnValue({
    assessment: a,
    shape: "shape",
    loading: a === null && !opts.error,
    error: opts.error ?? null,
  });
  return render(
    <PlanTable
      plan={PLAN}
      legs={[leg(0), leg(1)]}
      legsLoading={false}
      legsError={null}
      canWrite={opts.canWrite ?? true}
      selectedRowKey={null}
      addingNight={false}
      onSelect={() => {}}
      onAddNight={opts.onAddNight ?? (() => {})}
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
    vi.mocked(usePlanSlips).mockReturnValue({ slips: null, loading: false, error: null });
    vi.mocked(usePlanChangeMarks).mockReturnValue({ marks: new Map(), error: null });
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

  it("shows the arrival stop's first night on a leg that arrives after midnight, keyed by the plan's arrive date", () => {
    // Leaves Alpha at 6 PM on the 10th and reaches Bravo at 2 AM on the 11th. The
    // server lists Bravo's nights from its plan arrive date (the 10th, two nights).
    const plan: Plan = {
      ...PLAN,
      stops: [
        stop("Alpha", "2026-11-10", 0, "2026-11-10", { departTime: "18:00" }),
        stop("Bravo", "2026-11-10", 2, "2026-11-12"),
        stop("Charlie", "2026-11-12", 0, "2026-11-12"),
      ],
    };
    vi.mocked(usePlanAssessment).mockReturnValue({
      shape: "shape",
      assessment: assessment({
        legs: [
          legAssessment({ date: "2026-11-10", verdict: "go", depart: "2026-11-10T18:00:00+10:00", arrive: "2026-11-11T02:00:00+10:00" }),
          legAssessment({ leg: 1, date: "2026-11-12" }),
        ],
        stops: [
          {
            stop: 1,
            nights: [
              { date: "2026-11-10", warning: "Strong Wind Warning", url: "" },
              { date: "2026-11-11", warning: "Gale Warning", url: "" },
            ],
          },
        ],
      }),
      loading: false,
      error: null,
    });
    render(
      <PlanTable plan={plan} legs={[leg(0), leg(1)]} legsLoading={false} legsError={null} canWrite selectedRowKey={null}
        addingNight={false} onSelect={() => {}} onAddNight={() => {}} />,
    );
    const all = rows();
    expect(all[0]).toHaveTextContent("Strong Wind Warning");
    expect(all[0]).not.toHaveTextContent("Gale Warning");
    const stopRows = all.filter((r) => r.getAttribute("data-kind") === "stop");
    const text = stopRows.map((r) => r.textContent ?? "").join("|");
    expect(text.match(/Gale Warning/g)).toHaveLength(1);
    expect(text).not.toContain("Strong Wind Warning");
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

describe("PlanTable progress, slack, slips and change marks", () => {
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
    vi.mocked(usePlanSlips).mockReturnValue({ slips: null, loading: false, error: null });
    vi.mocked(usePlanChangeMarks).mockReturnValue({ marks: new Map(), error: null });
  });
  afterEach(() => vi.useRealTimers());

  const slipOf = (over = {}) => ({
    days: 1, lateByDays: 0, leg: 1, date: "2026-11-16", verdict: "go" as const, limiting: "", note: "", noteKind: "" as const, ...over,
  });

  it("states the slack above the table, and nothing when no stop has an arrive-by", () => {
    show(assessment({ slack: { days: 1, stop: 2 } }));
    expect(screen.getByTestId("plan-slack")).toHaveTextContent("Slack 1 day");
    expect(screen.getByTestId("plan-slack")).not.toHaveClass("text-destructive");
    cleanup();
    show(assessment());
    expect(screen.queryByTestId("plan-slack")).toBeNull();
  });

  it("says Late by in red once the plan is past its arrive-by, and prices no slips", () => {
    vi.mocked(usePlanSlips).mockClear();
    show(assessment({ slack: { days: -2, stop: 2 } }));
    expect(screen.getByTestId("plan-slack")).toHaveTextContent("Late by 2 days");
    expect(screen.getByTestId("plan-slack")).toHaveClass("text-destructive");
    expect(usePlanSlips).not.toHaveBeenCalled();
  });

  it("lists what each slip meets, worded from the worst leg", () => {
    vi.mocked(usePlanSlips).mockReturnValue({
      slips: {
        problem: "", capped: false,
        slips: [
          slipOf(),
          slipOf({ days: 2, leg: 0, date: "2026-11-17", verdict: "no_go", limiting: "wind 18 kn ahead", lateByDays: 1 }),
        ],
      },
      loading: false,
      error: null,
    });
    show(assessment({ slack: { days: 1, stop: 2 } }));
    const items = screen.getAllByTestId("plan-slip").map((i) => i.textContent);
    expect(items).toEqual([
      "Slip 1 day: Bravo → Charlie Mon, Go.",
      "Slip 2 days: Alpha → Bravo Tue meets wind 18 kn ahead, No-Go (late by 1 day).",
    ]);
  });

  it("says the slips are being priced, and why they could not be, without holding up the rows", () => {
    vi.mocked(usePlanSlips).mockReturnValue({ slips: null, loading: true, error: null });
    show(assessment({ slack: { days: 1, stop: 2 } }));
    expect(screen.getByText("Pricing the slips…")).toBeInTheDocument();
    expect(rows()).toHaveLength(3);
    cleanup();
    vi.mocked(usePlanSlips).mockReturnValue({ slips: null, loading: false, error: "router down" });
    show(assessment({ slack: { days: 1, stop: 2 } }));
    expect(screen.getByText(/Slips could not be priced: router down/)).toBeInTheDocument();
  });

  it("says when the slips stop at the forecast's reach, and a problem that stopped them", () => {
    vi.mocked(usePlanSlips).mockReturnValue({ slips: { problem: "", capped: true, slips: [slipOf()] }, loading: false, error: null });
    show(assessment({ slack: { days: 12, stop: 2 } }));
    expect(screen.getByText(/Slips are checked as far as the forecast reaches/)).toBeInTheDocument();
    cleanup();
    vi.mocked(usePlanSlips).mockReturnValue({ slips: { problem: "Progress could not be read: disk", capped: false, slips: [] }, loading: false, error: null });
    show(assessment({ slack: { days: 1, stop: 2 } }));
    expect(screen.getByText("Progress could not be read: disk")).toBeInTheDocument();
  });

  it("shows a done leg as Done, with no verdict, suggestion or operator time", () => {
    show(
      assessment({
        legs: [
          legAssessment({ progress: "done" }),
          legAssessment({ leg: 1, date: "2026-11-16", key: "k1", verdict: "go" }),
        ],
      }),
    );
    const done = rows()[0];
    expect(done).toHaveTextContent("Done");
    expect(done).not.toHaveTextContent(/Go|Marginal|No-Go/);
    expect(done).not.toHaveTextContent("6.30 AM");
    expect(done).toHaveClass("opacity-60");
    expect(rows().find((r) => r.textContent?.includes("Charlie"))).not.toHaveClass("opacity-60");
  });

  it("shows a skipped leg, and its stop's lay-day rows, as Skipped", () => {
    show(
      assessment({
        legs: [
          legAssessment({ progress: "skipped" }),
          legAssessment({ leg: 1, date: "2026-11-16", key: "k1", progress: "done" }),
        ],
      }),
    );
    expect(rows()[0]).toHaveTextContent("Skipped");
    const bravoLay = rows().filter((r) => r.getAttribute("data-kind") === "stop" && r.textContent?.includes("Bravo"));
    expect(bravoLay.length).toBeGreaterThan(0);
    for (const r of bravoLay) expect(r).toHaveTextContent("Skipped");
  });

  it("marks a leg whose verdict changed since the last visit", () => {
    vi.mocked(usePlanChangeMarks).mockReturnValue({
      marks: new Map([["k0", { was: "go", now: "marginal" }]]),
      error: null,
    });
    show(assessment({ legs: [legAssessment({ verdict: "marginal", limiting: "wind 15 kn ahead" }), legAssessment({ leg: 1, date: "2026-11-16", key: "k1" })] }));
    expect(rows()[0]).toHaveTextContent("was Go, now Marginal");
    expect(rows()[1]).not.toHaveTextContent("was ");
  });

  it("says when the change marks could not be read", () => {
    vi.mocked(usePlanChangeMarks).mockReturnValue({ marks: new Map(), error: "boom" });
    show(assessment());
    expect(screen.getByText(/Changes since your last visit not shown: boom/)).toBeInTheDocument();
  });

  it("asks the change marks hook for this plan's legs and whether the viewer can save", () => {
    show(assessment(), { canWrite: false });
    expect(usePlanChangeMarks).toHaveBeenCalledWith(1, expect.any(Array), false);
  });

  it("prompts on a leg the boat should have left, with Slip a day adding a night at that stop", () => {
    const onAddNight = vi.fn();
    show(assessment({ legs: [legAssessment({ stillInPort: true, verdict: "go" }), legAssessment({ leg: 1, date: "2026-11-16", key: "k1" })] }), { onAddNight });
    expect(rows()[0]).toHaveTextContent("Planned to leave today — still at Alpha");
    fireEvent.click(screen.getByRole("button", { name: "Slip a day" }));
    expect(onAddNight).toHaveBeenCalledWith(0);
  });

  it("shows the prompt but no button to a viewer who cannot write", () => {
    show(assessment({ legs: [legAssessment({ stillInPort: true, verdict: "go" }), legAssessment({ leg: 1, date: "2026-11-16", key: "k1" })] }), { canWrite: false });
    expect(rows()[0]).toHaveTextContent("Planned to leave today — still at Alpha");
    expect(screen.queryByRole("button", { name: "Slip a day" })).toBeNull();
  });

  it("says the recorded passages could not be read, instead of leaving every leg to do silently", () => {
    show(assessment({ progressError: "the passage record could not be read: disk" }));
    expect(screen.getByText(/Progress not checked: the passage record could not be read: disk/)).toBeInTheDocument();
  });
});
