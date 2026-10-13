/**
 * useNavigationGuard (ADR 0171): a panel registers a guard while it holds
 * something worth stopping for, and only then.
 */
import { describe, it, expect, vi } from 'vitest'
import { useLayoutEffect } from 'react'
import { renderHook } from '@testing-library/react'
import { useAppRouter } from '@/hooks/use-app-location'
import { useHasNavigationGuard, useNavigationGuard } from '@/hooks/use-navigation-guard'
import type { NavigationGuard } from '@/lib/app-router'
import { testRouter } from './helpers/router'

const guard = (overrides: Partial<NavigationGuard> = {}): NavigationGuard => ({
  owner: 'settings',
  label: 'Settings',
  blocks: () => ({ kind: 'dirty' }),
  ...overrides,
})

describe('useNavigationGuard', () => {
  it('is live for a navigate made in the same commit that mounted the guard', () => {
    const { wrapper, router } = testRouter('/settings')
    let moved: boolean | null = null
    renderHook(() => {
      const r = useAppRouter()
      useNavigationGuard(guard())
      useLayoutEffect(() => { moved = r.navigate({ panel: 'radar' }) }, [r])
    }, { wrapper })
    expect(moved).toBe(false)
    expect(router.getSnapshot().pending).not.toBeNull()
  })

  it('registers while given a guard and unregisters when given null', () => {
    const { wrapper, router } = testRouter('/settings')
    const { rerender } = renderHook(({ g }: { g: NavigationGuard | null }) => useNavigationGuard(g), {
      wrapper,
      initialProps: { g: null as NavigationGuard | null },
    })
    expect(router.hasGuards()).toBe(false)

    rerender({ g: guard() })
    expect(router.hasGuards()).toBe(true)
    expect(router.getSnapshot().guardOwners).toEqual(['settings'])

    rerender({ g: null })
    expect(router.hasGuards()).toBe(false)
  })

  it('unregisters when the panel unmounts', () => {
    const { wrapper, router } = testRouter('/settings')
    const { unmount } = renderHook(() => useNavigationGuard(guard()), { wrapper })
    expect(router.hasGuards()).toBe(true)
    unmount()
    expect(router.hasGuards()).toBe(false)
  })

  it('asks the latest guard, not the one it registered with', () => {
    const { wrapper, router } = testRouter('/settings')
    const first = vi.fn((): { kind: 'dirty' } | null => ({ kind: 'dirty' }))
    const second = vi.fn((): { kind: 'dirty' } | null => null)
    const { rerender } = renderHook(({ blocks }: { blocks: NavigationGuard['blocks'] }) => useNavigationGuard(guard({ blocks })), {
      wrapper,
      initialProps: { blocks: first },
    })
    rerender({ blocks: second })

    expect(router.navigate({ panel: 'radar' })).toBe(true)
    expect(first).not.toHaveBeenCalled()
    expect(second).toHaveBeenCalled()
  })

  it('does nothing outside an App', () => {
    expect(() => renderHook(() => useNavigationGuard(guard()))).not.toThrow()
  })
})

describe('useHasNavigationGuard', () => {
  it('is true exactly while a guard with that owner is registered', () => {
    const { wrapper } = testRouter('/settings')
    const { result, rerender } = renderHook(
      ({ g }: { g: NavigationGuard | null }) => {
        useNavigationGuard(g)
        return { settings: useHasNavigationGuard('settings'), documents: useHasNavigationGuard('document-details') }
      },
      { wrapper, initialProps: { g: null as NavigationGuard | null } },
    )
    expect(result.current).toEqual({ settings: false, documents: false })

    rerender({ g: guard() })
    expect(result.current).toEqual({ settings: true, documents: false })

    rerender({ g: null })
    expect(result.current.settings).toBe(false)
  })
})
