import { useSyncExternalStore } from 'react'

const QUERY = '(prefers-reduced-motion: reduce)'
const supported = () => typeof window !== 'undefined' && typeof window.matchMedia === 'function'

function subscribe(onChange: () => void) {
  if (!supported()) return () => {}
  const query = window.matchMedia(QUERY)
  query.addEventListener('change', onChange)
  return () => query.removeEventListener('change', onChange)
}

/** The operator's reduced-motion setting, live. A browser without media
 * queries (a bare test DOM) reports no preference. */
export function usePrefersReducedMotion(): boolean {
  return useSyncExternalStore(subscribe, () => supported() && window.matchMedia(QUERY).matches, () => false)
}
