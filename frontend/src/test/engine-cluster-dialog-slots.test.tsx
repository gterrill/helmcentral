import { fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { EngineClusterConfigDialog } from '@/components/engine-cluster-config-dialog'
import { GaugeFields } from '@/components/gauge-fields'
import type { DashboardLayoutItem, EngineClusterConfig, GaugeWidgetConfig } from '@/lib/dashboard-widgets'
import type { EquipmentTileContext } from '@/lib/equipment-tiles'
import { setViewportWidth } from './viewport'

const ctx: EquipmentTileContext = {
  items: [
    { id: 'eng-port', name: 'Port engine', signalk_source: 'propulsion.port', source_kind: 'engine', profile_id: '' },
    { id: 'batt', name: 'Port start battery', signalk_source: 'electrical.batteries.292', source_kind: 'battery', profile_id: '' },
    { id: 'batt-quiet', name: 'Quiet battery', signalk_source: 'electrical.batteries.777', source_kind: 'battery', profile_id: '' },
  ],
  profiles: [],
  reporting: null,
}
vi.mock('@/hooks/use-equipment-tile-context', () => ({ useEquipmentTileContext: () => ctx }))
const feed = vi.hoisted(() => ({ loading: false }))
vi.mock('@/hooks/use-signalk-paths', () => ({ useSignalKPaths: () => ({ paths: [], loading: feed.loading }) }))
vi.mock('@/hooks/use-vessel-roles', () => ({ useVesselRoles: () => null }))
vi.mock('@/hooks/use-vessel-discovery', () => ({
  useVesselDiscovery: () => ({ discovery: { connected: true, groups: [], detectors: {} }, loading: false, error: null }),
}))

afterEach(() => { feed.loading = false })

const cluster: EngineClusterConfig = {
  equipmentId: 'eng-port',
  title: 'Port',
  ring: { path: '', suffix: 'revolutions', label: 'RPM', display: 'radial', quantity: 'frequency', unit: 'rpm' },
  centre: { path: '', suffix: 'runTime', label: 'Hours', display: 'numeric', quantity: 'duration', unit: 'h' },
  corners: [],
}
const widget: DashboardLayoutItem = { id: 'cluster:abcd1234', x: 0, y: 0, w: 6, h: 8, cluster }

function open(overrides: Partial<EngineClusterConfig> = {}) {
  const onSave = vi.fn()
  render(<EngineClusterConfigDialog
    widget={{ ...widget, cluster: { ...cluster, ...overrides } }}
    onCancel={vi.fn()} onSave={onSave} />)
  return onSave
}

const emptyRing = { ...cluster.ring, suffix: '' }
const oilRow: GaugeWidgetConfig = { path: '', suffix: 'oilPressure', label: 'Oil', display: 'numeric', quantity: 'pressure', unit: 'Pa' }
const oilCorner = { label: 'Oil', rows: [oilRow] }

/** A slot of the card, found by the name a screen reader gives it. */
const slot = (name: RegExp) => screen.getByRole('button', { name })
/** The label field of whichever editor is showing: hidden editors are not in the accessibility tree. */
const shownLabel = () => screen.getByRole('textbox', { name: 'Label' }) as HTMLInputElement

describe('the card is the menu', () => {
  test('has no tabs', () => {
    open()
    expect(screen.queryByRole('tablist')).toBeNull()
    expect(screen.queryByRole('tab')).toBeNull()
  })

  test('opens on the ring when nothing needs attention', () => {
    open()
    expect(slot(/^ring, revolutions$/i)).toHaveAttribute('aria-pressed', 'true')
    expect(shownLabel().value).toBe('RPM')
  })

  test('shows the editor of the slot that is clicked, and only that one', () => {
    open()
    fireEvent.click(slot(/^centre, runTime$/i))
    expect(slot(/^centre, runTime$/i)).toHaveAttribute('aria-pressed', 'true')
    expect(slot(/^ring, revolutions$/i)).toHaveAttribute('aria-pressed', 'false')
    expect(shownLabel().value).toBe('Hours')
    expect(screen.getAllByRole('textbox', { name: 'Label' })).toHaveLength(1)
  })

  test('puts the arcs, readouts, footer rows and fuel rail on the card as buttons', () => {
    open({ corners: [oilCorner] })
    for (const name of [
      /^coolant arc, not shown$/i, /^gearbox arc, not shown$/i, /^oil, oilPressure$/i,
      /^starter battery voltage, not shown$/i, /^alternator, not set$/i, /^alternator current, not shown$/i,
      /^transmission oil pressure, not shown$/i, /^exhaust temperature, not shown$/i, /^fuel rail, not shown$/i,
    ]) expect(slot(name)).toBeInTheDocument()
  })

  test('names the item on a footer slot that reads one', () => {
    open({
      footer: {
        battery: { path: '', suffix: 'voltage', label: 'Battery', display: 'numeric', quantity: 'potential', unit: 'V', equipmentId: 'batt' },
      },
    })
    expect(within(slot(/^starter battery voltage/i)).getByText('Port start battery')).toBeInTheDocument()
  })

  test('says the equipment is missing when the bound item no longer exists', () => {
    open({
      footer: {
        battery: { path: '', suffix: 'voltage', label: 'Battery', display: 'numeric', quantity: 'potential', unit: 'V', equipmentId: 'gone' },
      },
    })
    expect(within(slot(/^starter battery voltage/i)).getByText('Equipment missing')).toBeInTheDocument()
  })
})

describe('what needs attention', () => {
  test('marks a ring with no reading, and opens on it', () => {
    open({ ring: emptyRing })
    const ring = slot(/^ring, needs a reading$/i)
    expect(ring).toHaveAttribute('data-state', 'attention')
    expect(ring).toHaveAttribute('aria-pressed', 'true')
  })

  test('marks a centre with no reading', () => {
    open({ centre: { ...cluster.centre, suffix: '' } })
    expect(slot(/^centre, needs a reading$/i)).toHaveAttribute('data-state', 'attention')
  })

  test('opens on the first slot that needs attention, not on the ring', () => {
    open({ corners: [oilCorner, { label: 'Boost', rows: [{ ...oilRow, suffix: '' }] }] })
    expect(slot(/^boost, needs a reading$/i)).toHaveAttribute('aria-pressed', 'true')
    expect(slot(/^ring, revolutions$/i)).toHaveAttribute('aria-pressed', 'false')
  })

  test('stays on the slot being edited when its reading is fixed', () => {
    open({
      footer: {
        battery: { path: '', suffix: '', label: 'Battery', display: 'numeric', quantity: 'potential', unit: 'V', equipmentId: 'batt-quiet' },
      },
    })
    expect(slot(/^starter battery voltage, needs a reading$/i)).toHaveAttribute('aria-pressed', 'true')
    fireEvent.change(document.querySelector('#cluster-footer-battery-suffix') as HTMLInputElement, { target: { value: 'voltage' } })
    expect(slot(/^starter battery voltage, quiet battery voltage$/i)).toHaveAttribute('aria-pressed', 'true')
  })

  test('marks a footer slot with equipment but no reading', () => {
    open()
    fireEvent.click(slot(/^starter battery voltage/i))
    fireEvent.change(document.querySelector('#cluster-footer-battery-equipment') as HTMLSelectElement, { target: { value: 'batt-quiet' } })
    expect(slot(/^starter battery voltage, needs a reading$/i)).toHaveAttribute('data-state', 'attention')
  })

  test('marks a fuel rail with a bar that has no tank', () => {
    open({ fuel: { side: 'left', bars: [{ tankId: '', label: 'Fwd' }] } })
    expect(slot(/^fuel rail, needs a tank$/i)).toHaveAttribute('data-state', 'attention')
    expect(slot(/^fuel rail/i)).toHaveAttribute('aria-pressed', 'true')
  })

  test('draws a slot that shows nothing with a dashed outline', () => {
    open()
    const egt = slot(/^exhaust temperature, not shown$/i)
    expect(egt).toHaveAttribute('data-state', 'empty')
    expect(egt.className).toContain('border-dashed')
  })
})

describe('the slot editor', () => {
  test('has no Display select, which the card never reads', () => {
    open({ corners: [oilCorner] })
    expect(screen.queryByRole('combobox', { name: 'Display' })).toBeNull()
    expect(document.querySelector('[id$="-display"]')).toBeNull()
  })

  test('keeps the scale of the ring in view', () => {
    open()
    expect(screen.getByRole('spinbutton', { name: /scale minimum/i })).toBeInTheDocument()
    expect(screen.getByRole('spinbutton', { name: /scale maximum/i })).toBeInTheDocument()
  })

  test('keeps Units and quantity folded away until asked for', () => {
    open({ corners: [oilCorner] })
    const folds = Array.from(document.querySelectorAll('details')).filter((d) => d.textContent?.includes('Units and quantity'))
    expect(folds.length).toBeGreaterThan(0)
    for (const fold of folds) expect(fold).not.toHaveAttribute('open')
  })

  test('picks the exhaust sensor, not an engine', () => {
    open()
    fireEvent.click(slot(/^exhaust temperature/i))
    const box = document.querySelector('#cluster-footer-egt-equipment') as HTMLSelectElement
    expect(box.options[0].textContent).toBe('Pick the exhaust sensor…')
  })

  test('picks the starter battery', () => {
    open()
    fireEvent.click(slot(/^starter battery voltage/i))
    const box = document.querySelector('#cluster-footer-battery-equipment') as HTMLSelectElement
    expect(box.options[0].textContent).toBe('Pick the starter battery…')
  })

  test('puts the alternator picker with the alternator', () => {
    open()
    fireEvent.click(slot(/^alternator, not set$/i))
    expect(screen.getByRole('combobox', { name: 'Alternator' })).toBe(document.querySelector('#cluster-alternator-equipment'))
  })
})

describe('adding and removing readout cards', () => {
  test('adds a card, opens it, and stops at four', () => {
    open({ corners: [oilCorner] })
    fireEvent.click(screen.getByRole('button', { name: /add readout/i }))
    expect(slot(/^new, needs a reading$/i)).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(screen.getByRole('button', { name: /add readout/i }))
    fireEvent.click(screen.getByRole('button', { name: /add readout/i }))
    expect(screen.queryByRole('button', { name: /add readout/i })).toBeNull()
  })

  test('removes a card and falls back to the ring', () => {
    open({ corners: [oilCorner] })
    fireEvent.click(slot(/^oil, oilPressure$/i))
    fireEvent.click(screen.getByRole('button', { name: /remove readout 1/i }))
    expect(screen.queryByRole('button', { name: /^oil/i })).toBeNull()
    expect(slot(/^ring, revolutions$/i)).toHaveAttribute('aria-pressed', 'true')
  })

  test('calls them readouts, never corners', () => {
    open({ corners: [oilCorner] })
    fireEvent.click(slot(/^oil, oilPressure$/i))
    expect(screen.getByRole('textbox', { name: 'Readout 1 label' })).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: 'Readout 1 icon' })).toBeInTheDocument()
    expect(screen.queryByLabelText(/corner/i)).toBeNull()
  })
})

