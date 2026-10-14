import { useApiQuery } from '@/lib/api-query'
import type { EngineProfile, EngineProfileProblem } from '@/lib/engine-profiles'

interface ProfileList {
  profiles?: unknown
  problems?: unknown
}

/**
 * Engine profiles are static drop-in files (ADR 0053), so this fetches once
 * when the dialog opens rather than polling — mirroring useSignalKPaths.
 *
 * `problems` carries files that failed to load. A bad drop-in file must not be
 * silent, so the dialog shows them.
 */
const NONE: EngineProfile[] = []
const NO_PROBLEMS: EngineProfileProblem[] = []

// A failed fetch surfaces as an error with no profiles, so a 500 never renders
// like an empty profiles directory.
export function useEngineProfiles(enabled: boolean) {
  const path = '/api/engine-profiles'
  const { data, isLoading, error } = useApiQuery<{
    profiles: EngineProfile[]
    problems: EngineProfileProblem[]
  }>({
    queryKey: [path],
    path,
    enabled,
    select: (raw: ProfileList | null) => ({
      profiles: Array.isArray(raw?.profiles) ? (raw.profiles as EngineProfile[]) : NONE,
      problems: Array.isArray(raw?.problems) ? (raw.problems as EngineProfileProblem[]) : NO_PROBLEMS,
    }),
  })
  return {
    profiles: error ? NONE : (data?.profiles ?? NONE),
    problems: error ? NO_PROBLEMS : (data?.problems ?? NO_PROBLEMS),
    loading: isLoading,
    error: error?.message ?? null,
  }
}
