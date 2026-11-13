import { useEffect, useMemo, useRef, useState } from "react";

import { FORECAST_REFRESH_SECONDS } from "@/config/app-config";
import { PLANS_PATH } from "@/hooks/use-plans";
import { useApiQuery } from "@/lib/api-query";
import type { LegVerdict, Plan, PlanAssessment, PlanLegAssessment, PlanNoteKind } from "@/lib/plans";

// The plan's verdicts, worked out on the server with Mate's passage rules and
// the operator's passage limits (ADR 0286). Asked again on the forecast's own
// refresh interval, so the "as of" time moves with the forecast, and again half
// a second after the plan stops changing. The last assessment stays on screen
// while the next one is worked out, marked as updating, and only for the legs
// that still run between the same stops.

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
  const { data, error, isLoading } = useApiQuery<PlanAssessment>({
    queryKey: [PLANS_PATH, planId, "assessment", settled],
    path: `${PLANS_PATH}/${planId}/assessment`,
    refetchInterval: FORECAST_REFRESH_SECONDS * 1000,
    enabled: plan !== null && plan.stops.length > 1,
    select: mapAssessment,
  });
  // The last answer for this plan stays until the next lands: a question for a
  // changed plan has no answer yet, and the table must not blank in between. Its
  // legs are kept only where they still line up with the plan, and it is marked
  // as updating until the answer for the plan as it now stands arrives.
  const last = useRef<{ planId: number; assessment: PlanAssessment; shape: string } | null>(null);
  if (data) last.current = { planId, assessment: data, shape: settled };
  const source = last.current?.planId === planId ? last.current : null;
  const sourceAssessment = source?.assessment ?? null;
  const sourceShape = source?.shape ?? "";
  const assessment = useMemo(
    () => (sourceAssessment ? carryOver(sourceAssessment, sourceShape, shape) : null),
    [sourceAssessment, sourceShape, shape],
  );
  return {
    assessment,
    loading: isLoading,
    /** The verdicts on screen are for the plan as it was before an edit. */
    updating: assessment !== null && (data === undefined || settled !== shape),
    error: error ? error.message || "Failed to check the plan against the forecast" : null,
  };
}

/** The stop names of a plan's shape, in order. */
function stopNames(shape: string): string[] {
  try {
    return (JSON.parse(shape) as unknown[][]).map((s) => String(s[0]));
  } catch {
    return [];
  }
}

/**
 * The part of an assessment still true of the plan as it now stands: a leg stays
 * only while it runs between the same two stops at the same index, a stop's
 * warnings only while the same place is at that index. An assessment for the
 * plan as it is comes back whole.
 */
function carryOver(a: PlanAssessment, fromShape: string, toShape: string): PlanAssessment {
  if (fromShape === toShape) return a;
  const was = stopNames(fromShape);
  const now = stopNames(toShape);
  const same = (i: number) => was[i] !== undefined && was[i] === now[i];
  return {
    ...a,
    legs: a.legs.filter((l) => same(l.leg) && same(l.leg + 1)),
    stops: a.stops.filter((s) => same(s.stop)),
  };
}
