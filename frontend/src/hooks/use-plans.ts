import { useCallback, useMemo } from "react";

import { apiRequest, refetchQuery, useApiQuery } from "@/lib/api-query";
import type {
  Plan,
  PlanInput,
  PlanLeg,
  PlanStatus,
  PlanStop,
  PlanVia,
} from "@/lib/plans";

// Passage plans, kept on the server so every device sees them. The server
// owns the date rule and works out each plan's status at its first stop.

export const PLANS_PATH = "/api/plans";

const FIVE_MINUTES_MS = 5 * 60_000;

interface PlanViaApi {
  name: string;
  lat: number;
  lon: number;
}

interface PlanStopApi {
  name: string;
  lat: number;
  lon: number;
  arrive: string;
  nights: number;
  depart: string;
  vias: PlanViaApi[] | null;
  depart_time: string;
  timezone: string;
}

interface PlanApi {
  id: number;
  name: string;
  stops: PlanStopApi[];
  status: PlanStatus;
  first_date: string;
  last_date: string;
}

interface PlansEnvelope {
  plans: PlanApi[];
  current_id: number | null;
}

interface PlanLegApi {
  from: number;
  to: number;
  distance_nm: number;
  course_deg: number;
  route: Array<[number, number]> | null;
  note?: string;
  error?: string;
  reason?: string;
  error_kind?: string;
  error_stop?: string;
  moved?: Array<{ stop: string; nm: number }>;
}

function mapStop(s: PlanStopApi): PlanStop {
  return {
    name: s.name,
    lat: s.lat,
    lon: s.lon,
    arrive: s.arrive,
    nights: s.nights,
    depart: s.depart,
    vias: (s.vias ?? []).map((v): PlanVia => ({ name: v.name, lat: v.lat, lon: v.lon })),
    departTime: s.depart_time,
    timezone: s.timezone,
  };
}

export function mapPlan(p: PlanApi): Plan {
  return {
    id: p.id,
    name: p.name,
    stops: p.stops.map(mapStop),
    status: p.status,
    firstDate: p.first_date,
    lastDate: p.last_date,
  };
}

export function planToApi(input: PlanInput) {
  return {
    name: input.name,
    stops: input.stops.map((s) => ({
      name: s.name,
      lat: s.lat,
      lon: s.lon,
      arrive: s.arrive,
      nights: s.nights,
      vias: s.vias,
      depart_time: s.departTime,
    })),
  };
}

export function mapLeg(l: PlanLegApi): PlanLeg {
  const error = l.error ?? "";
  return {
    from: l.from,
    to: l.to,
    // A leg that failed to route has no figure; 0 would read as "no distance".
    distanceNm: error ? null : l.distance_nm,
    courseDeg: error ? null : l.course_deg,
    route: l.route ?? [],
    note: l.note ?? "",
    error,
    reason: l.reason ?? "",
    errorKind: l.error_kind ?? "",
    errorStop: l.error_stop ?? "",
    moved: l.moved ?? [],
  };
}

export function usePlans() {
  const { data, error, isLoading, refetch } = useApiQuery<PlansEnvelope>({
    queryKey: [PLANS_PATH],
    path: PLANS_PATH,
    refetchInterval: FIVE_MINUTES_MS,
  });

  const plans = useMemo(() => (data?.plans ?? []).map(mapPlan), [data]);

  const reload = useCallback(() => refetchQuery([PLANS_PATH], PLANS_PATH), []);

  const create = useCallback(
    async (input: PlanInput): Promise<Plan> => {
      const saved = await apiRequest<PlanApi>(PLANS_PATH, {
        method: "POST",
        json: planToApi(input),
      });
      await reload();
      return mapPlan(saved);
    },
    [reload],
  );

  const update = useCallback(
    async (id: number, input: PlanInput): Promise<Plan> => {
      const saved = await apiRequest<PlanApi>(`${PLANS_PATH}/${id}`, {
        method: "PUT",
        json: planToApi(input),
      });
      await reload();
      return mapPlan(saved);
    },
    [reload],
  );

  /** Change one stop's arrival or nights; the server shifts every later date. */
  const patchStop = useCallback(
    async (
      id: number,
      index: number,
      change: { arrive?: string; nights?: number },
    ): Promise<Plan> => {
      const saved = await apiRequest<PlanApi>(
        `${PLANS_PATH}/${id}/stops/${index}`,
        { method: "PATCH", json: change },
      );
      await reload();
      return mapPlan(saved);
    },
    [reload],
  );

  const remove = useCallback(
    async (id: number): Promise<void> => {
      await apiRequest(`${PLANS_PATH}/${id}`, { method: "DELETE" });
      await reload();
    },
    [reload],
  );

  return {
    plans,
    currentId: data?.current_id ?? null,
    loading: isLoading,
    error: error ? error.message || "Failed to load the plans" : null,
    create,
    update,
    patchStop,
    remove,
    refetch,
  };
}

/**
 * The routed legs of one plan. The key carries the stops' places so an edit
 * to a stop or a via asks again; dates and nights do not change a route.
 */
const selectLegs = (raw: { legs: PlanLegApi[] }) => raw.legs.map(mapLeg);

/** The stops' places and their vias, as one string: the part of a plan a route depends on. */
const routeShape = (plan: Plan) =>
  plan.stops.map((s) => `${s.lat},${s.lon}${s.vias.map((v) => `;${v.lat},${v.lon}`).join("")}`).join("|");

export function usePlanLegs(plan: Plan | null) {
  const shape = useMemo(() => (plan ? routeShape(plan) : ""), [plan]);
  const { data, error, isLoading } = useApiQuery<PlanLeg[]>({
    queryKey: [PLANS_PATH, plan?.id ?? 0, "legs", shape],
    path: `${PLANS_PATH}/${plan?.id ?? 0}/legs`,
    enabled: plan !== null && plan.stops.length > 1,
    select: selectLegs,
  });
  return {
    legs: data ?? [],
    loading: isLoading,
    error: error ? error.message || "Failed to route the plan" : null,
  };
}
