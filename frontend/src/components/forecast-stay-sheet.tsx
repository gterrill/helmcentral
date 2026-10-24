import { useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { searchPlaces, type PlaceCandidate } from "@/hooks/use-place-search";
import { todayISO } from "@/lib/local-date";
import type { PlannedStay } from "@/lib/stay";

interface ForecastStaySheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The saved stay, to edit or clear; null when none is planned. */
  stay: PlannedStay | null;
  onSave: (stay: PlannedStay) => Promise<unknown>;
  onClear: () => Promise<unknown>;
}

interface ChosenPlace {
  name: string;
  lat: number;
  lon: number;
}

const bearingLabel = (deg: number) =>
  `${String(Math.round(deg) % 360).padStart(3, "0")}°`;

// Distance and bearing from the boat when the server gave them, then the kind
// of place; whatever is missing is left out.
const candidateLabel = (candidate: PlaceCandidate) =>
  [
    candidate.distanceNm !== null
      ? `${candidate.distanceNm.toFixed(1)} NM`
      : "",
    candidate.bearingDeg !== null ? bearingLabel(candidate.bearingDeg) : "",
    candidate.kind,
  ]
    .filter((part) => part !== "")
    .join(" · ");

/**
 * Plan or change the stay: find the place by name, then say when the boat
 * arrives and leaves. The search runs from the boat's position, so each
 * candidate comes with how far away it is and which way.
 */
export function ForecastStaySheet({
  open,
  onOpenChange,
  stay,
  onSave,
  onClear,
}: ForecastStaySheetProps) {
  const [query, setQuery] = useState("");
  const [candidates, setCandidates] = useState<PlaceCandidate[] | null>(null);
  const [searchNote, setSearchNote] = useState<string | null>(null);
  const [searchError, setSearchError] = useState<string | null>(null);
  const [searching, setSearching] = useState(false);
  const [place, setPlace] = useState<ChosenPlace | null>(null);
  const [label, setLabel] = useState("");
  const [arrive, setArrive] = useState(todayISO());
  const [depart, setDepart] = useState(todayISO());
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  // Each opening starts from the saved stay, or from a blank one that arrives
  // today. Only the opening resets: the saved stay is refetched in the
  // background and must not wipe what the operator is typing.
  const stayRef = useRef(stay);
  stayRef.current = stay;
  useEffect(() => {
    if (!open) return;
    const stay = stayRef.current;
    setQuery("");
    setCandidates(null);
    setSearchNote(null);
    setSearchError(null);
    setSaveError(null);
    if (stay) {
      setPlace({ name: stay.name, lat: stay.lat, lon: stay.lon });
      setLabel(stay.name);
      setArrive(stay.arrive);
      setDepart(stay.depart);
    } else {
      const today = todayISO();
      setPlace(null);
      setLabel("");
      setArrive(today);
      setDepart(today);
    }
  }, [open]);

  const runSearch = async () => {
    const q = query.trim();
    if (q === "") return;
    setSearching(true);
    setSearchError(null);
    setSearchNote(null);
    try {
      const found = await searchPlaces(q);
      setCandidates(found.candidates);
      setSearchNote(
        found.candidates.length === 0
          ? `No places found matching "${q}".`
          : null,
      );
    } catch (err) {
      setCandidates(null);
      setSearchError(err instanceof Error ? err.message : String(err));
    } finally {
      setSearching(false);
    }
  };

  const choose = (candidate: PlaceCandidate) => {
    setPlace({ name: candidate.name, lat: candidate.lat, lon: candidate.lon });
    setLabel(candidate.name);
    setCandidates(null);
  };

  const changeArrive = (value: string) => {
    setArrive(value);
    // Leaving can never come before arriving.
    if (value !== "" && depart < value) setDepart(value);
  };

  const datesValid = arrive !== "" && depart !== "" && depart >= arrive;
  const canSave =
    place !== null && label.trim() !== "" && datesValid && !saving;

  const save = async () => {
    if (!place || !canSave) return;
    setSaving(true);
    setSaveError(null);
    try {
      await onSave({
        name: label.trim(),
        lat: place.lat,
        lon: place.lon,
        arrive,
        depart,
      });
      onOpenChange(false);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const clear = async () => {
    setSaving(true);
    setSaveError(null);
    try {
      await onClear();
      onOpenChange(false);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] max-w-lg overflow-y-auto">
        <DialogHeader>
          <DialogTitle>
            {stay ? "Change planned stay" : "Plan a stay"}
          </DialogTitle>
          <DialogDescription>
            Pick the place you will be berthed or anchored and the dates. The
            Forecast page can then show the weather there for those days.
          </DialogDescription>
        </DialogHeader>

        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="forecast-stay-place">Place</FieldLabel>
            <div className="flex gap-2">
              <Input
                id="forecast-stay-place"
                value={query}
                placeholder={
                  stay
                    ? "Search to choose a different place"
                    : "A marina, harbour or bay"
                }
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    void runSearch();
                  }
                }}
              />
              <Button
                type="button"
                variant="outline"
                disabled={searching || query.trim() === ""}
                onClick={() => void runSearch()}
              >
                Search
              </Button>
            </div>
            {searching && <FieldDescription>Searching…</FieldDescription>}
            {searchError && <FieldError>{searchError}</FieldError>}
            {searchNote && <FieldDescription>{searchNote}</FieldDescription>}
          </Field>

          {candidates && candidates.length > 0 && (
            <ul className="flex flex-col gap-1.5" aria-label="Places found">
              {candidates.map((candidate) => (
                <li key={`${candidate.name}-${candidate.lat}-${candidate.lon}`}>
                  <button
                    type="button"
                    className="flex w-full min-w-0 items-center justify-between gap-3 rounded-md border border-border px-3 py-2 text-left text-sm hover:bg-muted/40"
                    onClick={() => choose(candidate)}
                  >
                    <span className="min-w-0 truncate font-medium">
                      {candidate.name}
                    </span>
                    <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
                      {candidateLabel(candidate)}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}

          {place && (
            <Field>
              <FieldLabel htmlFor="forecast-stay-label">
                Name on the chip
              </FieldLabel>
              <Input
                id="forecast-stay-label"
                value={label}
                onChange={(e) => setLabel(e.target.value)}
              />
              <FieldDescription>
                {place.lat.toFixed(3)}, {place.lon.toFixed(3)}
              </FieldDescription>
            </Field>
          )}

          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel htmlFor="forecast-stay-arrive">Arrive</FieldLabel>
              <Input
                id="forecast-stay-arrive"
                type="date"
                value={arrive}
                onChange={(e) => changeArrive(e.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="forecast-stay-depart">Depart</FieldLabel>
              <Input
                id="forecast-stay-depart"
                type="date"
                min={arrive}
                value={depart}
                onChange={(e) => setDepart(e.target.value)}
              />
            </Field>
          </div>
          {!datesValid && arrive !== "" && depart !== "" && (
            <FieldError>Departure cannot be before arrival.</FieldError>
          )}
          <FieldDescription>
            Dates are local to the place. The forecast reaches about ten days
            ahead; later days are listed as waiting.
          </FieldDescription>

          {saveError && <FieldError>{saveError}</FieldError>}
        </FieldGroup>

        <DialogFooter>
          {stay && (
            <Button
              type="button"
              variant="outline"
              className="mr-auto"
              disabled={saving}
              onClick={() => void clear()}
            >
              Clear stay
            </Button>
          )}
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
          >
            Cancel
          </Button>
          <Button type="button" disabled={!canSave} onClick={() => void save()}>
            Save stay
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
