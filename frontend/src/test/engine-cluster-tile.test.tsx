import { render, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { EngineClusterTile } from '@/components/engine-cluster-tile'
import type { EngineClusterConfig } from '@/lib/dashboard-widgets'
import { setViewportWidth } from './viewport'

/** The live idle readings from the vessel, in the SI the stream carries. */
const values = {
  'propulsion.port.revolutions': 11.638333,        // 698 RPM
  'propulsion.port.fuel.rate': 4.1666666e-7,       // 1.5 L/h
  'propulsion.port.oilPressure': 151900,           // 22.0 psi
  'propulsion.port.boostPressure': 0,              // genuinely zero at idle
  'propulsion.port.temperature': 344.15,           // 71.0 C
  'propulsion.port.transmission.oilTemperature': 310.8, // 37.7 C
  'propulsion.port.runTime': 3219295,              // 894 h
  // exhaust deliberately absent
}

const port: EngineClusterConfig = {
  title: 'Port',
  ring: { path: 'propulsion.port.revolutions', label: 'RPM', display: 'radial', quantity: 'frequency', unit: 'rpm', min: 0, max: 3000, decimals: 0, labelDivisor: 100 },
  centre: { path: 'propulsion.port.runTime', label: 'Hours', display: 'numeric', quantity: 'duration', unit: 'h', decimals: 0 },
  corners: [
    { label: 'Oil', rows: [{ path: 'propulsion.port.oilPressure', label: 'Oil', display: 'numeric', quantity: 'pressure', unit: 'psi' }] },
    { label: 'Boost', rows: [{ path: 'propulsion.port.boostPressure', label: 'Boost', display: 'numeric', quantity: 'pressure', unit: 'psi' }] },
    { label: 'Temps', rows: [
      { path: 'propulsion.port.temperature', label: 'Coolant', display: 'numeric', quantity: 'temperature', unit: 'C' },
      { path: 'propulsion.port.transmission.oilTemperature', label: 'Gearbox', display: 'numeric', quantity: 'temperature', unit: 'C' },
      { path: 'propulsion.port.exhaustTemperature', label: 'Exhaust', display: 'numeric', quantity: 'temperature', unit: 'C' },
    ] },
    { label: 'Fuel', rows: [{ path: 'propulsion.port.fuel.rate', label: 'Fuel Flow', display: 'numeric', quantity: 'volumetricFlow', unit: 'Lph', decimals: 1 }] },
  ],
}

function renderCluster(config = port) {
  return render(
    <EngineClusterTile config={config} values={values} editing={false} onConfigure={vi.fn()} />,
  )
}

beforeEach(() => setViewportWidth(1440))

/**
 * Geometry. The bezel turned the dial into a solid circle, and a circle cannot
 * be cropped the way an arc could: the old canvas stopped 40px short of the
 * ring box's bottom, which was invisible while the lower half painted nothing
 * and became the dial bulging out of the tile once it did.
 */
describe('cluster canvas geometry', () => {
  test('gives all four boxes the same height', () => {
    const { container } = renderCluster()
    const heights = [0, 1, 2, 3].map((i) => {
      const el = container.querySelector(`[data-testid="cluster-corner-${i}"]`) as HTMLElement
      return el.style.height
    })
    expect(new Set(heights).size).toBe(1)
  })

  test('contains the whole dial, bezel included, inside the canvas', () => {
    const { container } = renderCluster()
    const canvas = container.querySelector('[data-cluster-canvas]') as HTMLElement
    const dial = container.querySelector('[data-cluster-dial]') as HTMLElement

    const overhang = parseFloat(dial.style.getPropertyValue('--dial-bezel-overhang'))
    const top = parseFloat(dial.style.top) - overhang
    const bottom = parseFloat(dial.style.top) + parseFloat(dial.style.height) + overhang

    // happy-dom rounds a serialised inline px value to 6 decimal places on
    // read-back (jsdom keeps the full string React wrote), so summing three
    // un-rounded parseFloat results and comparing against one rounded one can
    // land a few millionths of a pixel over. PX_EPSILON is an order of
    // magnitude above that rounding noise and eight orders below anything
    // that would matter on screen.
    const PX_EPSILON = 1e-4
    expect(top).toBeGreaterThanOrEqual(0)
    expect(bottom).toBeLessThanOrEqual(parseFloat(canvas.style.height) + PX_EPSILON)
  })

  test('centres the dial on the block of boxes', () => {
    const { container } = renderCluster()
    const dial = container.querySelector('[data-cluster-dial]') as HTMLElement
    const top = container.querySelector('[data-testid="cluster-corner-0"]') as HTMLElement
    const bottom = container.querySelector('[data-testid="cluster-corner-2"]') as HTMLElement

    const blockTop = parseFloat(top.style.top)
    const blockBottom = parseFloat(bottom.style.top) + parseFloat(bottom.style.height)
    const dialCentre = parseFloat(dial.style.top) + parseFloat(dial.style.height) / 2

    expect(dialCentre).toBeCloseTo((blockTop + blockBottom) / 2, 0)
  })
})

describe('temperature telltales', () => {
  const withTelltales = (extra: Partial<typeof port> = {}) => ({
    ...port,
    telltales: [
      { path: 'propulsion.port.temperature', label: 'Coolant', display: 'numeric' as const, quantity: 'temperature', unit: 'C',
        min: 0, max: 120, zones: [{ from: 0, to: 95, state: 'normal' as const }, { from: 95, to: 120, state: 'alarm' as const }] },
      { path: 'propulsion.port.transmission.oilTemperature', label: 'Gearbox', display: 'numeric' as const, quantity: 'temperature', unit: 'C' },
      { path: 'propulsion.port.exhaustTemperature', label: 'Exhaust', display: 'numeric' as const, quantity: 'temperature', unit: 'C' },
    ],
    ...extra,
  })

  test('reads each configured telltale', () => {
    renderCluster(withTelltales())
    const row = screen.getByTestId('cluster-telltales')
    expect(within(row).getByText('71.0')).toBeInTheDocument()
    expect(within(row).getByText('37.7')).toBeInTheDocument()
  })

  /**
   * The whole point of drawing three different symbols: without them the row is
   * three identical thermometers and every one needs a word next to it.
   */
  test('gives each temperature its own symbol, and no label', () => {
    const { container } = renderCluster(withTelltales())
    const row = screen.getByTestId('cluster-telltales')
    const symbols = [...container.querySelectorAll('[data-telltale-icon]')]
      .map((el) => el.getAttribute('data-telltale-icon'))
    expect(symbols).toEqual(['coolant', 'gearbox', 'exhaust'])
    expect(within(row).queryByText('Coolant')).not.toBeInTheDocument()
    expect(within(row).queryByText('Gearbox')).not.toBeInTheDocument()
  })

  /**
   * A telltale is a warning light: grey when the engine is not turning, green
   * while the reading sits in its normal band, red once it is over. Anything
   * else is a number you have to read rather than a colour you can glance at.
   */
  test('is grey with no reading, green in band, red when too hot', () => {
    const { container, rerender } = render(
      <EngineClusterTile config={withTelltales()} values={{}} editing={false} onConfigure={vi.fn()} />,
    )
    const coolant = () => container.querySelector('[data-telltale="coolant"]')!.className

    expect(coolant()).toContain('text-muted-foreground')

    rerender(<EngineClusterTile config={withTelltales()} values={values} editing={false} onConfigure={vi.fn()} />)
    expect(coolant()).toContain('text-emerald-500')

    rerender(<EngineClusterTile config={withTelltales()}
      values={{ ...values, 'propulsion.port.temperature': 380.15 }} editing={false} onConfigure={vi.fn()} />)
    expect(coolant()).toContain('text-red-600')
  })

  /**
   * Over the normal band is not the same as over an alarm threshold. This
   * engine's profile leaves warn and alarm null, so every hot reading is
   * "in no band at all", and painting that red would assert an overheat the
   * configuration has no thresholds to detect.
   */
  test('is amber, not red, when it is past a band with no alarm configured', () => {
    const { container } = render(
      <EngineClusterTile
        config={withTelltales({
          telltales: [{ path: 'propulsion.port.temperature', label: 'Coolant', display: 'numeric' as const,
            quantity: 'temperature', unit: 'C', min: 0, max: 120,
            zones: [{ from: 0, to: 85, state: 'normal' as const }] }],
        })}
        values={{ ...values, 'propulsion.port.temperature': 361.15 }} editing={false} onConfigure={vi.fn()} />,
    )
    expect(container.querySelector('[data-telltale="coolant"]')!.className).toContain('text-amber-600')
  })

  // The row belongs with the hours badge inside the dial, not on a strip of its
  // own below the boxes, so the canvas is back to the height of the box block.
  test('sits inside the dial, under the hours notch', () => {
    const { container } = renderCluster(withTelltales())
    const notch = screen.getByTestId('cluster-notch')
    const row = screen.getByTestId('cluster-telltales')
    expect(notch.compareDocumentPosition(row) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(container.querySelector('[data-cluster-dial]')!.contains(row)).toBe(true)
  })

  /**
   * Down in the wedge with the notch, not stacked under the reading. Together
   * the four came to half the dial's height, which drove the reading up off the
   * middle and through the scale numbers.
   */
  test('rides the dial foot rather than the readout', () => {
    renderCluster(withTelltales())
    const foot = screen.getByTestId('cluster-foot')

    expect(foot.contains(screen.getByTestId('cluster-telltales'))).toBe(true)
    expect(foot.contains(screen.getByTestId('cluster-notch'))).toBe(true)
    expect(foot.className).toContain('absolute')
    expect(foot.className).toContain('bottom-')
    expect(foot.contains(screen.getByTestId('cluster-centre'))).toBe(false)
  })

  test('renders no row at all when none are configured', () => {
    renderCluster()
    expect(screen.queryByTestId('cluster-telltales')).not.toBeInTheDocument()
  })
})

/**
 * The fuel rail (ADR 0061) hangs off one edge of the tile. It widens only the
 * design the canvas scales against: everything the corner masks were computed
 * against has to stay where it was, or every cluster gains a hole in the middle.
 */
describe('fuel rail', () => {
  const rail = {
    side: 'left' as const,
    bars: [{
      level: { path: 'tanks.fuel.5.currentLevel', label: 'Fwd', display: 'bar' as const,
        quantity: 'ratio', unit: 'percent', decimals: 0, min: 0, max: 100 },
      capacity: { path: 'tanks.fuel.5.capacity', label: '', display: 'numeric' as const,
        quantity: 'volume', unit: 'L', decimals: 0 },
    }],
  }
  const withRail = (side: 'left' | 'right' = 'left'): EngineClusterConfig =>
    ({ ...port, fuel: { ...rail, side } })

  const fuelValues = { ...values, 'tanks.fuel.5.currentLevel': 0.7416, 'tanks.fuel.5.capacity': 1.2 }
  const renderRailed = (config: EngineClusterConfig) =>
    render(<EngineClusterTile config={config} values={fuelValues} editing={false} onConfigure={vi.fn()} />)

  test('renders nothing extra when no rail is configured', () => {
    const { container } = renderCluster()
    expect(container.querySelector('[data-fuel-rail]')).toBeNull()
  })

  test('reads the tanks it is given', () => {
    renderRailed(withRail())
    expect(screen.getByTestId('fuel-total')).toHaveTextContent('890')
  })

  test('puts the rail on the configured edge and the dial block beside it', () => {
    const { container: left } = renderRailed(withRail('left'))
    const leftBody = left.querySelector('[data-cluster-body]') as HTMLElement
    expect(parseFloat(leftBody.style.left)).toBeGreaterThan(0)

    const { container: right } = renderRailed(withRail('right'))
    const rightBody = right.querySelector('[data-cluster-body]') as HTMLElement
    expect(parseFloat(rightBody.style.left)).toBe(0)
  })

  /**
   * The corner masks are computed at module scope against a 520-wide canvas.
   * If the rail moved the cards inside that box rather than shifting the whole
   * box, every mask would cut in the wrong place.
   */
  test('leaves the corner geometry exactly where it was', () => {
    const offsets = (config: EngineClusterConfig) => {
      const { container } = renderRailed(config)
      return [0, 1, 2, 3].map((i) => {
        const el = container.querySelector(`[data-testid="cluster-corner-${i}"]`) as HTMLElement
        return `${el.style.left}/${el.style.top}/${el.style.width}/${el.style.height}`
      })
    }
    expect(offsets(withRail())).toEqual(offsets(port))
  })

  test('costs the tile no extra height, only width', () => {
    const canvas = (config: EngineClusterConfig) =>
      (renderRailed(config).container.querySelector('[data-cluster-canvas]') as HTMLElement).style

    const bare = canvas(port)
    const railed = canvas(withRail())
    expect(railed.height).toBe(bare.height)
    expect(parseFloat(railed.width)).toBeGreaterThan(parseFloat(bare.width))
  })
})

describe('EngineClusterTile', () => {
  test('reads the ring, converted from the SI on the stream', () => {
    renderCluster()
    expect(screen.getByText('698')).toBeInTheDocument()
    expect(screen.getByText('RPM x100')).toBeInTheDocument()
  })

  /**
   * Hours belong in the notch and fuel rate in a card, not the other way
   * round. Hours barely move and want a small badge; fuel rate changes with
   * every throttle input and earns a full readout with a scale under it.
   */
  test('reads engine hours in the notch', () => {
    renderCluster()
    const notch = screen.getByTestId('cluster-notch')
    expect(notch).toHaveTextContent('894')
    expect(notch).toHaveTextContent('h')
    expect(notch.innerHTML).toContain('lucide-clock')
  })

  test('reads fuel rate in a corner card', () => {
    renderCluster()
    const fuel = screen.getByTestId('cluster-corner-3')
    expect(fuel).toHaveTextContent('1.5')
    expect(fuel.innerHTML).toContain('lucide-fuel')
  })

  test('renders one card per corner, with a row each', () => {
    renderCluster()
    expect(screen.getByText('22.0')).toBeInTheDocument()   // oil psi
    expect(screen.getByText('1.5')).toBeInTheDocument()    // fuel L/h
  })

  test('stacks several rows in one card', () => {
    renderCluster()
    const temps = screen.getByTestId('cluster-corner-2')
    expect(within(temps).getByText('71.0')).toBeInTheDocument()
    expect(within(temps).getByText('37.7')).toBeInTheDocument()
  })

  /**
   * The exhaust path is under propulsion.0 on this vessel, so a cluster keyed
   * on propulsion.port cannot reach it. It has to read as no data, never zero.
   */
  test('shows the structural dash for a path that is absent', () => {
    renderCluster()
    const temps = screen.getByTestId('cluster-corner-2')
    expect(within(temps).getByText('--')).toBeInTheDocument()
  })

  // Boost really is 0 at idle. That is a reading, not an absence.
  test('shows a genuine zero as zero', () => {
    renderCluster()
    const boost = screen.getByTestId('cluster-corner-1')
    expect(within(boost).getByText('0.0')).toBeInTheDocument()
    expect(within(boost).queryByText('--')).not.toBeInTheDocument()
  })

  test('offers the config button only in layout mode', () => {
    const onConfigure = vi.fn()
    const { rerender } = render(
      <EngineClusterTile config={port} values={values} editing={false} onConfigure={onConfigure} />,
    )
    expect(screen.queryByLabelText('Configure Port')).not.toBeInTheDocument()

    rerender(<EngineClusterTile config={port} values={values} editing onConfigure={onConfigure} />)
    screen.getByLabelText('Configure Port').click()
    expect(onConfigure).toHaveBeenCalledOnce()
  })
})

/**
 * The instrument skin works by redefining tokens, so a token it forgets falls
 * through to the light theme. --card-foreground did exactly that, and the
 * config gear — which inherits it — rendered near-black on a near-black card.
 *
 * Now that the skin can sit at page scope (rather than only on a cluster tile),
 * every non-indirected :root token is in play, not just the -foreground ones:
 * anything a page-level board might paint with — board chrome, dial chrome, an
 * alarm colour — needs a skin-appropriate value or it leaks the light theme
 * onto a dark board.
 */
describe('instrument skin token coverage', () => {
  test('redefines every :root token the skin does not merely inherit', async () => {
    const [fs, path] = await Promise.all([import('node:fs'), import('node:path')])
    const raw = fs.readFileSync(path.resolve(process.cwd(), 'src/index.css'), 'utf8')
    // Strip comments first, or a token name mentioned in prose (e.g. this
    // file's own dial-chrome commentary) reads as a declaration.
    const css = raw.replace(/\/\*[\s\S]*?\*\//g, '')

    const section = (selector: string) =>
      css.slice(css.indexOf(selector), css.indexOf('}', css.indexOf(selector)))

    // name -> value, so a var() indirection (which resolves through the skin
    // automatically, since the skin redefines what it points at) can be told
    // apart from a token the skin actually has to restate.
    const declarations = (block: string) =>
      new Map([...block.matchAll(/--([\w-]+):\s*([^;]+);/g)].map((m) => [m[1], m[2].trim()]))

    const light = declarations(section(':root {'))
    const instrument = declarations(section('[data-skin="instrument"] {'))

    // Each entry is a token the skin deliberately leaves inherited, with the
    // reason it's safe to.
    const ALLOWED_TO_INHERIT = new Map<string, string>([
      ['radius', 'not colour, correct as inherited'],
      ['font-display', 'not colour, correct as inherited'],
      ['font-sans', 'not colour, correct as inherited'],
      // Only forecast-drawer.tsx and tide-chart.tsx read the chart tokens, and
      // neither renders on the bento grid — a known gap if a charting tile
      // ever lands on a page.
      ['chart-wind', 'unread on the bento grid (forecast-drawer.tsx / tide-chart.tsx only)'],
      ['chart-gust', 'unread on the bento grid (forecast-drawer.tsx / tide-chart.tsx only)'],
      ['chart-wave', 'unread on the bento grid (forecast-drawer.tsx / tide-chart.tsx only)'],
      ['chart-swell', 'unread on the bento grid (forecast-drawer.tsx / tide-chart.tsx only)'],
      ['chart-temp', 'unread on the bento grid (forecast-drawer.tsx / tide-chart.tsx only)'],
      ['chart-grid', 'unread on the bento grid (forecast-drawer.tsx / tide-chart.tsx only)'],
      ['chart-precip', 'unread on the bento grid (forecast-drawer.tsx / tide-chart.tsx only)'],
      ['chart-uv', 'unread on the bento grid (forecast-drawer.tsx / tide-chart.tsx only)'],
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
})

describe('box icons', () => {
  test('infers an icon per box from what it measures', () => {
    const { container } = renderCluster()
    // lucide stamps the name onto the svg class, so the mapping is checkable.
    const classes = container.innerHTML
    expect(classes).toContain('lucide-cog')          // oil pressure
    expect(classes).toContain('lucide-rabbit')       // boost
    expect(classes).toContain('lucide-thermometer')  // temperatures
    expect(classes).toContain('lucide-fuel')         // fuel rate
    expect(classes).toContain('lucide-clock')        // engine hours in the notch
  })

  // A card stacking three temperatures wants one thermometer, not three.
  test('draws one icon per box, not one per row', () => {
    const { container } = renderCluster()
    const temps = container.querySelector('[data-testid="cluster-corner-2"]')!
    expect(temps.querySelectorAll('svg')).toHaveLength(1)
  })

  test('an explicit icon overrides the inferred one', () => {
    const { container } = render(
      <EngineClusterTile
        config={{ ...port, corners: port.corners.map((c, i) => (i === 0 ? { ...c, icon: 'zap' } : c)) }}
        values={values} editing={false} onConfigure={vi.fn()}
      />,
    )
    const oil = container.querySelector('[data-testid="cluster-corner-0"]')!
    expect(oil.innerHTML).toContain('lucide-zap')
    expect(oil.innerHTML).not.toContain('lucide-cog')
  })
})

describe('composition', () => {
  /**
   * A tachometer says one number. RPM and fuel flow stacked in the centre at
   * near-identical weights read as a pair and push the group off the dial's
   * optical centre, so fuel flow moves to the notch the sweep leaves.
   */
  test('gives the hero readout to the ring alone', () => {
    render(<EngineClusterTile config={port} values={values} editing={false} onConfigure={vi.fn()} />)

    // Hours share the centre stack but as a small pill beneath, not as a
    // second reading at comparable weight.
    const hero = screen.getByTestId('cluster-centre-value')
    expect(hero).toHaveTextContent('698')
    expect(hero).not.toHaveTextContent('894')
    expect(hero.className).toContain('text-4xl')
    expect(screen.getByTestId('cluster-notch').className).not.toContain('text-4xl')
  })

  test('puts the secondary reading in the notch', () => {
    render(<EngineClusterTile config={port} values={values} editing={false} onConfigure={vi.fn()} />)
    const notch = screen.getByTestId('cluster-notch')
    expect(notch).toHaveTextContent('894')
    expect(notch).toHaveTextContent('h')
  })

  test('halves the scale labels so they do not compete with the reading', () => {
    render(<EngineClusterTile config={port} values={values} editing={false} onConfigure={vi.fn()} />)
    expect(screen.getByText('RPM x100')).toBeInTheDocument()
    expect(screen.getByText('10')).toBeInTheDocument()
    expect(screen.queryByText('1500')).not.toBeInTheDocument()
  })
})

describe('state at a glance', () => {
  const zoned = {
    ...port,
    corners: port.corners.map((c, i) => (i === 0
      ? { ...c, rows: [{ ...c.rows[0], min: 0, max: 100, zones: [{ from: 40, to: 100, state: 'normal' as const }] }] }
      : c)),
  }

  // The whole job of an engine panel is answering "is anything wrong".
  test('draws a zone bar under a reading that has bands', () => {
    const { container } = render(
      <EngineClusterTile config={zoned} values={values} editing={false} onConfigure={vi.fn()} />,
    )
    expect(container.querySelector('[data-testid="cluster-corner-0"] [data-zone-bar]')).not.toBeNull()
  })

  test('draws none where a reading has no bands', () => {
    const { container } = render(
      <EngineClusterTile config={zoned} values={values} editing={false} onConfigure={vi.fn()} />,
    )
    expect(container.querySelector('[data-testid="cluster-corner-3"] [data-zone-bar]')).toBeNull()
  })

  // 22 psi is below the healthy band's floor of 40, so it must not read normal.
  test('colours a reading outside its healthy band', () => {
    render(<EngineClusterTile config={zoned} values={values} editing={false} onConfigure={vi.fn()} />)
    expect(screen.getByText('22.0').className).not.toContain('text-gauge-primary')
  })

  test('leaves a reading inside its band in the normal colour', () => {
    render(
      <EngineClusterTile config={zoned} values={{ ...values, 'propulsion.port.oilPressure': 413685 }}
        editing={false} onConfigure={vi.fn()} />,
    )
    expect(screen.getByText('60.0').className).toContain('text-gauge-primary')
  })
})

// A three-row card has no dead space for a bar; colour carries the state.
test('draws no zone bars in a card that stacks several readings', () => {
  const zonedTemps = {
    ...port,
    corners: port.corners.map((c, i) => (i === 2
      ? { ...c, rows: c.rows.map((r) => ({ ...r, min: 0, max: 120, zones: [{ from: 0, to: 85, state: 'normal' as const }] })) }
      : c)),
  }
  const { container } = render(
    <EngineClusterTile config={zonedTemps} values={values} editing={false} onConfigure={vi.fn()} />,
  )
  expect(container.querySelectorAll('[data-testid="cluster-corner-2"] [data-zone-bar]')).toHaveLength(0)
})

describe('stacked cards', () => {
  test('stacks peers at equal weight', () => {
    render(<EngineClusterTile config={port} values={values} editing={false} onConfigure={vi.fn()} />)
    const temps = screen.getByTestId('cluster-corner-2')
    expect(within(temps).getByText('71.0').className).toContain('text-base')
    expect(within(temps).getByText('37.7').className).toContain('text-base')
  })

  test('gives a single reading the hero treatment', () => {
    render(<EngineClusterTile config={port} values={values} editing={false} onConfigure={vi.fn()} />)
    expect(screen.getByText('1.5').className).toContain('text-2xl')
  })
})

/**
 * The tile edge carries the worst of the ring, centre, every corner row and
 * the telltales (ADR 0081) — the same worst-of that already governs the
 * telltale row's own colour, extended up to the tile.
 */
describe('tile state (ADR 0081)', () => {
  test('carries a telltale past its alarm band up to the tile edge', () => {
    const withTelltales = {
      ...port,
      telltales: [
        { path: 'propulsion.port.temperature', label: 'Coolant', display: 'numeric' as const, quantity: 'temperature', unit: 'C',
          min: 0, max: 120, zones: [{ from: 0, to: 95, state: 'normal' as const }, { from: 95, to: 120, state: 'alarm' as const }] },
      ],
    }
    const { container } = render(
      <EngineClusterTile config={withTelltales}
        values={{ ...values, 'propulsion.port.temperature': 380.15 }}
        editing={false} onConfigure={vi.fn()} />,
    )
    expect(container.querySelector('[data-slot="card"]')).toHaveAttribute('data-state', 'alarm')
  })

  /**
   * 22.0 psi (this vessel's live oil pressure) sits below the healthy band's
   * floor of 40, so it reads `outside`. This is exactly the bundled engine
   * profile's own case (ADR 0054 §5a: warn/alarm thresholds left null), and
   * it must not light the tile: an engine idling all day with no thresholds
   * filled in would otherwise carry a permanent amber edge on a healthy
   * boat, the noise floor ADR 0080 removed from the dial in the first place.
   */
  test('does not carry an out-of-band reading to the tile edge', () => {
    const zoned = {
      ...port,
      corners: port.corners.map((c, i) => (i === 0
        ? { ...c, rows: [{ ...c.rows[0], min: 0, max: 100, zones: [{ from: 40, to: 100, state: 'normal' as const }] }] }
        : c)),
    }
    const { container } = render(
      <EngineClusterTile config={zoned} values={values} editing={false} onConfigure={vi.fn()} />,
    )
    expect(container.querySelector('[data-slot="card"]')).not.toHaveAttribute('data-state')
  })

  test('carries no state when nothing on the cluster has a band configured', () => {
    const { container } = render(
      <EngineClusterTile config={port} values={values} editing={false} onConfigure={vi.fn()} />,
    )
    expect(container.querySelector('[data-slot="card"]')).not.toHaveAttribute('data-state')
  })
})

/**
 * Ages ride the same gauge-values stream (ADR 0083). A stale slot renders the
 * dash -- the ring blanks, a corner row carries its own badge -- without
 * staling the whole tile until every slot whose age is actually known has
 * frozen, the exact shape a dead engine feed (still reading 698 RPM an hour
 * after the engine stopped) needs.
 */
describe('staleness (ADR 0083)', () => {
  // Every path the default `port` config binds, all fresh, so a test can
  // stale exactly one of them without the rest going along for the ride.
  const freshAges: Record<string, number> = {
    'propulsion.port.revolutions': 3,
    'propulsion.port.runTime': 3,
    'propulsion.port.oilPressure': 3,
    'propulsion.port.boostPressure': 3,
    'propulsion.port.temperature': 3,
    'propulsion.port.transmission.oilTemperature': 3,
    'propulsion.port.fuel.rate': 3,
  }

  test('a frozen corner row shows the dash and its own badge, without staling the whole tile', () => {
    const { container } = render(
      <EngineClusterTile
        config={port} values={values}
        ages={{ ...freshAges, 'propulsion.port.oilPressure': 300 }}
        editing={false} onConfigure={vi.fn()}
      />,
    )
    const oil = screen.getByTestId('cluster-corner-0')
    expect(within(oil).getByText('--')).toBeInTheDocument()
    expect(within(oil).getByText(/Stale/)).toBeInTheDocument()
    expect(within(oil).queryByText('22.0')).not.toBeInTheDocument()
    expect(container.querySelector('[data-slot="card"]')).not.toHaveAttribute('data-stale')
  })

  test('blanks the ring when its own path is stale', () => {
    render(
      <EngineClusterTile
        config={port} values={values}
        ages={{ ...freshAges, 'propulsion.port.revolutions': 300 }}
        editing={false} onConfigure={vi.fn()}
      />,
    )
    expect(screen.getByTestId('cluster-centre-value')).toHaveTextContent('--')
    expect(screen.queryByText('698')).not.toBeInTheDocument()
  })

  test('goes stale as a whole only once every slot with a known age has frozen', () => {
    const staleAges = Object.fromEntries(Object.keys(freshAges).map((path) => [path, 300]))
    const { container } = render(
      <EngineClusterTile config={port} values={values} ages={staleAges} editing={false} onConfigure={vi.fn()} />,
    )
    expect(container.querySelector('[data-slot="card"]')).toHaveAttribute('data-stale', 'true')
    expect(screen.getByTestId('tile-stale-badge')).toBeInTheDocument()
    expect(screen.queryByText('698')).not.toBeInTheDocument()
    expect(screen.queryByText('22.0')).not.toBeInTheDocument()
  })

  test('an unknown age never counts as stale', () => {
    const { container } = render(
      <EngineClusterTile config={port} values={values} editing={false} onConfigure={vi.fn()} />,
    )
    expect(container.querySelector('[data-slot="card"]')).not.toHaveAttribute('data-stale')
    expect(screen.getByText('698')).toBeInTheDocument()
  })
})
