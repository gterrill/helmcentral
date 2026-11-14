import { useMemo } from "react";

import { FORECAST_REFRESH_SECONDS } from "@/config/app-config";
import { ASSESSMENT_DEBOUNCE_MS, assessmentShape, useSettledShape } from "@/hooks/use-plan-shape";
import { PLANS_PATH } from "@/hooks/use-plans";
import { invalidateQueries, useApiQuery } from "@/lib/api-query";
import { mapLegResult, type LegResultApi, type Plan, type PlanAssessment, type PlanLegAssessment } from "@/lib/plans";

export { ASSESSMENT_DEBOUNCE_MS };

// The plan's verdicts, worked out on the server with Mate's passage rules and
// the operator's passage limits (ADR 0286). Asked again on the forecast's own
// refresh interval, so the "as of" time moves with the forecast, and again half
// a second after the plan stops changing. Nothing from before an edit is shown:
// until the answer for the edited plan lands there is no assessment, and the
// table reads "…" where a verdict goes.

interface PlanLegAssessmentApi extends LegResultApi {
  key?: string;
  progress?: string;
  still_in_port?: boolean;
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
  progress_error?: string;
  slack?: { days: number; stop: number; behind_days: number } | null;
  legs: PlanLegAssessmentApi[] | null;
  stops: Array<{ stop: number; nights: Array<{ date: string; warning: string; url: string }> | null }> | null;
}

function mapLegAssessment(l: PlanLegAssessmentApi): PlanLegAssessment {
  return {
    ...mapLegResult(l),
    key: l.key ?? "",
    progress: l.progress === "done" || l.progress === "skipped" ? l.progress : "",
    stillInPort: l.still_in_port === true,
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
    progressError: raw.progress_error ?? "",
    slack: raw.slack ? { days: raw.slack.days, stop: raw.slack.stop, behindDays: raw.slack.behind_days } : null,
    legs: (raw.legs ?? []).map(mapLegAssessment),
    stops: (raw.stops ?? []).map((s) => ({ stop: s.stop, nights: s.nights ?? [] })),
  };
}

/**
 * Asks every mounted plan assessment, and every mounted slip price, again,
 * whatever its plan: the verdicts depend on the passage limits, which are saved
 * outside the plan. The plan queries that are neither are left alone.
 */
export function invalidatePlanAssessments(): Promise<void> {
  return invalidateQueries([PLANS_PATH], (key) => key[2] !== "assessment" && key[2] !== "slips");
}

export function usePlanAssessment(plan: Plan | null) {
  const planId = plan?.id ?? 0;
  const shape = useMemo(() => (plan ? assessmentShape(plan) : ""), [plan]);
  const settled = useSettledShape(planId, shape, ASSESSMENT_DEBOUNCE_MS);
  const { data, error } = useApiQuery<PlanAssessment>({
    queryKey: [PLANS_PATH, planId, "assessment", settled],
    path: `${PLANS_PATH}/${planId}/assessment`,
    refetchInterval: FORECAST_REFRESH_SECONDS * 1000,
    // The verdicts follow the operator's passage limits, which are saved elsewhere:
    // a mount always asks again (the query store revalidates on mount), and so does
    // coming back to the window.
    refetchOnFocus: true,
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
    /** The plan's shape the answer is for: what the slips are asked for too. */
    shape: settled,
    /** The check for the plan as it stands has not answered yet. */
    loading: plan !== null && assessment === null && failure === null,
    error: failure,
  };
}
