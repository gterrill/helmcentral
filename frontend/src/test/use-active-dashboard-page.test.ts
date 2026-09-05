/**
 * Covers the optional `initialId` App.tsx passes through from a deep link
 * (ADR 0074, e.g. `/dashboard/<pageId>`) — it must win over whatever is
 * already in localStorage on mount, and be written through so the linked
 * page becomes the remembered one for next time. Reconciliation against an
 * unknown/deleted page id (already covered for the no-initialId case
 * elsewhere) must hold for this path too.
 */
import { describe, it, expect, beforeEach } from 'vitest'
import { renderHook } from '@testing-library/react'
import { useActiveDashboardPageId } from '@/hooks/use-active-dashboard-page'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'

const ACTIVE_DASHBOARD_PAGE_KEY = 'dashboard.activePageId'

function page(id: string): DashboardPage {
  return { id, name: id, widgets: [], created_at: '', updated_at: '' }
}

describe('useActiveDashboardPageId initialId', () => {
  beforeEach(() => {
    globalThis.localStorage?.clear()
  })

  it('wins over the stored id and is written through to localStorage', () => {
    globalThis.localStorage?.setItem(ACTIVE_DASHBOARD_PAGE_KEY, 'p1')
    const pages = [page('p1'), page('p2')]

    const { result } = renderHook(() => useActiveDashboardPageId(pages, 'p2'))

    expect(result.current[0]).toBe('p2')
    expect(globalThis.localStorage?.getItem(ACTIVE_DASHBOARD_PAGE_KEY)).toBe('p2')
  })

  it('reconciles an unknown initialId to the first page', () => {
    const pages = [page('p1'), page('p2')]

    const { result } = renderHook(() => useActiveDashboardPageId(pages, 'zzz'))

    expect(result.current[0]).toBe('p1')
  })
})
