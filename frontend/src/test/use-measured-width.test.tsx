import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

import { describe, it, expect, vi } from 'vitest'
import { render } from '@testing-library/react'

import { useMeasuredWidth } from '@/hooks/use-measured-width'

function Probe({ onWidth }: { onWidth: (width: number) => void }) {
  const [ref, width] = useMeasuredWidth()
  onWidth(width)
  return <div ref={ref} />
}

describe('useMeasuredWidth', () => {
  it('reports the element real width once measured', () => {
    const rectSpy = vi.spyOn(Element.prototype, 'getBoundingClientRect').mockReturnValue({
      width: 742,
      height: 0,
      top: 0,
      left: 0,
      right: 0,
      bottom: 0,
      x: 0,
      y: 0,
      toJSON() {},
    } as DOMRect)

    const widths: number[] = []
    render(<Probe onWidth={(width) => widths.push(width)} />)

    expect(widths.at(-1)).toBe(742)
    rectSpy.mockRestore()
  })

  // A chart card measured in a plain useEffect visibly reflows on first
  // paint: React guarantees useEffect runs only AFTER the browser has
  // already painted the current frame, so a caller that renders a fallback
  // width while width is still 0 (forecast-drawer.tsx's forecastChartWidth)
  // briefly paints that fallback before snapping to the real size on the
  // very next frame. useLayoutEffect runs synchronously after DOM mutations
  // but before the browser paints, so only the corrected width is ever
  // presented.
  //
  // That paint-timing difference isn't observable as a behavioral test under
  // jsdom + Testing Library - act() flushes both effect types before
  // render() returns either way - so this pins the source directly, the same
  // technique forecast-drawer.test.tsx's own design-token guard tests
  // already use for other properties a DOM assertion can't reach.
  it('measures in useLayoutEffect, not useEffect, so a caller does not flash a fallback width before the real one', () => {
    const testDir = dirname(fileURLToPath(import.meta.url))
    const source = readFileSync(resolve(testDir, '../hooks/use-measured-width.ts'), 'utf8')

    expect(source).toMatch(/\buseLayoutEffect\b/)
    expect(source).not.toMatch(/\buseEffect\(/)
  })
})
