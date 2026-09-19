import { useEffect, useRef } from 'react'

export interface DisplayRemoteActions {
  /** Step forward and restart the dwell (useDisplayRotation's `next`). */
  next: () => void
  /** Step backward and restart the dwell (useDisplayRotation's `previous`). */
  previous: () => void
  /** Flip between paused and running - the caller decides which
   * (`paused ? resume() : pause()`), since this hook only dispatches named
   * actions and has no idea whether the rotation is currently paused. */
  togglePause: () => void
  /** Explicitly resume (useDisplayRotation's `resume`), for a key that only
   * ever means "get back to the rotation" and never "pause it". */
  resume: () => void
}

type RemoteAction = keyof DisplayRemoteActions

interface RemoteKeyMatch {
  /** KeyboardEvent.key values that trigger this action. */
  keys?: string[]
  /** KeyboardEvent.code values - kept alongside `keys` for Space, whose
   * `key` is the easy-to-miss literal " " character. */
  codes?: string[]
  /** KeyboardEvent.keyCode aliases, for a key whose `key`/`code` can't be
   * trusted to identify it at all - webOS's Back button is exactly this. */
  keyCodes?: number[]
}

/**
 * GUESS pending the Phase 0 probe capture on the real C5 hardware (ADR 0110
 * §7, docs/adr/0110-wall-displays-are-records.md) - what a Magic Remote
 * actually emits in webOS's browser isn't knowable from a dev machine. Once
 * that capture exists, correct THIS TABLE ONLY against the real key/code/
 * keyCode readout; useDisplayRemote's own dispatch logic below (match an
 * event against every action's entry, preventDefault, call the first match)
 * shouldn't need to change.
 */
export const DISPLAY_REMOTE_KEYS: Record<RemoteAction, RemoteKeyMatch> = {
  next: { keys: ['ArrowRight', 'PageDown', 'MediaTrackNext'] },
  previous: { keys: ['ArrowLeft', 'PageUp', 'MediaTrackPrevious'] },
  togglePause: { keys: ['Enter', ' ', 'MediaPlayPause'], codes: ['Space'] },
  // webOS's Back key: 461 is the widely-reported legacy keyCode for it, but
  // unverified against this project's actual hardware - see the module
  // comment above.
  resume: { keys: ['Escape'], keyCodes: [461] },
}

function matchesAction(event: KeyboardEvent, match: RemoteKeyMatch): boolean {
  if (match.keys?.includes(event.key)) return true
  if (match.codes?.includes(event.code)) return true
  if (match.keyCodes?.includes(event.keyCode)) return true
  return false
}

/**
 * A window `keydown` listener for the wall's remote-control keys (ADR 0110
 * §5b) - a Magic Remote, a plain keyboard and an air mouse all land here
 * alike, which is also why this isn't the Gamepad API (a real gamepad needs
 * pairing; a remote is not one anyway).
 *
 * Always on, with no `enabled` flag: arrow keys and Space do nothing on a
 * dashboard page today, and the flybridge ODROID has no input device at
 * all, so there is nothing an always-on listener could break. These are
 * GLOBAL handlers, deliberately not focus navigation - there is no tab
 * order or focus ring to manage, and the wall's tiles stay non-interactive.
 *
 * `preventDefault()` on every handled key, so Space never scrolls the page
 * out from under the board.
 */
export function useDisplayRemote(actions: DisplayRemoteActions): void {
  const actionsRef = useRef(actions)
  actionsRef.current = actions

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      for (const action of Object.keys(DISPLAY_REMOTE_KEYS) as RemoteAction[]) {
        if (matchesAction(event, DISPLAY_REMOTE_KEYS[action])) {
          event.preventDefault()
          actionsRef.current[action]()
          return
        }
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [])
}
