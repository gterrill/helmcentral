import { useState } from 'react'

const DARK_MODE_KEY = 'ui.darkMode'

function applyDarkMode(isDark: boolean) {
  document.documentElement.classList.toggle('dark', isDark)
}

export function useDarkMode(): [boolean, () => void] {
  const [isDark, setIsDark] = useState(() => {
    const stored = globalThis.localStorage?.getItem(DARK_MODE_KEY)
    const initial = stored === null
      ? window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false
      : stored === 'true'
    applyDarkMode(initial)
    return initial
  })

  function toggle() {
    const next = !isDark
    globalThis.localStorage?.setItem(DARK_MODE_KEY, String(next))
    applyDarkMode(next)
    setIsDark(next)
  }

  return [isDark, toggle]
}
