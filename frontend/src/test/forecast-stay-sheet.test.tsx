import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

import { ForecastStaySheet } from "@/components/forecast-stay-sheet";
import { searchPlaces } from "@/hooks/use-place-search";
import { ApiError } from "@/lib/api-query";
import { todayISO } from "@/lib/local-date";

vi.mock("@/hooks/use-place-search");
const mockedSearch = vi.mocked(searchPlaces);

const CANDIDATES = [
  {
    name: "Coral Coast Marina",
    kind: "marina",
    lat: -27.2,
    lon: 153.1,
    distanceNm: 12.34,
    bearingDeg: 45,
  },
  {
    name: "Coral Coast Yacht Club",
    kind: "club",
    lat: -27.3,
    lon: 153.2,
    distanceNm: 20.1,
    bearingDeg: 120,
  },
];

function setup(
  props: Partial<React.ComponentProps<typeof ForecastStaySheet>> = {},
) {
  const onSave = vi.fn().mockResolvedValue(undefined);
  const onClear = vi.fn().mockResolvedValue(undefined);
  const onOpenChange = vi.fn();
  render(
    <ForecastStaySheet
      open
      onOpenChange={onOpenChange}
      stay={null}
      onSave={onSave}
      onClear={onClear}
      {...props}
    />,
  );
  return { onSave, onClear, onOpenChange };
}

