import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import { PlanTable } from "@/components/plan-table";
import { mapAssessment, usePlanAssessment } from "@/hooks/use-plan-assessment";
import { usePlanChangeMarks } from "@/hooks/use-plan-change-marks";
import { usePlanDayForecast } from "@/hooks/use-plan-day-forecast";
import { usePlanSlips } from "@/hooks/use-plan-slips";
import { useWaveForecast } from "@/hooks/use-wave-forecast";
import { useWeatherForecast } from "@/hooks/use-weather-forecast";
import { changeMarks, currentVerdicts } from "@/lib/plans";
import type { Plan, PlanAssessment, PlanLeg, PlanLegAssessment, PlanLegComfort } from "@/lib/plans";

vi.mock("@/hooks/use-plan-assessment", async (orig) => ({
  ...(await orig<typeof import("@/hooks/use-plan-assessment")>()),
  usePlanAssessment: vi.fn(),
}));
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
const PLAN: Plan = {
  id: 1, name: "Run", status: "active", firstDate: "2026-11-14", lastDate: "2026-11-14",
  stops: [stop("Alpha", "2026-11-14", 0, "2026-11-14", { departTime: "08:00" }), stop("Bravo", "2026-11-14", 0, "2026-11-14")],
};
const leg: PlanLeg = {
  from: 0, to: 1, distanceNm: 30, courseDeg: 190, route: [[-27, 153], [-27.5, 153]], note: "", error: "", reason: "", errorKind: "", errorStop: "", errorTo: "", moved: [],
};

const comfort = (over: Partial<PlanLegComfort> = {}): PlanLegComfort => ({
  text: "Lively for the first 2 h: roll, swell 0.6 m 4 s on the port bow. Then Comfortable.",
  motion: "Snap up to 0.12 g at the helm · pounding up to 0.30 g",
  hours: [
    {
      at: "2026-11-14T08:00:00+10:00", label: "8 AM", band: "lively", beyond: false,
      line: "8 AM · Lively · roll 2.1°, pitch 0.8° · swell 0.6 m 6 s on the port bow · wind wave 0.4 m 4 s on the port bow (fetch-limited from 1.4 m 5 s: 1.2 nm fetch in 20 kn) · snap 0.12 g at the helm · pounding 0.30 g",
    },
    { at: "2026-11-14T09:00:00+10:00", label: "9 AM", band: "", beyond: false, line: "9 AM · Not judged: swell not judged in enclosed water" },
  ],
  ...over,
});

const legAssessment = (over: Partial<PlanLegAssessment> = {}): PlanLegAssessment => ({
  leg: 0, date: "2026-11-14", key: "k0", progress: "", stillInPort: false, verdict: "go", limiting: "", note: "", noteKind: "",
  depart: "2026-11-14T08:00:00+10:00", departSuggested: false, arrive: "2026-11-14T13:00:00+10:00",
  hours: 5, speedKn: 6, currentKn: null, currentNote: "", notChecked: [],
  windChanges: { changes: [], notChecked: null }, comfort: null, ...over,
});

const assessment = (l: PlanLegAssessment): PlanAssessment => ({
  asOf: "2026-11-13T20:30:00Z", forecastAsOf: "2026-11-13T19:12:00Z", problem: "", warningsError: "", progressError: "",
  slack: null, legs: [l], stops: [], nightWinds: [],
});

function show(a: PlanAssessment) {
  vi.mocked(usePlanAssessment).mockReturnValue({ assessment: a, fresh: true, shape: "s", loading: false, error: null });
  return render(
    <PlanTable plan={PLAN} legs={[leg]} legsLoading={false} legsError={null} canWrite selectedRowKey={null}
      addingNight={false} onSelect={() => {}} onAddNight={() => {}} />,
  );
}

