import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";

import { PlanEditor } from "@/components/plan-editor";
import type { Plan } from "@/lib/plans";

vi.mock("@/components/place-search-field", () => ({
  PlaceSearchField: () => null,
}));

const via = { name: "Inskip Point", lat: -25.8, lon: 153 };
const stop = (name: string, arrive: string, nights: number, extra = {}) => ({
  name,
  lat: -27.2,
  lon: 153.1,
  arrive,
  nights,
  depart: arrive,
  vias: [],
  departTime: "",
  arriveBy: "",
  timezone: "Australia/Brisbane",
  ...extra,
});
const PLAN: Plan = {
  id: 1,
  name: "Home run",
  status: "upcoming",
  firstDate: "2026-11-14",
  lastDate: "2026-11-16",
  stops: [
    stop("A", "2026-11-14", 1, { vias: [via], departTime: "06:30" }),
    stop("B", "2026-11-15", 1, { vias: [via], departTime: "07:00" }),
    stop("C", "2026-11-16", 0, { arriveBy: "2026-11-17T17:00" }),
  ],
};

function open(onSave = vi.fn().mockResolvedValue(undefined)) {
  render(<PlanEditor open onOpenChange={() => {}} plan={PLAN} onSave={onSave} />);
  return onSave;
}

describe("PlanEditor", () => {
  it("keeps a stop's row mounted while its name is typed over", () => {
    open();
    const before = screen.getByLabelText("Stop 1 name").closest("li");
    fireEvent.change(screen.getByLabelText("Stop 1 name"), { target: { value: "Ab" } });
    expect(screen.getByLabelText("Stop 1 name").closest("li")).toBe(before);
  });

  it("clears the vias and departure time of a stop whose next stop changed on a move", async () => {
    const onSave = open();
    fireEvent.click(screen.getByRole("button", { name: "Move stop 3 up" }));
    // Order is now A, C, B. A and B had legs to a different stop.
    fireEvent.click(screen.getByRole("button", { name: "Save plan" }));
    await vi.waitFor(() => expect(onSave).toHaveBeenCalled());
    const saved = onSave.mock.calls[0][0];
    expect(saved.stops.map((s: { name: string }) => s.name)).toEqual(["A", "C", "B"]);
    expect(saved.stops[0]).toMatchObject({ vias: [], departTime: "" });
  });

  it("offers an arrive-by on every stop but the first, the last one included, filled from the saved plan", () => {
    open();
    // The plan starts where the boat is, so the first stop has no arrive-by.
    expect(screen.getAllByLabelText("Arrive by")).toHaveLength(2);
    expect(screen.getAllByLabelText("Arrive by")[1]).toHaveValue("2026-11-17T17:00");
    expect(screen.getAllByLabelText("Arrive by")[0]).toHaveValue("");
  });

  it("saves an arrive-by set on a middle stop, and an emptied one as none", async () => {
    const onSave = open();
    fireEvent.change(screen.getAllByLabelText("Arrive by")[0], { target: { value: "2026-11-16T09:30" } });
    fireEvent.change(screen.getAllByLabelText("Arrive by")[1], { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: "Save plan" }));
    await vi.waitFor(() => expect(onSave).toHaveBeenCalled());
    const saved = onSave.mock.calls[0][0];
    expect(saved.stops.map((s: { arriveBy: string }) => s.arriveBy)).toEqual(["", "2026-11-16T09:30", ""]);
  });

  it("sends no arrive-by for a stop moved to the front, where the field is hidden", async () => {
    const onSave = open();
    fireEvent.click(screen.getByRole("button", { name: "Move stop 3 up" }));
    fireEvent.click(screen.getByRole("button", { name: "Move stop 2 up" }));
    fireEvent.click(screen.getByRole("button", { name: "Save plan" }));
    await vi.waitFor(() => expect(onSave).toHaveBeenCalled());
    const saved = onSave.mock.calls[0][0];
    expect(saved.stops.map((s: { name: string; arriveBy: string }) => [s.name, s.arriveBy])).toEqual([
      ["C", ""], ["A", ""], ["B", ""],
    ]);
  });

  const FIRST_STOP_NOTICE =
    "The first stop can't have an arrive-by, so this one will be removed when you save. Move the stop back to keep it.";

  it("says so on a stop whose arrive-by will be dropped because a move made it the first stop", () => {
    open();
    expect(screen.queryByText(FIRST_STOP_NOTICE)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Move stop 3 up" }));
    fireEvent.click(screen.getByRole("button", { name: "Move stop 2 up" }));
    // C, with its arrive-by, is now stop 1; the notice sits on its row.
    const notice = screen.getByText(FIRST_STOP_NOTICE);
    expect(notice.closest("li")).toBe(screen.getByLabelText("Stop 1 name").closest("li"));
    // Moving it back keeps the arrive-by, and the notice goes.
    fireEvent.click(screen.getByRole("button", { name: "Move stop 1 down" }));
    expect(screen.queryByText(FIRST_STOP_NOTICE)).toBeNull();
    expect(screen.getAllByLabelText("Arrive by")[0]).toHaveValue("2026-11-17T17:00");
  });

  it("says so when removing the stops before it makes a stop with an arrive-by the first", () => {
    open();
    fireEvent.click(screen.getByRole("button", { name: "Remove stop 1" }));
    expect(screen.queryByText(FIRST_STOP_NOTICE)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Remove stop 1" }));
    expect(screen.getByText(FIRST_STOP_NOTICE)).toBeInTheDocument();
  });

  it("keeps a stop's arrive-by when stops are moved, since it belongs to the stop", async () => {
    const onSave = open();
    fireEvent.click(screen.getByRole("button", { name: "Move stop 3 up" }));
    fireEvent.click(screen.getByRole("button", { name: "Save plan" }));
    await vi.waitFor(() => expect(onSave).toHaveBeenCalled());
    const saved = onSave.mock.calls[0][0];
    expect(saved.stops.map((s: { name: string; arriveBy: string }) => [s.name, s.arriveBy])).toEqual([
      ["A", ""], ["C", "2026-11-17T17:00"], ["B", ""],
    ]);
  });

  it("keeps a leg's vias when the stops either side of it are untouched", async () => {
    const onSave = open();
    // Removing the last stop leaves A -> B alone and ends B's leg.
    fireEvent.click(screen.getByRole("button", { name: "Remove stop 3" }));
    fireEvent.click(screen.getByRole("button", { name: "Save plan" }));
    await vi.waitFor(() => expect(onSave).toHaveBeenCalled());
    const saved = onSave.mock.calls[0][0];
    expect(saved.stops[0]).toMatchObject({ vias: [via], departTime: "06:30" });
    expect(saved.stops[1]).toMatchObject({ vias: [], departTime: "" });
    expect(within(screen.getByRole("list", { name: "Stops" })).getAllByRole("listitem").length).toBeGreaterThan(0);
  });
});
