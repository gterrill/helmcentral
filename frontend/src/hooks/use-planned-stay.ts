import { useCallback } from "react";

import { apiRequest, setQueryData, useApiQuery } from "@/lib/api-query";
import type { PlannedStay } from "@/lib/stay";

// The stay the operator is booked into: one place and its dates, kept on the
// server so every device sees it. The server drops it the day after departure,
// judged by the date at the place itself.

export const PLANNED_STAY_PATH = "/api/planned-stay";

interface PlannedStayEnvelope {
  stay: PlannedStay | null;
}

const FIVE_MINUTES_MS = 5 * 60_000;

export function usePlannedStay() {
  const { data, error, isLoading, refetch } = useApiQuery<PlannedStayEnvelope>({
    queryKey: [PLANNED_STAY_PATH],
    path: PLANNED_STAY_PATH,
    refetchInterval: FIVE_MINUTES_MS,
  });

  const save = useCallback(async (stay: PlannedStay): Promise<PlannedStay> => {
    const saved = await apiRequest<PlannedStayEnvelope>(PLANNED_STAY_PATH, {
      method: "PUT",
      json: stay,
    });
    setQueryData<PlannedStayEnvelope>([PLANNED_STAY_PATH], saved);
    return saved.stay as PlannedStay;
  }, []);

  const clear = useCallback(async (): Promise<void> => {
    const cleared = await apiRequest<PlannedStayEnvelope>(PLANNED_STAY_PATH, {
      method: "DELETE",
    });
    setQueryData<PlannedStayEnvelope>([PLANNED_STAY_PATH], cleared);
  }, []);

  return {
    stay: data?.stay ?? null,
    loading: isLoading,
    error: error ? error.message || "Failed to load the planned stay" : null,
    save,
    clear,
    refetch,
  };
}
