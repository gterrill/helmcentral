import { memo, useEffect, useMemo, useState, type ReactNode } from "react";

import { Map as MapIcon, Plus } from "lucide-react";

import { PlanLegMap } from "@/components/plan-leg-map";
import { Button } from "@/components/ui/button";
import { FORECAST_REFRESH_SECONDS } from "@/config/app-config";
import { usePlanAssessment } from "@/hooks/use-plan-assessment";
import { usePlanDayForecast } from "@/hooks/use-plan-day-forecast";
import { useWaveForecast } from "@/hooks/use-wave-forecast";
import { useWeatherForecast } from "@/hooks/use-weather-forecast";
import type { ForecastPlace } from "@/lib/forecast-place";
import { notCheckedWords } from "@/lib/passage-measures";
import { PlanForecastContext, PlanForecastStore, placeKey } from "@/lib/plan-forecast-store";
import {
  buildPlanRows,
  currentText,
  formatWaveRange,
  formatWindRange,
  legForecastPlace,
  verdictText,
  windRelation,
  withinForecastHorizon,
  type LegVerdict,
  type Plan,
  type PlanAssessment,
  type PlanLeg,
  type PlanLegAssessment,
  type PlanStopWarning,
  legErrorText,
  legNoteText,
  type PlanRow,
} from "@/lib/plans";
import { formatDayMonth, formatWeekday } from "@/lib/stay";
import { formatDayKey, formatHourMinute, formatTimeOfDay, zoneKnown } from "@/lib/vessel-time";

// The columns, widest first. The grid switches on the table's own width (the
// table is an @container), not the screen's: beside the sidebar and a second
// column a 768 px screen leaves the table about 480 px. Narrower than that
// each row is a card and each cell carries its own label. Leg and Depart → ETA
// get the most room and wrap; nothing in them is cut short with an ellipsis.
// There is no Lightning column: the lightning forecast is read for the boat's
// own cell only, so it cannot be read for a leg (ADR 0286).
const GRID =
  "@[72rem]:grid @[72rem]:grid-cols-[3rem_6rem_minmax(14rem,2fr)_4rem_minmax(9rem,1.2fr)_minmax(6.5rem,1fr)_3.5rem_4rem_minmax(5.5rem,0.8fr)_minmax(9.5rem,1.2fr)] @[72rem]:items-start @[72rem]:gap-x-2";

const VERDICT_CLASS: Record<LegVerdict, string> = {
  go: "text-emerald-600 dark:text-emerald-400",
  marginal: "text-amber-600 dark:text-amber-400",
  no_go: "text-red-600 dark:text-red-400",
  no_data: "text-muted-foreground",
};

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
  /** A night is being added: +1 night waits so a quick second tap can't skip one. */
  addingNight: boolean;
  /** Stable between renders, so rows that did not change do not re-render. */
  onSelect: (rowKey: string) => void;
  /** Stable between renders. */
  onAddNight: (stopIndex: number) => void;
}

/**
 * Today at the plan's first place, whose clock its dates keep. Null when this
 * browser doesn't know the zone; the page already says so, and no day is then
 * held back as past the forecast.
 */
const todayAtPlan = (plan: Plan): string | null => {
  const zone = plan.stops[0]?.timezone;
  return zone && zoneKnown(zone) ? formatDayKey(new Date(), zone) : null;
};

const inHorizon = (dayKey: string, today: string | null) =>
  today === null || withinForecastHorizon(dayKey, today);

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
  // Nothing else reads this place once its reader goes, so its feed goes too.
  useEffect(() => () => store.remove(key), [store, key]);
  return null;
}

