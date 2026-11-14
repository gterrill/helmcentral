import { FORECAST_REFRESH_SECONDS } from "@/config/app-config";
import { PLANS_PATH } from "@/hooks/use-plans";
import { useApiQuery } from "@/lib/api-query";
import { mapLegResult, type LegResultApi, type PlanSlips } from "@/lib/plans";

// What slipping the plan a day at a time meets (ADR 0288). Asked apart from the
// assessment: pricing the slips re-judges the plan once for each, and the table
// must not wait for that. Asked only for a plan that has slack to spend, so the
// caller mounts it for that case alone.

interface PlanSlipsApi {
  problem: string;
  capped: boolean;
  slips: Array<LegResultApi & { days: number; late_by_days: number }> | null;
}

export function mapSlips(raw: PlanSlipsApi): PlanSlips {
  return {
    problem: raw.problem,
    capped: raw.capped,
    slips: (raw.slips ?? []).map((s) => ({ ...mapLegResult(s), days: s.days, lateByDays: s.late_by_days })),
  };
}

/**
 * The slips of a plan. `shape` is the plan's settled shape (the assessment's
 * own), so nothing from before an edit is shown and a plan that is still being
 * edited is not asked. The poll follows the forecast's refresh.
 */
export function usePlanSlips(planId: number, shape: string) {
  const { data, error } = useApiQuery<PlanSlips>({
    queryKey: [PLANS_PATH, planId, "slips", shape],
    path: `${PLANS_PATH}/${planId}/slips`,
    refetchInterval: FORECAST_REFRESH_SECONDS * 1000,
    select: mapSlips,
  });
  const slips = data ?? null;
  const failure = error ? error.message || "Failed to price the slips" : null;
  return {
    slips,
    loading: slips === null && failure === null,
    error: failure,
  };
}
