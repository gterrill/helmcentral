import { createContext, useContext, useMemo, useSyncExternalStore } from 'react'

import type { AppLocation } from '@/lib/app-location'
import type { AppRouter, NavigateOptions, ResumablePanel } from '@/lib/app-router'

// ADR 0171: React's way into the router. The router is a plain object
// (lib/app-router.ts); this file is the context that carries it and the
// useSyncExternalStore reads, in the same style as hooks/use-vessel-identity.ts.

export const AppRouterContext = createContext<AppRouter | null>(null)

/** The router itself, for the few callers that need more than a location. */
export function useAppRouter(): AppRouter {
  const router = useContext(AppRouterContext)
  if (router === null) throw new Error('useAppRouter needs an AppRouterContext provider (App mounts one)')
  return router
}

/** Like useAppRouter, but null outside an App. Leaf components that register a
 * navigation guard are rendered on their own in tests and stories. */
export function useOptionalAppRouter(): AppRouter | null {
  return useContext(AppRouterContext)
}

/** What the app is showing. */
export function useAppLocation(): AppLocation {
  const router = useAppRouter()
  return useSyncExternalStore(router.subscribe, () => router.getSnapshot().location)
}

export interface Navigator {
  (target: AppLocation, options?: NavigateOptions): boolean
  /** Moves to a panel at the place it was last left (its start page the first
   * time, and always for Mate), through the guards. */
  toPanel: (panel: ResumablePanel, options?: NavigateOptions) => boolean
}

/** Stable across renders. Returns false when a guard held the move. */
export function useNavigate(): Navigator {
  const router = useAppRouter()
  return useMemo(
    () => Object.assign(
      (target: AppLocation, options?: NavigateOptions) => router.navigate(target, options),
      { toPanel: (panel: ResumablePanel, options?: NavigateOptions) => router.navigate(router.resume(panel), options) },
    ),
    [router],
  )
}