export function PlanTable({
  plan,
  legs,
  legsLoading,
  legsError,
  canWrite,
  selectedRowKey,
  addingNight,
  onSelect,
  onAddNight,
}: PlanTableProps) {
  const rows = useMemo(() => buildPlanRows(plan), [plan]);
  const legByFrom = useMemo(() => new Map(legs.map((l) => [l.from, l])), [legs]);
  const store = useMemo(() => new PlanForecastStore(), []);
  const today = todayAtPlan(plan);
  // Each distinct place is read once, however many rows sit at it. A place
  // whose rows all fall past the forecast is not read: its rows say so.
  const places = useMemo(() => {
    const byKey = new Map<string, ForecastPlace>();
    for (const row of rows) {
      if (!inHorizon(row.dayKey, today)) continue;
      const place = rowPlace(row, plan, row.kind === "passage" ? legByFrom.get(row.legIndex) : undefined);
      if (place && !byKey.has(placeKey(place))) byKey.set(placeKey(place), place);
    }
    return [...byKey.values()];
  }, [rows, plan, legByFrom, today]);
  const { assessment, error: assessmentError } = usePlanAssessment(plan);
  const assessmentState: AssessmentState = assessment ? "ready" : assessmentError ? "failed" : "loading";
  const rowAssessments = useMemo(
    () => buildRowAssessments(rows, plan, assessment, assessmentState),
    [rows, plan, assessment, assessmentState],
  );
  const zone = plan.stops[0]?.timezone;
  return (
    <div role="table" aria-label={`${plan.name} day by day`} className="@container space-y-2">
      {legsError && (
        <p role="alert" className="text-xs text-destructive">
          The legs could not be routed: {legsError}
        </p>
      )}
      {assessmentError && (
        <p role="alert" className="text-xs text-destructive">
          The plan could not be checked against the forecast: {assessmentError}
        </p>
      )}
      {assessment?.problem && (
        <p role="alert" className="text-xs text-destructive">
          {assessment.problem}
        </p>
      )}
      {assessment?.warningsError && (
        <p className="text-xs text-muted-foreground">Warnings not checked: {assessment.warningsError}</p>
      )}
      {assessment && zone && (
        <p data-testid="plan-assessment-asof" className="text-xs text-muted-foreground">
          Verdicts as of {formatTimeOfDay(assessment.asOf, zone)}
          {assessment.forecastAsOf ? ` · forecast fetched ${formatTimeOfDay(assessment.forecastAsOf, zone)}` : ""}
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
        <span role="columnheader">Current</span>
        <span role="columnheader">Verdict</span>
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
              assessed={rowAssessments.get(row.key)!}
              legsLoading={legsLoading}
              canWrite={canWrite}
              selected={selectedRowKey === row.key}
              addingNight={addingNight}
              onSelect={onSelect}
              onAddNight={onAddNight}
            />
          ))}
        </div>
      </PlanForecastContext.Provider>
    </div>
  );
}

type AssessmentState = "loading" | "ready" | "failed";

/** What the assessment says about one row, worked out once per assessment. */
interface RowAssessment {
  /** A passage row's leg verdict; undefined until the assessment has one for it. */
  leg: PlanLegAssessment | undefined;
  state: AssessmentState;
  /** A stop row's warning for the night it is read for. */
  nightWarning: PlanStopWarning | undefined;
  /** A passage row's warning at the stop it arrives at, for the night of arrival. */
  arrivalWarning: PlanStopWarning | undefined;
}

/**
 * The day a leg reaches its arrival stop, which is how the server keys that
 * stop's nights: the assessed arrival in the stop's zone, else the stop's own
 * arrival date from the plan. A leg that arrives after midnight is not on the
 * day it leaves.
 */
function arrivalDay(leg: PlanLegAssessment | undefined, to: Plan["stops"][number]): string {
  if (leg?.arrive && zoneKnown(to.timezone)) return formatDayKey(new Date(leg.arrive), to.timezone);
  return to.arrive;
}

/**
 * Every row's view of the assessment, built from one pass over its legs and
 * warnings. With no assessment (none yet, or the check failed) a row has none
 * either: nothing from an earlier plan is passed on.
 */
function buildRowAssessments(
  rows: PlanRow[],
  plan: Plan,
  assessment: PlanAssessment | null,
  state: AssessmentState,
): Map<string, RowAssessment> {
  const legs = new Map((assessment?.legs ?? []).map((l) => [l.leg, l]));
  const warnings = new Map<string, PlanStopWarning>();
  for (const s of assessment?.stops ?? []) for (const n of s.nights) warnings.set(`${s.stop}|${n.date}`, n);
  return new Map(
    rows.map((row): [string, RowAssessment] => {
      if (row.kind === "passage") {
        const leg = legs.get(row.legIndex);
        const to = plan.stops[row.legIndex + 1];
        const arrivalWarning = to ? warnings.get(`${row.legIndex + 1}|${arrivalDay(leg, to)}`) : undefined;
        return [row.key, { leg, state, nightWarning: undefined, arrivalWarning }];
      }
      const nightWarning = warnings.get(`${row.stopIndex}|${row.dayKey}`);
      return [row.key, { leg: undefined, state, nightWarning, arrivalWarning: undefined }];
    }),
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
      <span className="mr-1.5 text-[10px] font-medium uppercase tracking-wider text-muted-foreground @[72rem]:hidden">
        {label}
      </span>
      <span className="text-sm tabular-nums">{children}</span>
    </div>
  );
}

