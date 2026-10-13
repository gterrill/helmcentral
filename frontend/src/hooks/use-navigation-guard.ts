import { useLayoutEffect, useRef, useSyncExternalStore } from 'react'

import { useOptionalAppRouter, useAppRouter } from '@/hooks/use-app-location'
import type { NavigationGuard } from '@/lib/app-router'

// ADR 0171: a panel's say in whether the app location may change. The panel
// that owns the unsaved state registers a guard while it holds some, and the
// router asks it, with the move in hand, before it goes anywhere.
//
// Pass null when there is nothing to protect: the guard is then not
// registered at all, which is also how "is there unsaved work" is answered
// for the update reload and the sidebar's unsaved dot. The guard unregisters
// when the panel unmounts, so nothing has to clear a stale dirty flag.
//
// Outside an App (a component rendered on its own) there is no router to
// register with and the hook does nothing.
export function useNavigationGuard(guard: NavigationGuard | null): void {
  const router = useOptionalAppRouter()
  const latest = useRef<NavigationGuard | null>(guard)
  useLayoutEffect(() => {
    latest.current = guard
  })
  const active = guard !== null
  // A layout effect, so the guard is live before any same-tick navigate (a
  // click handler or a child's layout effect that runs in this commit).
  useLayoutEffect(() => {
    if (!router || !active) return
    return router.registerGuard(() => latest.current)
  }, [router, active])
}

/** Whether a guard with this owner is registered, i.e. that panel holds unsaved work. */
export function useHasNavigationGuard(owner: string): boolean {
  const router = useAppRouter()
  return useSyncExternalStore(router.subscribe, () => router.getSnapshot().guardOwners.includes(owner))
}
