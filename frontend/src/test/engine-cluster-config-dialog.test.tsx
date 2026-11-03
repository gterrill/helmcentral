import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import {
  EngineClusterConfigDialog,
  clusterFromProfile,
  defaultAlternatorId,
  fillBlankAlternatorRows,
  switchAlternatorRows,
} from '@/components/engine-cluster-config-dialog'
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
  extraPaths: [] as string[],
  loading: false,
  error: null as Error | null,
}))

vi.mock('@/hooks/use-equipment-tile-context', () => ({ useEquipmentTileContext: () => state.ctx }))
vi.mock('@/hooks/use-signalk-paths', () => ({
  useSignalKPaths: () => ({ paths: [...captured.paths, ...state.extraPaths].map((path) => ({ path })), loading: state.loading, error: state.error }),
}))
vi.mock('@/hooks/use-vessel-roles', () => ({ useVesselRoles: () => state.roles }))
vi.mock('@/hooks/use-vessel-discovery', () => ({
  useVesselDiscovery: () => ({ discovery: { connected: true, groups: [], detectors: {} }, loading: false, error: null }),
}))

afterEach(() => {
  state.extraPaths = []
  state.loading = false
  state.error = null
})

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
      item('batt-quiet', 'Quiet battery', 'electrical.batteries.777', 'battery'),
      item('batt-nosrc', 'Unmapped battery', '', 'battery'),
      item('alt-quiet', 'Quiet alternator', 'electrical.alternator.99', 'alternator'),
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
].filter((s): s is GaugeWidgetConfig => typeof s === 'object')

/** The footer's slots, without its alternator id. */
const footerSlots = (c: { footer?: object }): GaugeWidgetConfig[] =>
  Object.values(c.footer ?? {}).filter((s): s is GaugeWidgetConfig => typeof s === 'object')

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
    const slots = [c.ring, c.centre, ...c.corners.flatMap((x) => x.rows), c.arcs?.left, ...footerSlots(c)]
      .filter((s): s is GaugeWidgetConfig => s !== undefined && s.path !== '')
    expect(slots.length).toBeGreaterThan(8)
    for (const slot of slots) expect(published.has(slot.path), `${slot.path} is not published by the vessel`).toBe(true)
  })

  test('ships no warn or alarm thresholds', async () => {
    const saved = await fillAndSave('port')
    const resolved = resolveCluster(saved, state.ctx as EquipmentTileContext)
    if (!resolved.ok) throw new Error('did not resolve')
    const c = resolved.config
    for (const slot of [c.ring, ...c.corners.flatMap((x) => x.rows), c.arcs?.left, ...footerSlots(c)]) {
      expect((slot?.zones ?? []).filter((z) => z.state !== 'normal'), slot?.suffix).toEqual([])
    }
  })

  // A migrated telltale's held zones are keyed by the position it was issued
  // (zoneIndex). Filling from the profile rebuilds the slot, and must keep it or
  // the held zones move to a new alarm id.
  test('keeps the issued alarm position of a slot whose zones it carries over', async () => {
    await setup('port')
    const held: GaugeWidgetConfig = {
      path: '', suffix: 'transmission.oilTemperature', label: 'Gearbox', display: 'numeric', quantity: 'temperature', unit: 'C',
      zones: [{ from: 90, to: 120, state: 'alarm' }], zoneIndex: 19,
    }
    const profile = await loadProfile()
    const filled = clusterFromProfile(profile, { ...blank, arcs: { right: held } }, state.ctx as EquipmentTileContext, null)
    expect(filled.arcs?.right?.zones).toEqual(held.zones)
    expect(filled.arcs?.right?.zoneIndex).toBe(19)
  })

  test('takes the side from the engine item', async () => {
    expect((await fillAndSave('port')).side).toBe('port')
  })
})

/** The fixture the dialog is reading, for a test to add an item or a profile to. */
const editable = () => state.ctx as { items: Array<{ id: string; profile_id: string }>; profiles: unknown[] }
const alternatorBox = () => document.querySelector('#cluster-alternator-equipment') as HTMLSelectElement
const rowBox = (key: string) => document.querySelector(`#cluster-footer-${key}-suffix`) as HTMLInputElement
const pickAlternator = (id: string) => fireEvent.change(alternatorBox(), { target: { value: id } })
const saveButton = () => screen.getByRole('button', { name: /^save$/i }) as HTMLButtonElement
const savedConfig = (onSave: ReturnType<typeof vi.fn>) => vi.waitFor(() => onSave.mock.calls[0][0] as EngineClusterConfig)
const altRows = ['altAmps', 'altWatts', 'altTemp', 'altStage', 'altLimit'] as const

