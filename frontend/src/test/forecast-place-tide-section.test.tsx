import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";

import { ForecastPlaceTideSection } from "@/components/forecast-tide-section";
import { useConfiguredTideChart } from "@/hooks/use-configured-tide-chart";
import { useTideSettings } from "@/hooks/use-tide-settings";

vi.mock("@/hooks/use-tide-settings");
vi.mock("@/hooks/use-configured-tide-chart");

const mockedSettings = vi.mocked(useTideSettings);
const mockedChart = vi.mocked(useConfiguredTideChart);

const PLACE = { name: "Coral Coast Marina", lat: -27.2, lon: 153.1, timezone: "Australia/Brisbane" };
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

function chartFor(day: string, station = STATION, overrides: Record<string, unknown> = {}) {
  const midday = new Date(`${day}T02:00:00Z`).getTime(); // noon at UTC+10
  return {
    chart: {
      station,
      extremes: [
        { time: new Date(midday - 6 * 3600_000).toISOString(), heightM: 0.4, high: false },
        { time: new Date(midday + 1 * 3600_000).toISOString(), heightM: 1.9, high: true },
        { time: new Date(midday + 7 * 3600_000).toISOString(), heightM: 0.5, high: false },
      ],
      currentHeightM: 1,
      direction: "Rising",
    },
    loading: false,
    error: null,
    noStation: false,
    notSetUp: false,
    isCached: false,
    updatedAt: null,
    ttlSeconds: null,
    providerName: "BOM",
    positionBased: false,
    refetch: vi.fn(),
    ...overrides,
  };
}

const noChart = { ...chartFor("2026-10-27"), chart: null };

function renderPlace(place = PLACE) {
  return render(<ForecastPlaceTideSection isImperial={false} place={place} dayKey="2026-10-27" />);
}

describe("ForecastPlaceTideSection", () => {
  beforeEach(() => {
    mockedSettings.mockReturnValue(settings());
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("asks the server about the place, not the boat, with no station lookup first, and names the station and its distance", () => {
    mockedChart.mockReturnValue(chartFor("2026-10-27"));
    renderPlace();

    expect(mockedChart).toHaveBeenCalledWith("bom", true, { lat: -27.2, lon: 153.1 });
    expect(screen.getByText("Brisbane Bar")).toBeInTheDocument();
    expect(screen.getByTestId("forecast-place-tide-distance")).toHaveTextContent(/NM from Coral Coast Marina/);
    expect(screen.queryByTestId("forecast-place-tide-far")).not.toBeInTheDocument();
  });

  it("shows an error in place of the chart when this browser does not know the place's zone", () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    mockedChart.mockReturnValue(chartFor("2026-10-27"));
    renderPlace({ ...PLACE, timezone: "Not/AZone" });

    expect(screen.getByTestId("forecast-place-tide-zone-error")).toHaveTextContent("Zone Not/AZone unknown to this browser");
  });

  it("flags a station more than twenty nautical miles away", () => {
    mockedChart.mockReturnValue(chartFor("2026-10-27", { ...STATION, lat: -28.2 }));
    renderPlace();

    expect(screen.getByTestId("forecast-place-tide-far")).toHaveTextContent(/NM away/);
  });

  it("shows the station with no distance and no far warning when its position is unknown", () => {
    mockedChart.mockReturnValue(chartFor("2026-10-27", { ...STATION, lat: 0, lon: 0 }));
    renderPlace();

    expect(screen.getByText("Brisbane Bar")).toBeInTheDocument();
    expect(screen.queryByTestId("forecast-place-tide-distance")).not.toBeInTheDocument();
    expect(screen.queryByTestId("forecast-place-tide-far")).not.toBeInTheDocument();
  });

  it("never offers to change or save the boat's own station", () => {
    const saveStation = vi.fn();
    mockedSettings.mockReturnValue(settings({ saveStation }));
    mockedChart.mockReturnValue(chartFor("2026-10-27"));
    renderPlace();

    expect(screen.queryByRole("button", { name: /change station/i })).not.toBeInTheDocument();
    expect(saveStation).not.toHaveBeenCalled();
  });

  it("says no tide was found near the place, with the reason and a Retry", () => {
    mockedChart.mockReturnValue({ ...noChart, error: "no tide station found near this place" });
    renderPlace();

    expect(screen.getByText(/No tide found near Coral Coast Marina \(no tide station found near this place\)/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("says the tide provider is not set up, not that no station is near", () => {
    mockedChart.mockReturnValue({ ...noChart, notSetUp: true, error: "no tide provider is chosen" });
    renderPlace();

    expect(screen.getByTestId("forecast-place-tide-not-set-up")).toHaveTextContent("not set up");
    expect(screen.queryByText(/No tide found/)).not.toBeInTheDocument();
  });

  it("uses the default tide provider when none is saved (ADR 0273)", () => {
    mockedSettings.mockReturnValue(settings({ tideProvider: "" }));
    mockedChart.mockReturnValue(noChart);
    renderPlace();

    expect(mockedChart).toHaveBeenCalledWith("open-meteo", true, { lat: PLACE.lat, lon: PLACE.lon });
  });

  it("names the place and no station for a provider that follows a position, from the chart's own datum", () => {
    mockedChart.mockReturnValue(
      chartFor("2026-10-27", { ...STATION, name: "Selected place" }, { positionBased: true, chart: { ...chartFor("2026-10-27").chart, station: { ...STATION, name: "Selected place" }, heightDatum: "mean_sea_level" } }),
    );
    renderPlace();

    expect(screen.getByTestId("forecast-place-tide-modelled")).toHaveTextContent("mean sea level");
    expect(screen.getByText("Coral Coast Marina")).toBeInTheDocument();
    expect(screen.queryByTestId("forecast-place-tide-distance")).not.toBeInTheDocument();
  });
});
