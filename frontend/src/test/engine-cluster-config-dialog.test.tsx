import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { EngineClusterConfigDialog } from '@/components/engine-cluster-config-dialog'
import type { DashboardLayoutItem, EngineClusterConfig, GaugeWidgetConfig } from '@/lib/dashboard-widgets'
import type { EngineProfile } from '@/lib/engine-profiles'
import type { SourceKind } from '@/lib/vessel-settings'
import { resolveCluster, type EquipmentTileContext } from '@/lib/equipment-tiles'
import captured from './fixtures/engine-cluster-paths-2026-10-09.json'

/**
 * The paths a real vessel publishes, captured from the server rather than
 * assumed. A suffix nothing publishes applies cleanly and renders dashes for
 * ever, which is the failure these tests exist for.
 */
const published = new Set<string>(captured.paths)

const state = vi.hoisted(() => ({
  ctx: null as unknown,
  roles: null as unknown,
}))

vi.mock('@/hooks/use-equipment-tile-context', () => ({ useEquipmentTileContext: () => state.ctx }))
vi.mock('@/hooks/use-signalk-paths', () => ({
  useSignalKPaths: () => ({ paths: captured.paths.map((path) => ({ path })), loading: false }),
}))
vi.mock('@/hooks/use-vessel-roles', () => ({ useVesselRoles: () => state.roles }))
vi.mock('@/hooks/use-vessel-discovery', () => ({
  useVesselDiscovery: () => ({ discovery: { connected: true, groups: [], detectors: {} }, loading: false, error: null }),
}))

async function loadProfile(): Promise<EngineProfile> {
  const [fs, path] = await Promise.all([import('node:fs'), import('node:path')])
  const file = path.resolve(process.cwd(), '../backend/profile_catalogue/cummins-qsb67-550.json')
  return JSON.parse(fs.readFileSync(file, 'utf8')) as EngineProfile
}

const item = (id: string, name: string, source: string, kind: SourceKind, profile = '') =>
  ({ id, name, signalk_source: source, source_kind: kind, profile_id: profile })

async function setup(side: 'port' | 'starboard', roles: unknown = { port_alternator: 'alt-2', starboard_alternator: 'alt-12' }) {
  const engineProfile = await loadProfile()
  state.ctx = {
    items: [
      item('eng', `${side} engine`, `propulsion.${side}`, 'engine', engineProfile.id),
      item('alt-2', 'Port alternator', 'electrical.alternator.2', 'alternator'),
      item('alt-12', 'Starboard alternator', 'electrical.alternator.12', 'alternator'),
      item('batt', 'Port start battery', 'electrical.batteries.292', 'battery'),
      item('exh', 'Exhaust node 0', 'propulsion.0', 'engine'),
    ],
    profiles: [engineProfile],
    reporting: null,
  } satisfies EquipmentTileContext
  state.roles = roles
}

const blank: EngineClusterConfig = {
  equipmentId: 'eng',
  title: '',
  ring: { path: '', suffix: 'revolutions', label: 'RPM', display: 'radial', quantity: 'frequency', unit: 'rpm' },
  centre: { path: '', suffix: 'runTime', label: 'Hours', display: 'numeric', quantity: 'duration', unit: 'h' },
  corners: [],
}
const widget = (cluster: EngineClusterConfig): DashboardLayoutItem => ({ id: 'cluster:abcd1234', x: 0, y: 0, w: 6, h: 14, cluster })

function open(cluster: EngineClusterConfig = blank) {
  const onSave = vi.fn()
  render(<EngineClusterConfigDialog widget={widget(cluster)} onCancel={vi.fn()} onSave={onSave} />)
  return onSave
}

async function fillAndSave(side: 'port' | 'starboard', roles?: unknown) {
  await setup(side, roles)
  const onSave = open()
  fireEvent.click(screen.getByRole('button', { name: /fill slots from profile/i }))
  fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
  return vi.waitFor(() => onSave.mock.calls[0][0] as EngineClusterConfig)
}

const allSlots = (c: EngineClusterConfig): GaugeWidgetConfig[] => [
  c.ring, c.centre, ...c.corners.flatMap((x) => x.rows),
  ...Object.values(c.arcs ?? {}), ...Object.values(c.footer ?? {}),
].filter((s): s is GaugeWidgetConfig => s !== undefined)