describe('the alternator belongs to the engine side', () => {
  test('a port engine takes the port alternator from the Roles setting, and its readings', async () => {
    const saved = await fillAndSave('port')
    expect(saved.footer?.alternatorId).toBe('alt-2')
    expect(saved.footer?.altAmps?.suffix).toBe('current')
    expect(saved.footer?.altWatts?.suffix).toBe('power')
    expect(saved.footer?.altTemp?.suffix).toBe('temperature')
    expect(saved.footer?.altStage?.suffix).toBe('chargingModeNumber')
  })

  test('the alternator rows hold their readings and no equipment of their own', async () => {
    const saved = await fillAndSave('port')
    for (const key of altRows) expect(saved.footer?.[key]?.equipmentId, key).toBeUndefined()
  })

  test('a starboard engine takes the starboard alternator', async () => {
    // The captured vessel has one alternator; give the starboard one the same readings.
    state.extraPaths = ['current'].map((r) => `electrical.alternator.12.${r}`)
    const saved = await fillAndSave('starboard')
    expect(saved.side).toBe('stbd')
    expect(saved.footer?.alternatorId).toBe('alt-12')
    expect(saved.footer?.altAmps?.suffix).toBe('current')
  })

  // There is no default and no guess: with no role the alternator is left for
  // the operator to pick, and rows that could read nothing are not saved.
  test('without a role the alternator is left unset', async () => {
    const saved = await fillAndSave('port', null)
    expect(saved.footer?.alternatorId).toBeUndefined()
    expect(saved.footer?.altAmps).toBeUndefined()
    expect(saved.footer?.altStage).toBeUndefined()
  })

  test('a role of "none" leaves the alternator unset too', async () => {
    const saved = await fillAndSave('port', { port_alternator: 'none', starboard_alternator: 'none' })
    expect(saved.footer?.alternatorId).toBeUndefined()
  })

  test('the alternator resolves through the footer, to the path the vessel publishes', async () => {
    const saved = await fillAndSave('port')
    const resolved = resolveCluster(saved, state.ctx as EquipmentTileContext)
    if (!resolved.ok) throw new Error('did not resolve')
    expect(resolved.config.footer?.altAmps?.path).toBe('electrical.alternator.2.current')
    expect(published.has('electrical.alternator.2.current')).toBe(true)
  })
})

