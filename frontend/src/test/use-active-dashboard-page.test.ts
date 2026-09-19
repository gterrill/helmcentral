/**
 * Covers the optional `initialId` App.tsx passes through from a deep link
 * (ADR 0074, e.g. `/dashboard/<pageId>`) — it must win over whatever is
 * already in localStorage on mount, and be written through so the linked
 * page becomes the remembered one for next time. Reconciliation against an
 * unknown/deleted page id (already covered for the no-initialId case
 * elsewhere) must hold for this path too.
 */
import { describe, it, expect, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useActiveDashboardPageId } from '@/hooks/use-active-dashboard-page'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'

const ACTIVE_DASHBOARD_PAGE_KEY = 'dashboard.activePageId'

function page(id: string, overrides: Partial<DashboardPage> = {}): DashboardPage {
  return { id, name: id, widgets: [], created_at: '', updated_at: '', ...overrides }
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

  // ADR 0110: a wall page (one with a `display_id`) sitting at position 0
  // in server order must never become the id '/' resolves to just because
  // nothing was stored yet — that's the exact bug moving wall pages out of
  // the Dashboard list is meant to fix. An id that DOES resolve to a page
  // (a deliberate selection, even of a wall page) still wins unchanged —
  // only the "nothing to resolve" fallback skips wall pages.
  it('reconciles a missing initialId to the first page that is not on a display, skipping one at position 0', () => {
    const pages = [page('p1', { display_id: 'd1' }), page('p2')]

    const { result } = renderHook(() => useActiveDashboardPageId(pages))

    expect(result.current[0]).toBe('p2')
  })

  it('still resolves to a wall page when it was explicitly the stored id', () => {
    globalThis.localStorage?.setItem(ACTIVE_DASHBOARD_PAGE_KEY, 'p1')
    const pages = [page('p1', { display_id: 'd1' }), page('p2')]

    const { result } = renderHook(() => useActiveDashboardPageId(pages))

    expect(result.current[0]).toBe('p1')
  })
})

describe('the wall display tab does not poison the shared remembered page', () => {
  it('keeps the rotating page in memory without writing it to localStorage', () => {
    // The sidebar's display group opens /display/<slug> in a NEW TAB of the
    // same browser, so the wall and the helm dashboard share one
    // localStorage. The rotation calls setActivePageId every few seconds;
    // persisting those would leave the operator's own '/' remembering a
    // wall page, which is deliberately absent from the Dashboard list.
    localStorage.setItem(ACTIVE_DASHBOARD_PAGE_KEY, 'p-helm')
    const pages = [
      { id: 'p-helm', name: 'Passage', widgets: [] },
      { id: 'p-wall', name: 'Wall: Engines', widgets: [], display_id: 'd1' },
    ] as unknown as DashboardPage[]

    const { result } = renderHook(() => useActiveDashboardPageId(pages, null, false))
    act(() => { result.current[1]('p-wall') })

    expect(result.current[0]).toBe('p-wall')
    expect(localStorage.getItem(ACTIVE_DASHBOARD_PAGE_KEY)).toBe('p-helm')
  })

  it('still persists on the ordinary dashboard', () => {
    localStorage.setItem(ACTIVE_DASHBOARD_PAGE_KEY, 'p-helm')
    const pages = [
      { id: 'p-helm', name: 'Passage', widgets: [] },
      { id: 'p-other', name: 'Anchored', widgets: [] },
    ] as unknown as DashboardPage[]

    const { result } = renderHook(() => useActiveDashboardPageId(pages))
    act(() => { result.current[1]('p-other') })

    expect(localStorage.getItem(ACTIVE_DASHBOARD_PAGE_KEY)).toBe('p-other')
  })
})
