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
    stop("C", "2026-11-16", 0),
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
