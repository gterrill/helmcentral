import { memo, useEffect, useMemo, useState, type ReactNode } from "react";

import { Map as MapIcon, Plus } from "lucide-react";

import { PlanLegMap } from "@/components/plan-leg-map";
import { Button } from "@/components/ui/button";
import { FORECAST_REFRESH_SECONDS } from "@/config/app-config";
import { usePlanDayForecast } from "@/hooks/use-plan-day-forecast";
import { useWaveForecast } from "@/hooks/use-wave-forecast";
import { useWeatherForecast } from "@/hooks/use-weather-forecast";
import type { ForecastPlace } from "@/lib/forecast-place";
import { PlanForecastContext, PlanForecastStore, placeKey } from "@/lib/plan-forecast-store";
import {
  buildPlanRows,
  formatWaveRange,
  formatWindRange,
  legForecastPlace,
  windRelation,
  type Plan,
  type PlanLeg,
  legErrorText,
  legNoteText,
  type PlanRow,
} from "@/lib/plans";
import { formatDayMonth, formatWeekday } from "@/lib/stay";

// The columns, widest screen first. Below md each row is a card and each cell
// carries its own label. The Verdict column joins the end of this template
// when the verdicts ship; the row cells are kept narrow enough to take it.
const GRID =
  "md:grid md:grid-cols-[3rem_6.5rem_minmax(0,1fr)_4.5rem_5.5rem_minmax(0,8rem)_4rem_4.5rem] md:items-start md:gap-x-3";

/**
 * The row the operator chose, by its key (PlanRow.key). Its stop and dates are
 * read from the plan as it is now, so a change to the plan never leaves them
 * stale.
 */
export interface PlanPlaceSelection {
  planId: number;
  rowKey: string;
}

interface PlanTableProps {
  plan: Plan;
  legs: PlanLeg[];
  legsLoading: boolean;
  legsError: string | null;
  canWrite: boolean;
  selectedRowKey: string | null;
  /** Stable between renders, so rows that did not change do not re-render. */
  onSelect: (rowKey: string) => void;
  /** Stable between renders. */
  onAddNight: (stopIndex: number) => void;
}

/** Where a row's forecast is read: a stop's own place, a leg's mid-route; null for an unrouted leg. */
const rowPlace = (row: PlanRow, plan: Plan, leg: PlanLeg | undefined): ForecastPlace | null => {
  if (row.kind === "passage") return legForecastPlace(leg);
  const stop = plan.stops[row.stopIndex];
  return { lat: stop.lat, lon: stop.lon };
};

/** The one forecast read for a place, written to the store for the rows at it. */
function PlaceFeedReader({ place, store }: { place: ForecastPlace; store: PlanForecastStore }) {
  const weather = useWeatherForecast(FORECAST_REFRESH_SECONDS, { place, enabled: true });
  const waves = useWaveForecast(FORECAST_REFRESH_SECONDS, { place, enabled: true });
  const key = placeKey(place);
  useEffect(() => {
    store.set(key, { weather, waves });
  }, [store, key, weather, waves]);
  return null;
}

export function PlanTable({
  plan,
  legs,
  legsLoading,
  legsError,
  canWrite,
  selectedRowKey,
  onSelect,
  onAddNight,
}: PlanTableProps) {
  const rows = useMemo(() => buildPlanRows(plan), [plan]);
  const legByFrom = useMemo(() => new Map(legs.map((l) => [l.from, l])), [legs]);
  const store = useMemo(() => new PlanForecastStore(), []);
  // Each distinct place is read once, however many rows sit at it.
  const places = useMemo(() => {
    const byKey = new Map<string, ForecastPlace>();
    for (const row of rows) {
      const place = rowPlace(row, plan, row.kind === "passage" ? legByFrom.get(row.legIndex) : undefined);
      if (place && !byKey.has(placeKey(place))) byKey.set(placeKey(place), place);
    }
    return [...byKey.values()];
  }, [rows, plan, legByFrom]);
  return (
    <div role="table" aria-label={`${plan.name} day by day`} className="space-y-2">
      {legsError && (
        <p role="alert" className="text-xs text-destructive">
          The legs could not be routed: {legsError}
        </p>
      )}
      <div
        role="row"
        className={`${GRID} hidden px-3 text-[10px] font-medium uppercase tracking-wider text-muted-foreground`}
      >
        <span role="columnheader">Day</span>
        <span role="columnheader">Date</span>
        <span role="columnheader">Leg</span>
        <span role="columnheader">Distance</span>
        <span role="columnheader">Depart → ETA</span>
        <span role="columnheader">Wind</span>
        <span role="columnheader">Gusts</span>
        <span role="columnheader">Waves</span>
      </div>
      <PlanForecastContext.Provider value={store}>
        {places.map((place) => (
          <PlaceFeedReader key={placeKey(place)} place={place} store={store} />
        ))}
        <div className="space-y-2">
          {rows.map((row) => (
            <PlanDayRow
              key={row.key}
              row={row}
              plan={plan}
              leg={row.kind === "passage" ? legByFrom.get(row.legIndex) : undefined}
              legsLoading={legsLoading}
              canWrite={canWrite}
              selected={selectedRowKey === row.key}
              onSelect={onSelect}
              onAddNight={onAddNight}
            />
          ))}
        </div>
      </PlanForecastContext.Provider>
    </div>
  );
}

