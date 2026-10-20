import { describe, it, expect } from 'vitest'

import { nextWaypoint, traversalOrder, etaToRouteEnd, etaToDestination, computeClockTripEta } from '@/lib/next-waypoint'
import { haversineMeters } from '@/lib/geo'
import type { RouteWaypoint } from '@/lib/route-calc'
import type { ActiveRouteStatus } from '@/hooks/use-route-activation'

function wp(lat: number, lon: number, name?: string): RouteWaypoint {
  return name === undefined ? { lat, lon } : { lat, lon, name }
}

const FORWARD_WAYPOINTS = [
  wp(1, 1, 'Start'),
  wp(2, 2),
  wp(3, 3, 'Mid'),
  wp(4, 4),
]

function activeStatus(pointIndex: number, reverse: boolean, waypoints: RouteWaypoint[] = FORWARD_WAYPOINTS): ActiveRouteStatus {
  return { state: 'active', route: { name: 'Test Route', waypoints }, pointIndex, reverse, destination: null }
}

describe('nextWaypoint', () => {
  it('indexes the authored array directly when reverse is false, at pointIndex 0', () => {
    const result = nextWaypoint(activeStatus(0, false))
    expect(result).not.toBeNull()
    expect(result!.index).toBe(0)
    expect(result!.waypoint).toEqual({ lat: 1, lon: 1, name: 'Start' })
    expect(result!.label).toBe('Start')
  })

  it('indexes the authored array directly when reverse is false, at pointIndex 1', () => {
    const result = nextWaypoint(activeStatus(1, false))
    expect(result!.index).toBe(1)
    expect(result!.waypoint).toEqual({ lat: 2, lon: 2 })
    // No name on this waypoint - label falls back to 1-based traversal position.
    expect(result!.label).toBe('WP 2')
  })

  it('indexes the authored array directly when reverse is false, mid-route', () => {
    const result = nextWaypoint(activeStatus(2, false))
    expect(result!.index).toBe(2)
    expect(result!.waypoint).toEqual({ lat: 3, lon: 3, name: 'Mid' })
    expect(result!.label).toBe('Mid')
  })

  it('counts pointIndex from the end of the array when reverse is true, at pointIndex 0', () => {
    // reverse true, pointIndex 0 -> last authored waypoint (lat 4, lon 4).
    const result = nextWaypoint(activeStatus(0, true))
    expect(result!.index).toBe(0)
    expect(result!.waypoint).toEqual({ lat: 4, lon: 4 })
    expect(result!.label).toBe('WP 1')
  })

  it('walks backward through the authored array as pointIndex increases when reverse is true', () => {
    const r1 = nextWaypoint(activeStatus(1, true))
    expect(r1!.waypoint).toEqual({ lat: 3, lon: 3, name: 'Mid' })
    expect(r1!.label).toBe('Mid')

    const r2 = nextWaypoint(activeStatus(2, true))
    expect(r2!.waypoint).toEqual({ lat: 2, lon: 2 })
    expect(r2!.label).toBe('WP 3')

    const r3 = nextWaypoint(activeStatus(3, true))
    expect(r3!.waypoint).toEqual({ lat: 1, lon: 1, name: 'Start' })
    expect(r3!.label).toBe('Start')
  })

  it('clamps a pointIndex past the end of the array rather than throwing', () => {
    const result = nextWaypoint(activeStatus(99, false))
    expect(result).not.toBeNull()
    expect(result!.index).toBe(3)
    expect(result!.waypoint).toEqual({ lat: 4, lon: 4 })
  })

  it('clamps a pointIndex past the end of the array when reverse is true too', () => {
    const result = nextWaypoint(activeStatus(99, true))
    expect(result).not.toBeNull()
    expect(result!.index).toBe(3)
    expect(result!.waypoint).toEqual({ lat: 1, lon: 1, name: 'Start' })
  })

  it('returns null when the status is not active', () => {
    expect(nextWaypoint({ state: 'inactive', destination: null })).toBeNull()
    expect(nextWaypoint({ state: 'unknown', reason: 'error' })).toBeNull()
  })


  it('returns null when the route has no waypoints', () => {
    const result = nextWaypoint(activeStatus(0, false, []))
    expect(result).toBeNull()
  })
})

