import { useEffect } from "react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";

import { ForecastPage } from "@/components/forecast-page";
import { usePlanAssessment } from "@/hooks/use-plan-assessment";
import { usePlanLegs, usePlans } from "@/hooks/use-plans";
import { usePlanDayForecast } from "@/hooks/use-plan-day-forecast";
import { useWeatherForecast } from "@/hooks/use-weather-forecast";
import { useWaveForecast } from "@/hooks/use-wave-forecast";
import { useUpperAir } from "@/hooks/use-upper-air";
import { useForecastWarnings } from "@/hooks/use-forecast-warnings";

// The drawer is a stub that reports what it was given, so these tests are
// about which place's data the page hands it and what the page says around it.
const drawerMounts = vi.fn();
vi.mock("@/components/forecast-drawer", () => ({
  ForecastDrawer: (props: Record<string, unknown>) => {
    useEffect(() => {
      drawerMounts(props.stay ? "stay" : "boat");
      // Counts mounts only: a change of place must show up as a new mount.
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);
    return (
      <div
        data-testid="drawer"
        data-stay={props.stay ? JSON.stringify(props.stay) : ""}
        data-forecast={JSON.stringify(props.forecast)}
        data-wave-days={JSON.stringify(props.waveDays)}
        data-warning={JSON.stringify(props.activeForecastWarning)}
        data-lightning={JSON.stringify(props.lightning ?? null)}
      />
    );
  },
}));
vi.mock("@/components/plan-editor", () => ({
  PlanEditor: ({ open, plan }: { open: boolean; plan: { name: string } | null }) =>
    open ? <div data-testid="plan-editor" data-plan={plan?.name ?? ""} /> : null,
}));
vi.mock("@/components/plan-leg-map", () => ({
  PlanLegMap: () => <div data-testid="leg-map" />,
}));
vi.mock("@/hooks/use-plans");
vi.mock("@/hooks/use-plan-assessment");
vi.mock("@/hooks/use-plan-day-forecast");
vi.mock("@/hooks/use-weather-forecast");
vi.mock("@/hooks/use-wave-forecast");
vi.mock("@/hooks/use-upper-air");
vi.mock("@/hooks/use-forecast-warnings");

const stop = (name: string, arrive: string, nights: number, extra = {}) => {
  const d = new Date(Date.UTC(+arrive.slice(0, 4), +arrive.slice(5, 7) - 1, +arrive.slice(8) + nights));
  return {
    name,
    lat: -27.2,
    lon: 153.1,
    arrive,
    nights,
    depart: d.toISOString().slice(0, 10),
    vias: [],
    departTime: "",
    timezone: "Australia/Brisbane",
    ...extra,
  };
};

const PLAN = {
  id: 1,
  name: "Home run",
  status: "active" as const,
  firstDate: "2026-11-14",
  lastDate: "2026-11-17",
  stops: [
    stop("Great Sandy Straits Marina", "2026-11-14", 2),
    stop("Pelican Bay", "2026-11-16", 1, { vias: [{ name: "Inskip Point", lat: -25.8, lon: 153 }], departTime: "06:30" }),
    stop("Mooloolaba", "2026-11-17", 0),
  ],
};
const PAST = { ...PLAN, id: 2, name: "Old run", status: "past" as const, firstDate: "2026-09-01", lastDate: "2026-09-03" };

const BOAT_DRAY = { dayKey: "boat-day" };
const STAY_DAY = { dayKey: "stay-day" };

const weatherResult = (forecast: unknown[]) => ({
  forecast,
  hourlyToday: [],
  nextHour: null,
  summary: null,
  provider: "p",
  loading: false,
  error: null,
  isCached: false,
  updatedAt: null,
  ttlSeconds: null,
  refetch: vi.fn(),
});

const patchStop = vi.fn();
function plansState(plans: unknown[], currentId: number | null, legs: unknown[] = []) {
  vi.mocked(usePlans).mockReturnValue({
    plans,
    currentId,
    loading: false,
    error: null,
    create: vi.fn(),
    update: vi.fn(),
    patchStop,
    remove: vi.fn(),
    refetch: vi.fn(),
  } as never);
  vi.mocked(usePlanLegs).mockReturnValue({ legs, loading: false, error: null } as never);
  vi.mocked(usePlanAssessment).mockReturnValue({ assessment: null, loading: false, updating: false, error: null });
}

const LEGS = [
  { from: 0, to: 1, distanceNm: 17.5, courseDeg: 90, route: [[-27, 153], [-26.9, 153.2]], note: "", error: "", reason: "", errorKind: "", errorStop: "", errorTo: "", moved: [] },
  { from: 1, to: 2, distanceNm: null, courseDeg: null, route: [], note: "", error: "no passage distance for Pelican Bay to Mooloolaba: start is on land", reason: "start is on land", errorKind: "", errorStop: "", errorTo: "", moved: [] },
];

const ROUTED = [
  LEGS[0],
  { from: 1, to: 2, distanceNm: 9, courseDeg: 10, route: [[-27, 153], [-26.9, 153.2]], note: "", error: "", reason: "", errorKind: "", errorStop: "", errorTo: "", moved: [] },
];

const boatProps = {
  forecast: [BOAT_DRAY] as never,
  unit: "metric" as const,
  activeForecastWarning: null,
  waveDays: [],
};

function mockFeeds() {
  vi.mocked(useWeatherForecast).mockImplementation(
    (_s, options) => weatherResult(options?.place ? [STAY_DAY] : []) as never,
  );
  vi.mocked(usePlanDayForecast).mockImplementation((_place, dayKey) => ({
    loading: false,
    error: null,
    coverage: dayKey > "2026-11-16" ? "beyond" : "inside",
    wind: dayKey > "2026-11-16" ? null : { minKts: 12, maxKts: 18, gustKts: 26, fromDeg: 90 },
    windDirection: "E",
    waveMinM: 0.8,
    waveMaxM: 1.4,
    wavesLoading: false,
    wavesError: null,
  }));
  vi.mocked(useWaveForecast).mockImplementation(
    () =>
      ({
        days: [],
        seaTemperatureF: null,
        provider: null,
        loading: false,
        error: null,
        isCached: false,
        updatedAt: null,
        ttlSeconds: null,
        refetch: vi.fn(),
      }) as never,
  );
  vi.mocked(useUpperAir).mockImplementation(
    () =>
      ({
        days: [],
        series: [],
        windowBand: { present: false, lowM: 0, highM: 0, lowQuintileM: 0 },
        provider: null,
        loading: false,
        error: null,
        updatedAt: null,
        ttlSeconds: null,
        refetch: vi.fn(),
      }) as never,
  );
  vi.mocked(useForecastWarnings).mockImplementation(
    (_s, options) =>
      ({
        warnings: null,
        activeWarning: options?.place
          ? { provider: "bom", region: "Gold Coast", bulletins: [] }
          : null,
        loading: false,
        error: null,
        isCached: false,
        updatedAt: null,
        ttlSeconds: null,
        refetch: vi.fn(),
      }) as never,
  );
}

const drawerStay = () =>
  JSON.parse(screen.getByTestId("drawer").getAttribute("data-stay") || "null");
const drawerForecast = () =>
  JSON.parse(
    screen.getByTestId("drawer").getAttribute("data-forecast") || "[]",
  );

describe("ForecastPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockFeeds();
    // The forecast horizon is counted from today.
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date(2026, 10, 13, 9, 0, 0));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  const readerPlaces = () =>
    vi
      .mocked(useWeatherForecast)
      .mock.calls.map((c) => c[1])
      .filter((o) => o?.place && o.enabled)
      .map((o) => `${o?.place?.lat},${o?.place?.lon}`);

  it("holds back the feed of a place whose days are all past the forecast", () => {
    vi.setSystemTime(new Date(2026, 9, 1, 9, 0, 0));
    plansState([PLAN], 1, LEGS);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    expect(readerPlaces()).toEqual([]);
    // The rows still say so.
    expect(screen.getAllByTestId("plan-row")[0]).toBeInTheDocument();
  });

  it("reads a place with any day inside the forecast, once", () => {
    plansState([PLAN], 1, LEGS);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    expect(new Set(readerPlaces()).size).toBeGreaterThan(0);
  });

  it("opens a passage row at the place its wind is read, mid-leg, and says so", () => {
    plansState([PLAN], 1, LEGS);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    fireEvent.click(screen.getByRole("button", { name: "Great Sandy Straits Marina → Pelican Bay" }));
    expect(screen.getByTestId("forecast-place-label")).toHaveTextContent(
      "Forecast mid-way from Great Sandy Straits Marina to Pelican Bay — not the boat's position",
    );
    expect(drawerStay()).toMatchObject({ lat: -26.9, lon: 153.2, arrive: "2026-11-16", depart: "2026-11-16" });
    const optionsSeen = vi.mocked(useWeatherForecast).mock.calls.map((c) => c[1]);
    expect(optionsSeen).toContainEqual({ place: { lat: -26.9, lon: 153.2 }, enabled: true });
  });

  it("does not open a leg that did not route", () => {
    plansState([PLAN], 1, LEGS);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    const button = screen.getByRole("button", { name: "Pelican Bay → Mooloolaba" });
    expect(button).toBeDisabled();
    fireEvent.click(button);
    expect(drawerStay()).toBeNull();
    expect(screen.getByTestId("forecast-place-label")).toHaveTextContent("Forecast for the boat's position");
  });

  it("calls the last stop's departure day 'leave', not a lay day, and keeps +1 night on it", () => {
    const plan = {
      ...PLAN,
      lastDate: "2026-11-16",
      stops: [stop("Alpha", "2026-11-14", 0), stop("Bravo", "2026-11-14", 2)],
    };
    plansState([plan], 1, [LEGS[0]]);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    const rows = screen.getAllByTestId("plan-row");
    expect(rows.map((r) => r.querySelector("button")?.textContent)).toEqual([
      "Alpha → Bravo",
      "Bravo · lay day",
      "Bravo · leave",
    ]);
    expect(rows[2]).toHaveTextContent("+1 night");
  });

  it("calls a single stop's final day 'leave' too", () => {
    const plan = { ...PLAN, lastDate: "2026-11-15", stops: [stop("Alpha", "2026-11-14", 1)] };
    plansState([plan], 1, []);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    expect(screen.getAllByTestId("plan-row").map((r) => r.querySelector("button")?.textContent)).toEqual([
      "Alpha · lay day",
      "Alpha · leave",
    ]);
  });

  it("sends one night while the first is still in flight, and disables +1 night until it lands", async () => {
    plansState([PLAN], 1, LEGS);
    let resolve!: (p: unknown) => void;
    patchStop.mockReturnValue(new Promise((r) => (resolve = r)));
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    const button = screen.getAllByRole("button", { name: "+1 night" })[0];
    fireEvent.click(button);
    fireEvent.click(button);
    expect(patchStop).toHaveBeenCalledTimes(1);
    expect(screen.getAllByRole("button", { name: "+1 night" })[0]).toBeDisabled();
    await act(async () => {
      resolve(PLAN);
    });
    expect(screen.getAllByRole("button", { name: "+1 night" })[0]).toBeEnabled();
  });

  it("with no plan, shows the boat's forecast and offers a new plan", () => {
    plansState([], null);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    expect(screen.getByTestId("forecast-place-label")).toHaveTextContent("Forecast for the boat's position");
    expect(drawerForecast()).toEqual([BOAT_DRAY]);
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "New plan" }));
    expect(screen.getByTestId("plan-editor")).toHaveAttribute("data-plan", "");
  });

  it("opens on the current plan with one row per day and the boat's forecast below", () => {
    plansState([PLAN, PAST], 1, LEGS);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    expect(screen.getByRole("button", { name: /Home run · 14–17 Nov/ })).toHaveAttribute("aria-pressed", "true");
    const rows = screen.getAllByTestId("plan-row");
    expect(rows.map((r) => r.getAttribute("data-kind"))).toEqual(["stop", "stop", "passage", "passage"]);
    expect(drawerForecast()).toEqual([BOAT_DRAY]);
  });

  it("shows a passage row's distance, vias, departure time and wind against the course", () => {
    plansState([PLAN], 1, LEGS);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    const passage = screen.getAllByTestId("plan-row")[2];
    expect(passage).toHaveTextContent("Great Sandy Straits Marina → Pelican Bay");
    expect(passage).toHaveTextContent("17.5 nm");
    expect(passage).toHaveTextContent("12–18 kn ahead");
    expect(passage).toHaveTextContent("26 kn");
    expect(passage).toHaveTextContent("0.8–1.4 m");
    const second = screen.getAllByTestId("plan-row")[3];
    expect(second).toHaveTextContent("via");
  });
  it("shows a stop row's overnight wind with no shelter label", () => {
    plansState([PLAN], 1, LEGS);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    const stopRow = screen.getAllByTestId("plan-row")[0];
    expect(stopRow).toHaveTextContent("12–18 kn E");
    expect(stopRow).not.toHaveTextContent(/shelter/i);
  });

  it("fails a leg that could not be routed in words, with no distance", () => {
    plansState([PLAN], 1, LEGS);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    const leg2 = screen.getAllByTestId("plan-row")[3];
    expect(leg2).toHaveTextContent("start is on land");
    expect(leg2).toHaveTextContent("Not routed");
  });

  it("says No forecast yet beyond the horizon and shows no figures", () => {
    plansState([PLAN], 1, ROUTED);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    const last = screen.getAllByTestId("plan-row")[3];
    expect(last).toHaveTextContent("No forecast yet");
    expect(last).not.toHaveTextContent(/ kn/);
  });

  it("reads no forecast for a leg that did not route, and says why", () => {
    plansState([PLAN], 1, LEGS);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    const leg2 = screen.getAllByTestId("plan-row")[3];
    expect(leg2).toHaveTextContent("start is on land");
    expect(leg2).toHaveTextContent("No forecast: the leg is not routed");
    expect(leg2).not.toHaveTextContent(/ kn/);
    expect(vi.mocked(usePlanDayForecast)).toHaveBeenCalledWith(null, "2026-11-17", "day");
  });

  it("keeps a row's drawer on the plan's current dates after a night is added", () => {
    plansState([PLAN], 1, LEGS);
    const { rerender } = render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    fireEvent.click(screen.getAllByRole("button", { name: "Great Sandy Straits Marina · lay day" })[0]);
    expect(drawerStay()).toMatchObject({ arrive: "2026-11-14", depart: "2026-11-16" });
    const longer = {
      ...PLAN,
      stops: [stop("Great Sandy Straits Marina", "2026-11-14", 3), stop("Pelican Bay", "2026-11-17", 1), stop("Mooloolaba", "2026-11-18", 0)],
    };
    plansState([longer], 1, LEGS);
    rerender(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    expect(drawerStay()).toMatchObject({ arrive: "2026-11-14", depart: "2026-11-17" });
  });

  it("drops back to the boat when the chosen row is no longer in the plan", () => {
    plansState([PLAN], 1, LEGS);
    const { rerender } = render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    fireEvent.click(screen.getAllByRole("button", { name: "Great Sandy Straits Marina · lay day" })[1]);
    expect(drawerStay()).not.toBeNull();
    const shorter = {
      ...PLAN,
      stops: [stop("Great Sandy Straits Marina", "2026-11-14", 1), stop("Pelican Bay", "2026-11-15", 1), stop("Mooloolaba", "2026-11-16", 0)],
    };
    plansState([shorter], 1, LEGS);
    rerender(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    expect(drawerStay()).toBeNull();
    expect(screen.getByTestId("forecast-place-label")).toHaveTextContent("Forecast for the boat's position");
  });

  it("keeps past plans in a Past section and lets the operator open one", () => {
    plansState([PLAN, PAST], 1, LEGS);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    expect(screen.getByText("Past (1)")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Old run/ }));
    expect(screen.getByRole("table", { name: "Old run day by day" })).toBeInTheDocument();
  });

  it("adds a night to a stop through the server's shifting rule", () => {
    plansState([PLAN], 1, LEGS);
    patchStop.mockResolvedValue(PLAN);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    fireEvent.click(screen.getAllByRole("button", { name: "+1 night" })[0]);
    expect(patchStop).toHaveBeenCalledWith(1, 0, { nights: 3 });
  });

  it("opens a row's place in the drawer with the row's own days, and returns to the boat", () => {
    plansState([PLAN], 1, LEGS);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    fireEvent.click(screen.getAllByRole("button", { name: "Great Sandy Straits Marina · lay day" })[0]);
    expect(screen.getByTestId("forecast-place-label")).toHaveTextContent("Forecast for Great Sandy Straits Marina — not the boat's position");
    expect(drawerForecast()).toEqual([STAY_DAY]);
    expect(drawerStay()).toEqual({
      placeName: "Great Sandy Straits Marina",
      lat: -27.2,
      lon: 153.1,
      arrive: "2026-11-14",
      depart: "2026-11-16",
      timezone: "Australia/Brisbane",
    });
    // The table reads its own places too, so look for the page's call among them.
    const optionsSeen = vi.mocked(useWeatherForecast).mock.calls.map((c) => c[1]);
    expect(optionsSeen).toContainEqual({ place: { lat: -27.2, lon: 153.1 }, enabled: true });
    expect(JSON.parse(screen.getByTestId("drawer").getAttribute("data-warning") || "null")).toMatchObject({ region: "Gold Coast" });

    fireEvent.click(screen.getByRole("button", { name: "Boat" }));
    expect(drawerStay()).toBeNull();
    expect(drawerMounts.mock.calls.map((c) => c[0])).toEqual(["boat", "stay", "boat"]);
  });

  it("hands a place view the boat's lightning feed rather than fetching one for the place", () => {
    plansState([PLAN], 1, LEGS);
    const lightning = { windows: [], provider: "ukmo-lightning" };
    render(<ForecastPage boat={{ ...boatProps, lightning: lightning as never }} boatTimeZone="Australia/Brisbane" canWrite />);
    fireEvent.click(screen.getAllByRole("button", { name: /lay day/ })[0]);
    expect(JSON.parse(screen.getByTestId("drawer").getAttribute("data-lightning") || "null")).toEqual(lightning);
  });

  it("holds the place feeds back while the boat is showing", () => {
    plansState([PLAN], 1, LEGS);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    for (const hook of [useWeatherForecast, useWaveForecast, useUpperAir, useForecastWarnings]) {
      const [, options] = vi.mocked(hook).mock.calls[0] as unknown as [number, { enabled?: boolean }];
      expect(options.enabled).toBe(false);
    }
  });

  it("notes the place's clock only when it differs from the boat's", () => {
    plansState([{ ...PLAN, stops: PLAN.stops.map((s) => ({ ...s, timezone: "Pacific/Tarawa" })) }], 1, LEGS);
    const { unmount } = render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    fireEvent.click(screen.getAllByRole("button", { name: /lay day/ })[0]);
    expect(screen.getByTestId("forecast-stay-clock-note")).toHaveTextContent("Times here are UTC+12");
    unmount();

    plansState([PLAN], 1, LEGS);
    render(<ForecastPage boat={boatProps} boatTimeZone="Etc/GMT-10" canWrite />);
    fireEvent.click(screen.getAllByRole("button", { name: /lay day/ })[0]);
    expect(screen.queryByTestId("forecast-stay-clock-note")).not.toBeInTheDocument();
  });

  it("says so, without throwing, when this browser does not know the stop's zone", () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    plansState([{ ...PLAN, stops: PLAN.stops.map((s) => ({ ...s, timezone: "Not/AZone" })) }], 1, LEGS);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    fireEvent.click(screen.getAllByRole("button", { name: /lay day/ })[0]);
    expect(screen.getByTestId("forecast-stay-zone-error")).toHaveTextContent("Zone Not/AZone unknown to this browser");
  });

  it("opens the editor on the plan to change it", () => {
    plansState([PLAN], 1, LEGS);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    fireEvent.click(screen.getByRole("button", { name: "Change plan" }));
    expect(screen.getByTestId("plan-editor")).toHaveAttribute("data-plan", "Home run");
  });

  it("offers a reader no way to add, change, delete or lengthen anything", () => {
    plansState([PLAN], 1, LEGS);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite={false} />);
    for (const name of ["New plan", "Change plan", "Delete plan", "+1 night"]) {
      expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
    }
    expect(screen.getByRole("table")).toBeInTheDocument();
  });

  it("falls back to the boat when the plan it was showing is deleted", () => {
    plansState([PLAN], 1, LEGS);
    const { rerender } = render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    fireEvent.click(screen.getAllByRole("button", { name: /lay day/ })[0]);
    plansState([], null);
    rerender(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    expect(screen.getByTestId("forecast-place-label")).toHaveTextContent("Forecast for the boat's position");
    expect(drawerStay()).toBeNull();
  });
});