describe('the one alternator picker', () => {
  const tab = () => fireEvent.click(screen.getByRole('tab', { name: /alternator & more/i }))

  test('the tab is called Alternator & more, with the picker above the five rows', async () => {
    await setup('port')
    open()
    expect(screen.queryByRole('tab', { name: /^footer/i })).toBeNull()
    tab()
    expect(screen.getByLabelText('Alternator')).toBe(alternatorBox())
    const order = Array.from(document.querySelectorAll('details')).map((d) => d.querySelector('summary span')?.textContent)
    expect(order.slice(0, 5)).toEqual([
      'Alternator current (also drawn as a one-hour trace)', 'Alternator output power', 'Alternator temperature',
      'Charge stage (regulator state number)', 'Charge current limit',
    ])
    expect(screen.queryByText(/battery management system/i)).toBeNull()
  })

  test('an alternator row has no equipment picker of its own, only a reading', async () => {
    await setup('port')
    open()
    for (const key of altRows) {
      expect(document.querySelector(`#cluster-footer-${key}-equipment`), key).toBeNull()
      expect(rowBox(key), key).not.toBeNull()
    }
    expect(document.querySelector('#cluster-footer-battery-equipment')).not.toBeNull()
  })

  test('opening a card fills the picker from Roles and the rows from what the alternator sends', async () => {
    await setup('port')
    open()
    expect(alternatorBox().value).toBe('alt-2')
    expect(rowBox('altAmps').value).toBe('current')
    expect(rowBox('altWatts').value).toBe('power')
    // Charge limit has no expected reading, so it waits to be chosen.
    expect(rowBox('altLimit').value).toBe('')
  })

  test('picking an engine fills the picker from the Engine side', async () => {
    await setup('port')
    open({ ...blank, equipmentId: '' })
    expect(alternatorBox().value).toBe('')
    fireEvent.change(document.querySelector('#cluster-equipment') as HTMLSelectElement, { target: { value: 'eng' } })
    expect(alternatorBox().value).toBe('alt-2')
    expect(rowBox('altAmps').value).toBe('current')
  })

  test('changing the Engine side moves the picker while the operator has not chosen it', async () => {
    await setup('port')
    open()
    expect(alternatorBox().value).toBe('alt-2')
    fireEvent.change(document.querySelector('#cluster-side') as HTMLSelectElement, { target: { value: 'stbd' } })
    expect(alternatorBox().value).toBe('alt-12')
  })

  test('changing the Engine side does not undo an alternator the operator chose', async () => {
    await setup('port')
    open()
    pickAlternator('alt-quiet')
    fireEvent.change(document.querySelector('#cluster-side') as HTMLSelectElement, { target: { value: 'stbd' } })
    expect(alternatorBox().value).toBe('alt-quiet')
  })

  test('a Starboard card may hold the Port alternator, with no warning', async () => {
    await setup('port')
    open({ ...blank, side: 'stbd' })
    pickAlternator('alt-2')
    expect(alternatorBox().value).toBe('alt-2')
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.queryByRole('status')).toBeNull()
    expect(saveButton()).toBeEnabled()
  })

  test('opening a saved card does not refill its blank rows', async () => {
    await setup('port')
    const onSave = open({ ...blank, footer: { alternatorId: 'alt-2', altAmps: { ...blankAmps, suffix: 'current' } } })
    expect(alternatorBox().value).toBe('alt-2')
    expect(rowBox('altAmps').value).toBe('current')
    expect(rowBox('altWatts').value).toBe('')
    fireEvent.click(saveButton())
    const saved = await savedConfig(onSave)
    expect(saved.footer?.altWatts).toBeUndefined()
    expect(saved.footer?.altAmps?.suffix).toBe('current')
  })

  test('a saved alternator is not replaced by the Roles default or by a side change', async () => {
    await setup('port')
    open({ ...blank, footer: { alternatorId: 'alt-12' } })
    expect(alternatorBox().value).toBe('alt-12')
    fireEvent.change(document.querySelector('#cluster-side') as HTMLSelectElement, { target: { value: 'stbd' } })
    fireEvent.change(document.querySelector('#cluster-side') as HTMLSelectElement, { target: { value: 'port' } })
    expect(alternatorBox().value).toBe('alt-12')
  })

  test('a cleared alternator stays cleared on open and survives a save as an empty string', async () => {
    await setup('port')
    const onSave = open({ ...blank, footer: { alternatorId: '' } })
    expect(alternatorBox().value).toBe('')
    fireEvent.click(saveButton())
    const saved = await savedConfig(onSave)
    expect(saved.footer).toEqual({ alternatorId: '' })
  })

  test('clearing the picker blanks all five rows and saves the empty alternator', async () => {
    await setup('port')
    const onSave = open()
    expect(rowBox('altAmps').value).toBe('current')
    pickAlternator('')
    for (const key of altRows) expect(rowBox(key).value, key).toBe('')
    fireEvent.click(saveButton())
    const saved = await savedConfig(onSave)
    expect(saved.footer?.alternatorId).toBe('')
    for (const key of altRows) expect(saved.footer?.[key], key).toBeUndefined()
  })

  test('an old card with a per-row alternator keeps its readings, shows no alternator, and drops them on save', async () => {
    await setup('port', null)
    const onSave = open({
      ...blank,
      footer: { altAmps: { ...blankAmps, suffix: 'current', equipmentId: 'alt-2' }, altWatts: { ...blankAmps, suffix: 'power', equipmentId: 'alt-2' } },
    })
    expect(alternatorBox().value).toBe('')
    expect(rowBox('altAmps').value).toBe('current')
    expect(rowBox('altWatts').value).toBe('power')
    expect(saveButton()).toBeEnabled()
    fireEvent.click(saveButton())
    const saved = await savedConfig(onSave)
    expect(saved.footer).toBeUndefined()
  })

  test('an old card gets the Roles alternator, and its rows hold no equipment', async () => {
    await setup('port')
    const onSave = open({ ...blank, footer: { altAmps: { ...blankAmps, suffix: 'current', equipmentId: 'alt-12' } } })
    expect(alternatorBox().value).toBe('alt-2')
    fireEvent.click(saveButton())
    const saved = await savedConfig(onSave)
    expect(saved.footer?.alternatorId).toBe('alt-2')
    expect(saved.footer?.altAmps).not.toHaveProperty('equipmentId')
  })

  test('picking an alternator fills each row with the reading it sends, and leaves the rest off the card', async () => {
    state.extraPaths = ['electrical.alternator.99.current']
    await setup('port', null)
    const onSave = open()
    pickAlternator('alt-quiet')
    expect(rowBox('altAmps').value).toBe('current')
    for (const key of ['altWatts', 'altTemp', 'altStage', 'altLimit']) expect(rowBox(key).value, key).toBe('')
    expect(saveButton()).toBeEnabled()
    fireEvent.click(saveButton())
    const saved = await savedConfig(onSave)
    expect(saved.footer?.alternatorId).toBe('alt-quiet')
    expect(saved.footer?.altAmps).toMatchObject({ suffix: 'current' })
    expect(saved.footer?.altWatts).toBeUndefined()
  })

  test('a row that is blank because the alternator does not send it says Not sending', async () => {
    state.extraPaths = ['electrical.alternator.99.current']
    await setup('port', null)
    open()
    pickAlternator('alt-quiet')
    const summary = (title: string) => (screen.getByText(title).closest('summary') as HTMLElement).textContent
    expect(summary('Alternator output power')).toContain('Not sending')
    expect(summary('Charge current limit')).toContain('Not shown')
    expect(summary('Alternator current (also drawn as a one-hour trace)')).not.toContain('Not sending')
  })

  test('with no alternator chosen the rows say Not shown, not Not sending', async () => {
    await setup('port', null)
    open()
    const summary = (title: string) => (screen.getByText(title).closest('summary') as HTMLElement).textContent
    expect(summary('Alternator output power')).toContain('Not shown')
  })

  test('an alternator that sends nothing is warned about, and Save stays available', async () => {
    await setup('port', null)
    open()
    pickAlternator('alt-quiet')
    expect(screen.getByText("Quiet alternator isn't sending anything. Check its source in Equipment.")).toBeInTheDocument()
    expect(saveButton()).toBeEnabled()
  })

  test('an alternator with no source says so', async () => {
    await setup('port', null)
    editable().items.push(item('alt-nosrc', 'Unmapped alternator', '', 'alternator'))
    open()
    pickAlternator('alt-nosrc')
    expect(screen.getByText(/Unmapped alternator has no SignalK source/i)).toBeInTheDocument()
    expect(screen.queryByText(/isn't sending anything/i)).toBeNull()
  })

  test('while the paths load it fills nothing and warns of nothing, then fills when they arrive', async () => {
    state.loading = true
    await setup('port', null)
    const onSave = vi.fn()
    const view = render(<EngineClusterConfigDialog widget={widget(blank)} onCancel={vi.fn()} onSave={onSave} />)
    pickAlternator('alt-2')
    expect(rowBox('altAmps').value).toBe('')
    expect(screen.queryByText(/isn't sending/i)).toBeNull()
    state.loading = false
    view.rerender(<EngineClusterConfigDialog widget={widget(blank)} onCancel={vi.fn()} onSave={onSave} />)
    expect(rowBox('altAmps').value).toBe('current')
    expect(rowBox('altWatts').value).toBe('power')
  })

  test('the Roles default also fills once the paths arrive', async () => {
    state.loading = true
    await setup('port')
    const onSave = vi.fn()
    const view = render(<EngineClusterConfigDialog widget={widget(blank)} onCancel={vi.fn()} onSave={onSave} />)
    expect(alternatorBox().value).toBe('alt-2')
    expect(rowBox('altAmps').value).toBe('')
    state.loading = false
    view.rerender(<EngineClusterConfigDialog widget={widget(blank)} onCancel={vi.fn()} onSave={onSave} />)
    expect(rowBox('altAmps').value).toBe('current')
  })

  test('when SignalK cannot be read the rows stay empty and the dialog says so', async () => {
    state.error = new Error('down')
    await setup('port', null)
    open()
    pickAlternator('alt-2')
    for (const key of altRows) expect(rowBox(key).value, key).toBe('')
    expect(screen.getByText(/can't read what SignalK is sending/i)).toBeInTheDocument()
    expect(screen.queryByText(/isn't sending/i)).toBeNull()
    expect(saveButton()).toBeEnabled()
  })

  test('picking a different alternator keeps a reading it also sends and replaces one it does not', async () => {
    state.extraPaths = ['electrical.alternator.99.current', 'electrical.alternator.99.rpm']
    await setup('port', null)
    open()
    pickAlternator('alt-2')
    fireEvent.change(rowBox('altLimit'), { target: { value: 'fieldDrive' } })
    pickAlternator('alt-quiet')
    // current: sent by both. Power: not sent by the new one, so blank. fieldDrive: not sent, no expected reading.
    expect(rowBox('altAmps').value).toBe('current')
    expect(rowBox('altWatts').value).toBe('')
    expect(rowBox('altLimit').value).toBe('')
  })

  test('the alternator rows offer what the alternator sends', async () => {
    await setup('port')
    open()
    const options = Array.from(document.querySelectorAll('#cluster-footer-altAmps-suffixes option')).map((o) => (o as HTMLOptionElement).value)
    expect(options).toContain('fieldDrive')
    expect(options).toContain('voltage')
  })
})

describe('a reading typed on an alternator row', () => {
  test('blocks Save when the alternator does not send it, and names the reading and the tab', async () => {
    await setup('port')
    const onSave = open()
    fireEvent.change(rowBox('altLimit'), { target: { value: 'foo' } })
    expect(saveButton()).toBeDisabled()
    expect(screen.getByRole('status').textContent).toBe(
      "On the Alternator & more tab, Port alternator isn't sending foo. Pick a reading it sends, or clear it.",
    )
    fireEvent.click(saveButton())
    expect(onSave).not.toHaveBeenCalled()
  })

  test('lets a reading the alternator sends through', async () => {
    await setup('port')
    open()
    fireEvent.change(rowBox('altLimit'), { target: { value: 'fieldDrive' } })
    expect(saveButton()).toBeEnabled()
  })

  test('lets a reading on the alternator\'s profile through, though nothing sends it now', async () => {
    await setup('port')
    const profile = {
      id: 'alt-prof', name: 'Alternator', kind: 'alternator',
      gauges: [{ path_suffix: 'limitAmps', label: 'Limit', display: 'numeric', quantity: 'current', unit: 'A', zones: [] }],
    } as unknown as EngineProfile
    editable().items.find((i) => i.id === 'alt-2')!.profile_id = 'alt-prof'
    editable().profiles.push(profile)
    open()
    fireEvent.change(rowBox('altLimit'), { target: { value: 'limitAmps' } })
    expect(saveButton()).toBeEnabled()
  })

  test('lets the row\'s own expected reading through even if it is not sent', async () => {
    await setup('port')
    open()
    fireEvent.change(rowBox('altWatts'), { target: { value: 'foo' } })
    expect(saveButton()).toBeDisabled()
    fireEvent.change(rowBox('altWatts'), { target: { value: 'power' } })
    expect(saveButton()).toBeEnabled()
  })

  test('does not block a reading that was already there when the dialog opened', async () => {
    await setup('port')
    open({ ...blank, footer: { alternatorId: 'alt-2', altLimit: { ...blankAmps, suffix: 'foo' } } })
    expect(rowBox('altLimit').value).toBe('foo')
    expect(saveButton()).toBeEnabled()
    fireEvent.change(rowBox('altLimit'), { target: { value: 'bar' } })
    expect(saveButton()).toBeDisabled()
    fireEvent.change(rowBox('altLimit'), { target: { value: 'foo' } })
    expect(saveButton()).toBeEnabled()
  })

  test('does not block while the paths are unknown or unreadable', async () => {
    state.error = new Error('down')
    await setup('port')
    open()
    pickAlternator('alt-2')
    fireEvent.change(rowBox('altLimit'), { target: { value: 'foo' } })
    expect(saveButton()).toBeEnabled()
  })

  test('the old "needs a reading" block stays for the battery', async () => {
    await setup('port')
    open()
    fireEvent.change(document.querySelector('#cluster-footer-battery-equipment') as HTMLSelectElement, { target: { value: 'batt-quiet' } })
    expect(saveButton()).toBeDisabled()
    expect(screen.getByText(/On the Alternator & more tab, Starter battery voltage needs a reading/i)).toBeInTheDocument()
  })
})

describe('switching alternators before SignalK can be read', () => {
  const saved = (rows: Partial<EngineClusterConfig['footer'] & object> = {}): EngineClusterConfig => ({
    ...blank, footer: { alternatorId: 'alt-2', altAmps: { ...blankAmps, suffix: 'current' }, ...rows },
  })

  test('leaves the readings alone while the paths load, then switches them when they arrive', async () => {
    state.extraPaths = ['electrical.alternator.99.rpm']
    await setup('port', null)
    state.loading = true
    const onSave = vi.fn()
    const w = widget(saved())
    const view = render(<EngineClusterConfigDialog widget={w} onCancel={vi.fn()} onSave={onSave} />)
    pickAlternator('alt-quiet')
    expect(rowBox('altAmps').value).toBe('current')
    state.loading = false
    view.rerender(<EngineClusterConfigDialog widget={w} onCancel={vi.fn()} onSave={onSave} />)
    // The new alternator does not send current, so the switch drops it.
    expect(rowBox('altAmps').value).toBe('')
  })

  test('keeps a reading the new alternator sends once the paths arrive', async () => {
    state.extraPaths = ['electrical.alternator.99.current']
    await setup('port', null)
    state.loading = true
    const w = widget(saved())
    const view = render(<EngineClusterConfigDialog widget={w} onCancel={vi.fn()} onSave={vi.fn()} />)
    pickAlternator('alt-quiet')
    state.loading = false
    view.rerender(<EngineClusterConfigDialog widget={w} onCancel={vi.fn()} onSave={vi.fn()} />)
    expect(rowBox('altAmps').value).toBe('current')
  })

  test('leaves the readings alone when SignalK cannot be read', async () => {
    state.error = new Error('down')
    await setup('port', null)
    open(saved())
    pickAlternator('alt-quiet')
    expect(rowBox('altAmps').value).toBe('current')
    expect(rowBox('altWatts').value).toBe('')
  })

  test('clearing the picker blanks the rows at once, even while the paths load', async () => {
    state.loading = true
    await setup('port', null)
    open(saved())
    pickAlternator('')
    expect(rowBox('altAmps').value).toBe('')
  })

  test('opening a saved card with the paths still loading fills nothing when they arrive', async () => {
    await setup('port')
    state.loading = true
    const w = widget(saved())
    const view = render(<EngineClusterConfigDialog widget={w} onCancel={vi.fn()} onSave={vi.fn()} />)
    state.loading = false
    view.rerender(<EngineClusterConfigDialog widget={w} onCancel={vi.fn()} onSave={vi.fn()} />)
    expect(rowBox('altWatts').value).toBe('')
  })
})

describe('the default alternator when the card has no side', () => {
  test('a card with no side and a starboard engine gets the starboard alternator', async () => {
    await setup('starboard')
    open()
    expect(alternatorBox().value).toBe('alt-12')
  })

  test('an explicit Engine side still wins over the engine', async () => {
    await setup('starboard')
    open({ ...blank, side: 'port' })
    expect(alternatorBox().value).toBe('alt-2')
  })
})

describe('an alternator that is no longer in Equipment', () => {
  const gone: EngineClusterConfig = { ...blank, footer: { alternatorId: 'deleted', altAmps: { ...blankAmps, suffix: 'current' } } }
  const summary = () => (screen.getByText('Alternator current (also drawn as a one-hour trace)').closest('summary') as HTMLElement).textContent

  test('is said under the picker, and the bound rows say Equipment missing', async () => {
    await setup('port')
    open(gone)
    expect(screen.getByText('That alternator is no longer in Equipment. Pick another.')).toBeInTheDocument()
    expect(summary()).toContain('Equipment missing')
  })

  test('says nothing while the equipment list is empty, and the row shows two dashes', async () => {
    await setup('port')
    editable().items.length = 0
    open(gone)
    expect(screen.queryByText(/no longer in Equipment/)).toBeNull()
    expect(summary()).toContain('--')
  })

  test('says nothing when no alternator was chosen', async () => {
    await setup('port', null)
    open()
    expect(screen.queryByText(/no longer in Equipment/)).toBeNull()
  })
})

describe('typing in an alternator row', () => {
  test('does not clear the zones or scale of a reading that is not on the profile', async () => {
    await setup('port')
    const zones = [{ from: 80, to: 100, state: 'warn' as const }]
    const onSave = open({
      ...blank,
      footer: { alternatorId: 'alt-2', altLimit: { ...blankAmps, suffix: 'foo', min: 1, max: 9, zones } },
    })
    fireEvent.change(rowBox('altLimit'), { target: { value: 'fo' } })
    fireEvent.change(rowBox('altLimit'), { target: { value: 'foo' } })
    fireEvent.click(saveButton())
    const saved = await savedConfig(onSave)
    expect(saved.footer?.altLimit).toMatchObject({ suffix: 'foo', min: 1, max: 9, zones })
  })
})

const blankAmps: GaugeWidgetConfig = { path: '', label: 'Alt', display: 'numeric', quantity: 'current', unit: 'A', decimals: 0, suffix: '' }

describe('switching the alternator, as a function', () => {
  const profileGauge = { path_suffix: 'limitAmps', label: 'Limit', display: 'numeric', quantity: 'current', unit: 'A', decimals: 1, min: 0, max: 300, zones: [] }
  const alt = (id: string, source: string, profile_id = '') => ({ id, name: id, signalk_source: source, source_kind: 'alternator' as const, profile_id })
  const ctx = (): EquipmentTileContext => ({
    items: [alt('a', 'electrical.alternator.1', 'p'), alt('b', 'electrical.alternator.2')],
    profiles: [{ id: 'p', name: 'P', kind: 'alternator', gauges: [profileGauge] } as unknown as EngineProfile],
    reporting: null,
  })
  const row = (suffix: string, extra: Partial<GaugeWidgetConfig> = {}): GaugeWidgetConfig => ({ ...blankAmps, suffix, ...extra })
  const itemOf = (c: EquipmentTileContext, id: string) => c.items.find((i) => i.id === id)

  test('a row keeps its reading when the new alternator sends it live', () => {
    const c = ctx()
    const rows = switchAlternatorRows({ altLimit: row('fieldDrive') }, itemOf(c, 'b'), c, ['electrical.alternator.2.fieldDrive'])
    expect(rows.altLimit.suffix).toBe('fieldDrive')
  })

  test('a row keeps its reading when it is on the new alternator\'s profile', () => {
    const c = ctx()
    const rows = switchAlternatorRows({ altLimit: row('limitAmps') }, itemOf(c, 'a'), c, [])
    expect(rows.altLimit.suffix).toBe('limitAmps')
    // Profile gauges apply: the quantity, unit and scale are the profile's.
    expect(rows.altLimit).toMatchObject({ decimals: 1, min: 0, max: 300, unit: 'A' })
  })

  test('a row the new alternator does not send goes to its expected reading if that is sent', () => {
    const c = ctx()
    const rows = switchAlternatorRows({ altAmps: row('foo') }, itemOf(c, 'b'), c, ['electrical.alternator.2.current'])
    expect(rows.altAmps.suffix).toBe('current')
  })

  test('otherwise it goes blank', () => {
    const c = ctx()
    const rows = switchAlternatorRows({ altAmps: row('foo'), altLimit: row('foo') }, itemOf(c, 'b'), c, ['electrical.alternator.2.rpm'])
    expect(rows.altAmps.suffix).toBe('')
    expect(rows.altLimit.suffix).toBe('')
    expect(rows.altWatts.suffix).toBe('')
  })

  test('quantity, unit and scale reset to the row\'s own, and zones go', () => {
    const c = ctx()
    const old = row('foo', { quantity: 'pressure', unit: 'Pa', min: 1, max: 9, decimals: 3, zones: [{ from: 1, to: 2, state: 'warn' }] })
    const rows = switchAlternatorRows({ altAmps: old }, itemOf(c, 'b'), c, [])
    expect(rows.altAmps).toMatchObject({ suffix: '', quantity: 'current', unit: 'A', decimals: 0 })
    expect(rows.altAmps.min).toBeUndefined()
    expect(rows.altAmps.max).toBeUndefined()
    expect(rows.altAmps.zones).toBeUndefined()
  })

  test('the rows hold no equipment', () => {
    const c = ctx()
    const rows = switchAlternatorRows({ altAmps: row('current', { equipmentId: 'old' }) }, itemOf(c, 'b'), c, ['electrical.alternator.2.current'])
    expect(rows.altAmps).not.toHaveProperty('equipmentId')
  })

  test('with the paths unknown only the profile can vouch for a reading', () => {
    const c = ctx()
    const rows = switchAlternatorRows({ altAmps: row('current'), altLimit: row('limitAmps') }, itemOf(c, 'a'), c, null)
    expect(rows.altAmps.suffix).toBe('')
    expect(rows.altLimit.suffix).toBe('limitAmps')
  })

  test('no alternator blanks every row', () => {
    const c = ctx()
    const rows = switchAlternatorRows({ altAmps: row('current'), altLimit: row('limitAmps') }, undefined, c, ['x'])
    for (const r of Object.values(rows)) expect(r.suffix).toBe('')
  })

  test('filling only touches blank rows and leaves chosen readings alone', () => {
    const c = ctx()
    const rows = fillBlankAlternatorRows(
      { altAmps: row('foo'), altWatts: row('') }, itemOf(c, 'b'), c,
      ['electrical.alternator.2.current', 'electrical.alternator.2.power'],
    )
    expect(rows.altAmps.suffix).toBe('foo')
    expect(rows.altWatts.suffix).toBe('power')
    expect(rows.altLimit.suffix).toBe('')
  })

  test('filling with the paths unknown fills nothing', () => {
    const c = ctx()
    expect(fillBlankAlternatorRows({}, itemOf(c, 'b'), c, null).altAmps.suffix).toBe('')
  })

  test('the default alternator follows the side, and none is empty', () => {
    const roles = { port_alternator: 'p', starboard_alternator: 's' }
    expect(defaultAlternatorId('port', roles)).toBe('p')
    expect(defaultAlternatorId(undefined, roles)).toBe('p')
    expect(defaultAlternatorId('stbd', roles)).toBe('s')
    expect(defaultAlternatorId('stbd', { port_alternator: 'p', starboard_alternator: 'none' })).toBe('')
    expect(defaultAlternatorId('port', { port_alternator: '', starboard_alternator: 's' })).toBe('')
    expect(defaultAlternatorId('port', null)).toBe('')
  })
})

describe('the slots no profile fills', () => {
  async function fillThen(edit: () => void) {
    await setup('port', null)
    const onSave = open()
    fireEvent.click(screen.getByRole('button', { name: /fill slots from profile/i }))
    // The battery and exhaust rows live on the Alternator & more tab.
    fireEvent.click(screen.getByRole('tab', { name: /alternator & more/i }))
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
  test('the exhaust starts with no scale or threshold of ours', async () => {
    const saved = await fillThen(() => {
      fireEvent.change(document.querySelector('#cluster-footer-egt-equipment') as HTMLSelectElement, { target: { value: 'exh' } })
    })
    const egt = saved.footer!.egt!
    expect([egt.equipmentId, egt.suffix, egt.quantity, egt.unit, egt.min, egt.max]).toEqual(['exh', 'exhaustTemperature', 'temperature', 'C', undefined, undefined])
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
    await setup('port', null)
    const onSave = open()
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    const saved = await vi.waitFor(() => onSave.mock.calls[0][0] as EngineClusterConfig)
    expect(saved.arcs).toBeUndefined()
    expect(saved.footer).toBeUndefined()
  })
})

describe('a footer slot reads what the picked item publishes', () => {
  const pick = (slot: string, id: string) =>
    fireEvent.change(document.querySelector(`#cluster-footer-${slot}-equipment`) as HTMLSelectElement, { target: { value: id } })

  async function openFresh(extra: string[]) {
    state.extraPaths = extra
    await setup('port')
    return open()
  }

  test('the expected reading is set from the item without typing', async () => {
    const onSave = await openFresh(['electrical.batteries.292.voltage', 'electrical.batteries.292.current'])
    pick('battery', 'batt')
    expect((document.querySelector('#cluster-footer-battery-suffix') as HTMLInputElement).value).toBe('voltage')
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    const saved = await vi.waitFor(() => onSave.mock.calls[0][0] as EngineClusterConfig)
    expect(saved.footer?.battery).toMatchObject({ equipmentId: 'batt', suffix: 'voltage' })
  })

  test('a reading the item does not send is not filled, and the field says so', async () => {
    const onSave = await openFresh(['electrical.batteries.777.current', 'electrical.batteries.777.capacity.stateOfCharge'])
    pick('battery', 'batt-quiet')
    expect((document.querySelector('#cluster-footer-battery-suffix') as HTMLInputElement).value).toBe('')
    expect(screen.getByText(/Quiet battery isn't sending voltage/i)).toBeTruthy()
    const options = Array.from(document.querySelectorAll('#cluster-footer-battery-suffixes option')).map((o) => (o as HTMLOptionElement).value)
    expect(options).toContain('current')
    expect(options).toContain('capacity.stateOfCharge')
    expect((screen.getByRole('button', { name: /^save$/i }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText(/Starter battery voltage needs a reading/i)).toBeTruthy()
    expect(onSave).not.toHaveBeenCalled()
  })

  test('picking a reading the item sends lets the save through', async () => {
    const onSave = await openFresh(['electrical.batteries.777.current'])
    pick('battery', 'batt-quiet')
    fireEvent.change(document.querySelector('#cluster-footer-battery-suffix') as HTMLInputElement, { target: { value: 'current' } })
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    const saved = await vi.waitFor(() => onSave.mock.calls[0][0] as EngineClusterConfig)
    expect(saved.footer?.battery).toMatchObject({ equipmentId: 'batt-quiet', suffix: 'current' })
  })

  const suffixBox = (slot: string) => document.querySelector(`#cluster-footer-${slot}-suffix`) as HTMLInputElement

  test('a reading the newly picked item does not send does not carry over', async () => {
    await openFresh(['electrical.batteries.777.current'])
    pick('battery', 'batt')
    expect(suffixBox('battery').value).toBe('voltage')
    pick('battery', 'batt-quiet')
    expect(suffixBox('battery').value).toBe('')
    expect(screen.getByText(/Quiet battery isn't sending voltage/i)).toBeTruthy()
  })

  test('a reading the newly picked item sends carries over', async () => {
    await openFresh(['electrical.batteries.777.current'])
    pick('battery', 'batt')
    fireEvent.change(suffixBox('battery'), { target: { value: 'current' } })
    pick('battery', 'batt-quiet')
    expect(suffixBox('battery').value).toBe('current')
  })

  test('clearing the reading leaves it clear', async () => {
    await openFresh([])
    pick('battery', 'batt')
    fireEvent.change(suffixBox('battery'), { target: { value: '' } })
    expect(suffixBox('battery').value).toBe('')
  })

  test('while the paths load it claims nothing, then fills when they arrive', async () => {
    state.loading = true
    state.extraPaths = []
    await setup('port')
    const onSave = vi.fn()
    const view = render(<EngineClusterConfigDialog widget={widget(blank)} onCancel={vi.fn()} onSave={onSave} />)
    pick('battery', 'batt')
    expect(suffixBox('battery').value).toBe('')
    expect(screen.queryByText(/isn't sending/i)).toBeNull()
    state.loading = false
    view.rerender(<EngineClusterConfigDialog widget={widget(blank)} onCancel={vi.fn()} onSave={onSave} />)
    expect(suffixBox('battery').value).toBe('voltage')
  })

  test('when SignalK cannot be read it says so and does not claim the item is silent', async () => {
    state.error = new Error('down')
    await setup('port', null)
    open()
    pick('battery', 'batt')
    expect(screen.getByText(/can't read what SignalK is sending/i)).toBeTruthy()
    expect(screen.queryByText(/isn't sending/i)).toBeNull()
  })

  test('an item with no SignalK source says so', async () => {
    await openFresh([])
    pick('battery', 'batt-nosrc')
    expect(screen.getByText(/Unmapped battery has no SignalK source/i)).toBeTruthy()
  })

  test('filling from the profile leaves out an alternator row the alternator does not send', async () => {
    await setup('port', { port_alternator: 'alt-quiet', starboard_alternator: 'alt-quiet' })
    open()
    fireEvent.click(screen.getByRole('button', { name: /fill slots from profile/i }))
    expect(suffixBox('altTemp').value).toBe('')
    expect((screen.getByRole('button', { name: /^save$/i }) as HTMLButtonElement).disabled).toBe(false)
  })

  test('an alternator sending only current seeds the current row and leaves the rest blank', async () => {
    state.extraPaths = ['electrical.alternator.99.current']
    await setup('port', { port_alternator: 'alt-quiet', starboard_alternator: 'alt-quiet' })
    const onSave = open()
    fireEvent.click(screen.getByRole('button', { name: /fill slots from profile/i }))
    for (const k of ['altWatts', 'altTemp', 'altStage']) expect(suffixBox(k).value, k).toBe('')
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    const saved = await vi.waitFor(() => onSave.mock.calls[0][0] as EngineClusterConfig)
    expect(saved.footer?.alternatorId).toBe('alt-quiet')
    expect(saved.footer?.altAmps).toMatchObject({ suffix: 'current' })
    expect(saved.footer?.altWatts).toBeUndefined()
    expect(saved.footer?.altTemp).toBeUndefined()
    expect(saved.footer?.altStage).toBeUndefined()
  })

  // The Fill button refills from the alternator on the picker, whatever Roles says.
  test('filling from the profile refills blank alternator rows from the picker\'s alternator, not Roles', async () => {
    state.extraPaths = ['electrical.alternator.99.current', 'electrical.alternator.99.power']
    await setup('port')
    open()
    pickAlternator('alt-quiet')
    for (const key of ['altWatts']) fireEvent.change(suffixBox(key), { target: { value: '' } })
    fireEvent.change(suffixBox('altAmps'), { target: { value: 'current' } })
    fireEvent.click(screen.getByRole('button', { name: /fill slots from profile/i }))
    expect(alternatorBox().value).toBe('alt-quiet')
    expect(suffixBox('altAmps').value).toBe('current')
    expect(suffixBox('altWatts').value).toBe('power')
  })

  test('filling from the profile keeps readings the operator chose', async () => {
    await setup('port')
    open()
    fireEvent.change(suffixBox('altAmps'), { target: { value: 'fieldDrive' } })
    fireEvent.click(screen.getByRole('button', { name: /fill slots from profile/i }))
    expect(suffixBox('altAmps').value).toBe('fieldDrive')
  })

  test('a changed item does not keep the old item\'s scale', async () => {
    const onSave = await openFresh(['propulsion.0.revolutions'])
    pick('egt', 'eng')
    fireEvent.change(suffixBox('egt'), { target: { value: 'revolutions' } })
    pick('egt', 'exh')
    expect(suffixBox('egt').value).toBe('revolutions')
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    const saved = await vi.waitFor(() => onSave.mock.calls[0][0] as EngineClusterConfig)
    const egt = saved.footer!.egt!
    expect([egt.equipmentId, egt.quantity, egt.unit, egt.min, egt.max, egt.zones]).toEqual(['exh', 'temperature', 'C', undefined, undefined, undefined])
  })

  test('several slots missing a reading are named in one alert', async () => {
    await openFresh([])
    pick('battery', 'batt-quiet')
    pick('egt', 'exh')
    fireEvent.change(suffixBox('egt'), { target: { value: '' } })
    const alert = screen.getByRole('status')
    expect(alert.textContent).toMatch(/Starter battery voltage/)
    expect(alert.textContent).toMatch(/Exhaust temperature/)
    expect(alert.textContent).toMatch(/need a reading/)
  })

  test('a folded footer row says when it has equipment but no reading', async () => {
    await openFresh([])
    pick('battery', 'batt-quiet')
    expect(screen.getByText('Needs a reading')).toBeTruthy()
  })
})
