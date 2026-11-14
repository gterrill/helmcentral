import { useEffect, useRef, useState } from "react";

import { ArrowDown, ArrowUp, Plus, Trash2, X } from "lucide-react";

import { PlaceSearchField } from "@/components/place-search-field";
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
import { errorMessage } from "@/lib/errors";
import { todayISO } from "@/lib/local-date";
import {
  clearMovedLegs,
  draftToInput,
  previewArrivals,
  type DraftStop,
  type Plan,
  type PlanInput,
} from "@/lib/plans";
import { formatDayMonth, formatWeekday } from "@/lib/stay";

interface PlanEditorProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The plan to change; null starts a new one. */
  plan: Plan | null;
  onSave: (input: PlanInput) => Promise<unknown>;
}

// A stop in the editor carries an id of its own, so its row keeps its identity
// while its name is typed over and while it is moved up or down.
type EditorStop = DraftStop & { id: string };

let nextStopId = 0;
const newStopId = () => `stop-${(nextStopId += 1)}`;

const toDraft = (plan: Plan): EditorStop[] =>
  plan.stops.map((s) => ({
    id: newStopId(),
    name: s.name,
    lat: s.lat,
    lon: s.lon,
    nights: s.nights,
    vias: s.vias,
    departTime: s.departTime,
    arriveBy: s.arriveBy,
  }));

/**
 * Make or change a plan: name it, add stops by searching for each place, say
 * how many nights at each, and add via places on a leg where the route needs
 * steering. Only the first arrival is typed; every later one follows from the
 * nights before it.
 */
