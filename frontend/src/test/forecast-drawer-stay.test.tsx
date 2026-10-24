import { describe, it, expect, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";

// The tide sections fetch for themselves and are covered on their own; here
// they are stubs that show which one the drawer chose and what it was given.
vi.mock("@/components/forecast-tide-section", () => ({
  ForecastTideSection: () => <div data-testid="mock-boat-tide" />,
  ForecastPlaceTideSection: ({
    place,
    dayKey,
  }: {
    place: { name: string };
    dayKey: string;
  }) => (
    <div
      data-testid="mock-place-tide"
      data-place={place.name}
      data-day-key={dayKey}
    />
  ),
}));

import { ForecastDrawer } from "@/components/forecast-drawer";
import type { ForecastWarnings } from "@/hooks/use-forecast-warnings";
import { addDaysISO } from "@/lib/stay";

const FIRST_DAY = "2026-06-14";
const STAY = {
  placeName: "Coral Coast Marina",
  lat: -27.2,
  lon: 153.1,
  arrive: "2026-06-22",
  depart: "2026-06-27",
};
const DAY_NAMES = [
  "Sunday",
  "Monday",
  "Tuesday",
  "Wednesday",
  "Thursday",
  "Friday",
  "Saturday",
];

function buildDay(idx: number) {
  const dayKey = addDaysISO(FIRST_DAY, idx);
  return {
    dayKey,
    date: `Jun ${14 + idx}`,
    dayName: DAY_NAMES[idx % 7],
    condition: "Clear",
    high: 76,
    low: 62,
    windSpeed: 10,
    windGust: 14,
    windDirection: "NE",
    windSummary: null,
    precipitationSummary: null,
    precipitation: 5,
    humidityPct: 58,
    visibilityNm: 9.5,
    sunriseTime: "6:32AM",
    sunsetTime: "5:47PM",
    moonPhase: null,
    hourlyWind: [],
    hourlyPrecip: [],
    hourlyUV: [],
    hourlyCloud: [],
  };
}

const tenDays = () => Array.from({ length: 10 }, (_, i) => buildDay(i));

function upperAirSeries(dayKeys: string[]) {
  return dayKeys.flatMap((dayKey, dayIdx) =>
    [0, 6, 12, 18].map((hour, block) => ({
      time: `${dayKey}T${String(hour).padStart(2, "0")}:00:00Z`,
      dayKey,
      localHour: hour,
      height500M: 5900 - (dayIdx * 4 + block) * 2,
      thicknessM: 5645,
      wind500Kts: 30,
      temperature500C: -8,
    })),
  );
}

function upperAirDay(dayKey: string, idx: number) {
  return {
    dayKey,
    date: `Jun ${14 + idx}`,
    dayName: DAY_NAMES[idx % 7],
    outlook: {
      present: true,
      height500M: 5835,
      thicknessM: 5645,
      peakWind500Kts: 30,
      heightPercentile: 0.5,
      tendency24hM: 0,
      troughSupport: false,
    },
  };
}

const windWarning: ForecastWarnings = {
  provider: "bom",
  region: "QLD Gold Coast",
  bulletins: [
    {
      id: "W1",
      title: "Strong Wind Warning",
      issuedAt: null,
      detailsUrl: "",
      category: "wind",
      sections: [{ day: "Tuesday", warningType: "Strong Wind Warning" }],
    },
  ],
};

describe("ForecastDrawer stay view", () => {
  it('says the forecast is for the place, not "your position", while loading', () => {
    render(
      <ForecastDrawer
        forecast={[]}
        loading
        error={null}
        unit="metric"
        stay={STAY}
      />,
    );
    expect(
      screen.getByText(/guidance for Coral Coast Marina/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/your position/)).not.toBeInTheDocument();
  });

  it("keeps the boat's own loading copy when there is no stay", () => {
    render(<ForecastDrawer forecast={[]} loading error={null} unit="metric" />);
    expect(screen.getByText(/guidance for your position/)).toBeInTheDocument();
  });

  it("marks the stay days in the day list and no others", () => {
    render(
      <ForecastDrawer
        forecast={tenDays()}
        loading={false}
        error={null}
        unit="metric"
        stay={STAY}
      />,
    );
    const marked = screen.getAllByTestId("forecast-stay-day-marker");
    // 22, 23 June are the last two of the ten days (14..23).
    expect(marked).toHaveLength(2);
    const tab22 = screen.getByRole("button", {
      name: /Select forecast day .* Jun 22,/,
    });
    expect(tab22).toHaveAttribute("data-stay", "true");
    expect(tab22.getAttribute("aria-label")).toMatch(/planned stay/);
    expect(
      screen.getByRole("button", { name: /Select forecast day .* Jun 21,/ }),
    ).not.toHaveAttribute("data-stay");
  });

  it('lists stay days past the forecast as "No forecast yet" with the date each arrives', () => {
    render(
      <ForecastDrawer
        forecast={tenDays()}
        loading={false}
        error={null}
        unit="metric"
        stay={STAY}
      />,
    );
    const pending = screen.getAllByTestId("forecast-stay-pending-day");
    // 24..27 June are beyond the window that ends 23 June.
    expect(pending).toHaveLength(4);
    // 24 June is the tenth day once the window starts on 15 June.
    expect(
      within(pending[0]).getByText(/No forecast yet · from 15 Jun/),
    ).toBeInTheDocument();
    expect(
      within(pending[3]).getByText(/No forecast yet · from 18 Jun/),
    ).toBeInTheDocument();
    // They are not days that can be opened.
    expect(within(pending[0]).queryByRole("button")).not.toBeInTheDocument();
  });

  it("lists no pending rows when the stay is inside the forecast", () => {
    render(
      <ForecastDrawer
        forecast={tenDays()}
        loading={false}
        error={null}
        unit="metric"
        stay={{ ...STAY, arrive: "2026-06-16", depart: "2026-06-18" }}
      />,
    );
    expect(
      screen.queryByTestId("forecast-stay-pending-day"),
    ).not.toBeInTheDocument();
    expect(screen.getAllByTestId("forecast-stay-day-marker")).toHaveLength(3);
  });

  it("shows no stay marks without a stay", () => {
    render(
      <ForecastDrawer
        forecast={tenDays()}
        loading={false}
        error={null}
        unit="metric"
      />,
    );
    expect(
      screen.queryByTestId("forecast-stay-day-marker"),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByTestId("forecast-stay-pending-day"),
    ).not.toBeInTheDocument();
  });

  it("shades the stay window on the upper-air trace and calls it a trend", () => {
    const dayKeys = Array.from({ length: 16 }, (_, i) =>
      addDaysISO(FIRST_DAY, i),
    );
    render(
      <ForecastDrawer
        forecast={tenDays()}
        upperAirDays={dayKeys.map(upperAirDay)}
        upperAirSeries={upperAirSeries(dayKeys)}
        upperAirWindow={{
          present: true,
          lowM: 5800,
          highM: 5900,
          lowQuintileM: 5820,
        }}
        loading={false}
        error={null}
        unit="metric"
        stay={STAY}
      />,
    );
    expect(
      screen.getByTestId("forecast-upper-air-stay-band"),
    ).toBeInTheDocument();
    expect(screen.getByTestId("forecast-upper-air-stay-key")).toHaveTextContent(
      /upper-air trend/i,
    );
    expect(screen.getByTestId("forecast-upper-air-stay-key")).toHaveTextContent(
      /not a forecast for each day/i,
    );
  });

  it("draws no stay band when the stay is beyond the trace, and says so", () => {
    const dayKeys = Array.from({ length: 4 }, (_, i) =>
      addDaysISO(FIRST_DAY, i),
    );
    render(
      <ForecastDrawer
        forecast={tenDays()}
        upperAirDays={dayKeys.map(upperAirDay)}
        upperAirSeries={upperAirSeries(dayKeys)}
        loading={false}
        error={null}
        unit="metric"
        stay={STAY}
      />,
    );
    expect(
      screen.queryByTestId("forecast-upper-air-stay-band"),
    ).not.toBeInTheDocument();
    expect(screen.getByTestId("forecast-upper-air-stay-key")).toHaveTextContent(
      /beyond/i,
    );
  });

  it("reads the tide at the place for the selected day, not the boat's station", () => {
    render(
      <ForecastDrawer
        forecast={tenDays()}
        loading={false}
        error={null}
        unit="metric"
        stay={STAY}
      />,
    );
    const tide = screen.getByTestId("mock-place-tide");
    expect(tide).toHaveAttribute("data-place", "Coral Coast Marina");
    expect(tide).toHaveAttribute("data-day-key", FIRST_DAY);
    expect(screen.queryByTestId("mock-boat-tide")).not.toBeInTheDocument();
  });

  it("keeps the boat's tide section when there is no stay", () => {
    render(
      <ForecastDrawer
        forecast={tenDays()}
        loading={false}
        error={null}
        unit="metric"
      />,
    );
    expect(screen.getByTestId("mock-boat-tide")).toBeInTheDocument();
    expect(screen.queryByTestId("mock-place-tide")).not.toBeInTheDocument();
  });

  it("words a warning as the place's", () => {
    render(
      <ForecastDrawer
        forecast={tenDays()}
        hourlyToday={[
          {
            label: "Now",
            condition: "Clear",
            temperatureF: 70,
            windSpeedKts: 10,
            windGustKts: 14,
            windDirection: "NE",
            windDirectionDeg: 45,
            kind: "forecast",
            isDaylight: true,
          },
        ]}
        activeForecastWarning={windWarning}
        loading={false}
        error={null}
        unit="metric"
        stay={STAY}
      />,
    );
    expect(screen.getByTestId("forecast-wind-warning")).toHaveTextContent(
      "Wind warning in effect for Coral Coast Marina (QLD Gold Coast).",
    );
  });

  it("says there is no wave forecast for the spot when the model has no values there", () => {
    render(
      <ForecastDrawer
        forecast={tenDays()}
        waveDays={[]}
        loading={false}
        error={null}
        unit="metric"
        stay={STAY}
      />,
    );
    expect(screen.getByTestId("forecast-wave-none")).toHaveTextContent(
      "No wave forecast for this spot",
    );
  });

  it('keeps a wave outage an outage, not "no forecast for this spot"', () => {
    render(
      <ForecastDrawer
        forecast={tenDays()}
        waveDays={[]}
        waveError="wave provider unavailable"
        loading={false}
        error={null}
        unit="metric"
        stay={STAY}
      />,
    );
    expect(screen.queryByTestId("forecast-wave-none")).not.toBeInTheDocument();
    expect(screen.getByTestId("forecast-wave-error")).toBeInTheDocument();
  });

  it("leaves the boat's wave message alone without a stay", () => {
    render(
      <ForecastDrawer
        forecast={tenDays()}
        waveDays={[]}
        loading={false}
        error={null}
        unit="metric"
      />,
    );
    expect(screen.queryByTestId("forecast-wave-none")).not.toBeInTheDocument();
    expect(screen.getByTestId("forecast-wave-unavailable")).toBeInTheDocument();
  });
});