describe('the card follows the engine side', () => {
  const before = (a: HTMLElement, b: HTMLElement) => Boolean(a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING)

  test('a port card lists the dial before the readouts, a starboard card after', () => {
    open({ corners: [oilCorner] })
    expect(before(slot(/^ring/i), slot(/^oil/i))).toBe(true)
    expect(before(slot(/^fuel rail/i), slot(/^ring/i))).toBe(true)
  })

  test('a starboard card reverses them, so the tab order still follows the picture', () => {
    open({ side: 'stbd', corners: [oilCorner] })
    expect(before(slot(/^oil/i), slot(/^ring/i))).toBe(true)
    expect(before(slot(/^ring/i), slot(/^fuel rail/i))).toBe(true)
    expect(before(slot(/^exhaust temperature/i), slot(/^starter battery voltage/i))).toBe(true)
  })
})

describe('why Save is disabled', () => {
  test('names the ring, not a tab', () => {
    open({ ring: emptyRing })
    expect(screen.getByRole('button', { name: /^save$/i })).toBeDisabled()
    expect(screen.getByText('Choose a reading for the ring.')).toBeInTheDocument()
  })

  test('names the centre when only it is blank', () => {
    open({ centre: { ...cluster.centre, suffix: '' } })
    expect(screen.getByText('Choose a reading for the centre.')).toBeInTheDocument()
  })

  test('names the footer slot, not a tab', () => {
    open()
    fireEvent.click(slot(/^starter battery voltage/i))
    fireEvent.change(document.querySelector('#cluster-footer-battery-equipment') as HTMLSelectElement, { target: { value: 'batt-quiet' } })
    const reason = screen.getByRole('status').textContent ?? ''
    expect(reason).toMatch(/^Starter battery voltage needs a reading/)
    expect(reason).not.toMatch(/\btab\b/i)
  })

  test('names the fuel rail when a bar has no tank', () => {
    open({ fuel: { side: 'left', bars: [{ tankId: '', label: 'Fwd' }] } })
    expect(screen.getByText('Choose a tank for every fuel bar, or remove the rail.')).toBeInTheDocument()
  })

  test('says nothing when Save is enabled', () => {
    open()
    expect(screen.getByRole('button', { name: /^save$/i })).toBeEnabled()
    expect(screen.queryByText(/^Choose a/)).not.toBeInTheDocument()
  })
})