export function PlanEditor({ open, onOpenChange, plan, onSave }: PlanEditorProps) {
  const [name, setName] = useState("");
  const [firstArrive, setFirstArrive] = useState(todayISO());
  const [stops, setStops] = useState<EditorStop[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Each opening starts from the saved plan, or a blank one. Only the opening
  // resets: the plans are refetched in the background and must not wipe what
  // the operator is typing.
  const planRef = useRef(plan);
  planRef.current = plan;
  useEffect(() => {
    if (!open) return;
    const p = planRef.current;
    setError(null);
    setName(p?.name ?? "");
    setFirstArrive(p?.stops[0]?.arrive ?? todayISO());
    setStops(p ? toDraft(p) : []);
  }, [open]);

  const patchStop = (i: number, patch: Partial<DraftStop>) =>
    setStops((all) => all.map((s, k) => (k === i ? { ...s, ...patch } : s)));
  const remove = (i: number) =>
    setStops((all) =>
      clearMovedLegs(
        all,
        all.filter((_, k) => k !== i),
      ),
    );
  const move = (i: number, by: -1 | 1) =>
    setStops((all) => {
      const j = i + by;
      if (j < 0 || j >= all.length) return all;
      const next = [...all];
      [next[i], next[j]] = [next[j], next[i]];
      return clearMovedLegs(all, next);
    });

  const arrivals = previewArrivals(
    firstArrive,
    stops.map((s) => s.nights),
  );
  const canSave = name.trim() !== "" && stops.length > 0 && firstArrive !== "" && !saving;

  const save = async () => {
    if (!canSave) return;
    setSaving(true);
    setError(null);
    try {
      await onSave(draftToInput(name, firstArrive, stops));
      onOpenChange(false);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{plan ? "Change plan" : "New plan"}</DialogTitle>
          <DialogDescription>
            List the places in order with the nights at each. The Forecast page
            shows the weather for every day of the plan, and the distance of each leg
            by water.
          </DialogDescription>
        </DialogHeader>

        <FieldGroup>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-[minmax(0,1fr)_11rem]">
            <Field>
              <FieldLabel htmlFor="plan-name">Name</FieldLabel>
              <Input
                id="plan-name"
                value={name}
                placeholder="Home run"
                onChange={(e) => setName(e.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="plan-first-arrive">First arrival</FieldLabel>
              <Input
                id="plan-first-arrive"
                type="date"
                value={firstArrive}
                onChange={(e) => setFirstArrive(e.target.value)}
              />
            </Field>
          </div>

          <ol className="space-y-3" aria-label="Stops">
            {stops.map((stop, i) => {
              const last = i === stops.length - 1;
              return (
                <li key={stop.id} className="space-y-2 rounded-md border border-border bg-card p-3">
                  <div className="flex items-start gap-2">
                    <div className="min-w-0 flex-1">
                      <Input
                        aria-label={`Stop ${i + 1} name`}
                        value={stop.name}
                        onChange={(e) => patchStop(i, { name: e.target.value })}
                      />
                      <p className="mt-1 text-xs tabular-nums text-muted-foreground">
                        {stop.lat.toFixed(3)}, {stop.lon.toFixed(3)} · arrive{" "}
                        {arrivals[i]
                          ? `${formatWeekday(arrivals[i])} ${formatDayMonth(arrivals[i])}`
                          : "—"}
                      </p>
                    </div>
                    <div className="flex shrink-0 gap-1">
                      <Button type="button" size="icon" variant="ghost" aria-label={`Move stop ${i + 1} up`} disabled={i === 0} onClick={() => move(i, -1)}>
                        <ArrowUp size={14} aria-hidden />
                      </Button>
                      <Button type="button" size="icon" variant="ghost" aria-label={`Move stop ${i + 1} down`} disabled={last} onClick={() => move(i, 1)}>
                        <ArrowDown size={14} aria-hidden />
                      </Button>
                      <Button type="button" size="icon" variant="ghost" aria-label={`Remove stop ${i + 1}`} onClick={() => remove(i)}>
                        <Trash2 size={14} aria-hidden />
                      </Button>
                    </div>
                  </div>

                  <div className="flex flex-wrap items-end gap-3">
                    <Field className="w-28">
                      <FieldLabel htmlFor={`plan-nights-${i}`}>Nights</FieldLabel>
                      <Input
                        id={`plan-nights-${i}`}
                        type="number"
                        inputMode="numeric"
                        min={0}
                        max={365}
                        value={stop.nights}
                        onChange={(e) =>
                          patchStop(i, { nights: Math.max(0, Math.min(365, Math.trunc(Number(e.target.value)) || 0)) })
                        }
                      />
                    </Field>
                    <Button type="button" variant="outline" size="sm" className="h-9" onClick={() => patchStop(i, { nights: stop.nights + 1 })}>
                      <Plus size={13} aria-hidden className="mr-1" />
                      +1 night
                    </Button>
                    {!last && (
                      <Field className="w-32">
                        <FieldLabel htmlFor={`plan-time-${i}`}>Leave at</FieldLabel>
                        <Input
                          id={`plan-time-${i}`}
                          type="time"
                          value={stop.departTime}
                          onChange={(e) => patchStop(i, { departTime: e.target.value })}
                        />
                      </Field>
                    )}
                    {i > 0 && (
                      <Field className="w-52">
                        <FieldLabel htmlFor={`plan-arrive-by-${i}`}>Arrive by</FieldLabel>
                        <Input
                          id={`plan-arrive-by-${i}`}
                          type="datetime-local"
                          value={stop.arriveBy}
                          onChange={(e) => patchStop(i, { arriveBy: e.target.value })}
                        />
                      </Field>
                    )}
                  </div>

                  {!last && (
                    <div className="space-y-1.5">
                      <p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                        Via, on the way to {stops[i + 1].name}
                      </p>
                      {stop.vias.length > 0 && (
                        <ul className="flex flex-wrap gap-1.5" aria-label={`Via places after stop ${i + 1}`}>
                          {stop.vias.map((via, k) => (
                            <li key={`${k}-${via.name}`} className="flex items-center gap-1 rounded-md border border-border px-2 py-1 text-xs">
                              <span className="truncate">{via.name}</span>
                              <button
                                type="button"
                                aria-label={`Remove via ${via.name}`}
                                className="text-muted-foreground hover:text-foreground"
                                onClick={() => patchStop(i, { vias: stop.vias.filter((_, n) => n !== k) })}
                              >
                                <X size={12} aria-hidden />
                              </button>
                            </li>
                          ))}
                        </ul>
                      )}
                      <PlaceSearchField
                        id={`plan-via-${i}`}
                        placeholder="Add a via place"
                        onPick={(c) => patchStop(i, { vias: [...stop.vias, { name: c.name, lat: c.lat, lon: c.lon }] })}
                      />
                    </div>
                  )}
                </li>
              );
            })}
          </ol>

          <Field>
            <FieldLabel htmlFor="plan-add-stop">Add a stop</FieldLabel>
            <PlaceSearchField
              id="plan-add-stop"
              placeholder="A marina, harbour or bay"
              onPick={(c) =>
                setStops((all) => [...all, { id: newStopId(), name: c.name, lat: c.lat, lon: c.lon, nights: all.length === 0 ? 1 : 0, vias: [], departTime: "", arriveBy: "" }])
              }
            />
            <FieldDescription>
              Dates are local to each place. The forecast reaches about ten days ahead; later
              days read &ldquo;No forecast yet&rdquo;. An arrive-by is the latest you want to be
              at a stop; the plan then shows its slack.
            </FieldDescription>
          </Field>

          {error && <FieldError>{error}</FieldError>}
        </FieldGroup>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="button" disabled={!canSave} onClick={() => void save()}>
            Save plan
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
