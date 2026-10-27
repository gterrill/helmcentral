/**
 * The phase 5 stores (ADR 0215): each keeps its snapshot, and the objects in
 * it, while a tick changes nothing a tile shows, and claims nothing before its
 * first event. Payloads are the live server's (fixtures/live-stream-payloads.ts).
 */
import { renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import * as live from './fixtures/live-stream-payloads'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

import { useNearbyVesselList, useNearbyVessels } from '@/hooks/use-nearby-vessels'
import { useRadarTargets } from '@/hooks/use-radar-targets'
import { useSolarState } from '@/hooks/use-solar-state'
import { useTankAlarmLevels, useTanksState } from '@/hooks/use-tanks-state'

const stream = installFakeTelemetryStream()
const emit = (event: string, payload: unknown) => stream.emit(event, payload)

describe('solar store', () => {
  it('has no controller list before the first event', () => {
    const { result } = renderHook(() => useSolarState())
    expect(result.current.controllers).toBeNull()
    expect(result.current.currentW).toBeNull()
  })

  it('turns the empty controller list into "no controllers" once the first event says so', () => {
    const { result } = renderHook(() => useSolarState())
    expect(result.current.controllers).toBeNull()
    emit('solar-state', { controllers: [] })
    expect(result.current.controllers).toEqual([])
  })

  it('keeps the snapshot when only datetime and the trend moved', () => {
    const { result } = renderHook(() => useSolarState())
    emit('solar-state', { ...live.SOLAR, trend_24h_total: [{ time: '2026-10-27T04:19:46Z', total_w: 1361.5 }] })
    const first = result.current

    emit('solar-state', { ...live.SOLAR, datetime: '2026-10-27T04:20:16Z', trend_24h_total: [{ time: '2026-10-27T04:20:16Z', total_w: 1361.6 }] })

    expect(result.current).toBe(first)
  })

  it('keeps each controller that reads the same when another moves', () => {
    const { result } = renderHook(() => useSolarState())
    emit('solar-state', live.SOLAR)
    const [first, second] = result.current.controllers ?? []

    emit('solar-state', { ...live.SOLAR, controllers: live.SOLAR.controllers.map((c, i) => (i === 1 ? { ...c, current_w: 395.1 } : { ...c })) })

    expect(result.current.controllers?.[0]).toBe(first)
    expect(result.current.controllers?.[1]).not.toBe(second)
    expect(result.current.controllers?.[1].currentW).toBe(395.1)
  })
})

describe('tanks store', () => {
  it('is loading, with no tanks, before the first event', () => {
    const { result } = renderHook(() => useTanksState())
    expect(result.current.loading).toBe(true)
    expect(result.current.tanks).toEqual([])
  })

  it('keeps the snapshot when only datetime and fresh ages moved', () => {
    const { result } = renderHook(() => useTanksState())
    emit('tanks-state', live.TANKS)
    const first = result.current

    emit('tanks-state', live.TANKS_AGES_ONLY)

    expect(result.current).toBe(first)
  })

  it('keeps every other tank, and the alarm levels, when one level moves', () => {
    const { result } = renderHook(() => ({ state: useTanksState(), levels: useTankAlarmLevels() }))
    emit('tanks-state', live.TANKS)
    const { state, levels } = result.current

    emit('tanks-state', { ...live.TANKS, tanks: live.TANKS.tanks.map((t) => (t.id === 'blackWater.1' ? { ...t, level_percent: 12 } : t)) })

    expect(result.current.state.tanks[0]).toBe(state.tanks[0])
    expect(result.current.state.tanks[4].level_percent).toBe(12)
    expect(result.current.levels).toBe(levels)
  })
})

describe('nearby vessels store', () => {
  it('is loading, with no vessels, before the first event', () => {
    const { result } = renderHook(() => useNearbyVessels())
    expect(result.current.loading).toBe(true)
    expect(result.current.vessels).toEqual([])
  })

  it('keeps the vessel list when only datetime and the fresh feed age moved', () => {
    const { result } = renderHook(() => useNearbyVesselList())
    emit('nearby-vessels', live.NEARBY)
    const first = result.current

    emit('nearby-vessels', { ...live.NEARBY, datetime: '2026-10-27T04:20:13Z', last_update_age_s: 83 })

    expect(result.current).toBe(first)
  })

  it('hands down a new list when a vessel\'s age moves, since the list shows it', () => {
    const { result } = renderHook(() => useNearbyVesselList())
    emit('nearby-vessels', live.NEARBY)
    const first = result.current

    emit('nearby-vessels', live.NEARBY_NEXT)

    expect(result.current).not.toBe(first)
    expect(result.current[0].age_seconds).toBe(126)
  })
})

describe('radar store', () => {
  it('is loading, with the source unclaimed, before the first event', () => {
    const { result } = renderHook(() => useRadarTargets())
    expect(result.current.loading).toBe(true)
    expect(result.current.targets).toEqual([])
    expect(result.current.source).toBeNull()
  })

  it('keeps the snapshot when only datetime and a fresh target age moved', () => {
    const { result } = renderHook(() => useRadarTargets())
    emit('radar-targets', live.RADAR)
    const first = result.current

    emit('radar-targets', { ...live.RADAR, datetime: '2026-10-27T04:20:15Z', targets: live.RADAR.targets.map((t) => ({ ...t, age_seconds: 3.2 })) })

    expect(result.current).toBe(first)
  })

  it('hands down a new target when its range moves', () => {
    const { result } = renderHook(() => useRadarTargets())
    emit('radar-targets', live.RADAR)
    const first = result.current

    emit('radar-targets', { ...live.RADAR, targets: live.RADAR.targets.map((t) => ({ ...t, range_m: 1800 })) })

    expect(result.current.targets[0].range_m).toBe(1800)
    expect(result.current.radars).toBe(first.radars)
  })
})

describe('solar controllers without an id', () => {
  // The server may send controllers with no id. Each still compares against
  // its own previous reading, by its place in the list, not against whichever
  // id-less controller came last.
  it('keeps each id-less controller that reads the same', () => {
    const { result } = renderHook(() => useSolarState())
    const noIds = live.SOLAR.controllers.slice(0, 2).map((c) => ({ ...c, id: '' }))
    emit('solar-state', { ...live.SOLAR, controllers: noIds })
    const [first] = result.current.controllers ?? []

    emit('solar-state', { ...live.SOLAR, controllers: noIds.map((c, i) => (i === 1 ? { ...c, current_w: 400 } : { ...c })) })

    expect(result.current.controllers?.[0]).toBe(first)
    expect(result.current.controllers?.[1].currentW).toBe(400)
  })
})
