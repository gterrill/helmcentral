import { useEffect, useRef, useState } from 'react'

import { resolveAutoTheme } from '@/lib/auto-theme'

const DARK_MODE_KEY = 'ui.darkMode'
const AUTO_LAST_KEY = 'ui.darkMode.autoLast'
const AUTO_RECHECK_MS = 60_000

export type ThemeMode = 'day' | 'night' | 'auto'

export interface AutoThemeSun {
  /** forecast[0].sunriseTime, the string the Clock tile shows. */
  sunriseTime: string | null
  sunsetTime: string | null
  /** The vessel's IANA zone, which the sunrise and sunset strings are written in. */
  timeZone: string | undefined
}

export interface ThemeState {
  mode: ThemeMode
  /**
   * In auto mode: when the theme in effect ends ("6:12 AM"), or null when
   * sunrise and sunset are unavailable and auto cannot decide.
   */
  autoUntil: string | null
}

function applyDarkMode(isDark: boolean) {
  document.documentElement.classList.toggle('dark', isDark)
}

function systemPrefersDark(): boolean {
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false
}

function readStoredMode(): ThemeMode | null {
  const stored = globalThis.localStorage?.getItem(DARK_MODE_KEY)
  if (stored === 'auto') return 'auto'
  if (stored === null || stored === undefined) return null
  return stored === 'true' ? 'night' : 'day'
}

/** The theme Auto last resolved from sunrise and sunset in this browser, if any. */
function readAutoLast(): boolean | null {
  const stored = globalThis.localStorage?.getItem(AUTO_LAST_KEY)
  return stored === 'true' ? true : stored === 'false' ? false : null
}

const NEXT_MODE: Record<ThemeMode, ThemeMode> = { day: 'night', night: 'auto', auto: 'day' }
const STORED_VALUE: Record<ThemeMode, string> = { day: 'false', night: 'true', auto: 'auto' }

/**
 * The stored theme: Day, Night or Auto. Returns the theme in effect, a
 * function that steps Day, Night, Auto, Day, and the mode with auto's status.
 * Auto picks night between sunset and sunrise from `sun`; with no usable
 * sunrise and sunset it keeps the theme already showing rather than guessing.
 */
export function useDarkMode(sun?: AutoThemeSun): [boolean, () => void, ThemeState] {
  // null = nothing stored yet: follow the system preference until the operator
  // first presses the button.
  const [mode, setMode] = useState<ThemeMode | null>(readStoredMode)
  const [, setRecheck] = useState(0)
  const held = useRef<boolean>(
    mode === 'day' ? false
      : mode === 'night' ? true
      : mode === 'auto' ? (readAutoLast() ?? systemPrefersDark())
      : systemPrefersDark(),
  )

  useEffect(() => {
    if (mode !== 'auto') return
    const id = setInterval(() => setRecheck((n) => n + 1), AUTO_RECHECK_MS)
    return () => clearInterval(id)
  }, [mode])

  const resolved = mode === 'auto' && sun
    ? resolveAutoTheme({ now: new Date(), ...sun })
    : null

  const isDark = mode === 'day' ? false
    : mode === 'night' ? true
    : mode === 'auto' ? (resolved?.isDark ?? held.current)
    : held.current

  useEffect(() => {
    held.current = isDark
  }, [isDark])

  const resolvedDark = resolved?.isDark
  useEffect(() => {
    // Remember what Auto decided so a reload without sun times keeps it.
    if (resolvedDark !== undefined) {
      globalThis.localStorage?.setItem(AUTO_LAST_KEY, String(resolvedDark))
    }
  }, [resolvedDark])

  useEffect(() => {
    // Initial paint for the stored or system theme. Later changes are applied
    // by cycle() and by App, which owns the wall display's forced dark theme.
    applyDarkMode(isDark)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function cycle() {
    const current: ThemeMode = mode ?? (isDark ? 'night' : 'day')
    const next = NEXT_MODE[current]
    globalThis.localStorage?.setItem(DARK_MODE_KEY, STORED_VALUE[next])
    held.current = isDark
    const nextDark = next === 'day' ? false
      : next === 'night' ? true
      : (sun ? resolveAutoTheme({ now: new Date(), ...sun })?.isDark : undefined) ?? isDark
    applyDarkMode(nextDark)
    setMode(next)
  }

  return [
    isDark,
    cycle,
    { mode: mode ?? (isDark ? 'night' : 'day'), autoUntil: resolved?.until ?? null },
  ]
}
