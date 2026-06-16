import { useState } from 'react'

const THEME_KEY = 'ui.theme'

export type Theme = 'marine' | 'classic' | 'clean'

function applyTheme(theme: Theme) {
  if (theme === 'marine') {
    delete document.documentElement.dataset.theme
  } else {
    document.documentElement.dataset.theme = theme
  }
}

export function useTheme(): [Theme, (theme: Theme) => void] {
  const [theme, setThemeState] = useState<Theme>(() => {
    const stored = globalThis.localStorage?.getItem(THEME_KEY) as Theme | null
    const initial = stored ?? 'marine'
    applyTheme(initial)
    return initial
  })

  function setTheme(next: Theme) {
    globalThis.localStorage?.setItem(THEME_KEY, next)
    applyTheme(next)
    setThemeState(next)
  }

  return [theme, setTheme]
}
