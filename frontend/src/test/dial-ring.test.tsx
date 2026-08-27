import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { DialRing, arcEndFraction } from '@/components/ui/dial-ring'

function ticks(container: HTMLElement) {
  return [...container.querySelectorAll('[data-tick]')]
}

describe('DialRing', () => {
  test('draws a major tick and a label per step, inclusive of both ends', () => {
    const { container } = render(<DialRing value={null} min={0} max={3000} majorStep={500} />)

    // 0, 500, 1000, 1500, 2000, 2500, 3000
    expect(ticks(container).filter((t) => t.getAttribute('data-tick') === 'major')).toHaveLength(7)
    expect(screen.getByText('0')).toBeInTheDocument()
    expect(screen.getByText('3000')).toBeInTheDocument()
  })

  test('subdivides between majors with minor ticks', () => {
    const { container } = render(
      <DialRing value={null} min={0} max={1000} majorStep={500} minorPerMajor={5} />,
    )
    // 2 intervals x 4 interior minors
    expect(ticks(container).filter((t) => t.getAttribute('data-tick') === 'minor')).toHaveLength(8)
  })

  test('divides the scale labels', () => {
    render(<DialRing value={null} min={0} max={3000} majorStep={500} labelDivisor={100} />)

    expect(screen.getByText('30')).toBeInTheDocument()
    expect(screen.queryByText('3000')).not.toBeInTheDocument()
  })

  /**
   * The pointer is a bar across the band, so how far in it reaches is CSS now,
   * not geometry in this file. Every skin still has to keep it off the readout.
   */
  test('the pointer stays clear of the middle in every skin', async () => {
    const [fs, path] = await Promise.all([import('node:fs'), import('node:path')])
    const css = fs.readFileSync(path.resolve(process.cwd(), 'src/index.css'), 'utf8')

    const radii = [...css.matchAll(/--dial-needle-r:\s*([\d.]+)px/g)].map((m) => Number(m[1]))
    const widths = [...css.matchAll(/--dial-needle-w:\s*([\d.]+)px/g)].map((m) => Number(m[1]))

    expect(radii).toHaveLength(widths.length)
    expect(radii.length).toBeGreaterThan(1)
    radii.forEach((r, i) => expect(r - widths[i] / 2).toBeGreaterThan(60))
  })

  /**
   * Zones render as segments on the rim rather than a wash across the arc:
   * a red band you can see at a glance is the whole point of the reference.
   */
  test('marks zones as rim segments', () => {
    const { container } = render(
      <DialRing value={null} min={0} max={100} majorStep={50}
        zones={[{ from: 80, to: 100, state: 'alarm' }]} />,
    )
    const segments = [...container.querySelectorAll('[data-zone]')]
    expect(segments.length).toBeGreaterThan(0)
    expect(segments[0].getAttribute('stroke')).toContain('hsl(0')
  })

  /**
   * On the same rule as the ticks. A green arc down the whole normal band is
   * 250 degrees of rim saying nothing, and it buried the value arc, the redline
   * and the pointer under itself.
   */
  test('leaves the normal band off the rim', () => {
    const { container } = render(
      <DialRing value={null} min={0} max={100} majorStep={50}
        zones={[{ from: 0, to: 80, state: 'normal' }, { from: 80, to: 100, state: 'alarm' }]} />,
    )
    const states = [...container.querySelectorAll('[data-zone]')].map((z) => z.getAttribute('data-zone'))
    expect(states).toEqual(['alarm'])
  })

  // An arc drawn at zero length reads as a real reading of the minimum.
  test('draws no value arc when there is no value', () => {
    const { container } = render(<DialRing value={null} min={0} max={100} majorStep={50} />)
    expect(container.querySelector('[data-value-arc]')).toBeNull()
  })

  test('draws the value arc when there is one', () => {
    const { container } = render(<DialRing value={50} min={0} max={100} majorStep={50} />)
    expect(container.querySelector('[data-value-arc]')).not.toBeNull()
  })

  test('clamps a value beyond either end rather than overdrawing', () => {
    const { container } = render(<DialRing value={9999} min={0} max={100} majorStep={50} />)
    const dash = container.querySelector('[data-value-arc]')!.getAttribute('stroke-dasharray')!
    expect(dash).not.toContain('NaN')
    // Full scale is the whole sweep and no more: 250/360 of the circle.
    expect(Number(dash.split(' ')[0])).toBeCloseTo(69.44, 1)
  })

  test('renders its children in the middle', () => {
    render(<DialRing value={50} min={0} max={100} majorStep={50}><span>698 RPM</span></DialRing>)
    expect(screen.getByText('698 RPM')).toBeInTheDocument()
  })

  test('uses theme tokens rather than hardcoded colours', () => {
    const { container } = render(<DialRing value={50} min={0} max={100} majorStep={50} />)
    const svg = container.querySelector('svg')!.outerHTML
    expect(svg).toContain('hsl(var(--')
    expect(svg).not.toMatch(/#[0-9a-f]{6}/i)
  })
})

describe('needle', () => {
  test('points at the value, and is absent without one', () => {
    const { container, rerender } = render(<DialRing value={50} min={0} max={100} majorStep={50} />)
    expect(container.querySelector('[data-needle]')).not.toBeNull()

    rerender(<DialRing value={null} min={0} max={100} majorStep={50} />)
    expect(container.querySelector('[data-needle]')).toBeNull()
  })

  /**
   * A bar across the band rather than a blade riding the rim, which is what the
   * reference draws and what the skinned dial needs: with the band moved in to
   * r96 w40 the blade sat on top of its own bright end and disappeared into it.
   * Drawn as a dash so the radius and width are the skin's tokens, the same
   * trick the value arc already uses.
   */
  test('is a bar across whatever band the skin sets', () => {
    const { container } = render(<DialRing value={50} min={0} max={100} majorStep={50} />)
    const needle = container.querySelector('[data-needle]')!

    expect(needle.tagName.toLowerCase()).toBe('circle')
    expect(needle.getAttribute('style')).toContain('var(--dial-needle-r)')
    expect(needle.getAttribute('style')).toContain('var(--dial-needle-w)')
  })

  // Half scale on a symmetric sweep points straight down the vertical: 270
  // degrees round a pathLength-100 circle, less half the bar's own width.
  test('sits at the midpoint for a mid-scale value', () => {
    const { container } = render(<DialRing value={50} min={0} max={100} majorStep={50} />)
    const offset = Number(container.querySelector('[data-needle]')!.getAttribute('stroke-dashoffset'))
    expect(offset).toBeCloseTo(-74.77, 1)
  })

  test('clamps rather than swinging past the end of the scale', () => {
    const { container } = render(<DialRing value={9999} min={0} max={100} majorStep={50} />)
    const { container: full } = render(<DialRing value={100} min={0} max={100} majorStep={50} />)

    const over = container.querySelector('[data-needle]')!.getAttribute('stroke-dashoffset')!
    expect(over).not.toContain('NaN')
    expect(over).toBe(full.querySelector('[data-needle]')!.getAttribute('stroke-dashoffset'))
  })
})


/**
 * A redline. The reference paints the top of the tachometer's scale red on the
 * ticks and the numbers themselves rather than only out on the rim, which is
 * what makes it readable at the angle a helm dial is actually read from.
 */
describe('redline', () => {
  /** jsdom normalises the hsl() the zone vocabulary is written in. */
  function isRed(style: string | null) {
    const [r, g, b] = (style ?? '').match(/rgb\((\d+), (\d+), (\d+)\)/)?.slice(1).map(Number) ?? []
    return r > 150 && g < 90 && b < 90
  }

  const redlined = (
    <DialRing value={null} min={0} max={3300} majorStep={500} labelEvery={2} labelDivisor={100}
      zones={[{ from: 0, to: 3000, state: 'normal' }, { from: 3000, to: 3300, state: 'alarm' }]} />
  )

  test('marks the ticks the alarm zone covers', () => {
    const { container } = render(redlined)
    const red = [...container.querySelectorAll('[data-tick-zone="alarm"]')]

    // 3100, 3200, 3300, and the 3000 major the band opens on.
    expect(red).toHaveLength(4)
    expect(red.every((t) => isRed(t.getAttribute('style')))).toBe(true)
  })

  /**
   * The normal band is not a marking. Painting every tick under it green would
   * make the scale itself a readout and leave nothing for the redline to stand
   * out against.
   */
  test('leaves the ticks under the normal band alone', () => {
    const { container } = render(redlined)
    const marked = [...container.querySelectorAll('[data-tick-zone]')]
      .map((t) => t.getAttribute('data-tick-zone'))
    expect(new Set(marked)).toEqual(new Set(['alarm']))
  })

  test('reddens the scale number inside it too', () => {
    const { container } = render(
      <DialRing value={null} min={0} max={3300} majorStep={500}
        zones={[{ from: 3000, to: 3300, state: 'alarm' }]} />,
    )
    const label = [...container.querySelectorAll('text')].find((t) => t.textContent === '3000')!
    expect(isRed(label.getAttribute('style'))).toBe(true)
  })

  /**
   * A range whose top is not a whole number of major steps still has to carry
   * ticks over its last stretch. The minors used to be generated only between
   * majors, so a 3300 redline on a 500 rpm scale left its top 300 rpm blank -
   * exactly the stretch the red is there to mark.
   */
  test('ticks the stretch above the last whole major step', () => {
    const { container } = render(
      <DialRing value={null} min={0} max={3300} majorStep={500} minorPerMajor={5} />,
    )
    // Six whole intervals of four interior minors, plus 3100, 3200 and 3300.
    expect(container.querySelectorAll('[data-tick="minor"]')).toHaveLength(27)
  })
})


describe('scale labels', () => {
  test('labels every major by default', () => {
    render(<DialRing value={null} min={0} max={3000} majorStep={500} />)
    expect(screen.getByText('1500')).toBeInTheDocument()
  })

  /**
   * Six four-digit numbers ringing a four-digit reading is the scale competing
   * with the value it surrounds. Labelling every other major keeps the ticks
   * carrying the resolution and the numbers carrying the orientation.
   */
  test('labels every other major when asked', () => {
    render(<DialRing value={null} min={0} max={3000} majorStep={500} labelEvery={2} labelDivisor={100} />)

    expect(screen.getByText('0')).toBeInTheDocument()
    expect(screen.getByText('10')).toBeInTheDocument()
    expect(screen.getByText('20')).toBeInTheDocument()
    expect(screen.getByText('30')).toBeInTheDocument()
    expect(screen.queryByText('5')).not.toBeInTheDocument()
    expect(screen.queryByText('15')).not.toBeInTheDocument()
  })

  test('keeps every major tick even when labelling fewer', () => {
    const { container } = render(
      <DialRing value={null} min={0} max={3000} majorStep={500} labelEvery={2} />,
    )
    expect(container.querySelectorAll('[data-tick="major"]')).toHaveLength(7)
  })
})


describe('arcEndFraction', () => {
  /**
   * Where the sweep stops, as a fraction of the dial's box height. The cluster
   * aligns its lower cards to this line, so it has to come from the same
   * geometry the arc is drawn with rather than a number copied by eye.
   */
  test('is symmetric and below the centre for a bottom-gap sweep', () => {
    const f = arcEndFraction(250)
    expect(f).toBeGreaterThan(0.5)
    expect(f).toBeLessThan(1)
    expect(f).toBeCloseTo(0.784, 2)
  })

  test('a wider sweep ends lower', () => {
    expect(arcEndFraction(300)).toBeGreaterThan(arcEndFraction(250))
  })

  test('a half sweep ends level with the centre', () => {
    expect(arcEndFraction(180)).toBeCloseTo(0.5, 2)
  })

  // The drawn zone rim sits outside the arc, so the line has to clear it too.
  test('accounts for the zone rim, not just the value arc', () => {
    const { container } = render(
      <DialRing value={null} min={0} max={100} majorStep={50}
        zones={[{ from: 0, to: 5, state: 'alarm' }]} />,
    )
    const zone = container.querySelector('[data-zone]')!.getAttribute('d')!
    const lowest = Math.max(...zone.match(/-?\d+\.?\d*/g)!.map(Number).filter((_, i) => i % 2 === 1))
    expect(arcEndFraction(250) * 280).toBeGreaterThanOrEqual(lowest - 1)
  })
})


/**
 * The dial's chrome is token-driven so a skin can turn it on without any
 * per-component branching. That only holds if every var it reads has a :root
 * default: a custom property with no value makes the whole declaration
 * invalid at computed-value time, and the property is dropped silently rather
 * than falling back to anything. The instrument skin already taught this
 * codebase the lesson once, with --card-foreground rendering the config gear
 * near-black on a near-black card.
 */
describe('dial chrome tokens', () => {
  test('every --dial-* var the component reads has a :root default', async () => {
    const [fs, path] = await Promise.all([import('node:fs'), import('node:path')])
    const read = (rel: string) => fs.readFileSync(path.resolve(process.cwd(), rel), 'utf8')

    const component = read('src/components/ui/dial-ring.tsx')
    const css = read('src/index.css')

    const root = css.slice(css.indexOf(':root {'), css.indexOf('}', css.indexOf(':root {')))
    const declared = new Set([...root.matchAll(/--([\w-]+):/g)].map((m) => m[1]))
    const used = new Set([...component.matchAll(/var\(--(dial-[\w-]+)/g)].map((m) => m[1]))

    expect(used.size).toBeGreaterThan(0)
    expect([...used].filter((t) => !declared.has(t))).toEqual([])
  })

  /**
   * The arc is positioned by dash offset, never by a transform on the element.
   * objectBoundingBox gradient coordinates live in the element's own space, so
   * rotating the circle drags its gradient round with it: the ramp came out
   * reversed, brightest at zero instead of at the reading, and it changed the
   * flat dial as well as the skinned one.
   */
  test('positions the arc without transforming it, so its gradient stays put', () => {
    const { container } = render(<DialRing value={50} min={0} max={100} majorStep={50} />)
    const arc = container.querySelector('[data-value-arc]')!
    expect(arc.getAttribute('transform')).toBeNull()
    // 250 degrees of sweep starting at 145 degrees round a pathLength-100 circle.
    expect(Number(arc.getAttribute('stroke-dashoffset'))).toBeCloseTo(-40.28, 1)
  })

  test('renders the bezel and hub layers so a skin has something to paint', () => {
    const { container } = render(<DialRing value={50} min={0} max={100} majorStep={50} />)
    expect(container.querySelector('[data-bezel]')).not.toBeNull()
    expect(container.querySelector('[data-hub]')).not.toBeNull()
  })
})