describe('where the dialog opens', () => {
  // An alternator that was deleted only shows as missing once the equipment
  // list has loaded, so what needs attention is not known before then.
  const gone = { ...widget, cluster: { ...cluster, footer: { alternatorId: 'deleted' } } }
  // The mock hands out this one object, so a test can empty the list and fill it again.
  const items = ctx.items as Array<EquipmentTileContext['items'][number]>
  const loaded = [...items]
  afterEach(() => { items.splice(0, items.length, ...loaded) })

  test('waits for the equipment list before choosing the slot to open on', () => {
    items.splice(0)
    const view = render(<EngineClusterConfigDialog widget={gone} onCancel={vi.fn()} onSave={vi.fn()} />)
    items.push(...loaded)
    view.rerender(<EngineClusterConfigDialog widget={gone} onCancel={vi.fn()} onSave={vi.fn()} />)
    expect(slot(/^alternator, equipment missing$/i)).toHaveAttribute('aria-pressed', 'true')
  })

  test('keeps a slot the operator clicked while it waited', () => {
    items.splice(0)
    const view = render(<EngineClusterConfigDialog widget={gone} onCancel={vi.fn()} onSave={vi.fn()} />)
    fireEvent.click(slot(/^centre, runTime$/i))
    items.push(...loaded)
    view.rerender(<EngineClusterConfigDialog widget={gone} onCancel={vi.fn()} onSave={vi.fn()} />)
    expect(slot(/^centre, runTime$/i)).toHaveAttribute('aria-pressed', 'true')
  })
})

