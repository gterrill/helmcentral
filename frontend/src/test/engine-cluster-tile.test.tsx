import { render, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { EngineClusterTile } from '@/components/engine-cluster-tile'
import type { ResolvedClusterConfig, GaugeWidgetConfig } from '@/lib/dashboard-widgets'
import { setViewportWidth } from './viewport'

// The alternator's one-hour trace reads history from the server. The tile is
// tested against a fixed series so no test depends on a network call.
const history = vi.hoisted(() => ({
  result: { points: [] as Array<{ time: string; value: number }>, loading: false, error: null as string | null },
  calls: [] as Array<[string, string, boolean]>,
}))
vi.mock('@/hooks/use-telemetry-history', () => ({
  useTelemetryHistory: (path: string, window: string, enabled: boolean) => {
    history.calls.push([path, window, enabled])
    return enabled ? history.result : { points: [], loading: false, error: null }
  },
}))

const PSI = 6894.757

/** The live idle readings from the vessel, in the SI the stream carries. */
const values: Record<string, number | null> = {
  'propulsion.port.revolutions': 11.638333,        // 698 RPM
  'propulsion.port.fuel.rate': 4.1666666e-7,       // 1.5 L/h
  'propulsion.port.oilPressure': 22 * PSI,         // 22.0 psi
  'propulsion.port.boostPressure': 0,              // genuinely zero at idle
  'propulsion.port.engineLoad': 0.06,              // 6 %
  'propulsion.port.temperature': 344.15,           // 71 C
  'propulsion.port.transmission.oilTemperature': 310.8, // 37.7 C
  'propulsion.port.transmission.oilPressure': 2349900, // 341 psi
  'propulsion.port.runTime': 3219295,              // 894 h
  'electrical.batteries.292.voltage': 26.4,
  'electrical.alternator.2.current': 48,
  'electrical.alternator.2.power': 1290,
  'electrical.alternator.2.temperature': 330.15,    // 57 C
  'electrical.alternator.2.chargingModeNumber': 4,
  'electrical.alternator.2.limit': 80,
  'propulsion.0.exhaustTemperature': 304.749,       // 31.6 C
  'tanks.fuel.2.currentLevel': 0.87,
  'tanks.fuel.2.capacity': 1.2,
  'tanks.fuel.4.currentLevel': 0.4,
  'tanks.fuel.4.capacity': 1.3,
}

const slot = (path: string, label: string, quantity: string, unit: string, extra: Partial<GaugeWidgetConfig> = {}): GaugeWidgetConfig =>
  ({ path, label, display: 'numeric', quantity, unit, ...extra })

const port: ResolvedClusterConfig = {
  title: 'Port',
  side: 'port',
  ring: slot('propulsion.port.revolutions', 'RPM', 'frequency', 'rpm', { display: 'radial', min: 0, max: 3000, decimals: 0, labelDivisor: 100 }),
  centre: slot('propulsion.port.runTime', 'Hours', 'duration', 'h', { decimals: 0 }),
  corners: [
    { label: 'Oil', rows: [slot('propulsion.port.oilPressure', 'Oil', 'pressure', 'psi', { min: 0, max: 100, decimals: 1 })] },
    { label: 'Boost', rows: [slot('propulsion.port.boostPressure', 'Boost', 'pressure', 'psi', { min: 0, max: 40, decimals: 1 })] },
    { label: 'Load', rows: [slot('propulsion.port.engineLoad', 'Load', 'ratio', 'percent', { min: 0, max: 100, decimals: 0 })] },
    { label: 'Fuel', rows: [slot('propulsion.port.fuel.rate', 'Fuel Flow', 'volumetricFlow', 'Lph', { min: 0, max: 120, decimals: 1 })] },
  ],
}

const arcs = {
  left: slot('propulsion.port.temperature', 'Coolant', 'temperature', 'C', { min: 0, max: 120, decimals: 0, zones: [{ from: 0, to: 85, state: 'normal' as const }] }),
}

const footer: NonNullable<ResolvedClusterConfig['footer']> = {
  battery: slot('electrical.batteries.292.voltage', 'Battery', 'potential', 'V', { decimals: 1, min: 20, max: 30 }),
  altAmps: slot('electrical.alternator.2.current', 'Alternator', 'current', 'A', { decimals: 0, min: 0, max: 200 }),
  altWatts: slot('electrical.alternator.2.power', 'Output', 'power', 'W', { decimals: 0 }),
  altTemp: slot('electrical.alternator.2.temperature', 'Temp', 'temperature', 'C', { decimals: 0 }),
  altStage: slot('electrical.alternator.2.chargingModeNumber', 'Stage', 'raw', 'raw', { decimals: 0 }),
  altLimit: slot('electrical.alternator.2.limit', 'Limit', 'current', 'A', { decimals: 0 }),
  transPressure: slot('propulsion.port.transmission.oilPressure', 'Gear press', 'pressure', 'psi', { decimals: 0, min: 0, max: 500 }),
  transTemp: slot('propulsion.port.transmission.oilTemperature', 'Gearbox', 'temperature', 'C', { decimals: 0 }),
  egt: slot('propulsion.0.exhaustTemperature', 'Exhaust', 'temperature', 'C', { decimals: 0, min: 0, max: 100 }),
}

const full: ResolvedClusterConfig = { ...port, arcs, footer }

const fuel = {
  side: 'left' as const,
  bars: [
    { level: slot('tanks.fuel.2.currentLevel', 'Fwd', 'ratio', 'percent', { min: 0, max: 100, decimals: 0 }), capacity: slot('tanks.fuel.2.capacity', '', 'volume', 'L', { decimals: 0 }) },
    { level: slot('tanks.fuel.4.currentLevel', 'Aft', 'ratio', 'percent', { min: 0, max: 100, decimals: 0 }), capacity: slot('tanks.fuel.4.capacity', '', 'volume', 'L', { decimals: 0 }) },
  ],
}

function renderCluster(config: ResolvedClusterConfig = full, v = values, extra: Partial<React.ComponentProps<typeof EngineClusterTile>> = {}) {
  return render(<EngineClusterTile config={config} values={v} editing={false} onConfigure={vi.fn()} {...extra} />)
}

beforeEach(() => {
  setViewportWidth(1440)
  history.result = { points: [], loading: false, error: null }
  history.calls = []
})

describe('the dial and its hours tab', () => {
  // The reference rounds revs to the nearest ten and shows them as plain rpm.
  test('reads the rpm as plain revs to the nearest ten, converted from the SI on the stream', () => {
    renderCluster()
    expect(screen.getByTestId('cluster-centre-value')).toHaveTextContent(/^700$/)
    expect(screen.getByTestId('cluster-centre')).toHaveTextContent('RPM')
  })

  // The scale numbers are divided by a hundred; the hub must not read as if it
  // were on the same scale, so the scale says what it is.
  test('labels the scale as x100 so the hub and the scale agree', () => {
    renderCluster()
    expect(screen.getByTestId('cluster-scale-note')).toHaveTextContent('scale x100')
    const labels = [...document.querySelectorAll('[data-cluster-dial-face] text')].map((t) => t.textContent)
    expect(labels).toEqual(['0', '5', '10', '15', '20', '25', '30'])
  })

  test('has no red on the scale when the rpm slot has no thresholds', () => {
    const { container } = renderCluster()
    expect(container.querySelectorAll('[data-tick-zone]')).toHaveLength(0)
  })

  test('draws the redline from the rpm slot\'s own zones', () => {
    const redlined: ResolvedClusterConfig = {
      ...full, ring: { ...full.ring, zones: [{ from: 0, to: 2600, state: 'normal' }, { from: 2600, to: 3000, state: 'alarm' }] },
    }
    const { container } = renderCluster(redlined)
    const red = [...container.querySelectorAll('[data-tick-zone="alarm"]')]
    expect(red.length).toBeGreaterThan(0)
    // 2600..3000 at 100 rpm a tick is five ticks and one major (3000), nothing below.
    expect(container.querySelectorAll('[data-tick-zone="alarm"][data-tick="major"]')).toHaveLength(1)
  })

  test('draws the band and the needle only with a reading', () => {
    const on = renderCluster()
    expect(on.container.querySelector('[data-value-arc]')).not.toBeNull()
    expect(on.container.querySelector('[data-needle]')).not.toBeNull()
    on.unmount()
    const off = renderCluster(full, { ...values, 'propulsion.port.revolutions': null })
    expect(off.container.querySelector('[data-value-arc]')).toBeNull()
    expect(off.container.querySelector('[data-needle]')).toBeNull()
  })

  test('reads engine hours in the tab', () => {
    renderCluster()
    const tab = screen.getByTestId('cluster-notch')
    expect(tab).toHaveTextContent('894')
    expect(tab).toHaveTextContent('HRS')
    expect(tab.innerHTML).toContain('lucide-clock')
  })

  test('says RUN when nothing is in a warning or alarm zone', () => {
    renderCluster()
    expect(screen.getByTestId('cluster-chip')).toHaveTextContent('RUN')
  })
})

describe('readout cards', () => {
  test('renders four cards, each with its own reading', () => {
    renderCluster()
    expect(within(screen.getByTestId('cluster-corner-0')).getByText('22.0')).toBeInTheDocument()
    expect(within(screen.getByTestId('cluster-corner-1')).getByText('0.0')).toBeInTheDocument()
    expect(within(screen.getByTestId('cluster-corner-2')).getByText('6')).toBeInTheDocument()
    expect(within(screen.getByTestId('cluster-corner-3')).getByText('1.5')).toBeInTheDocument()
  })

  // Boost really is 0 at idle. That is a reading, not an absence.
  test('shows a genuine zero as zero', () => {
    renderCluster()
    expect(within(screen.getByTestId('cluster-corner-1')).queryByText('--')).not.toBeInTheDocument()
  })

  test('shows the structural dash for a path that is absent', () => {
    renderCluster(full, { ...values, 'propulsion.port.engineLoad': null })
    expect(within(screen.getByTestId('cluster-corner-2')).getByText('--')).toBeInTheDocument()
  })

  test('uses one icon per card', () => {
    renderCluster()
    expect(screen.getByTestId('cluster-corner-3').innerHTML).toContain('lucide-fuel')
  })

  test('offers the config button only in layout mode', () => {
    const onConfigure = vi.fn()
    const { rerender } = render(<EngineClusterTile config={full} values={values} editing={false} onConfigure={onConfigure} />)
    expect(screen.queryByLabelText('Configure Port')).not.toBeInTheDocument()
    rerender(<EngineClusterTile config={full} values={values} editing onConfigure={onConfigure} />)
    screen.getByLabelText('Configure Port').click()
    expect(onConfigure).toHaveBeenCalledOnce()
  })
})

/**
 * The bullet graphs are drawn from each slot's zones. A bundled profile ships a
 * normal band and no warn or alarm threshold, so that is the case that has to
 * look right: one dim band, no setpoint ticks, amber (never red) when the
 * reading is outside it.
 */
describe('bullet graphs from zones', () => {
  const withOilZones = (zones: GaugeWidgetConfig['zones']): ResolvedClusterConfig => ({
    ...full,
    corners: [
      { label: 'Oil', rows: [{ ...full.corners[0].rows[0], zones }] },
      ...full.corners.slice(1),
    ],
  })
  const oil = () => screen.getByTestId('cluster-corner-0')

  test('draws only the normal band, with no setpoint ticks, when thresholds are null', () => {
    const { container } = renderCluster(withOilZones([{ from: 40, to: 100, state: 'normal' }]))
    expect(oil().querySelectorAll('[data-band]')).toHaveLength(1)
    expect(oil().querySelector('[data-band="normal"]')).not.toBeNull()
    expect(oil().querySelectorAll('[data-mark]')).toHaveLength(0)
    expect(container.querySelector('[data-cluster-canvas]')).not.toBeNull()
  })

  test('22 psi against a 40 psi floor is amber, not red', () => {
    renderCluster(withOilZones([{ from: 40, to: 100, state: 'normal' }]))
    expect(oil()).toHaveAttribute('data-tone', 'warn')
    expect(oil().querySelector('[data-reading]')!.className).toContain('text-amber')
    expect(oil().querySelector('[data-reading]')!.className).not.toContain('text-red')
    expect(oil().querySelector('[data-bullet-fill]')).toHaveAttribute('data-tone', 'warn')
  })

  test('a reading inside the normal band is neutral and lights that band', () => {
    renderCluster(withOilZones([{ from: 10, to: 100, state: 'normal' }]))
    expect(oil()).toHaveAttribute('data-tone', 'neutral')
    expect(oil().querySelector('[data-band="normal"]')).toHaveAttribute('data-band-active', 'true')
    expect(oil().querySelector('[data-reading]')!.className).toContain('text-foreground')
  })

  test('marks a real threshold with a tick and its number, and goes red in the alarm band', () => {
    renderCluster(withOilZones([
      { from: 0, to: 25, state: 'alarm' },
      { from: 25, to: 100, state: 'normal' },
    ]))
    expect(oil().querySelectorAll('[data-mark="alarm"]')).toHaveLength(1)
    expect(within(oil()).getByText('25')).toBeInTheDocument()
    expect(oil()).toHaveAttribute('data-tone', 'alarm')
    expect(oil().querySelector('[data-band="alarm"]')).toHaveAttribute('data-band-active', 'true')
  })

  test('colours come from tokens, never a hex', () => {
    const { container } = renderCluster(withOilZones([{ from: 40, to: 100, state: 'normal' }]), values, {})
    expect(container.innerHTML).not.toMatch(/#[0-9a-fA-F]{3,8}\b/)
  })
})

describe('dial gap arcs', () => {
  test('draws the bound left arc with its reading', () => {
    const { container } = renderCluster()
    const left = container.querySelector('[data-gap-arc="left"]')!
    expect(left).not.toBeNull()
    expect(left).toHaveTextContent('71°')
  })

  // The right arc is engine oil temperature, which this vessel has no sensor
  // for. An unbound slot draws nothing at all rather than an empty track.
  test('draws nothing for the unbound right arc', () => {
    const { container } = renderCluster()
    expect(container.querySelector('[data-gap-arc="right"]')).toBeNull()
  })

  test('draws the right arc once it is bound', () => {
    const bound: ResolvedClusterConfig = {
      ...full,
      arcs: { ...arcs, right: slot('propulsion.port.oilTemperature', 'Engine oil', 'temperature', 'C', { min: 0, max: 140, decimals: 0 }) },
    }
    const { container } = renderCluster(bound, { ...values, 'propulsion.port.oilTemperature': 363.15 })
    expect(container.querySelector('[data-gap-arc="right"]')).toHaveTextContent('90°')
  })

  test('is amber past a band with no alarm configured, never red', () => {
    const { container } = renderCluster(full, { ...values, 'propulsion.port.temperature': 361.15 })
    expect(container.querySelector('[data-gap-arc="left"]')).toHaveAttribute('data-tone', 'warn')
  })
})

describe('mirror', () => {
  const stbd: ResolvedClusterConfig = { ...full, side: 'stbd', title: 'Starboard', fuel: { ...fuel, side: 'right' } }
  const portFuel: ResolvedClusterConfig = { ...full, fuel }

  test('a starboard tile is laid out reflected', () => {
    const { container } = renderCluster(stbd)
    expect(container.querySelector('[data-cluster-canvas]')).toHaveAttribute('data-side', 'stbd')
    expect(screen.getByTestId('cluster-footer').style.flexDirection).toBe('row-reverse')
  })

  test('a port tile is not', () => {
    const { container } = renderCluster(portFuel)
    expect(container.querySelector('[data-cluster-canvas]')).toHaveAttribute('data-side', 'port')
    expect(screen.getByTestId('cluster-footer').style.flexDirection).toBe('row')
  })

  test('readout cards move to the other side of the dial', () => {
    const p = renderCluster(portFuel)
    const portLeft = parseFloat((screen.getByTestId('cluster-corner-0').parentElement as HTMLElement).style.left)
    p.unmount()
    renderCluster(stbd)
    const stbdLeft = parseFloat((screen.getByTestId('cluster-corner-0').parentElement as HTMLElement).style.left)
    expect(portLeft).toBeGreaterThan(400)
    expect(stbdLeft).toBeLessThan(400)
  })

  test('hours and status tabs swap sides', () => {
    const p = renderCluster(portFuel)
    expect((screen.getByTestId('cluster-notch') as HTMLElement).style.right).toBe('52px')
    expect((screen.getByTestId('cluster-chip-tab') as HTMLElement).style.left).toBe('52px')
    p.unmount()
    renderCluster(stbd)
    expect((screen.getByTestId('cluster-notch') as HTMLElement).style.left).toBe('52px')
    expect((screen.getByTestId('cluster-chip-tab') as HTMLElement).style.right).toBe('52px')
  })

  test('the fuel fan goes to the outer edge', () => {
    const p = renderCluster(portFuel)
    expect(p.container.querySelector('[data-fuel-fan]')).toHaveAttribute('data-side', 'port')
    p.unmount()
    const s = renderCluster(stbd)
    expect(s.container.querySelector('[data-fuel-fan]')).toHaveAttribute('data-side', 'stbd')
  })

  test('a tile with no side is port', () => {
    const { side: _side, ...noSide } = full
    void _side
    const { container } = renderCluster(noSide)
    expect(container.querySelector('[data-cluster-canvas]')).toHaveAttribute('data-side', 'port')
  })
})

describe('fuel fan', () => {
  test('draws no fan without a rail', () => {
    const { container } = renderCluster()
    expect(container.querySelector('[data-fuel-fan]')).toBeNull()
  })

  test('reads each tank and the total in litres', () => {
    const { container } = renderCluster({ ...full, fuel })
    expect(container.querySelectorAll('[data-fuel-bar]')).toHaveLength(2)
    // 0.87 * 1.2 m3 + 0.4 * 1.3 m3 = 1564 L
    expect(screen.getByTestId('fuel-total')).toHaveTextContent('1564')
  })

  test('says so rather than totalling a tank that is not reporting', () => {
    const { container } = renderCluster({ ...full, fuel }, { ...values, 'tanks.fuel.4.currentLevel': null })
    expect(screen.getByTestId('fuel-total')).toHaveTextContent('--')
    expect(container.querySelector('[data-fuel-bar="1"]')).toHaveAttribute('data-bar-state', 'absent')
  })
})

describe('footer', () => {
  test('reads the starter battery in volts', () => {
    renderCluster()
    expect(screen.getByTestId('cluster-battery')).toHaveTextContent('26.4')
  })

  test('reads the alternator: amps, watts, temperature and charge stage', () => {
    renderCluster()
    expect(screen.getByTestId('cluster-alt-amps')).toHaveTextContent('48')
    expect(screen.getByTestId('cluster-alt-watts')).toHaveTextContent('1290')
    expect(screen.getByTestId('cluster-alternator')).toHaveTextContent('57°')
    expect(screen.getByTestId('cluster-alt-stage')).toHaveTextContent('ABSORPTION')
  })

  test('reads the transmission: pressure large, temperature in the header', () => {
    renderCluster()
    const trans = screen.getByTestId('cluster-trans')
    expect(within(trans).getByText('341')).toBeInTheDocument()
    expect(trans.querySelector('[data-header-temp]')).toHaveTextContent('38°')
  })

  test('reads the wet exhaust', () => {
    renderCluster()
    expect(within(screen.getByTestId('cluster-egt')).getByText('32')).toBeInTheDocument()
  })

  test('shows dashes for slots that are not bound', () => {
    renderCluster(port)
    expect(screen.getByTestId('cluster-alt-amps')).toHaveTextContent('--')
    expect(within(screen.getByTestId('cluster-battery')).getByText('--')).toBeInTheDocument()
  })

  test('the exhaust is amber past its band, red only in a configured alarm zone', () => {
    const wet = (zones: GaugeWidgetConfig['zones']): ResolvedClusterConfig => ({
      ...full, footer: { ...footer, egt: { ...footer.egt!, zones } },
    })
    const hot = { ...values, 'propulsion.0.exhaustTemperature': 80 + 273.15 }
    const { unmount } = renderCluster(wet([{ from: 0, to: 60, state: 'normal' }]), hot)
    expect(screen.getByTestId('cluster-egt')).toHaveAttribute('data-tone', 'warn')
    unmount()
    renderCluster(wet([{ from: 0, to: 60, state: 'normal' }, { from: 60, to: 100, state: 'alarm' }]), hot)
    expect(screen.getByTestId('cluster-egt')).toHaveAttribute('data-tone', 'alarm')
  })
})

describe('the alternator trace', () => {
  const minute = (i: number, v: number) => ({ time: new Date(Date.UTC(2026, 9, 9, 3, i)).toISOString(), value: v })

  test('asks for one hour of the amps path', () => {
    renderCluster()
    expect(history.calls.some(([path, window, enabled]) => path === 'electrical.alternator.2.current' && window === '1h' && enabled)).toBe(true)
  })

  test('draws the trace and the 1 HR caption', () => {
    history.result = { points: [minute(0, 10), minute(30, 40), minute(59, 48)], loading: false, error: null }
    renderCluster()
    expect(screen.getByTestId('cluster-spark')).toHaveTextContent('1 HR')
  })

  test('draws the charge limit as a dashed line when it is bound', () => {
    history.result = { points: [minute(0, 10), minute(59, 48)], loading: false, error: null }
    renderCluster()
    expect(screen.getByTestId('cluster-spark-limit')).toBeInTheDocument()
    expect(screen.getByTestId('cluster-spark')).toHaveTextContent('BMS 80 A')
  })

  test('draws no limit line when the charge limit is unbound', () => {
    history.result = { points: [minute(0, 10), minute(59, 48)], loading: false, error: null }
    const { altLimit: _limit, ...rest } = footer
    void _limit
    renderCluster({ ...full, footer: rest })
    expect(screen.queryByTestId('cluster-spark-limit')).not.toBeInTheDocument()
  })

  // A flat line and a steady sensor look the same; only one of them is true.
  test('says why there is no trace instead of drawing a flat one', () => {
    history.result = { points: [], loading: false, error: 'history needs InfluxDB' }
    renderCluster()
    expect(screen.getByTestId('cluster-spark-error')).toHaveTextContent('history needs InfluxDB')
    expect(screen.queryByTestId('cluster-spark')).not.toBeInTheDocument()
  })
})

/**
 * The instrument skin works by redefining tokens, so a token it forgets falls
 * through to the light theme. Now that the skin sits at page scope, every
 * non-indirected :root token is in play.
 */
describe('instrument skin token coverage', () => {
  async function tokens() {
    const [fs, path] = await Promise.all([import('node:fs'), import('node:path')])
    const raw = fs.readFileSync(path.resolve(process.cwd(), 'src/index.css'), 'utf8')
    // Strip comments first, or a token name mentioned in prose reads as a declaration.
    const css = raw.replace(/\/\*[\s\S]*?\*\//g, '')
    const section = (selector: string) => css.slice(css.indexOf(selector), css.indexOf('}', css.indexOf(selector)))
    const declarations = (block: string) =>
      new Map([...block.matchAll(/--([\w-]+):\s*([^;]+);/g)].map((m) => [m[1], m[2].trim()]))
    return { light: declarations(section(':root {')), instrument: declarations(section('[data-skin="instrument"] {')) }
  }

  test('redefines every :root token the skin does not merely inherit', async () => {
    const { light, instrument } = await tokens()
    const ALLOWED_TO_INHERIT = new Map<string, string>([
      ['radius', 'not colour, correct as inherited'],
      ['font-display', 'not colour, correct as inherited'],
      ['font-sans', 'not colour, correct as inherited'],
      ['chart-wind', 'unread on the bento grid'],
      ['chart-gust', 'unread on the bento grid'],
      ['chart-wave', 'unread on the bento grid'],
      ['chart-swell', 'unread on the bento grid'],
      ['chart-temp', 'unread on the bento grid'],
      ['chart-grid', 'unread on the bento grid'],
      ['chart-precip', 'unread on the bento grid'],
      ['chart-uv', 'unread on the bento grid'],
      ['dial-bezel-overhang', 'set inline per tile by the cluster, not by the skin'],
      ['dial-track-r', "invisible under the skin's --dial-track-w: 0px"],
    ])
    const missing = [...light.keys()].filter((token) => {
      if (ALLOWED_TO_INHERIT.has(token)) return false
      if (/^var\(/.test(light.get(token)!)) return false
      return !instrument.has(token)
    })
    expect(missing, `instrument skin does not redefine: ${missing.join(', ')}`).toEqual([])
  })

  test('the cluster card tokens exist, inert in :root and restated by the skin', async () => {
    const { light, instrument } = await tokens()
    for (const token of ['cluster-band-idle', 'cluster-band-active', 'cluster-spark-fill', 'cluster-surface', 'cluster-stroke', 'cluster-track', 'cluster-dial-face', 'cluster-dial-rim', 'cluster-hub', 'cluster-hub-shadow', 'cluster-sweep-o0', 'cluster-sweep-o1', 'cluster-sweep-o2', 'cluster-needle-glow']) {
      expect(light.has(token), `:root is missing --${token}`).toBe(true)
    }
    for (const token of ['cluster-band-idle', 'cluster-band-active', 'cluster-spark-fill', 'cluster-surface', 'cluster-dial-face', 'cluster-dial-rim', 'cluster-hub', 'cluster-hub-shadow', 'cluster-sweep-o0', 'cluster-sweep-o1', 'cluster-sweep-o2', 'cluster-needle-glow']) {
      expect(instrument.has(token), `skin is missing --${token}`).toBe(true)
    }
  })

  test('the bands dim when idle and brighten when the reading is in them', async () => {
    const { light } = await tokens()
    expect(Number(light.get('cluster-band-idle'))).toBeLessThan(Number(light.get('cluster-band-active')))
  })
})

describe('tile state (ADR 0081)', () => {
  test('carries an arc past its alarm band up to the tile edge', () => {
    const alarmArc: ResolvedClusterConfig = {
      ...full,
      arcs: { left: { ...arcs.left, zones: [{ from: 0, to: 95, state: 'normal' }, { from: 95, to: 120, state: 'alarm' }] } },
    }
    const { container } = renderCluster(alarmArc, { ...values, 'propulsion.port.temperature': 380.15 })
    expect(container.querySelector('[data-slot="card"]')).toHaveAttribute('data-state', 'alarm')
    expect(screen.getByTestId('cluster-chip')).toHaveTextContent('ALARM')
  })

  test('carries a footer slot past its alarm band up to the tile edge', () => {
    const hotEgt: ResolvedClusterConfig = {
      ...full,
      footer: { ...footer, egt: { ...footer.egt!, zones: [{ from: 0, to: 60, state: 'normal' }, { from: 60, to: 100, state: 'alarm' }] } },
    }
    const { container } = renderCluster(hotEgt, { ...values, 'propulsion.0.exhaustTemperature': 353.15 })
    expect(container.querySelector('[data-slot="card"]')).toHaveAttribute('data-state', 'alarm')
  })

  // The bundled profile's own case: warn and alarm thresholds null, so a hot or
  // low reading is `outside` the normal band. It must not light the tile edge,
  // or a healthy boat idles all day with an amber border.
  test('does not carry an out-of-band reading to the tile edge', () => {
    const zoned: ResolvedClusterConfig = {
      ...full,
      corners: [
        { label: 'Oil', rows: [{ ...full.corners[0].rows[0], zones: [{ from: 40, to: 100, state: 'normal' }] }] },
        ...full.corners.slice(1),
      ],
    }
    const { container } = renderCluster(zoned)
    expect(container.querySelector('[data-slot="card"]')).not.toHaveAttribute('data-state')
    expect(screen.getByTestId('cluster-chip')).toHaveTextContent('RUN')
  })

  test('carries no state when nothing on the cluster has a band configured', () => {
    const { container } = renderCluster()
    expect(container.querySelector('[data-slot="card"]')).not.toHaveAttribute('data-state')
  })
})

describe('staleness (ADR 0083)', () => {
  const freshAges: Record<string, number> = Object.fromEntries(
    Object.keys(values).filter((p) => p.startsWith('propulsion.port.')).map((p) => [p, 3]),
  )

  test('a frozen card shows the dash and its own badge, without staling the whole tile', () => {
    const { container } = renderCluster(full, values, { ages: { ...freshAges, 'propulsion.port.oilPressure': 300 } })
    const oil = screen.getByTestId('cluster-corner-0')
    expect(within(oil).getByText('--')).toBeInTheDocument()
    expect(within(oil).getByText(/Stale/)).toBeInTheDocument()
    expect(within(oil).queryByText('22.0')).not.toBeInTheDocument()
    expect(container.querySelector('[data-slot="card"]')).not.toHaveAttribute('data-stale')
  })

  test('blanks the rpm when its own path is stale', () => {
    renderCluster(full, values, { ages: { ...freshAges, 'propulsion.port.revolutions': 300 } })
    expect(screen.getByTestId('cluster-centre-value')).toHaveTextContent('--')
  })

  test('goes stale as a whole only once every slot with a known age has frozen', () => {
    const staleAges = Object.fromEntries(Object.keys(freshAges).map((path) => [path, 300]))
    const { container } = renderCluster(full, values, { ages: staleAges })
    expect(container.querySelector('[data-slot="card"]')).toHaveAttribute('data-stale', 'true')
    expect(screen.getByTestId('tile-stale-badge')).toBeInTheDocument()
  })

  test('an unknown age never counts as stale', () => {
    const { container } = renderCluster()
    expect(container.querySelector('[data-slot="card"]')).not.toHaveAttribute('data-stale')
    expect(screen.getByTestId('cluster-centre-value')).toHaveTextContent('700')
  })
})

/**
 * Switching the key off powers the engine computer down, so its readings just
 * stop. The tile then shows OFF in muted chrome instead of a stale alarm, and a
 * lost feed keeps the amber stale treatment.
 */
describe('engine off (ADR 0163)', () => {
  const since = '2026-10-06T04:30:00Z'
  const engineAges = Object.fromEntries(Object.keys(values).filter((p) => p.startsWith('propulsion.port.')).map((p) => [p, 9000]))

  test('shows OFF with dashes, a muted pill and a neutral border', () => {
    const { container } = renderCluster(full, values, {
      ages: engineAges, engines: { port: { state: 'off', lastUpdate: since } },
    })
    const badge = screen.getByTestId('tile-off-badge')
    expect(badge).toHaveTextContent(/off/i)
    expect(badge.className).not.toMatch(/amber|red/)
    expect(badge.getAttribute('title')).toMatch(/^Off since \d{1,2}:\d{2}/)
    expect(screen.queryByTestId('tile-stale-badge')).not.toBeInTheDocument()
    // OFF is said once, in the title pill (above); the status chip is only for a running engine.
    expect(screen.queryByTestId('cluster-chip')).not.toBeInTheDocument()
    const card = container.querySelector('[data-slot="card"]')
    expect(card).not.toHaveAttribute('data-state')
    expect(card).not.toHaveAttribute('data-stale')
    expect(screen.getByTestId('cluster-centre-value')).toHaveTextContent('--')
    expect(screen.getByTestId('cluster-corner-0')).toHaveTextContent('--')
    expect(screen.getByTestId('cluster-trans')).toHaveTextContent('--')
  })

  test('stays neutral even when a footer slot would be in an alarm zone', () => {
    const hot: ResolvedClusterConfig = {
      ...full,
      footer: { ...footer, egt: { ...footer.egt!, zones: [{ from: 0, to: 20, state: 'normal' }, { from: 20, to: 100, state: 'alarm' }] } },
    }
    const { container } = renderCluster(hot, values, { engines: { port: { state: 'off', lastUpdate: since } } })
    expect(container.querySelector('[data-slot="card"]')).not.toHaveAttribute('data-state')
  })

  test('draws no alternator trace for an engine that is off', () => {
    renderCluster(full, values, { engines: { port: { state: 'off', lastUpdate: since } } })
    expect(history.calls.every(([, , enabled]) => enabled === false)).toBe(true)
  })

  test('a lost engine keeps the amber stale treatment', () => {
    const { container } = renderCluster(full, values, {
      ages: engineAges, engines: { port: { state: 'lost', lastUpdate: since } },
    })
    expect(container.querySelector('[data-slot="card"]')).toHaveAttribute('data-stale', 'true')
    expect(screen.queryByTestId('tile-off-badge')).not.toBeInTheDocument()
    expect(screen.getByTestId('cluster-chip')).not.toHaveTextContent('OFF')
  })

  test('a running engine, or one the backend says nothing about, is unchanged', () => {
    renderCluster(full, values, { engines: { port: { state: 'running' }, starboard: { state: 'off', lastUpdate: since } } })
    expect(screen.queryByTestId('tile-off-badge')).not.toBeInTheDocument()
    expect(screen.getByTestId('cluster-centre-value')).toHaveTextContent('700')
  })
})
