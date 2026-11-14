import { useEffect, useState } from "react";

import type { Plan } from "@/lib/plans";

// What the plan's server-side checks (verdicts, slips) wait on: the part of a
// plan that changes an answer, settled once the operator has stopped editing.

/** How long the plan must hold still before the verdicts are asked for again. */
export const ASSESSMENT_DEBOUNCE_MS = 500;

/**
 * Everything about a plan that changes a verdict: places, vias, dates, nights,
 * departure times and arrive-bys. A change to any of them asks again.
 */
export const assessmentShape = (plan: Plan) =>
  JSON.stringify(
    plan.stops.map((s) => [
      s.name, s.lat, s.lon, s.arrive, s.nights, s.departTime, s.arriveBy, s.vias.map((v) => [v.name, v.lat, v.lon]),
    ]),
  );

/**
 * The plan's shape once it has held still for `delayMs`. Another plan is not an
 * edit and is taken at once.
 */
export function useSettledShape(planId: number, shape: string, delayMs: number): string {
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
