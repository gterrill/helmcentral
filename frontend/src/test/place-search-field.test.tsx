import { describe, expect, it, vi, beforeEach } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";

import { PlaceSearchField } from "@/components/place-search-field";
import { searchPlaces } from "@/hooks/use-place-search";

vi.mock("@/hooks/use-place-search");

const candidate = (name: string) => ({ name, kind: "", lat: 1, lon: 2, distanceNm: null, bearingDeg: null });

function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

describe("PlaceSearchField", () => {
  beforeEach(() => vi.clearAllMocks());

  it("ignores a slower earlier answer that arrives after a later search", async () => {
    const first = deferred<{ candidates: ReturnType<typeof candidate>[] }>();
    const second = deferred<{ candidates: ReturnType<typeof candidate>[] }>();
    vi.mocked(searchPlaces)
      .mockReturnValueOnce(first.promise as never)
      .mockReturnValueOnce(second.promise as never);
    render(<PlaceSearchField id="s" placeholder="Find a place" onPick={() => {}} />);
    const input = screen.getByLabelText("Find a place");
    fireEvent.change(input, { target: { value: "moolo" } });
    fireEvent.click(screen.getByRole("button", { name: "Search" }));
    // Enter searches again while the first is still out.
    fireEvent.change(input, { target: { value: "mooloolaba" } });
    fireEvent.keyDown(input, { key: "Enter" });

    await act(async () => {
      second.resolve({ candidates: [candidate("Mooloolaba")] });
    });
    await act(async () => {
      first.resolve({ candidates: [candidate("Moolooloo")] });
    });

    expect(screen.getByText("Mooloolaba")).toBeInTheDocument();
    expect(screen.queryByText("Moolooloo")).not.toBeInTheDocument();
  });
});