describe('filling a cluster from the engine item', () => {
  test('stores suffixes and an item, never a path', async () => {
    const saved = await fillAndSave('port')
    expect(saved.equipmentId).toBe('eng')
    for (const slot of allSlots(saved)) expect(slot.path, slot.suffix).toBe('')
  })

  test('fills the ring, hours, four cards, coolant arc and the transmission from the profile', async () => {
    const saved = await fillAndSave('port')
    expect(saved.ring.suffix).toBe('revolutions')
    expect(saved.centre.suffix).toBe('runTime')
    expect(saved.corners.map((c) => c.rows[0].suffix)).toEqual(['oilPressure', 'boostPressure', 'engineLoad', 'fuel.rate'])
    expect(saved.arcs?.left?.suffix).toBe('temperature')
    expect(saved.footer?.transPressure?.suffix).toBe('transmission.oilPressure')
  })

  // The right arc under the rpm is the gearbox oil temperature, bound from the
  // engine item like the coolant arc; the transmission card keeps only pressure.
  test('binds the right arc to the gearbox oil temperature and keeps it off the footer', async () => {
    const saved = await fillAndSave('port')
    expect(saved.arcs?.right?.suffix).toBe('transmission.oilTemperature')
    expect(saved.arcs?.right?.equipmentId).toBeUndefined()
    expect(allSlots(saved).filter((s) => s.suffix === 'transmission.oilTemperature')).toHaveLength(1)
    expect(Object.keys(saved.footer ?? {})).not.toContain('transTemp')
  })

  test('every slot it fills resolves to a path the vessel publishes', async () => {
    const saved = await fillAndSave('port')
    const resolved = resolveCluster(saved, state.ctx as EquipmentTileContext)
    expect(resolved.ok).toBe(true)
    if (!resolved.ok) return
    const c = resolved.config
    const slots = [c.ring, c.centre, ...c.corners.flatMap((x) => x.rows), c.arcs?.left, ...Object.values(c.footer ?? {})]
      .filter((s): s is GaugeWidgetConfig => s !== undefined && s.path !== '')
    expect(slots.length).toBeGreaterThan(8)
    for (const slot of slots) expect(published.has(slot.path), `${slot.path} is not published by the vessel`).toBe(true)
  })

  test('ships no warn or alarm thresholds', async () => {
    const saved = await fillAndSave('port')
    const resolved = resolveCluster(saved, state.ctx as EquipmentTileContext)
    if (!resolved.ok) throw new Error('did not resolve')
    const c = resolved.config
    for (const slot of [c.ring, ...c.corners.flatMap((x) => x.rows), c.arcs?.left, ...Object.values(c.footer ?? {})]) {
      expect((slot?.zones ?? []).filter((z) => z.state !== 'normal'), slot?.suffix).toEqual([])
    }
  })

  test('takes the side from the engine item', async () => {
    expect((await fillAndSave('port')).side).toBe('port')
  })
})

describe('the alternator belongs to the engine side', () => {
  test('a port engine takes the port alternator from the Roles setting', async () => {
    const saved = await fillAndSave('port')
    for (const key of ['altAmps', 'altWatts', 'altTemp', 'altStage'] as const) {
      expect(saved.footer?.[key]?.equipmentId, key).toBe('alt-2')
    }
    expect(saved.footer?.altAmps?.suffix).toBe('current')
    expect(saved.footer?.altWatts?.suffix).toBe('power')
  })

  test('a starboard engine takes the starboard alternator', async () => {
    const saved = await fillAndSave('starboard')
    expect(saved.side).toBe('stbd')
    expect(saved.footer?.altAmps?.equipmentId).toBe('alt-12')
  })

  // There is no default and no guess: with no role the alternator is left for
  // the operator to pick, and the slots that could read nothing are not saved.
  test('without a role the alternator is left unset', async () => {
    const saved = await fillAndSave('port', null)
    expect(saved.footer?.altAmps).toBeUndefined()
    expect(saved.footer?.altStage).toBeUndefined()
  })

  test('the alternator resolves through its own item, to the path the vessel publishes', async () => {
    const saved = await fillAndSave('port')
    const resolved = resolveCluster(saved, state.ctx as EquipmentTileContext)
    if (!resolved.ok) throw new Error('did not resolve')
    expect(resolved.config.footer?.altAmps?.path).toBe('electrical.alternator.2.current')
    expect(published.has('electrical.alternator.2.current')).toBe(true)
  })
})

describe('the slots no profile fills', () => {
  async function fillThen(edit: () => void) {
    await setup('port')
    const onSave = open()
    fireEvent.click(screen.getByRole('button', { name: /fill slots from profile/i }))
    edit()
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    return vi.waitFor(() => onSave.mock.calls[0][0] as EngineClusterConfig)
  }

  test('the starter battery is picked as an item and read by suffix', async () => {
    const saved = await fillThen(() => {
      fireEvent.change(document.querySelector('#cluster-footer-battery-equipment') as HTMLSelectElement, { target: { value: 'batt' } })
    })
    expect(saved.footer?.battery).toMatchObject({ equipmentId: 'batt', suffix: 'voltage', quantity: 'potential', unit: 'V' })
    const resolved = resolveCluster(saved, state.ctx as EquipmentTileContext)
    if (!resolved.ok) throw new Error('did not resolve')
    expect(resolved.config.footer?.battery?.path).toBe('electrical.batteries.292.voltage')
    expect(published.has('electrical.batteries.292.voltage')).toBe(true)
  })

  // A wet exhaust reads tens of degrees after water injection, not hundreds.
  test('the exhaust starts on a 0 to 100 C scale with no threshold of ours', async () => {
    const saved = await fillThen(() => {
      fireEvent.change(document.querySelector('#cluster-footer-egt-equipment') as HTMLSelectElement, { target: { value: 'exh' } })
    })
    const egt = saved.footer!.egt!
    expect([egt.equipmentId, egt.suffix, egt.quantity, egt.unit, egt.min, egt.max]).toEqual(['exh', 'exhaustTemperature', 'temperature', 'C', 0, 100])
    expect(egt.zones).toBeUndefined()
    const resolved = resolveCluster(saved, state.ctx as EquipmentTileContext)
    if (!resolved.ok) throw new Error('did not resolve')
    expect(resolved.config.footer?.egt?.path).toBe('propulsion.0.exhaustTemperature')
    expect(published.has('propulsion.0.exhaustTemperature')).toBe(true)
  })

  test('a slot with no item picked is not saved: it could read nothing', async () => {
    const saved = await fillThen(() => {})
    expect(saved.footer?.battery).toBeUndefined()
    expect(saved.footer?.egt).toBeUndefined()
  })

  test('an existing cluster with no arcs or footer opens and saves without them', async () => {
    await setup('port')
    const onSave = open()
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    const saved = await vi.waitFor(() => onSave.mock.calls[0][0] as EngineClusterConfig)
    expect(saved.arcs).toBeUndefined()
    expect(saved.footer).toBeUndefined()
  })
})
