import { useState, type ReactNode } from "react";

import { Map as MapIcon, Plus } from "lucide-react";

import { PlanLegMap } from "@/components/plan-leg-map";
import { Button } from "@/components/ui/button";
import { usePlanDayForecast } from "@/hooks/use-plan-day-forecast";
import {
  buildPlanRows,
  formatWaveRange,
  formatWindRange,
  legForecastPlace,
  planRowRefs,
  windRelation,
  type Plan,
  type PlanLeg,
  type PlanRow,
  type PlanRowRef,
} from "@/lib/plans";
import { formatDayMonth, formatWeekday } from "@/lib/stay";

// The columns, widest screen first. Below md each row is a card and each cell
// carries its own label. The Verdict column joins the end of this template
// when the verdicts ship; the row cells are kept narrow enough to take it.
const GRID =
  "md:grid md:grid-cols-[3rem_6.5rem_minmax(0,1fr)_4.5rem_5.5rem_minmax(0,8rem)_4rem_4.5rem] md:items-start md:gap-x-3";

/**
 * The row the operator chose, by what it is. Its stop and dates are read from
 * the plan as it is now, so a change to the plan never leaves them stale.
 */
export interface PlanPlaceSelection {
  planId: number;
  row: PlanRowRef;
}

const sameRow = (a: PlanRowRef | null, b: PlanRowRef) =>
  a !== null && a.kind === b.kind && a.index === b.index && a.nth === b.nth;

interface PlanTableProps {
  plan: Plan;
  legs: PlanLeg[];
  legsLoading: boolean;
  legsError: string | null;
  canWrite: boolean;
  selectedRow: PlanRowRef | null;
  onSelect: (row: PlanRowRef) => void;
  onAddNight: (stopIndex: number) => void;
}

const rowKey = (row: PlanRow) =>
  row.kind === "stop"
    ? `stop-${row.stopIndex}-${row.dayKey}`
    : `leg-${row.legIndex}-${row.dayKey}`;

export function PlanTable({
  plan,
  legs,
  legsLoading,
  legsError,
  canWrite,
  selectedRow,
  onSelect,
  onAddNight,
}: PlanTableProps) {
  const rows = buildPlanRows(plan);
  const refs = planRowRefs(rows);
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
      <div className="space-y-2">
        {rows.map((row, at) => (
          <PlanDayRow
            key={rowKey(row)}
            row={row}
            plan={plan}
            leg={row.kind === "passage" ? legs.find((l) => l.from === row.legIndex) : undefined}
            legsLoading={legsLoading}
            canWrite={canWrite}
            selected={sameRow(selectedRow, refs[at])}
            onSelect={() => onSelect(refs[at])}
            onAddNight={onAddNight}
          />
        ))}
      </div>
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

function PlanDayRow({
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
  onSelect: () => void;
  onAddNight: (stopIndex: number) => void;
}) {
  const [mapOpen, setMapOpen] = useState(false);
  const passage = row.kind === "passage";
  const from = plan.stops[passage ? row.legIndex : row.stopIndex];
  const to = passage ? plan.stops[row.legIndex + 1] : null;
  // A leg that did not route has no place to read a forecast at.
  const place = passage ? legForecastPlace(leg) : { lat: from.lat, lon: from.lon };
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
          onClick={onSelect}
        >
          {passage && to ? `${from.name} → ${to.name}` : `${from.name} · lay day`}
        </button>
        {passage && from.vias.length > 0 && (
          <p className="truncate text-xs text-muted-foreground">
            via {from.vias.map((v) => v.name).join(", ")}
          </p>
        )}
        {passage && leg?.error && (
          <p role="alert" className="text-xs text-destructive">
            {leg.error}
          </p>
        )}
        {passage && leg?.note && <p className="text-xs text-muted-foreground">{leg.note}</p>}
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
}
