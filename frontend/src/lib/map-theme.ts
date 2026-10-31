import { useSyncExternalStore } from 'react'

// MapLibre paints with literal colours, not CSS classes, so a layer reads the
// app's own theme tokens at draw time. They follow day and night with the rest
// of the page and no map picks a colour of its own.

/** A theme token as a colour MapLibre can parse. A token that is not defined is an error. */
export function themeColour(name: string): string {
  const channels = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  if (!channels) throw new Error(`theme token ${name} is not defined`)
  // The tokens hold space-separated channels; MapLibre's colour parser wants commas.
  return `hsl(${channels.split(/\s+/).join(', ')})`
}

function subscribeTheme(onChange: () => void) {
  const observer = new MutationObserver(onChange)
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  return () => observer.disconnect()
}
const isDarkNow = () => document.documentElement.classList.contains('dark')

/** Whether the page is dark now; the map re-renders, and re-reads its tokens, when it changes. */
export function useIsDarkTheme(): boolean {
  return useSyncExternalStore(subscribeTheme, isDarkNow, () => false)
}
