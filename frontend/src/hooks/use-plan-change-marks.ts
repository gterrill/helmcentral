import { useEffect, useMemo, useRef, useState } from "react";

import { PLANS_PATH } from "@/hooks/use-plans";
import { apiRequest, fetchQuery, useApiQuery } from "@/lib/api-query";
import { errorMessage } from "@/lib/errors";
import {
  changeMarks,
  currentVerdicts,
  type ChangeMark,
  type LegVerdict,
  type PlanLegAssessment,
} from "@/lib/plans";

// Change marks (ADR 0288). The verdicts saved on the box at the previous visit
// are read once and kept as they were when this visit began: nothing refetches
// them, so the saving below never moves the marks. The legs that differ from them
// are marked ("was Go, now Marginal"). Then what is shown now is saved as the new
// snapshot, once per plan per visit, when the viewer can write and it differs.

interface SeenVerdicts {
  legs: Array<{ key: string; verdict: LegVerdict }>;
}

const sameVerdicts = (a: SeenVerdicts["legs"], b: SeenVerdicts["legs"]) =>
  a.length === b.length && a.every((s) => b.some((v) => v.key === s.key && v.verdict === s.verdict));

export function usePlanChangeMarks(planId: number, legs: PlanLegAssessment[] | null, canWrite: boolean) {
  // The store reads this once per mount and has no refresh for it: no interval, and
  // the saving below does not invalidate it.
  const queryKey = [PLANS_PATH, planId, "seen"];
  const path = `${PLANS_PATH}/${planId}/seen`;
  const { data: cached, error: readError } = useApiQuery<SeenVerdicts>({ queryKey, path });
  // On a return to the page inside the query cache window, `cached` is the answer
  // from the last visit, and the box has moved on since (that visit saved its own
  // snapshot). Marks and the save are decided only once a read made in this mount
  // has settled, so they never rest on the stale answer. The read is shared with
  // the one the query makes on mount.
  const [freshFor, setFreshFor] = useState<number | null>(null);
  useEffect(() => {
    let live = true;
    void fetchQuery([PLANS_PATH, planId, "seen"], `${PLANS_PATH}/${planId}/seen`).then(() => {
      if (live) setFreshFor(planId);
    });
    return () => {
      live = false;
    };
  }, [planId]);
  const seen = freshFor === planId && !readError ? cached : undefined;
  const marks = useMemo(() => (seen && legs ? changeMarks(seen.legs, legs) : NO_MARKS), [seen, legs]);

  const savedFor = useRef<number | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  useEffect(() => {
    if (!seen || !canWrite || savedFor.current === planId) return;
    const shown = currentVerdicts(legs ?? []);
    // Nothing to keep until a leg has a verdict.
    if (shown.length === 0) return;
    savedFor.current = planId;
    if (sameVerdicts(seen.legs, shown)) return;
    apiRequest(`${PLANS_PATH}/${planId}/seen`, { method: "PUT", json: { legs: shown } }).then(
      () => setSaveError(null),
      (err) => setSaveError(errorMessage(err)),
    );
  }, [seen, legs, canWrite, planId]);

  return { marks, error: readError ? errorMessage(readError) : saveError };
}

const NO_MARKS: ReadonlyMap<string, ChangeMark> = new Map();