describe("the comfort chip on a plan leg", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-11-13T20:00:00Z"));
    vi.mocked(usePlanDayForecast).mockReturnValue({
      loading: false, error: null, coverage: "inside", wind: { minKts: 12, maxKts: 18, gustKts: 26, fromDeg: 190 },
      windDirection: "S", waveMinM: 0.8, waveMaxM: 1.4, wavesLoading: false, wavesError: null,
    } as never);
    vi.mocked(useWeatherForecast).mockReturnValue({ loading: false } as never);
    vi.mocked(useWaveForecast).mockReturnValue({ loading: false } as never);
    vi.mocked(usePlanSlips).mockReturnValue({ slips: null, loading: false, error: null });
    vi.mocked(usePlanChangeMarks).mockReturnValue({ marks: new Map(), error: null });
  });
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("shows the sentence and the motion line in neutral text, never a verdict colour", () => {
    show(assessment(legAssessment({ comfort: comfort() })));
    const chip = screen.getByTestId("plan-comfort");
    expect(chip).toHaveTextContent("Lively for the first 2 h: roll, swell 0.6 m 4 s on the port bow. Then Comfortable.");
    expect(chip).toHaveTextContent("Snap up to 0.12 g at the helm");
    expect(chip.innerHTML).not.toMatch(/emerald|amber|red-/);
    expect(chip).toHaveTextContent(/comfort/i);
  });

  it("labels the cell COMFORT for the phone card and spans the wide grid", () => {
    show(assessment(legAssessment({ comfort: comfort() })));
    const cell = screen.getByTestId("plan-comfort").closest('[role="cell"]');
    expect(cell).not.toBeNull();
    expect(cell?.className).toContain("@[72rem]:col-start-3");
    expect(cell?.className).toContain("@[72rem]:col-span-8");
    expect(screen.getByText("Comfort", { selector: "span" }).className).toContain("@[72rem]:hidden");
  });

  it("opens the hour list on a tap and says why an hour was not judged", () => {
    show(assessment(legAssessment({ comfort: comfort() })));
    const button = screen.getByRole("button", { name: /Lively for the first 2 h/ });
    expect(button).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText(/swell not judged in enclosed water/)).toBeNull();
    fireEvent.click(button);
    expect(button).toHaveAttribute("aria-expanded", "true");
    const list = screen.getByTestId("plan-comfort-hours");
    expect(list).toHaveTextContent("8 AM · Lively · roll 2.1°, pitch 0.8° · swell 0.6 m 6 s on the port bow");
    expect(list).toHaveTextContent("wind wave 0.4 m 4 s on the port bow (fetch-limited from 1.4 m 5 s: 1.2 nm fetch in 20 kn)");
    expect(list).toHaveTextContent("9 AM · Not judged: swell not judged in enclosed water");
  });

  it("shows nothing for a leg with no comfort", () => {
    show(assessment(legAssessment({ comfort: null })));
    expect(screen.queryByTestId("plan-comfort")).toBeNull();
  });

  it("changes nothing in the verdict marks", () => {
    const a = legAssessment({ comfort: comfort() });
    const b = legAssessment({ comfort: null });
    expect(currentVerdicts([a])).toEqual(currentVerdicts([b]));
    expect(changeMarks([{ key: "k0", verdict: "marginal" }], [a])).toEqual(changeMarks([{ key: "k0", verdict: "marginal" }], [b]));
  });
});

describe("mapAssessment comfort", () => {
  const raw = (comfortWire: unknown) => ({
    as_of: "x", forecast_as_of: "", problem: "", warnings_error: "", night_winds: [], stops: [],
    legs: [{
      leg: 0, date: "2026-11-14", verdict: "go", limiting: "", note: "", note_kind: "", depart: "", depart_suggested: false, arrive: "",
      hours: 5, speed_kn: 6, current_kn: null, current_note: "", not_checked: [], wind_changes: { changes: [], not_checked: "" },
      comfort: comfortWire,
    }],
  });

  it("is null when the server did not look", () => {
    expect(mapAssessment(raw(null) as never).legs[0].comfort).toBeNull();
  });

  it("maps the sentence and the hours' lines", () => {
    const got = mapAssessment(raw({
      text: "Comfortable.", motion: "",
      hours: [{ at: "2026-11-14T08:00:00+10:00", label: "8 AM", band: "comfortable", beyond: true, line: "8 AM · Comfortable · sway 0.4°" }],
    }) as never).legs[0].comfort;
    expect(got?.text).toBe("Comfortable.");
    expect(got?.hours[0]).toEqual({ at: "2026-11-14T08:00:00+10:00", label: "8 AM", band: "comfortable", beyond: true, line: "8 AM · Comfortable · sway 0.4°" });
  });

  it("refuses a band this page does not know", () => {
    expect(() => mapAssessment(raw({
      text: "x", motion: "", hours: [{ at: "a", label: "8 AM", band: "purple", beyond: false, line: "x" }],
    }) as never)).toThrow(/purple/);
  });
});
