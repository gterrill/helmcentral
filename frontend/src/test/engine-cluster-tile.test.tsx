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
  skin: 'instrument',
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

    expect(top).toBeGreaterThanOrEqual(0)
    expect(bottom).toBeLessThanOrEqual(parseFloat(canvas.style.height))
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
    expect(coolant()).toContain('text-red-500')
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
    expect(container.querySelector('[data-telltale="coolant"]')!.className).toContain('text-amber-500')
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

  test('renders no row at all when none are configured', () => {
    renderCluster()
    expect(screen.queryByTestId('cluster-telltales')).not.toBeInTheDocument()
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

  test('applies the instrument skin, and not by default', () => {
    const { container, rerender } = renderCluster()
    expect(container.querySelector('[data-skin="instrument"]')).not.toBeNull()

    rerender(<EngineClusterTile config={{ ...port, skin: 'default' }} values={values} editing={false} onConfigure={vi.fn()} />)
    expect(container.querySelector('[data-skin="instrument"]')).toBeNull()
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
 */
describe('instrument skin token coverage', () => {
  test('redefines every foreground token the light theme sets', async () => {
    const [fs, path] = await Promise.all([import('node:fs'), import('node:path')])
    const css = fs.readFileSync(path.resolve(process.cwd(), 'src/index.css'), 'utf8')

    const section = (selector: string) =>
      css.slice(css.indexOf(selector), css.indexOf('}', css.indexOf(selector)))

    const tokens = (block: string) =>
      new Set([...block.matchAll(/--([\w-]+):/g)].map((m) => m[1]))

    const light = tokens(section(':root {'))
    const instrument = tokens(section('[data-skin="instrument"] {'))

    // The alert pair is deliberately shared: --destructive stays red in both
    // skins so a warning looks like a warning, and its foreground is near-white
    // either way. Everything else has to be redefined.
    const shared = new Set(['destructive-foreground'])

    const missing = [...light].filter(
      (t) => t.endsWith('-foreground')
        && !t.startsWith('sidebar')
        && !shared.has(t)
        && !instrument.has(t),
    )
    expect(missing).toEqual([])
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
    expect(hero.className).toContain('text-5xl')
    expect(screen.getByTestId('cluster-notch').className).not.toContain('text-5xl')
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
