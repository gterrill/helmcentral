import { apiRequest, invalidateQueries, useApiQuery } from '@/lib/api-query'
import type { EngineProfile, EngineProfileProblem } from '@/lib/engine-profiles'

interface ProfileList {
  profiles?: unknown
  problems?: unknown
}

const NONE: EngineProfile[] = []
const NO_PROBLEMS: EngineProfileProblem[] = []

// A failed fetch surfaces as an error with no profiles, so a 500 never renders
// like an empty profiles directory.
export function useEquipmentProfiles(enabled: boolean) {
  const path = '/api/equipment-profiles'
  const { data, isLoading, error, refetch } = useApiQuery<{
    profiles: EngineProfile[]
    problems: EngineProfileProblem[]
  }>({
    queryKey: [path],
    path,
    enabled,
    structuralSharing: true,
    select: (raw: ProfileList | null) => ({
      profiles: Array.isArray(raw?.profiles) ? (raw.profiles as EngineProfile[]) : NONE,
      problems: Array.isArray(raw?.problems) ? (raw.problems as EngineProfileProblem[]) : NO_PROBLEMS,
    }),
  })
  return {
    profiles: error ? NONE : (data?.profiles ?? NONE),
    problems: error ? NO_PROBLEMS : (data?.problems ?? NO_PROBLEMS),
    loading: isLoading,
    error: error?.message ?? null, reload: refetch,
  }
}

/**
 * Saves a profile as it stands. A tile's zone editor uses it: zones live on the
 * profile, so editing them from a tile is a profile edit.
 */
export async function saveEquipmentProfile(profile: EngineProfile): Promise<void> {
  await apiRequest(`/api/equipment-profiles/${encodeURIComponent(profile.id)}`, { method: 'PUT', json: profile })
  await invalidateQueries(['/api/equipment-profiles'])
  await invalidateQueries(['/api/engine-profiles'])
}