describe('a missing alternator', () => {
  test('blocks Save, because its outline is amber', () => {
    open({ footer: { alternatorId: 'deleted' } })
    expect(slot(/^alternator, equipment missing$/i)).toHaveAttribute('data-state', 'attention')
    expect(screen.getByRole('button', { name: /^save$/i })).toBeDisabled()
    expect(screen.getByRole('status').textContent).toBe(
      'The alternator on this card is no longer in Equipment. Pick another, or clear it.',
    )
  })
})

describe('the scale form on a cluster slot', () => {
  test('leaves out the trend window, which the card never draws', () => {
    const slotConfig: GaugeWidgetConfig = { ...oilRow, display: 'trend' }
    render(<GaugeFields value={slotConfig} paths={[]} idPrefix="t" onChange={vi.fn()} variant="cluster" scaled />)
    expect(screen.queryByLabelText('History window')).toBeNull()
    expect(screen.queryByLabelText('Display')).toBeNull()
  })
})

describe('zone notes', () => {
  const held = { ...oilRow, zones: [{ from: 0, to: 10, state: 'alarm' as const }] }

  test('show only in the editor of the slot they are about', () => {
    open({ corners: [{ label: 'Oil', rows: [held] }] })
    const note = screen.getByTestId('zone-migration')
    expect(note.textContent).toContain('Oil')
    expect(note.closest('[hidden]')).not.toBeNull()
    fireEvent.click(slot(/^oil, oilPressure$/i))
    expect(screen.getByTestId('zone-migration').closest('[hidden]')).toBeNull()
    fireEvent.click(slot(/^ring, revolutions$/i))
    expect(screen.getByTestId('zone-migration').closest('[hidden]')).not.toBeNull()
  })

  test('go by slot, not by label: two readouts called alike each show their own', () => {
    open({
      corners: [
        { label: 'A', rows: [{ ...held, label: 'Oil' }] },
        { label: 'B', rows: [{ ...held, suffix: 'boostPressure', label: 'Oil' }] },
      ],
    })
    const notes = screen.getAllByTestId('zone-migration')
    expect(notes).toHaveLength(2)
    for (const note of notes) expect(note.textContent).toMatch(/Zones on Oil stay on this tile/)
  })

  test('are not shown at all for slots with nothing to say', () => {
    open({ corners: [oilCorner] })
    expect(screen.queryByTestId('zone-migration')).toBeNull()
  })
})