async function findAndPick(name = "Coral Coast Marina") {
  fireEvent.change(screen.getByLabelText("Place"), {
    target: { value: "Coral Coast" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Search" }));
  fireEvent.click(
    await screen.findByRole("button", { name: new RegExp(name) }),
  );
}

describe("ForecastStaySheet", () => {
  beforeEach(() => {
    mockedSearch.mockResolvedValue({ candidates: CANDIDATES });
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("offers each candidate with its distance and bearing from the boat", async () => {
    setup();
    fireEvent.change(screen.getByLabelText("Place"), {
      target: { value: "Coral Coast" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Search" }));
    const first = await screen.findByRole("button", {
      name: /Coral Coast Marina/,
    });
    expect(first).toHaveTextContent("12.3 NM");
    expect(first).toHaveTextContent("045°");
    expect(mockedSearch).toHaveBeenCalledWith("Coral Coast");
  });

  it("shows a candidate with no distance or bearing without inventing one", async () => {
    mockedSearch.mockResolvedValue({
      candidates: [
        {
          name: "Bona Bay",
          kind: "bay",
          lat: -20.1,
          lon: 149.1,
          distanceNm: null,
          bearingDeg: null,
        },
      ],
    });
    setup();
    fireEvent.change(screen.getByLabelText("Place"), {
      target: { value: "Bona" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Search" }));
    const row = await screen.findByRole("button", { name: /Bona Bay/ });
    expect(row).not.toHaveTextContent("NM");
    expect(row).not.toHaveTextContent("000°");
    expect(row).toHaveTextContent("bay");
  });

  it("keeps the operator's edits when the saved stay is refetched as an equal new object", async () => {
    const stay = {
      name: "Coral Coast Marina",
      lat: -27.2,
      lon: 153.1,
      arrive: "2026-10-27",
      depart: "2026-10-31",
      timezone: "Australia/Brisbane",
    };
    const props = {
      open: true,
      onOpenChange: vi.fn(),
      onSave: vi.fn(),
      onClear: vi.fn(),
    };
    const { rerender } = render(<ForecastStaySheet {...props} stay={stay} />);
    fireEvent.change(screen.getByLabelText("Depart"), {
      target: { value: "2026-11-03" },
    });
    rerender(<ForecastStaySheet {...props} stay={{ ...stay }} />);
    expect(screen.getByLabelText("Depart")).toHaveValue("2026-11-03");
  });

  it("starts again from the saved stay each time it opens", () => {
    const stay = {
      name: "Coral Coast Marina",
      lat: -27.2,
      lon: 153.1,
      arrive: "2026-10-27",
      depart: "2026-10-31",
      timezone: "Australia/Brisbane",
    };
    const props = {
      onOpenChange: vi.fn(),
      onSave: vi.fn(),
      onClear: vi.fn(),
      stay,
    };
    const { rerender } = render(<ForecastStaySheet {...props} open />);
    fireEvent.change(screen.getByLabelText("Depart"), {
      target: { value: "2026-11-03" },
    });
    rerender(<ForecastStaySheet {...props} open={false} />);
    rerender(<ForecastStaySheet {...props} open />);
    expect(screen.getByLabelText("Depart")).toHaveValue("2026-10-31");
  });

  it("defaults arrival to today and cannot save before a place is chosen", () => {
    setup();
    expect(screen.getByLabelText("Arrive")).toHaveValue(todayISO());
    expect(screen.getByRole("button", { name: "Save stay" })).toBeDisabled();
  });

  it("saves the chosen place with the dates", async () => {
    const { onSave } = setup();
    await findAndPick();
    fireEvent.change(screen.getByLabelText("Arrive"), {
      target: { value: "2026-10-27" },
    });
    fireEvent.change(screen.getByLabelText("Depart"), {
      target: { value: "2026-10-31" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save stay" }));
    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({
        name: "Coral Coast Marina",
        lat: -27.2,
        lon: 153.1,
        arrive: "2026-10-27",
        depart: "2026-10-31",
      }),
    );
  });

  it("lets the operator rename the place before saving", async () => {
    const { onSave } = setup();
    await findAndPick();
    fireEvent.change(screen.getByLabelText("Name on the chip"), {
      target: { value: "Berth C14" },
    });
    fireEvent.change(screen.getByLabelText("Depart"), {
      target: { value: "2999-01-01" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save stay" }));
    await waitFor(() => expect(onSave).toHaveBeenCalled());
    expect(onSave.mock.calls[0][0].name).toBe("Berth C14");
  });

  it("moves departure with arrival so it is never before it", async () => {
    setup();
    await findAndPick();
    fireEvent.change(screen.getByLabelText("Arrive"), {
      target: { value: "2999-03-05" },
    });
    expect(screen.getByLabelText("Depart")).toHaveValue("2999-03-05");
  });

  it("refuses a departure before arrival without asking the server", async () => {
    const { onSave } = setup();
    await findAndPick();
    fireEvent.change(screen.getByLabelText("Arrive"), {
      target: { value: "2999-03-05" },
    });
    fireEvent.change(screen.getByLabelText("Depart"), {
      target: { value: "2999-03-01" },
    });
    expect(screen.getByRole("button", { name: "Save stay" })).toBeDisabled();
    expect(onSave).not.toHaveBeenCalled();
  });

  it("shows the server's refusal in its own words", async () => {
    const { onSave } = setup();
    onSave.mockRejectedValueOnce(
      new ApiError("depart cannot be before arrive", 400, {
        depart: "depart cannot be before arrive",
      }),
    );
    await findAndPick();
    fireEvent.change(screen.getByLabelText("Depart"), {
      target: { value: "2999-01-01" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save stay" }));
    expect(
      await screen.findByText("depart cannot be before arrive"),
    ).toBeInTheDocument();
  });

  it("says so when the search finds nothing, never repeating the note written for Mate", async () => {
    mockedSearch.mockResolvedValue({ candidates: [] });
    setup();
    fireEvent.change(screen.getByLabelText("Place"), {
      target: { value: "zzz" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Search" }));
    expect(
      await screen.findByText('No places found matching "zzz".'),
    ).toBeInTheDocument();
  });

  it("shows a failed search as a failure", async () => {
    mockedSearch.mockRejectedValue(
      new Error("place-names provider is not configured"),
    );
    setup();
    fireEvent.change(screen.getByLabelText("Place"), {
      target: { value: "x" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Search" }));
    expect(
      await screen.findByText(/place-names provider is not configured/),
    ).toBeInTheDocument();
  });

  it("opens on the saved stay and clears it", async () => {
    const stay = {
      name: "Coral Coast Marina",
      lat: -27.2,
      lon: 153.1,
      arrive: "2026-10-27",
      depart: "2026-10-31",
      timezone: "Australia/Brisbane",
    };
    const { onClear } = setup({ stay });
    expect(screen.getByLabelText("Arrive")).toHaveValue("2026-10-27");
    expect(screen.getByLabelText("Depart")).toHaveValue("2026-10-31");
    expect(screen.getByLabelText("Name on the chip")).toHaveValue(
      "Coral Coast Marina",
    );
    fireEvent.click(screen.getByRole("button", { name: "Clear stay" }));
    await waitFor(() => expect(onClear).toHaveBeenCalled());
  });

  it("offers no clear when nothing is planned", () => {
    setup();
    expect(
      screen.queryByRole("button", { name: "Clear stay" }),
    ).not.toBeInTheDocument();
  });
});
