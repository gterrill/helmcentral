import { useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { FieldDescription, FieldError } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { searchPlaces, type PlaceCandidate } from "@/hooks/use-place-search";
import { errorMessage } from "@/lib/errors";
import { isSubmitEnter } from "@/lib/keyboard";
import { formatBearingFigures } from "@/lib/poi";

// Distance and bearing from the boat when the server gave them, then the kind
// of place; whatever is missing is left out.
const candidateLabel = (candidate: PlaceCandidate) =>
  [
    candidate.distanceNm !== null ? `${candidate.distanceNm.toFixed(1)} NM` : "",
    candidate.bearingDeg !== null ? formatBearingFigures(candidate.bearingDeg) : "",
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

  // Only the latest search may write: a slower, earlier answer is dropped.
  const latest = useRef(0);

  const run = async () => {
    const q = query.trim();
    if (q === "") return;
    const mine = ++latest.current;
    setSearching(true);
    setError(null);
    setNote(null);
    try {
      const found = await searchPlaces(q);
      if (mine !== latest.current) return;
      setCandidates(found.candidates);
      setNote(found.candidates.length === 0 ? `No places found matching "${q}".` : null);
    } catch (err) {
      if (mine !== latest.current) return;
      setCandidates(null);
      setError(errorMessage(err));
    } finally {
      if (mine === latest.current) setSearching(false);
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
                  latest.current += 1;
                  setSearching(false);
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
