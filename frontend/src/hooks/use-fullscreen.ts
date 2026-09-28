import { useEffect, useState } from 'react'

export interface FullscreenState {
  /** `document.fullscreenEnabled === true` (MDN Fullscreen API). iPhone
   *  Safari has no Fullscreen API at all, so this is how the caller knows
   *  to leave the button out entirely rather than show one that will fail. */
  supported: boolean
  /** Derived from `document.fullscreenElement`, kept in sync via the
   *  `fullscreenchange` event so Esc or a browser chrome exit is reflected
   *  here too, not just a call to `exit()`. */
  isFullscreen: boolean
  /** Fullscreens the whole document (`document.documentElement`), not any
   *  single element - dialogs, popovers, tooltips, the Mate sheet and toasts
   *  all portal to `<body>` and would render invisibly under an element's
   *  own fullscreen. Per AGENTS.md's fallback policy this fails visibly: a
   *  rejection (denied permission, no user gesture, ...) is `console.error`d
   *  rather than swallowed. */
  enter: () => void
  /** Exits fullscreen, only if the document is currently in it. Rejection is
   *  `console.error`d, same as `enter()`. */
  exit: () => void
}

export function useFullscreen(): FullscreenState {
  const supported = document.fullscreenEnabled === true
  // Boolean(), not `!== null`: per spec `fullscreenElement` is always either
  // an Element or `null`, but a truthy check costs nothing and is what
  // actually holds in any environment that hasn't implemented the property
  // at all (it reads back `undefined`, not `null`, there).
  const [isFullscreen, setIsFullscreen] = useState(() => Boolean(document.fullscreenElement))

  useEffect(() => {
    function handleFullscreenChange() {
      setIsFullscreen(Boolean(document.fullscreenElement))
    }
    document.addEventListener('fullscreenchange', handleFullscreenChange)
    return () => document.removeEventListener('fullscreenchange', handleFullscreenChange)
  }, [])

  function enter() {
    document.documentElement.requestFullscreen().catch((error: unknown) => {
      console.error('Failed to enter full screen', error)
    })
  }

  function exit() {
    if (!document.fullscreenElement) return
    document.exitFullscreen().catch((error: unknown) => {
      console.error('Failed to exit full screen', error)
    })
  }

  return { supported, isFullscreen, enter, exit }
}
