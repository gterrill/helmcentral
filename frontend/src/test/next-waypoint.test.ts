import { describe, it, expect } from 'vitest'

import { nextWaypoint, etaToWaypoint } from '@/lib/next-waypoint'
import type { Route, RouteWaypoint } from '@/hooks/use-routes'
import type { ActiveRouteStatus } from '@/hooks/use-route-activation'

function wp(lat: number, lon: number, name?: string): RouteWaypoint {
  return name === undefined ? { lat, lon } : { lat, lon, name }
}

function route(waypoints: RouteWaypoint[], overrides: Partial<Route> = {}): Route {
  return {
    id: 'route-1',
    name: 'Test Route',
    waypoints,
    planning_speed_kts: 6,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

const FORWARD_ROUTE = route([
  wp(1, 1, 'Start'),
  wp(2, 2),
  wp(3, 3, 'Mid'),
  wp(4, 4),
])

function activeStatus(pointIndex: number, reverse: boolean, routeId: string | null = 'route-1'): ActiveRouteStatus {
  return { state: 'active', routeId, pointIndex, reverse }
}

describe('nextWaypoint', () => {
  it('indexes the authored array directly when reverse is false, at pointIndex 0', () => {
    const result = nextWaypoint(FORWARD_ROUTE, activeStatus(0, false))
    expect(result).not.toBeNull()
    expect(result!.index).toBe(0)
    expect(result!.waypoint).toEqual({ lat: 1, lon: 1, name: 'Start' })
    expect(result!.label).toBe('Start')
  })

  it('indexes the authored array directly when reverse is false, at pointIndex 1', () => {
    const result = nextWaypoint(FORWARD_ROUTE, activeStatus(1, false))
    expect(result!.index).toBe(1)
    expect(result!.waypoint).toEqual({ lat: 2, lon: 2 })
    // No name on this waypoint - label falls back to 1-based traversal position.
    expect(result!.label).toBe('WP 2')
  })

  it('indexes the authored array directly when reverse is false, mid-route', () => {
    const result = nextWaypoint(FORWARD_ROUTE, activeStatus(2, false))
    expect(result!.index).toBe(2)
    expect(result!.waypoint).toEqual({ lat: 3, lon: 3, name: 'Mid' })
    expect(result!.label).toBe('Mid')
  })

  it('counts pointIndex from the end of the array when reverse is true, at pointIndex 0', () => {
    // reverse true, pointIndex 0 -> last authored waypoint (lat 4, lon 4).
    const result = nextWaypoint(FORWARD_ROUTE, activeStatus(0, true))
    expect(result!.index).toBe(0)
    expect(result!.waypoint).toEqual({ lat: 4, lon: 4 })
    expect(result!.label).toBe('WP 1')
  })

  it('walks backward through the authored array as pointIndex increases when reverse is true', () => {
    const r1 = nextWaypoint(FORWARD_ROUTE, activeStatus(1, true))
    expect(r1!.waypoint).toEqual({ lat: 3, lon: 3, name: 'Mid' })
    expect(r1!.label).toBe('Mid')

    const r2 = nextWaypoint(FORWARD_ROUTE, activeStatus(2, true))
    expect(r2!.waypoint).toEqual({ lat: 2, lon: 2 })
    expect(r2!.label).toBe('WP 3')

    const r3 = nextWaypoint(FORWARD_ROUTE, activeStatus(3, true))
    expect(r3!.waypoint).toEqual({ lat: 1, lon: 1, name: 'Start' })
    expect(r3!.label).toBe('Start')
  })

  it('clamps a pointIndex past the end of the array rather than throwing', () => {
    const result = nextWaypoint(FORWARD_ROUTE, activeStatus(99, false))
    expect(result).not.toBeNull()
    expect(result!.index).toBe(3)
    expect(result!.waypoint).toEqual({ lat: 4, lon: 4 })
  })

  it('clamps a pointIndex past the end of the array when reverse is true too', () => {
    const result = nextWaypoint(FORWARD_ROUTE, activeStatus(99, true))
    expect(result).not.toBeNull()
    expect(result!.index).toBe(3)
    expect(result!.waypoint).toEqual({ lat: 1, lon: 1, name: 'Start' })
  })

  it('returns null when the status is not active', () => {
    expect(nextWaypoint(FORWARD_ROUTE, { state: 'inactive' })).toBeNull()
    expect(nextWaypoint(FORWARD_ROUTE, { state: 'unknown' })).toBeNull()
  })

  it('returns null when the active routeId does not match this route', () => {
    const result = nextWaypoint(FORWARD_ROUTE, activeStatus(0, false, 'some-other-route'))
    expect(result).toBeNull()
  })

  it('returns null when the route has no waypoints', () => {
    const result = nextWaypoint(route([]), activeStatus(0, false))
    expect(result).toBeNull()
  })
})

describe('etaToWaypoint', () => {
  const now = new Date('2026-01-01T12:00:00Z')
  const target = wp(0, 1) // 1 degree of longitude east at the equator, roughly 111.2km

  it('uses SOG as the basis when SOG is above the 0.5kt threshold', () => {
    const result = etaToWaypoint(0, 0, target, 5, 6, now)
    expect(result.basis).toBe('sog')
    expect(result.distanceM).toBeGreaterThan(111000)
    expect(result.etaAt).not.toBeNull()
  })

  it('falls back to planning speed when SOG is at or below the threshold', () => {
    const result = etaToWaypoint(0, 0, target, 0.5, 6, now)
    expect(result.basis).toBe('plan')
    expect(result.etaAt).not.toBeNull()
  })

  it('falls back to planning speed when SOG is null', () => {
    const result = etaToWaypoint(0, 0, target, null, 6, now)
    expect(result.basis).toBe('plan')
    expect(result.etaAt).not.toBeNull()
  })

  it('produces a slower (later) ETA on plan speed than a faster SOG would, all else equal', () => {
    const bySog = etaToWaypoint(0, 0, target, 10, 4, now)
    const byPlan = etaToWaypoint(0, 0, target, null, 4, now)
    expect(bySog.basis).toBe('sog')
    expect(byPlan.basis).toBe('plan')
    expect(bySog.etaAt!.getTime()).toBeLessThan(byPlan.etaAt!.getTime())
  })

  it('gives a null ETA when the resolved speed is zero via SOG-falls-to-plan-zero', () => {
    const result = etaToWaypoint(0, 0, target, null, 0, now)
    expect(result.etaAt).toBeNull()
  })

  it('gives a null ETA when the resolved speed is zero via SOG itself being zero and plan also zero', () => {
    // SOG 0 is <= 0.5 threshold, falls to plan speed, which is also 0.
    const result = etaToWaypoint(0, 0, target, 0, 0, now)
    expect(result.basis).toBe('plan')
    expect(result.etaAt).toBeNull()
  })

  it('gives a null ETA when the resolved speed is negative', () => {
    const result = etaToWaypoint(0, 0, target, null, -3, now)
    expect(result.etaAt).toBeNull()
  })

  it('gives an ETA equal to now when the distance is 0', () => {
    const samePoint = wp(0, 0)
    const result = etaToWaypoint(0, 0, samePoint, 5, 6, now)
    expect(result.distanceM).toBe(0)
    expect(result.etaAt).toEqual(now)
  })
})
