import { render } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { WindBarb, WaveDirectionArrow } from '@/components/forecast/direction-glyphs'

function lineLength(line: SVGLineElement) {
  const dx = Number(line.getAttribute('x2')) - Number(line.getAttribute('x1'))
  const dy = Number(line.getAttribute('y2')) - Number(line.getAttribute('y1'))
  return Math.hypot(dx, dy)
}

describe('direction glyph scale', () => {
  test('WindBarb draws its staff at the drawer size by default and shrinks with scale', () => {
    const { container: full } = render(<svg><WindBarb cx={50} cy={50} speedKts={15} directionDeg={90} /></svg>)
    const { container: small } = render(<svg><WindBarb cx={50} cy={50} speedKts={15} directionDeg={90} scale={0.5} /></svg>)

    const fullStaff = full.querySelector('[data-testid="forecast-wind-barb"] > line') as SVGLineElement
    const smallStaff = small.querySelector('[data-testid="forecast-wind-barb"] > line') as SVGLineElement
    expect(lineLength(fullStaff)).toBeCloseTo(24)
    expect(lineLength(smallStaff)).toBeCloseTo(12)
  })

  test('WaveDirectionArrow shrinks with scale', () => {
    const { container: full } = render(<svg><WaveDirectionArrow cx={50} cy={50} directionDeg={0} /></svg>)
    const { container: small } = render(<svg><WaveDirectionArrow cx={50} cy={50} directionDeg={0} scale={0.5} /></svg>)

    const fullShaft = full.querySelector('[data-testid="forecast-wave-arrow"] > line') as SVGLineElement
    const smallShaft = small.querySelector('[data-testid="forecast-wave-arrow"] > line') as SVGLineElement
    expect(lineLength(fullShaft)).toBeCloseTo(36)
    expect(lineLength(smallShaft)).toBeCloseTo(18)
  })
})
