import { useState } from "react";

import { Pencil, Plus, Trash2 } from "lucide-react";

import {
  ForecastDrawer,
  type ForecastDrawerProps,
} from "@/components/forecast-drawer";
import { ConfirmDelete } from "@/components/patterns";
import { PlanEditor } from "@/components/plan-editor";
import { PlanTable, type PlanPlaceSelection } from "@/components/plan-table";
import { Button } from "@/components/ui/button";
import { FORECAST_REFRESH_SECONDS } from "@/config/app-config";
import { useForecastWarnings } from "@/hooks/use-forecast-warnings";
import { usePlanLegs, usePlans } from "@/hooks/use-plans";
import { useUpperAir } from "@/hooks/use-upper-air";
import { useWaveForecast } from "@/hooks/use-wave-forecast";
import { useWeatherForecast } from "@/hooks/use-weather-forecast";
import {
  buildForecastDrawerProps,
  type ForecastBoatProps,
} from "@/lib/forecast-drawer-props";
import { errorMessage } from "@/lib/errors";
import { findPlanRow, type Plan, type PlanRowRef } from "@/lib/plans";
import { formatStayRange } from "@/lib/stay";
import { unknownZoneMessage, zoneClocksDiffer, zoneOffsetLabel } from "@/lib/vessel-time";

interface ForecastPageProps {
  boat: ForecastBoatProps;
  /** The boat's IANA zone, to tell whether a plan stop keeps a different clock. */
  boatTimeZone: string | undefined;
  canWrite: boolean;
}

const planRange = (plan: Plan) => formatStayRange(plan.firstDate, plan.lastDate);

/**
 * The Forecast page: the boat's forecast, and above it the passage plan's day
 * by day table.
 *
 * It opens on the boat. Choosing a row in the table shows that row's place in
 * the drawer and a Boat button returns; the header always names which place is
 * on screen. A row's feeds are their own queries, held back until it is
 * chosen, so the dashboard tiles, the clock's sunrise and sunset, Auto theme
 * and the Rode Planner keep reading the boat's.
 */
