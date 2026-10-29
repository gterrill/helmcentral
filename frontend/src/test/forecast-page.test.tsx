import { useEffect } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

import { ForecastPage } from "@/components/forecast-page";
import { usePlannedStay } from "@/hooks/use-planned-stay";
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
      />
    );
  },
}));
vi.mock("@/components/forecast-stay-sheet", () => ({
  ForecastStaySheet: ({ open }: { open: boolean }) =>
    open ? <div data-testid="stay-sheet" /> : null,
}));
vi.mock("@/hooks/use-planned-stay");
vi.mock("@/hooks/use-weather-forecast");
vi.mock("@/hooks/use-wave-forecast");
vi.mock("@/hooks/use-upper-air");
vi.mock("@/hooks/use-forecast-warnings");

const STAY = {
  name: "Coral Coast Marina",
  lat: -27.2,
  lon: 153.1,
  arrive: "2026-10-27",
  depart: "2026-10-31",
  timezone: "Australia/Brisbane",
};

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

function stayState(stay: typeof STAY | null) {
  vi.mocked(usePlannedStay).mockReturnValue({
    stay,
    loading: false,
    error: null,
    save: vi.fn(),
    clear: vi.fn(),
    refetch: vi.fn(),
  });
}

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
  });

  it("with no stay, shows the boat's forecast, names its position and offers to plan a stay", () => {
    stayState(null);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    expect(screen.getByTestId("forecast-place-label")).toHaveTextContent(
      "Forecast for the boat's position",
    );
    expect(drawerForecast()).toEqual([BOAT_DRAY]);
    expect(drawerStay()).toBeNull();
    expect(
      screen.getByRole("button", { name: "Plan a stay" }),
    ).toBeInTheDocument();
  });

  it("opens on the boat even when a stay is saved, with the stay as a chip", () => {
    stayState(STAY);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    expect(screen.getByTestId("forecast-place-label")).toHaveTextContent(
      "Forecast for the boat's position",
    );
    expect(drawerForecast()).toEqual([BOAT_DRAY]);
    expect(
      screen.getByRole("button", { name: "Coral Coast Marina · 27–31 Oct" }),
    ).toHaveAttribute("aria-pressed", "false");
  });

  it("holds the stay's feeds back while the boat is showing", () => {
    stayState(STAY);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    for (const hook of [
      useWeatherForecast,
      useWaveForecast,
      useUpperAir,
      useForecastWarnings,
    ]) {
      const [, options] = vi.mocked(hook).mock.calls[0] as unknown as [
        number,
        { place?: unknown; enabled?: boolean },
      ];
      expect(options.enabled).toBe(false);
    }
  });

  it("switches to the stay with one tap, naming the place and that it is not the boat", () => {
    stayState(STAY);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    fireEvent.click(
      screen.getByRole("button", { name: "Coral Coast Marina · 27–31 Oct" }),
    );

    expect(screen.getByTestId("forecast-place-label")).toHaveTextContent(
      "Forecast for Coral Coast Marina — not the boat's position",
    );
    expect(drawerForecast()).toEqual([STAY_DAY]);
    expect(drawerStay()).toEqual({
      placeName: "Coral Coast Marina",
      lat: -27.2,
      lon: 153.1,
      arrive: "2026-10-27",
      depart: "2026-10-31",
      timezone: "Australia/Brisbane",
    });
    const [, options] = vi
      .mocked(useWeatherForecast)
      .mock.calls.at(-1) as unknown as [
      number,
      { place?: unknown; enabled?: boolean },
    ];
    expect(options).toEqual({
      place: { lat: -27.2, lon: 153.1 },
      enabled: true,
    });
    // The warnings handed over are the place's.
    expect(
      JSON.parse(
        screen.getByTestId("drawer").getAttribute("data-warning") || "null",
      ),
    ).toMatchObject({ region: "Gold Coast" });
  });

  it("mounts a fresh drawer for each place so the selected day does not carry over", () => {
    stayState(STAY);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    fireEvent.click(
      screen.getByRole("button", { name: "Coral Coast Marina · 27–31 Oct" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Boat" }));
    expect(drawerMounts.mock.calls.map((c) => c[0])).toEqual([
      "boat",
      "stay",
      "boat",
    ]);
  });

  it("returns to the boat with one tap", () => {
    stayState(STAY);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    fireEvent.click(
      screen.getByRole("button", { name: "Coral Coast Marina · 27–31 Oct" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Boat" }));
    expect(screen.getByTestId("forecast-place-label")).toHaveTextContent(
      "Forecast for the boat's position",
    );
    expect(drawerForecast()).toEqual([BOAT_DRAY]);
    expect(drawerStay()).toBeNull();
  });

  it("notes the place's clock beside the chip only when it differs from the boat's", () => {
    stayState({ ...STAY, lon: 174.8, timezone: "Pacific/Tarawa" });
    const { unmount } = render(
      <ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />,
    );
    expect(screen.getByTestId("forecast-stay-clock-note")).toHaveTextContent(
      "Stay times are UTC+12",
    );
    unmount();

    stayState(STAY);
    render(<ForecastPage boat={boatProps} boatTimeZone="Etc/GMT-10" canWrite />);
    expect(
      screen.queryByTestId("forecast-stay-clock-note"),
    ).not.toBeInTheDocument();
  });

  it("says so, without throwing, when this browser does not know the stay's zone", () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    stayState({ ...STAY, timezone: "Not/AZone" });
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    expect(screen.getByTestId("forecast-stay-zone-error")).toHaveTextContent(
      "Zone Not/AZone unknown to this browser",
    );
    expect(screen.queryByTestId("forecast-stay-clock-note")).not.toBeInTheDocument();
  });

  it("opens the sheet to plan or change a stay", () => {
    stayState(null);
    const { unmount } = render(
      <ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Plan a stay" }));
    expect(screen.getByTestId("stay-sheet")).toBeInTheDocument();
    unmount();

    stayState(STAY);
    render(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    fireEvent.click(screen.getByRole("button", { name: "Change stay" }));
    expect(screen.getByTestId("stay-sheet")).toBeInTheDocument();
  });

  it("offers a reader no way to plan, change or clear a stay", () => {
    stayState(STAY);
    render(
      <ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite={false} />,
    );
    expect(
      screen.queryByRole("button", { name: "Plan a stay" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Change stay" }),
    ).not.toBeInTheDocument();
    // They can still read it.
    expect(
      screen.getByRole("button", { name: "Coral Coast Marina · 27–31 Oct" }),
    ).toBeInTheDocument();
  });

  it("falls back to the boat when the stay it was showing is cleared", () => {
    stayState(STAY);
    const { rerender } = render(
      <ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />,
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Coral Coast Marina · 27–31 Oct" }),
    );
    stayState(null);
    rerender(<ForecastPage boat={boatProps} boatTimeZone="Australia/Brisbane" canWrite />);
    expect(screen.getByTestId("forecast-place-label")).toHaveTextContent(
      "Forecast for the boat's position",
    );
    expect(drawerStay()).toBeNull();
  });
});