function Cell({
  label,
  className = "",
  children,
}: {
  label: string;
  className?: string;
  children: ReactNode;
}) {
  return (
    <div role="cell" className={`min-w-0 ${className}`}>
      <span className="mr-1.5 text-[10px] font-medium uppercase tracking-wider text-muted-foreground md:hidden">
        {label}
      </span>
      <span className="text-sm tabular-nums">{children}</span>
    </div>
  );
}

const PlanDayRow = memo(function PlanDayRow({
  row,
  plan,
  leg,
  legsLoading,
  canWrite,
  selected,
  onSelect,
  onAddNight,
}: {
  row: PlanRow;
  plan: Plan;
  leg: PlanLeg | undefined;
  legsLoading: boolean;
  canWrite: boolean;
  selected: boolean;
  onSelect: (rowKey: string) => void;
  onAddNight: (stopIndex: number) => void;
}) {
  const [mapOpen, setMapOpen] = useState(false);
  const passage = row.kind === "passage";
  const from = plan.stops[passage ? row.legIndex : row.stopIndex];
  const to = passage ? plan.stops[row.legIndex + 1] : null;
  // A leg that did not route has no place to read a forecast at.
  const place = rowPlace(row, plan, leg);
  const unrouted = passage && place === null;
  const forecast = usePlanDayForecast(place, row.dayKey);

  const course = leg?.courseDeg ?? null;
  const relation =
    passage && course !== null && forecast.wind?.fromDeg != null
      ? windRelation(course, forecast.wind.fromDeg)
      : null;

  const noForecast = forecast.coverage === "beyond";
  const hasMap = passage && leg !== undefined && leg.error === "" && leg.route.length > 1 && to !== null;

  return (
    <div
      role="row"
      data-testid="plan-row"
      data-kind={row.kind}
      className={`${GRID} rounded-md border bg-card p-3 ${selected ? "border-primary" : "border-border"}`}
    >
      <Cell label="Day">{`Day ${row.dayNumber}`}</Cell>
      <Cell label="Date">{`${formatWeekday(row.dayKey)} ${formatDayMonth(row.dayKey)}`}</Cell>

      <div role="cell" className="min-w-0">
        <button
          type="button"
          className="block w-full min-w-0 truncate text-left text-sm font-medium hover:underline"
          aria-pressed={selected}
          onClick={() => onSelect(row.key)}
        >
          {passage && to ? `${from.name} → ${to.name}` : `${from.name} · lay day`}
        </button>
        {passage && from.vias.length > 0 && (
          <p className="truncate text-xs text-muted-foreground">
            via {from.vias.map((v) => v.name).join(", ")}
          </p>
        )}
        {passage && leg?.error && (
          <p role="alert" className="text-xs text-destructive" title={legErrorText(leg).detail}>
            {legErrorText(leg).summary}
          </p>
        )}
        {passage && leg && leg.moved.length > 0 && (
          <p className="text-xs text-muted-foreground" title={leg.note}>
            {legNoteText(leg)}
          </p>
        )}
        <div className="mt-1 flex flex-wrap gap-1">
          {!passage && canWrite && (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="h-7 px-2 text-xs"
              onClick={() => onAddNight(row.stopIndex)}
            >
              <Plus size={12} aria-hidden className="mr-1" />
              +1 night
            </Button>
          )}
          {hasMap && (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="h-7 px-2 text-xs"
              aria-expanded={mapOpen}
              onClick={() => setMapOpen((v) => !v)}
            >
              <MapIcon size={12} aria-hidden className="mr-1" />
              {mapOpen ? "Hide route" : "Route"}
            </Button>
          )}
        </div>
      </div>

      <Cell label="Distance">
        {!passage
          ? "—"
          : leg === undefined
            ? legsLoading
              ? "…"
              : "—"
            : leg.distanceNm === null
              ? "Not routed"
              : `${leg.distanceNm.toFixed(1)} nm`}
      </Cell>
      <Cell label="Depart → ETA">{passage && from.departTime ? `${from.departTime} →` : "—"}</Cell>

      {unrouted ? (
        <div role="cell" className="min-w-0 text-sm text-muted-foreground md:col-span-3">
          {leg?.error ? "No forecast: the leg is not routed" : legsLoading ? "…" : "—"}
        </div>
      ) : noForecast ? (
        <div role="cell" className="min-w-0 text-sm text-muted-foreground md:col-span-3">
          No forecast yet
        </div>
      ) : forecast.error ? (
        <div role="cell" className="min-w-0 text-xs text-destructive md:col-span-3">
          Forecast failed: {forecast.error}
        </div>
      ) : (
        <>
          <Cell label="Wind">
            {forecast.wind
              ? `${formatWindRange(forecast.wind.minKts, forecast.wind.maxKts)}${
                  relation ? ` ${relation}` : !passage && forecast.windDirection ? ` ${forecast.windDirection}` : ""
                }`
              : forecast.loading
                ? "…"
                : "—"}
          </Cell>
          <Cell label="Gusts">
            {forecast.wind && forecast.wind.gustKts !== null ? `${Math.round(forecast.wind.gustKts)} kn` : "—"}
          </Cell>
          <Cell label="Waves">
            {forecast.waveMinM !== null && forecast.waveMaxM !== null
              ? formatWaveRange(forecast.waveMinM, forecast.waveMaxM)
              : "—"}
          </Cell>
        </>
      )}

      {mapOpen && hasMap && leg && to && (
        <div className="mt-2 md:col-span-8">
          <PlanLegMap from={from} to={to} leg={leg} />
        </div>
      )}
    </div>
  );
});