describe('zone choices', () => {
  const gaugeWithZone = (suffix: string, quantity = 'pressure', unit = 'Pa') => ({
    path_suffix: suffix, label: suffix, display: 'numeric', quantity, unit, min: 0, max: 1000,
    zones: [{ direction: 'below', threshold: 100, state: 'alarm' }],
  })
  const profile = {
    id: 'p', name: 'Test engine', kind: 'engine',
    gauges: [gaugeWithZone('oilPressure'), gaugeWithZone('boostPressure'), gaugeWithZone('runTime', 'duration', 'h')],
  } as unknown as EquipmentTileContext['profiles'][number]
  const profiles = ctx.profiles as EquipmentTileContext['profiles'][number][]
  const engine = ctx.items[0] as { profile_id: string }
  afterEach(() => { profiles.splice(0); engine.profile_id = '' })
  const held = (suffix: string): GaugeWidgetConfig => ({ ...oilRow, suffix, label: suffix, min: 0, max: 1000, zones: [{ from: 0, to: 50, state: 'warn' }] })
  const withProfile = () => { profiles.push(profile); engine.profile_id = 'p' }

  test('stay with the readout they were made for when an earlier readout is removed', () => {
    withProfile()
    open({ corners: [{ label: 'A', rows: [oilRow] }, { label: 'B', rows: [held('oilPressure')] }, { label: 'C', rows: [held('boostPressure')] }] })
    fireEvent.click(slot(/^B, oilPressure$/))
    fireEvent.click(screen.getByRole('radio', { name: /Keep the profile's/ }))
    fireEvent.click(slot(/^C, boostPressure$/))
    fireEvent.click(screen.getByRole('radio', { name: /Use this tile's, replacing the profile's/ }))
    fireEvent.click(slot(/^A, oilPressure$/))
    fireEvent.click(screen.getByRole('button', { name: /remove readout 1/i }))
    fireEvent.click(slot(/^B, oilPressure$/))
    expect(screen.getByRole('radio', { name: /Keep the profile's/ })).toBeChecked()
    expect(screen.getByRole('radio', { name: /Use this tile's, replacing the profile's/ })).not.toBeChecked()
    fireEvent.click(slot(/^C, boostPressure$/))
    expect(screen.getByRole('radio', { name: /Use this tile's, replacing the profile's/ })).toBeChecked()
  })

  test('a readout keeps its own editor state when an earlier readout is removed', () => {
    open({ corners: [{ label: 'A', rows: [oilRow] }, { label: 'B', rows: [{ ...oilRow, suffix: 'boostPressure' }] }] })
    fireEvent.click(slot(/^B, boostPressure$/))
    // The one fold that is showing: the others are in hidden editors.
    const shown = () => Array.from(document.querySelectorAll('details')).filter((d) => !d.closest('[hidden]'))
    expect(shown()).toHaveLength(1)
    shown()[0].open = true
    fireEvent.click(slot(/^A, oilPressure$/))
    fireEvent.click(screen.getByRole('button', { name: /remove readout 1/i }))
    fireEvent.click(slot(/^B, boostPressure$/))
    expect(shown()[0].open).toBe(true)
  })

  test('the centre can have its held zones cleared, though it has no scale', () => {
    withProfile()
    open({ centre: { ...cluster.centre, zones: [{ from: 0, to: 5, state: 'warn' }] } })
    fireEvent.click(slot(/^centre, runTime$/i))
    expect(screen.getByRole('combobox', { name: 'Zone 1 direction' })).toBeInTheDocument()
    expect(screen.queryByRole('spinbutton', { name: /scale minimum/i })).toBeNull()
  })

  test('the centre shows no zone editor when it holds none', () => {
    withProfile()
    open()
    fireEvent.click(slot(/^centre, runTime$/i))
    expect(screen.queryByRole('combobox', { name: 'Zone 1 direction' })).toBeNull()
  })
})

describe('the equipment list that never fills', () => {
  const items = ctx.items as Array<EquipmentTileContext['items'][number]>
  const loaded = [...items]
  afterEach(() => { items.splice(0, items.length, ...loaded); delete (ctx as { loaded?: boolean }).loaded })

  test('still settles on the first blocker once the list has answered empty', () => {
    items.splice(0)
    ;(ctx as { loaded?: boolean }).loaded = true
    open({ centre: { ...cluster.centre, suffix: '' } })
    expect(slot(/^centre, needs a reading$/i)).toHaveAttribute('aria-pressed', 'true')
  })
})

describe('the words around the dialog', () => {
  test('the Fill note says what it replaces and what it keeps', () => {
    const profiles = ctx.profiles as EquipmentTileContext['profiles'][number][]
    const engine = ctx.items[0] as { profile_id: string }
    profiles.push({ id: 'p2', name: 'Plain', kind: 'engine', gauges: [] } as EquipmentTileContext['profiles'][number])
    engine.profile_id = 'p2'
    try {
      open()
      expect(screen.getByText('Replaces the dial, arcs, readouts and gearbox pressure. Alternator, battery, exhaust and fuel stay.')).toBeInTheDocument()
    } finally {
      profiles.splice(0)
      engine.profile_id = ''
    }
  })

  test('the description is worded for the wide layout', () => {
    open()
    expect(screen.getByRole('dialog')).toHaveAccessibleDescription(/click any part of the card/i)
  })

  test('and for the narrow one', () => {
    setViewportWidth(400)
    open()
    expect(screen.getByRole('dialog')).toHaveAccessibleDescription(/tap a row/i)
    expect(screen.getByRole('dialog')).not.toHaveAccessibleDescription(/click any part/i)
  })
})

describe('on a phone', () => {
  test('lists the same slots and opens the editor under the row that was clicked', () => {
    setViewportWidth(400)
    open({ corners: [oilCorner] })
    const oil = slot(/^oil, oilPressure$/i)
    fireEvent.click(oil)
    expect(oil).toHaveAttribute('aria-expanded', 'true')
    const row = oil.parentElement as HTMLElement
    expect(within(row).getByRole('textbox', { name: 'Label' })).toBeInTheDocument()
    expect(screen.getAllByRole('textbox', { name: 'Label' })).toHaveLength(1)
    expect(slot(/^exhaust temperature, not shown$/i)).toBeInTheDocument()
  })

  test('a row is a disclosure: it says what it opens, and a second tap folds it', () => {
    setViewportWidth(400)
    open({ corners: [oilCorner] })
    const oil = slot(/^oil, oilPressure$/i)
    expect(oil).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(oil)
    expect(oil).toHaveAttribute('aria-expanded', 'true')
    const panel = document.getElementById(oil.getAttribute('aria-controls') as string) as HTMLElement
    expect(panel).not.toBeNull()
    expect(panel).not.toHaveAttribute('hidden')
    expect(within(panel).getByRole('textbox', { name: 'Label' })).toBeInTheDocument()
    fireEvent.click(oil)
    expect(oil).toHaveAttribute('aria-expanded', 'false')
    expect(panel).toHaveAttribute('hidden')
    expect(oil).not.toHaveAttribute('aria-pressed')
  })
})