describe('traversalOrder', () => {
  it('returns the authored order when reverse is false', () => {
    expect(traversalOrder(activeStatus(0, false))).toEqual(FORWARD_WAYPOINTS)
  })

  it('returns the reversed order when reverse is true', () => {
    expect(traversalOrder(activeStatus(0, true))).toEqual([...FORWARD_WAYPOINTS].reverse())
  })

  it('is the array nextWaypoint indexes into: nextWaypoint.index matches traversalOrder[index]', () => {
    const status = activeStatus(2, true)
    const traversal = traversalOrder(status)
    const next = nextWaypoint(status)
    expect(traversal![next!.index]).toEqual(next!.waypoint)
  })

  it('returns null when the status is not active', () => {
    expect(traversalOrder({ state: 'inactive', destination: null })).toBeNull()
    expect(traversalOrder({ state: 'unknown', reason: 'error' })).toBeNull()
  })


  it('returns null when the route has no waypoints', () => {
    expect(traversalOrder(activeStatus(0, false, []))).toBeNull()
  })
})

// ADR 0125: the clock tile's ETA is for the whole trip (the route's final
// waypoint in traversal order), not just the next leg. SignalK carries no
// planning speed, so ETA comes from SOG alone.
describe('etaToRouteEnd', () => {
  const now = new Date('2026-01-01T12:00:00Z')

  it('sums the vessel-to-next-waypoint leg plus every remaining leg to the final waypoint, forward order', () => {
    // pointIndex 1 (forward): current target is (2,2); the route continues
    // (2,2) -> (3,3) -> (4,4). Vessel starts at the origin.
    const result = etaToRouteEnd(0, 0, activeStatus(1, false), 10, now)
    expect(result).not.toBeNull()

    const expectedDistance =
      haversineMeters(0, 0, 2, 2) + haversineMeters(2, 2, 3, 3) + haversineMeters(3, 3, 4, 4)
    expect(result!.distanceM).toBeCloseTo(expectedDistance, 3)
    // (4,4) has no name -> falls back to its 1-based traversal position.
    expect(result!.label).toBe('WP 4')
    expect(result!.etaAt).not.toBeNull()
  })

  it('walks the reversed traversal order for its remaining legs and final waypoint, matching nextWaypoint', () => {
    const result = etaToRouteEnd(0, 0, activeStatus(0, true), 10, now)
    expect(result).not.toBeNull()

    const expectedDistance =
      haversineMeters(0, 0, 4, 4) + haversineMeters(4, 4, 3, 3) + haversineMeters(3, 3, 2, 2) + haversineMeters(2, 2, 1, 1)
    expect(result!.distanceM).toBeCloseTo(expectedDistance, 3)
    expect(result!.label).toBe('Start')
  })

  it('has no remaining legs when the current target is already the final waypoint', () => {
    const result = etaToRouteEnd(0, 0, activeStatus(3, false), 10, now)
    expect(result).not.toBeNull()
    expect(result!.distanceM).toBeCloseTo(haversineMeters(0, 0, 4, 4), 3)
    expect(result!.label).toBe('WP 4')
  })

  it('gives no ETA when SOG is at or below the threshold or unknown', () => {
    expect(etaToRouteEnd(0, 0, activeStatus(1, false), 0.5, now)!.etaAt).toBeNull()
    expect(etaToRouteEnd(0, 0, activeStatus(1, false), null, now)!.etaAt).toBeNull()
  })

  it('returns null when the status is not active', () => {
    expect(etaToRouteEnd(0, 0, { state: 'inactive', destination: null }, 10, now)).toBeNull()
    expect(etaToRouteEnd(0, 0, { state: 'unknown', reason: 'error' }, 10, now)).toBeNull()
  })

  it('returns null when the route has no waypoints', () => {
    expect(etaToRouteEnd(0, 0, activeStatus(0, false, []), 10, now)).toBeNull()
  })
})

