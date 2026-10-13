/**
 * Covers the page id the app was loaded with (ADR 0074, e.g.
 * `/dashboard/<pageId>`) — it must win over whatever is already in
 * localStorage on mount, and be written through so the linked page becomes
 * the remembered one for next time. Reconciliation against an unknown/deleted
 * page id (already covered for the no-deep-link case elsewhere) must hold for
 * this path too.
 *
 * ADR 0171: the router owns the id and this hook reconciles it, so these run
 * against a real router on an in-memory history.
 */
import { createElement, type ReactNode } from 'react'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useActiveDashboardPageId } from '@/hooks/use-active-dashboard-page'
import { AppRouterContext } from '@/hooks/use-app-location'
import { createAppRouter, memoryHistory, type AppRouter, type MemoryHistory } from '@/lib/app-router'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'

function withRouter(path: string): { router: AppRouter; history: MemoryHistory; wrapper: (props: { children: ReactNode }) => ReactNode } {
  const history = memoryHistory(path)
  const router = createAppRouter(history)
  router.start()
  router.setContext({ firstPageId: 'p1', knownPageIds: ['p1', 'p2'], canAdmin: true, shellVisible: true })
  const wrapper = ({ children }: { children: ReactNode }) => createElement(AppRouterContext.Provider, { value: router }, children)
  return { router, history, wrapper }
}

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

    const { wrapper } = withRouter('/dashboard/p2')
    const { result } = renderHook(() => useActiveDashboardPageId(pages), { wrapper })

    expect(result.current[0]).toBe('p2')
    expect(globalThis.localStorage?.getItem(ACTIVE_DASHBOARD_PAGE_KEY)).toBe('p2')
  })

  it('reconciles an unknown initialId to the first page', () => {
    const pages = [page('p1'), page('p2')]

    const { wrapper, history } = withRouter('/dashboard/zzz')
    const { result } = renderHook(() => useActiveDashboardPageId(pages), { wrapper })

    expect(result.current[0]).toBe('p1')
    // The correction rewrites the entry the app was loaded at; it does not add one.
    expect(history.entries).toEqual(['/'])
  })

  // ADR 0110: a wall page (one with a `display_id`) sitting at position 0
  // in server order must never become the id '/' resolves to just because
  // nothing was stored yet — that's the exact bug moving wall pages out of
  // the Dashboard list is meant to fix. An id that DOES resolve to a page
  // (a deliberate selection, even of a wall page) still wins unchanged —
  // only the "nothing to resolve" fallback skips wall pages.
  it('never remembers a deep-linked page the list does not hold', () => {
    const pages = [page('p1'), page('p2')]
    const setItem = vi.spyOn(globalThis.localStorage, 'setItem')
    try {
      const { wrapper } = withRouter('/dashboard/zzz')
      renderHook(() => useActiveDashboardPageId(pages), { wrapper })
      expect(setItem.mock.calls.map((call) => call[1])).not.toContain('zzz')
    } finally {
      setItem.mockRestore()
    }
  })

  it('reconciles a missing initialId to the first page that is not on a display, skipping one at position 0', () => {
    const pages = [page('p1', { display_id: 'd1' }), page('p2')]

    const { wrapper } = withRouter('/')
    const { result } = renderHook(() => useActiveDashboardPageId(pages), { wrapper })

    expect(result.current[0]).toBe('p2')
  })

  it('still resolves to a wall page when it was explicitly the stored id', () => {
    globalThis.localStorage?.setItem(ACTIVE_DASHBOARD_PAGE_KEY, 'p1')
    const pages = [page('p1', { display_id: 'd1' }), page('p2')]

    const { wrapper } = withRouter('/')
    const { result } = renderHook(() => useActiveDashboardPageId(pages), { wrapper })

    expect(result.current[0]).toBe('p1')
  })

  it('puts a remembered page that is not the first into the address, replacing the opening entry', () => {
    globalThis.localStorage?.setItem(ACTIVE_DASHBOARD_PAGE_KEY, 'p2')
    const pages = [page('p1'), page('p2')]

    const { wrapper, history } = withRouter('/')
    renderHook(() => useActiveDashboardPageId(pages), { wrapper })

    expect(history.entries).toEqual(['/dashboard/p2'])
  })

  it('makes a pick a new history entry on the dashboard', () => {
    const pages = [page('p1'), page('p2')]

    const { wrapper, history } = withRouter('/')
    const { result } = renderHook(() => useActiveDashboardPageId(pages), { wrapper })
    act(() => { result.current[1]('p2') })

    expect(result.current[0]).toBe('p2')
    expect(history.entries).toEqual(['/', '/dashboard/p2'])
  })

  it('follows a Back onto another page and remembers it', () => {
    const pages = [page('p1'), page('p2')]

    const { wrapper, history } = withRouter('/')
    const { result } = renderHook(() => useActiveDashboardPageId(pages), { wrapper })
    act(() => { result.current[1]('p2') })
    expect(globalThis.localStorage?.getItem(ACTIVE_DASHBOARD_PAGE_KEY)).toBe('p2')

    act(() => { history.back() })

    expect(result.current[0]).toBe('p1')
    expect(globalThis.localStorage?.getItem(ACTIVE_DASHBOARD_PAGE_KEY)).toBe('p1')
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

    const { wrapper, history } = withRouter('/display/engines')
    const { result } = renderHook(() => useActiveDashboardPageId(pages, false), { wrapper })
    act(() => { result.current[1]('p-wall') })

    expect(result.current[0]).toBe('p-wall')
    expect(localStorage.getItem(ACTIVE_DASHBOARD_PAGE_KEY)).toBe('p-helm')
    // And the rotation never touches history either.
    expect(history.entries).toEqual(['/display/engines'])
  })

  it('still persists on the ordinary dashboard', () => {
    localStorage.setItem(ACTIVE_DASHBOARD_PAGE_KEY, 'p-helm')
    const pages = [
      { id: 'p-helm', name: 'Passage', widgets: [] },
      { id: 'p-other', name: 'Anchored', widgets: [] },
    ] as unknown as DashboardPage[]

    const { wrapper } = withRouter('/')
    const { result } = renderHook(() => useActiveDashboardPageId(pages), { wrapper })
    act(() => { result.current[1]('p-other') })

    expect(localStorage.getItem(ACTIVE_DASHBOARD_PAGE_KEY)).toBe('p-other')
  })
})
