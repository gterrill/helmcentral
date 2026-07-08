import { useState } from 'react'

const ACTIVE_DASHBOARD_PAGE_KEY = 'dashboard.activePageId'

export function useActiveDashboardPageId(): [string | null, (id: string | null) => void] {
  const [id, setIdState] = useState<string | null>(
    () => globalThis.localStorage?.getItem(ACTIVE_DASHBOARD_PAGE_KEY) ?? null,
  )

  function setId(next: string | null) {
    if (next === null) {
      globalThis.localStorage?.removeItem(ACTIVE_DASHBOARD_PAGE_KEY)
    } else {
      globalThis.localStorage?.setItem(ACTIVE_DASHBOARD_PAGE_KEY, next)
    }
    setIdState(next)
  }

  return [id, setId]
}
