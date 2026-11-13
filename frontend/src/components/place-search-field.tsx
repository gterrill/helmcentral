import { useState } from "react";

import { Button } from "@/components/ui/button";
import { FieldDescription, FieldError } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { searchPlaces, type PlaceCandidate } from "@/hooks/use-place-search";
import { errorMessage } from "@/lib/errors";
import { isSubmitEnter } from "@/lib/keyboard";

const bearingLabel = (deg: number) =>
  `${String(Math.round(deg) % 360).padStart(3, "0")}°`;

// Distance and bearing from the boat when the server gave them, then the kind
// of place; whatever is missing is left out.
const candidateLabel = (candidate: PlaceCandidate) =>
  [
    candidate.distanceNm !== null ? `${candidate.distanceNm.toFixed(1)} NM` : "",
    candidate.bearingDeg !== null ? bearingLabel(candidate.bearingDeg) : "",
    candidate.kind,
  ]
    .filter((part) => part !== "")
    .join(" · ");

/**
 * Find a place by name and pick one. The search runs from the boat's position,
 * so each candidate comes with how far away it is and which way. Used to add a
 * stop and to add a via place.
 */
export function PlaceSearchField({
  id,
  placeholder,
  onPick,
}: {
  id: string;
  placeholder: string;
  onPick: (place: PlaceCandidate) => void;
}) {
  const [query, setQuery] = useState("");
  const [candidates, setCandidates] = useState<PlaceCandidate[] | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [searching, setSearching] = useState(false);

  const run = async () => {
    const q = query.trim();
    if (q === "") return;
    setSearching(true);
    setError(null);
    setNote(null);
    try {
      const found = await searchPlaces(q);
      setCandidates(found.candidates);
      setNote(found.candidates.length === 0 ? `No places found matching "${q}".` : null);
    } catch (err) {
      setCandidates(null);
      setError(errorMessage(err));
    } finally {
      setSearching(false);
    }
  };

  return (
    <div className="space-y-1.5">
      <div className="flex gap-2">
        <Input
          id={id}
          aria-label={placeholder}
          value={query}
          placeholder={placeholder}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (isSubmitEnter(e)) {
              e.preventDefault();
              void run();
            }
          }}
        />
        <Button
          type="button"
          variant="outline"
          disabled={searching || query.trim() === ""}
          onClick={() => void run()}
        >
          Search
        </Button>
      </div>
      {searching && <FieldDescription>Searching…</FieldDescription>}
      {error && <FieldError>{error}</FieldError>}
      {note && <FieldDescription>{note}</FieldDescription>}
      {candidates && candidates.length > 0 && (
        <ul className="flex flex-col gap-1.5" aria-label="Places found">
          {candidates.map((candidate) => (
            <li key={`${candidate.name}-${candidate.lat}-${candidate.lon}`}>
              <button
                type="button"
                className="flex w-full min-w-0 items-center justify-between gap-3 rounded-md border border-border px-3 py-2 text-left text-sm hover:bg-muted/40"
                onClick={() => {
                  onPick(candidate);
                  setCandidates(null);
                  setQuery("");
                }}
              >
                <span className="min-w-0 truncate font-medium">{candidate.name}</span>
                <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
                  {candidateLabel(candidate)}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