export function ForecastPage({
  boat,
  boatTimeZone,
  canWrite,
}: ForecastPageProps) {
  const { plans, currentId, create, update, patchStop, remove, error: plansError } = usePlans();
  const [pickedId, setPickedId] = useState<number | null>(null);
  const [selection, setSelection] = useState<PlanPlaceSelection | null>(null);
  const [editing, setEditing] = useState<"new" | "change" | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  // The plan on show is the one the operator picked, else the one the server
  // names as current. A picked plan that was deleted drops back to current.
  const plan =
    plans.find((p) => p.id === (pickedId ?? currentId)) ??
    plans.find((p) => p.id === currentId) ??
    null;
  const { legs, loading: legsLoading, error: legsError } = usePlanLegs(plan);

  // The chosen row is held as which row it is. Its stop and dates are read
  // from the plan as it is now, so +1 night or a refetch never leaves the
  // drawer on old dates. A row the plan no longer has drops the selection.
  const selectedRow =
    selection && plan && selection.planId === plan.id
      ? findPlanRow(plan, selection.row)
      : null;
  if (selection && !selectedRow) setSelection(null);
  const selected = selectedRow && plan
    ? {
        kind: selectedRow.kind,
        dayKey: selectedRow.dayKey,
        stop: plan.stops[selectedRow.kind === "stop" ? selectedRow.stopIndex : selectedRow.legIndex],
      }
    : null;
  const selectedRef: PlanRowRef | null = selection?.row ?? null;

  const place = selected
    ? { lat: selected.stop.lat, lon: selected.stop.lon }
    : undefined;
  const showingPlace = selected !== null;

  const weather = useWeatherForecast(FORECAST_REFRESH_SECONDS, {
    place,
    enabled: showingPlace,
  });
  const waves = useWaveForecast(FORECAST_REFRESH_SECONDS, {
    place,
    enabled: showingPlace,
  });
  const upperAir = useUpperAir(undefined, { place, enabled: showingPlace });
  const warnings = useForecastWarnings(FORECAST_REFRESH_SECONDS, {
    place,
    enabled: showingPlace,
  });

  const placeProps: ForecastDrawerProps | null =
    selected
      ? {
          ...buildForecastDrawerProps({
            weather,
            waves,
            upperAir,
            // Lightning has no place of its own. A row's view carries the
            // boat's feed only so the drawer can say it isn't drawn here.
            lightning: boat.lightning,
            warnings,
            unit: boat.unit,
          }),
          stay: {
            placeName: selected.stop.name,
            lat: selected.stop.lat,
            lon: selected.stop.lon,
            arrive: selected.kind === "stop" ? selected.stop.arrive : selected.dayKey,
            depart: selected.kind === "stop" ? selected.stop.depart : selected.dayKey,
            timezone: selected.stop.timezone,
          },
        }
      : null;

  const stopZone = selected?.stop.timezone;
  const zoneProblem = stopZone
    ? (unknownZoneMessage(stopZone) ?? unknownZoneMessage(boatTimeZone))
    : null;
  const clockDiffers =
    stopZone !== undefined &&
    !zoneProblem &&
    boatTimeZone !== undefined &&
    zoneClocksDiffer(stopZone, boatTimeZone, Date.now());

  const pick = (id: number) => {
    setPickedId(id);
    setSelection(null);
  };

  const addNight = async (stopIndex: number) => {
    if (!plan) return;
    setActionError(null);
    try {
      await patchStop(plan.id, stopIndex, {
        nights: plan.stops[stopIndex].nights + 1,
      });
    } catch (err) {
      setActionError(errorMessage(err));
    }
  };

  const deletePlan = async () => {
    if (!plan) return;
    setDeleting(true);
    setActionError(null);
    try {
      await remove(plan.id);
      setPickedId(null);
      setSelection(null);
      setConfirmDelete(false);
    } catch (err) {
      setActionError(errorMessage(err));
    } finally {
      setDeleting(false);
    }
  };

  const open = plans.filter((p) => p.status !== "past");
  const past = plans.filter((p) => p.status === "past");

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <p
          data-testid="forecast-place-label"
          className="min-w-0 flex-1 truncate text-sm font-semibold text-foreground"
        >
          {selected
            ? `Forecast for ${selected.stop.name} — not the boat's position`
            : "Forecast for the boat's position"}
        </p>
        {showingPlace && (
          <Button
            type="button"
            size="sm"
            variant="secondary"
            className="h-8 text-xs"
            onClick={() => setSelection(null)}
          >
            Boat
          </Button>
        )}
        {canWrite && (
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="h-8 text-xs"
            onClick={() => {
              setEditing("new");
            }}
          >
            <Plus size={13} aria-hidden className="mr-1" />
            New plan
          </Button>
        )}
      </div>

      {plansError && (
        <p role="alert" className="text-xs text-destructive">
          {plansError}
        </p>
      )}

      {plans.length > 0 && (
        <section aria-label="Passage plans" className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <div
              className="flex min-w-0 flex-wrap gap-1 rounded-md border border-border bg-card p-0.5"
              role="group"
              aria-label="Plans"
            >
              {open.map((p) => (
                <Button
                  key={p.id}
                  type="button"
                  size="sm"
                  variant={plan?.id === p.id ? "secondary" : "ghost"}
                  className="h-8 min-w-0 max-w-64 text-xs"
                  aria-pressed={plan?.id === p.id}
                  onClick={() => pick(p.id)}
                >
                  <span className="truncate">{`${p.name} · ${planRange(p)}`}</span>
                </Button>
              ))}
            </div>
            {canWrite && plan && (
              <>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  className="h-8 text-xs"
                  onClick={() => setEditing("change")}
                >
                  <Pencil size={13} aria-hidden className="mr-1" />
                  Change plan
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  className="h-8 text-xs"
                  onClick={() => setConfirmDelete(true)}
                >
                  <Trash2 size={13} aria-hidden className="mr-1" />
                  Delete plan
                </Button>
              </>
            )}
          </div>

          {past.length > 0 && (
            <details className="text-sm">
              <summary className="cursor-pointer text-xs font-medium text-muted-foreground">
                Past ({past.length})
              </summary>
              <div className="mt-1.5 flex flex-wrap gap-1.5">
                {past.map((p) => (
                  <Button
                    key={p.id}
                    type="button"
                    size="sm"
                    variant={plan?.id === p.id ? "secondary" : "outline"}
                    className="h-8 min-w-0 max-w-64 text-xs"
                    aria-pressed={plan?.id === p.id}
                    onClick={() => pick(p.id)}
                  >
                    <span className="truncate">{`${p.name} · ${planRange(p)}`}</span>
                  </Button>
                ))}
              </div>
            </details>
          )}

          {actionError && (
            <p role="alert" className="text-xs text-destructive">
              {actionError}
            </p>
          )}

          {plan && (
            <PlanTable
              plan={plan}
              legs={legs}
              legsLoading={legsLoading}
              legsError={legsError}
              canWrite={canWrite}
              selectedRow={selectedRef}
              onSelect={(row) => setSelection({ planId: plan.id, row })}
              onAddNight={(i) => void addNight(i)}
            />
          )}
        </section>
      )}

      {zoneProblem && (
        <p
          data-testid="forecast-stay-zone-error"
          className="text-xs text-destructive"
        >
          {zoneProblem}
        </p>
      )}

      {clockDiffers && selected && stopZone && (
        <p
          data-testid="forecast-stay-clock-note"
          className="text-xs text-muted-foreground"
        >
          Times here are {zoneOffsetLabel(stopZone, Date.now())}, local to {selected.stop.name}, not
          the boat&apos;s clock.
        </p>
      )}

      <ForecastDrawer
        key={
          selected && plan
            ? `${plan.id}-${selected.kind}-${selected.stop.name}-${selected.dayKey}-${selected.stop.arrive}-${selected.stop.depart}`
            : "boat"
        }
        {...(placeProps ?? boat)}
      />

      {canWrite && (
        <PlanEditor
          open={editing !== null}
          onOpenChange={(o) => {
            if (!o) setEditing(null);
          }}
          plan={editing === "change" ? plan : null}
          onSave={async (input) => {
            const saved =
              editing === "change" && plan
                ? await update(plan.id, input)
                : await create(input);
            setPickedId(saved.id);
            setSelection(null);
          }}
        />
      )}

      <ConfirmDelete
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={`Delete ${plan?.name ?? "plan"}?`}
        description="The plan and its dates are removed. Nothing else changes."
        deleting={deleting}
        onConfirm={() => void deletePlan()}
      />
    </div>
  );
}