function WarningLink({ warning, prefix = "" }: { warning: PlanStopWarning; prefix?: string }) {
  const text = `${prefix}${warning.warning}`;
  return warning.url ? (
    <a href={warning.url} target="_blank" rel="noreferrer" className="underline">
      {text}
    </a>
  ) : (
    <>{text}</>
  );
}

/**
 * Depart → ETA. An operator's time reads plain; one the server suggested reads
 * "Suggested 6.40 AM" in muted italics, so it is never taken for a time the
 * operator set. With no time at all there is nothing to show.
 */
function departEtaText(
  a: PlanLegAssessment | undefined,
  fromZone: string,
  toZone: string,
  operatorTime: string,
): { text: string; suggested: boolean } {
  if (a?.depart) {
    const dep = formatTimeOfDay(a.depart, fromZone);
    const eta = a.arrive ? ` → ${formatTimeOfDay(a.arrive, toZone)}` : " →";
    return { text: `${a.departSuggested ? "Suggested " : ""}${dep}${eta}`, suggested: a.departSuggested };
  }
  const m = /^(\d\d):(\d\d)$/.exec(operatorTime);
  if (m) return { text: `${formatHourMinute(Number(m[1]), Number(m[2]))} →`, suggested: false };
  return { text: "—", suggested: false };
}

function DepartEtaCell({
  passage,
  a,
  fromZone,
  toZone,
  operatorTime,
  state,
}: {
  passage: boolean;
  a: PlanLegAssessment | undefined;
  fromZone: string;
  toZone: string;
  operatorTime: string;
  state: AssessmentState;
}) {
  if (!passage) return <Cell label="Depart → ETA">—</Cell>;
  const { text, suggested } = departEtaText(a, fromZone, toZone, operatorTime);
  const waiting = text === "—" && a === undefined && state === "loading";
  return (
    <Cell label="Depart → ETA">
      <span
        className={`block break-words ${suggested ? "italic text-muted-foreground" : ""}`}
        title={
          suggested
            ? "Suggested: no departure time is set. The best verdict inside daylight (Go over Marginal, No-Go only when every time is No-Go), then the fairest stream."
            : undefined
        }
      >
        {waiting ? "…" : text}
      </span>
    </Cell>
  );
}

/** The verdict column for a passage row: the verdict and its limiting factor, or why there is none. */
function VerdictText({
  a,
  state,
  noForecast,
}: {
  a: PlanLegAssessment | undefined;
  state: AssessmentState;
  noForecast: boolean;
}) {
  if (a === undefined) {
    return <span className="text-sm text-muted-foreground">{state === "loading" ? "…" : "—"}</span>;
  }
  // The problem that stopped every verdict is said once, above the table.
  if (a.verdict === null && a.noteKind === "problem") {
    return <span className="text-sm text-muted-foreground">Not checked</span>;
  }
  // A day past the forecast already says so across the wind columns.
  if (a.verdict === null && noForecast && a.noteKind === "no_forecast") {
    return <span className="text-sm text-muted-foreground">—</span>;
  }
  const text = verdictText(a);
  if (text === null) return <span className="text-sm text-muted-foreground">—</span>;
  const notChecked = a.verdict === null ? [] : notCheckedWords(a.notChecked);
  return (
    <>
      <p
        className={`text-sm font-medium ${a.verdict ? VERDICT_CLASS[a.verdict] : "text-muted-foreground"}`}
        title={a.verdict === "go" && a.limiting ? `Closest to a limit: ${a.limiting}` : undefined}
      >
        {text}
      </p>
      {a.verdict === "no_data" && a.noteKind === "gap" && a.note !== "" && (
        <p className="text-xs text-muted-foreground">{a.note}</p>
      )}
      {notChecked.length > 0 && (
        <p className="text-xs text-muted-foreground">Not checked: {notChecked.join(", ")}</p>
      )}
    </>
  );
}

