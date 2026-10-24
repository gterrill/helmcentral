import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";

import { ForecastPlaceTideSection } from "@/components/forecast-tide-section";
import { useTideChart } from "@/hooks/use-tide-chart";
import { useTideProviders } from "@/hooks/use-tide-providers";
import { useTideSettings } from "@/hooks/use-tide-settings";
import { usePlaceTideStation } from "@/hooks/use-place-tide-station";

vi.mock("@/hooks/use-tide-settings");
vi.mock("@/hooks/use-tide-chart");
vi.mock("@/hooks/use-tide-providers");
vi.mock("@/hooks/use-place-tide-station");

const mockedSettings = vi.mocked(useTideSettings);
const mockedChart = vi.mocked(useTideChart);
const mockedProviders = vi.mocked(useTideProviders);
const mockedNearest = vi.mocked(usePlaceTideStation);

const PLACE = { name: "Coral Coast Marina", lat: -27.2, lon: 153.1 };
const STATION = {
  stationId: "BRI",
  name: "Brisbane Bar",
  state: "QLD",
  lat: -27.37,
  lon: 153.17,
  timezone: "Australia/Brisbane",
};

function settings(overrides: Record<string, unknown> = {}) {
  return {
    tideProvider: "bom",
    tideStationId: "HOME",
    tideStationName: "Home Station",
    tideAutoStation: true,
    loading: false,
    saving: false,
    error: null,
    refetch: vi.fn(),
    saveStation: vi.fn(),
    ...overrides,
  };
}

function chartFor(day: string) {
  const midday = new Date(`${day}T02:00:00Z`).getTime(); // noon at UTC+10
  return {
    chart: {
      station: STATION,
      extremes: [
        {
          time: new Date(midday - 6 * 3600_000).toISOString(),
          heightM: 0.4,
          high: false,
        },
        {
          time: new Date(midday + 1 * 3600_000).toISOString(),
          heightM: 1.9,
          high: true,
        },
        {
          time: new Date(midday + 7 * 3600_000).toISOString(),
          heightM: 0.5,
          high: false,
        },
      ],
      currentHeightM: 1,
      direction: "Rising",
    },
    loading: false,
    error: null,
    isCached: false,
    updatedAt: null,
    ttlSeconds: null,
    refetch: vi.fn(),
  };
}

describe("ForecastPlaceTideSection", () => {
  beforeEach(() => {
    mockedProviders.mockReturnValue({
      providers: [],
      loading: false,
      error: null,
    });
    mockedSettings.mockReturnValue(settings());
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("names the station and how far it is from the place, and asks the server about the place not the boat", () => {
    mockedNearest.mockReturnValue({
      nearest: { station: STATION, distanceNm: 9.4 },
      loading: false,
      error: null,
    });
    mockedChart.mockReturnValue(chartFor("2026-10-27"));

    render(
      <ForecastPlaceTideSection
        isImperial={false}
        place={PLACE}
        dayKey="2026-10-27"
      />,
    );

    expect(mockedNearest).toHaveBeenCalledWith("bom", {
      lat: -27.2,
      lon: 153.1,
    });
    expect(mockedChart).toHaveBeenCalledWith("bom", "BRI");
    expect(screen.getByText("Brisbane Bar")).toBeInTheDocument();
    expect(
      screen.getByTestId("forecast-place-tide-distance"),
    ).toHaveTextContent("9.4 NM from Coral Coast Marina");
    expect(
      screen.queryByTestId("forecast-place-tide-far"),
    ).not.toBeInTheDocument();
  });

  it("flags a station more than twenty nautical miles away", () => {
    mockedNearest.mockReturnValue({
      nearest: { station: STATION, distanceNm: 41.2 },
      loading: false,
      error: null,
    });
    mockedChart.mockReturnValue(chartFor("2026-10-27"));

    render(
      <ForecastPlaceTideSection
        isImperial={false}
        place={PLACE}
        dayKey="2026-10-27"
      />,
    );

    expect(screen.getByTestId("forecast-place-tide-far")).toHaveTextContent(
      "41.2 NM",
    );
  });

  it("shows the station with no distance and no far warning when its position is unknown", () => {
    mockedNearest.mockReturnValue({
      nearest: { station: STATION, distanceNm: null },
      loading: false,
      error: null,
    });
    mockedChart.mockReturnValue(chartFor("2026-10-27"));

    render(
      <ForecastPlaceTideSection
        isImperial={false}
        place={PLACE}
        dayKey="2026-10-27"
      />,
    );

    expect(screen.getByText("Brisbane Bar")).toBeInTheDocument();
    expect(
      screen.queryByTestId("forecast-place-tide-distance"),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByTestId("forecast-place-tide-far"),
    ).not.toBeInTheDocument();
  });

  it("never offers to change or save the boat's own station", () => {
    const saveStation = vi.fn();
    mockedSettings.mockReturnValue(settings({ saveStation }));
    mockedNearest.mockReturnValue({
      nearest: { station: STATION, distanceNm: 9.4 },
      loading: false,
      error: null,
    });
    mockedChart.mockReturnValue(chartFor("2026-10-27"));

    render(
      <ForecastPlaceTideSection
        isImperial={false}
        place={PLACE}
        dayKey="2026-10-27"
      />,
    );

    expect(
      screen.queryByRole("button", { name: /change station/i }),
    ).not.toBeInTheDocument();
    expect(saveStation).not.toHaveBeenCalled();
  });

  it("says so when no station is found near the place", () => {
    mockedNearest.mockReturnValue({
      nearest: null,
      loading: false,
      error: "no stations available",
    });
    mockedChart.mockReturnValue({ ...chartFor("2026-10-27"), chart: null });

    render(
      <ForecastPlaceTideSection
        isImperial={false}
        place={PLACE}
        dayKey="2026-10-27"
      />,
    );

    expect(
      screen.getByText(/No tide station found near Coral Coast Marina/),
    ).toBeInTheDocument();
  });

  it("says the tide provider is not set up rather than guessing one", () => {
    mockedSettings.mockReturnValue(settings({ tideProvider: "" }));
    mockedNearest.mockReturnValue({
      nearest: null,
      loading: false,
      error: null,
    });
    mockedChart.mockReturnValue({ ...chartFor("2026-10-27"), chart: null });

    render(
      <ForecastPlaceTideSection
        isImperial={false}
        place={PLACE}
        dayKey="2026-10-27"
      />,
    );

    expect(screen.getByText(/Tide provider is not set up/)).toBeInTheDocument();
  });
});