describe('etaToDestination', () => {
  const now = new Date('2026-01-01T12:00:00Z')
  const destination = { lat: 0, lon: 1 } // ~111.2km east of the origin at the equator

  it('gives an ETA from SOG when SOG is above the threshold', () => {
    const result = etaToDestination(0, 0, destination, 5, now)
    expect(result.distanceM).toBeGreaterThan(111000)
    expect(result.etaAt).not.toBeNull()
  })

  it('gives no ETA when SOG is at or below the threshold - there is no planning speed to fall back to', () => {
    const result = etaToDestination(0, 0, destination, 0.5, now)
    expect(result.etaAt).toBeNull()
  })

  it('gives no ETA when SOG is null', () => {
    const result = etaToDestination(0, 0, destination, null, now)
    expect(result.etaAt).toBeNull()
  })

  it('gives an ETA equal to now when the distance is 0', () => {
    const result = etaToDestination(0, 0, { lat: 0, lon: 0 }, 5, now)
    expect(result.distanceM).toBe(0)
    expect(result.etaAt).toEqual(now)
  })
})

// ADR 0125: the clock tile's trip-ETA decision, pulled out of App.tsx so it
// is unit-testable without rendering the whole app.
describe('computeClockTripEta', () => {
  const now = new Date('2026-01-01T12:00:00Z')

  it('returns null with no position fix', () => {
    expect(computeClockTripEta(activeStatus(1, false), null, 0, 10, now)).toBeNull()
    expect(computeClockTripEta(activeStatus(1, false), 0, null, 10, now)).toBeNull()
  })

  it('returns null when there is no status at all', () => {
    expect(computeClockTripEta(null, 0, 0, 10, now)).toBeNull()
  })

  it('returns null for status "unknown"', () => {
    expect(computeClockTripEta({ state: 'unknown', reason: 'error' }, 0, 0, 10, now)).toBeNull()
  })

  it('reports the FINAL waypoint via etaToRouteEnd for an active route', () => {
    const result = computeClockTripEta(activeStatus(1, false), 0, 0, 10, now)
    expect(result).not.toBeNull()
    expect(result!.label).toBe('WP 4') // the last waypoint has no name
    expect(result!.etaAt).not.toBeNull()
  })

  it('reports a bare chartplotter destination via etaToDestination when inactive', () => {
    const status = { state: 'inactive' as const, destination: { lat: 0, lon: 1, name: 'Hook Island' } }
    const result = computeClockTripEta(status, 0, 0, 5, now)
    expect(result).not.toBeNull()
    expect(result!.label).toBe('Hook Island')
    expect(result!.etaAt).not.toBeNull()
  })

  it('returns null when inactive with no destination', () => {
    const status = { state: 'inactive' as const, destination: null }
    expect(computeClockTripEta(status, 0, 0, 5, now)).toBeNull()
  })

  it('returns null for an active route with no waypoints and no destination', () => {
    expect(computeClockTripEta(activeStatus(0, false, []), 0, 0, 5, now)).toBeNull()
  })

  it('prefers the route ETA over a destination when both are present', () => {
    const status: ActiveRouteStatus = {
      ...(activeStatus(1, false) as Extract<ActiveRouteStatus, { state: 'active' }>),
      destination: { lat: 9, lon: 9, name: 'Somewhere Else' },
    }
    const result = computeClockTripEta(status, 0, 0, 10, now)
    expect(result).not.toBeNull()
    expect(result!.label).toBe('WP 4')
  })
})
