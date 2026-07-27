import { useEffect, useState } from 'react'

// Tailwind 3.4's stock `sm`/`md`/`lg` screens. `tailwind.config.ts` has no `screens`
// override, so these are the values Tailwind's own `sm:`/`md:`/`lg:` prefixes compile
// to — keep them in sync with Tailwind if that ever changes.
export const BREAKPOINTS = { sm: 640, md: 768, lg: 1024 } as const

// matchMedia only — never `window.innerWidth`, which doesn't fire a DOM event on its
// own and requires polling or a resize listener to react to. `matchMedia`'s `change`
// event is the platform's built-in notification for exactly this.
export function useMinWidth(px: number): boolean {
  const query = `(min-width: ${px}px)`

  const [matches, setMatches] = useState(() =>
    typeof window !== 'undefined' ? window.matchMedia(query).matches : false,
  )

  useEffect(() => {
    const mql = window.matchMedia(query)
    const onChange = (event: MediaQueryListEvent) => setMatches(event.matches)
    setMatches(mql.matches)
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [query])

  return matches
}
