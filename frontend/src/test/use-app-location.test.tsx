/**
 * The React side of the router (ADR 0171): useAppLocation re-renders on a
 * move, useNavigate hands out one stable function, and a hook used outside an
 * App says so rather than reading nothing.
 */
import { describe, it, expect } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useAppLocation, useAppRouter, useNavigate } from '@/hooks/use-app-location'
import { testRouter } from './helpers/router'

describe('useAppLocation', () => {
  it('reads the location and follows a move', () => {
    const { wrapper } = testRouter('/forecast')
    const { result } = renderHook(() => ({ location: useAppLocation(), navigate: useNavigate() }), { wrapper })
    expect(result.current.location).toEqual({ panel: 'forecast' })

    act(() => { result.current.navigate({ panel: 'radar' }) })
    expect(result.current.location).toEqual({ panel: 'radar' })
  })

  it('follows Back and Forward', () => {
    const { wrapper, history } = testRouter('/')
    const { result } = renderHook(() => ({ location: useAppLocation(), navigate: useNavigate() }), { wrapper })
    act(() => { result.current.navigate({ panel: 'radar' }) })
    act(() => { history.back() })
    expect(result.current.location.panel).toBeNull()
  })
})

describe('useNavigate', () => {
  it('keeps one identity across renders and moves', () => {
    const { wrapper } = testRouter('/')
    const { result, rerender } = renderHook(() => useNavigate(), { wrapper })
    const first = result.current
    act(() => { first({ panel: 'radar' }) })
    rerender()
    expect(result.current).toBe(first)
  })

  it('reports false when a guard held the move', () => {
    const { wrapper, router } = testRouter('/settings')
    router.registerGuard(() => ({ owner: 'x', label: 'X', blocks: () => ({ kind: 'dirty' }) }))
    const { result } = renderHook(() => useNavigate(), { wrapper })
    let moved = true
    act(() => { moved = result.current({ panel: 'radar' }) })
    expect(moved).toBe(false)
  })

  it('toPanel returns a panel to where it was left', () => {
    const { wrapper } = testRouter('/settings/signalk')
    const { result } = renderHook(() => ({ location: useAppLocation(), navigate: useNavigate() }), { wrapper })
    act(() => { result.current.navigate.toPanel('forecast') })
    expect(result.current.location.panel).toBe('forecast')
    act(() => { result.current.navigate.toPanel('settings') })
    expect(result.current.location).toEqual({ panel: 'settings', route: { view: 'section', section: 'signalk' } })
  })
})

describe('outside an App', () => {
  it('useAppRouter throws', () => {
    expect(() => renderHook(() => useAppRouter())).toThrow(/AppRouterContext/)
  })
})
