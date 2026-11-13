import { useEffect, useMemo, useState } from "react";

import { FORECAST_REFRESH_SECONDS } from "@/config/app-config";
import { PLANS_PATH } from "@/hooks/use-plans";
import { useApiQuery } from "@/lib/api-query";
import type { LegVerdict, Plan, PlanAssessment, PlanLegAssessment, PlanNoteKind } from "@/lib/plans";

// The plan's verdicts, worked out on the server with Mate's passage rules and
// the operator's passage limits (ADR 0286). Asked again on the forecast's own
// refresh interval, so the "as of" time moves with the forecast, and again half
// a second after the plan stops changing. Nothing from before an edit is shown:
// until the answer for the edited plan lands there is no assessment, and the
// table reads "…" where a verdict goes.

/** How long the plan must hold still before the verdicts are asked for again. */
export const ASSESSMENT_DEBOUNCE_MS = 500;

interface PlanLegAssessmentApi {
  leg: number;
  date: string;
  verdict: string;
  limiting: string;
  note: string;
  note_kind: string;
  depart: string;
  depart_suggested: boolean;
  arrive: string;
  hours: number | null;
  speed_kn: number | null;
  current_kn: number | null;
  current_note: string;
  not_checked: string[] | null;
}

interface PlanAssessmentApi {
  as_of: string;
  forecast_as_of: string;
  problem: string;
  warnings_error: string;
  legs: PlanLegAssessmentApi[] | null;
  stops: Array<{ stop: number; nights: Array<{ date: string; warning: string; url: string }> | null }> | null;
}

const VERDICTS: readonly string[] = ["go", "marginal", "no_go", "no_data"];
const NOTE_KINDS: readonly string[] = [
  "problem", "no_forecast", "not_routed", "past", "overnight", "no_window", "no_departure", "gap", "other",
];

function mapLegAssessment(l: PlanLegAssessmentApi): PlanLegAssessment {
  return {
    leg: l.leg,
    date: l.date,
    verdict: VERDICTS.includes(l.verdict) ? (l.verdict as LegVerdict) : null,
    limiting: l.limiting,
    note: l.note,
    // A kind this page does not know is still a note to read, as "other".
    noteKind: l.note_kind === "" ? "" : NOTE_KINDS.includes(l.note_kind) ? (l.note_kind as PlanNoteKind) : "other",
    depart: l.depart === "" ? null : l.depart,
    departSuggested: l.depart_suggested,
    arrive: l.arrive === "" ? null : l.arrive,
    hours: l.hours,
    speedKn: l.speed_kn,
    currentKn: l.current_kn,
    currentNote: l.current_note,
    notChecked: l.not_checked ?? [],
  };
}

export function mapAssessment(raw: PlanAssessmentApi): PlanAssessment {
  return {
    asOf: raw.as_of,
    forecastAsOf: raw.forecast_as_of === "" ? null : raw.forecast_as_of,
    problem: raw.problem,
    warningsError: raw.warnings_error,
    legs: (raw.legs ?? []).map(mapLegAssessment),
    stops: (raw.stops ?? []).map((s) => ({ stop: s.stop, nights: s.nights ?? [] })),
  };
}

/**
 * Everything about a plan that changes a verdict: places, vias, dates, nights
 * and departure times. A change to any of them asks again; the forecast's
 * refresh interval asks again on its own.
 */
const assessmentShape = (plan: Plan) =>
  JSON.stringify(
    plan.stops.map((s) => [s.name, s.lat, s.lon, s.arrive, s.nights, s.departTime, s.vias.map((v) => [v.name, v.lat, v.lon])]),
  );

/**
 * The plan's shape once it has held still for `delayMs`. Another plan is not an
 * edit and is taken at once.
 */
function useSettledShape(planId: number, shape: string, delayMs: number): string {
  const [settled, setSettled] = useState({ planId, shape });
  useEffect(() => {
    if (settled.planId === planId && settled.shape === shape) return;
    if (settled.planId !== planId) {
      setSettled({ planId, shape });
      return;
    }
    const timer = setTimeout(() => setSettled({ planId, shape }), delayMs);
    return () => clearTimeout(timer);
  }, [planId, shape, settled, delayMs]);
  return settled.planId === planId ? settled.shape : shape;
}

export function usePlanAssessment(plan: Plan | null) {
  const planId = plan?.id ?? 0;
  const shape = useMemo(() => (plan ? assessmentShape(plan) : ""), [plan]);
  const settled = useSettledShape(planId, shape, ASSESSMENT_DEBOUNCE_MS);
  const { data, error } = useApiQuery<PlanAssessment>({
    queryKey: [PLANS_PATH, planId, "assessment", settled],
    path: `${PLANS_PATH}/${planId}/assessment`,
    refetchInterval: FORECAST_REFRESH_SECONDS * 1000,
    enabled: plan !== null && plan.stops.length > 1,
    select: mapAssessment,
  });
  // What is on screen is the answer for the plan as it stands now, or nothing. From
  // the edit until the answer for the edited plan lands, `data` and `error` belong
  // to the shape before it, so neither is passed on. A refetch of the same shape
  // keeps its data: that answer is still the plan's.
  const current = settled === shape;
  const assessment = current ? (data ?? null) : null;
  const failure = current && error ? error.message || "Failed to check the plan against the forecast" : null;
  return {
    assessment,
    /** The check for the plan as it stands has not answered yet. */
    loading: plan !== null && assessment === null && failure === null,
    error: failure,
  };
}