const PlanDayRow = memo(function PlanDayRow({
  row,
  plan,
  leg,
  assessed,
  legsLoading,
  canWrite,
  selected,
  addingNight,
  onSelect,
  onAddNight,
}: {
  row: PlanRow;
  plan: Plan;
  leg: PlanLeg | undefined;
  /** What the server's assessment says about this row. */
  assessed: RowAssessment;
  legsLoading: boolean;
  canWrite: boolean;
  selected: boolean;
  addingNight: boolean;
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
  // A stop row is read for the night that follows it; a passage row for the day.
  const forecast = usePlanDayForecast(place, row.dayKey, passage ? "day" : "overnight");

  const course = leg?.courseDeg ?? null;
  const relation =
    passage && course !== null && forecast.wind?.fromDeg != null
      ? windRelation(course, forecast.wind.fromDeg)
      : null;

  // A day past the horizon has no feed read for it at all.
  const noForecast = forecast.coverage === "beyond" || !inHorizon(row.dayKey, todayAtPlan(plan));
  // The last stop's final row is the day the boat leaves it, not a lay day.
  const leaving = !passage && row.dayKey === from.depart;
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
          className="block w-full min-w-0 break-words text-left text-sm font-medium hover:underline"
          aria-pressed={selected}
          // A leg that did not route has no place to open the forecast at.
          disabled={unrouted}
          onClick={() => onSelect(row.key)}
        >
          {passage && to ? `${from.name} → ${to.name}` : `${from.name} · ${leaving ? "leave" : "lay day"}`}
        </button>
        {passage && from.vias.length > 0 && (
          <p className="break-words text-xs text-muted-foreground">
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
              disabled={addingNight}
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
      <DepartEtaCell
        passage={passage}
        a={assessed.leg}
        fromZone={from.timezone}
        toZone={to?.timezone ?? from.timezone}
        operatorTime={from.departTime}
        state={assessed.state}
      />

      {unrouted ? (
        <div role="cell" className="min-w-0 text-sm text-muted-foreground @[72rem]:col-span-3">
          {leg?.error ? "No forecast: the leg is not routed" : legsLoading ? "…" : "—"}
        </div>
      ) : noForecast ? (
        <div role="cell" className="min-w-0 text-sm text-muted-foreground @[72rem]:col-span-3">
          No forecast yet
        </div>
      ) : forecast.error ? (
        <div role="cell" className="min-w-0 text-xs text-destructive @[72rem]:col-span-3">
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
              : forecast.wavesError
                ? <span className="text-destructive" title={forecast.wavesError}>Failed</span>
                : forecast.wavesLoading
                  ? "…"
                  : "—"}
          </Cell>
        </>
      )}

      <Cell label="Current">
        {passage && assessed.leg?.currentKn != null ? (
          currentText(assessed.leg.currentKn)
        ) : (
          <span title={passage ? assessed.leg?.currentNote || undefined : undefined}>
            {passage && assessed.leg === undefined && assessed.state === "loading" ? "…" : "—"}
          </span>
        )}
      </Cell>

      <div role="cell" className="min-w-0">
        <span className="mr-1.5 text-[10px] font-medium uppercase tracking-wider text-muted-foreground @[72rem]:hidden">
          Verdict
        </span>
        {passage ? (
          <VerdictText
            a={assessed.leg}
            state={assessed.state}
            noForecast={noForecast}
          />
        ) : (
          assessed.nightWarning && (
            <p className="text-xs text-amber-600 dark:text-amber-400">
              <WarningLink warning={assessed.nightWarning} />
            </p>
          )
        )}
        {passage && assessed.arrivalWarning && to && (
          <p className="text-xs text-amber-600 dark:text-amber-400">
            <WarningLink warning={assessed.arrivalWarning} prefix={`${to.name} tonight: `} />
          </p>
        )}
      </div>

      {mapOpen && hasMap && leg && to && (
        <div className="mt-2 @[72rem]:col-span-10">
          <PlanLegMap from={from} to={to} leg={leg} />
        </div>
      )}
    </div>
  );
});
