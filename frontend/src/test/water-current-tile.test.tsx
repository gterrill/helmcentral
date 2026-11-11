import { render, screen } from '@testing-library/react'
import type { ComponentProps } from 'react'
import { describe, expect, test } from 'vitest'

import { WaterCurrentTile } from '@/components/water-current-tile'

type TileProps = ComponentProps<typeof WaterCurrentTile>

const underway: TileProps = {
  navigationState: 'motoring',
  currentSetDeg: 353,
  currentDriftKts: 2.1,
  currentDriftImpactKts: 0.4,
  headingTrue: 138,
  lastUpdateAgeS: null,
}

function renderTile(overrides: Partial<TileProps> = {}) {
  return render(<WaterCurrentTile {...underway} {...overrides} />)
}

describe('WaterCurrentTile', () => {
  test('reports the comes-from angle on the starboard side', () => {
    renderTile()
    expect(screen.getByTestId('current-speed')).toHaveTextContent('2.1')
    expect(screen.getByTestId('current-angle')).toHaveTextContent('S35')
  })

  test('reports the comes-from angle on the port side', () => {
    // set 90, heading 0: the water comes from 270, which is 90 off the port bow.
    renderTile({ currentSetDeg: 90, headingTrue: 0 })
    expect(screen.getByTestId('current-angle')).toHaveTextContent('P90')
  })

  test('dead ahead is 0 and dead astern is 180, plain numbers that fit two columns', () => {
    const { unmount } = renderTile({ currentSetDeg: 180, headingTrue: 0 })
    expect(screen.getByTestId('current-angle').textContent).toBe('0')
    unmount()
    renderTile({ currentSetDeg: 0, headingTrue: 0 })
    expect(screen.getByTestId('current-angle').textContent).toBe('180')
  })

  test('a current helping the boat reads teal with a plus sign, the figure in its own element', () => {
    renderTile({ currentDriftImpactKts: 0.4 })
    expect(screen.getByTestId('current-effect-label')).toHaveTextContent('Helping')
    expect(screen.getByTestId('current-effect-figure').textContent).toBe('+0.4')
    expect(screen.getByTestId('current-effect').className).toContain('text-gauge-secondary')
  })

  test('a current against the boat reads amber with a real minus sign', () => {
    renderTile({ currentDriftImpactKts: -1.8 })
    expect(screen.getByTestId('current-effect-label')).toHaveTextContent('Hindering')
    expect(screen.getByTestId('current-effect-figure').textContent).toBe('−1.8')
    expect(screen.getByTestId('current-effect').className).toContain('text-amber-600')
  })

  test('a current that rounds to zero along the course has no effect, and is not coloured', () => {
    renderTile({ currentDriftImpactKts: 0.03 })
    const effect = screen.getByTestId('current-effect')
    expect(screen.getByTestId('current-effect-label')).toHaveTextContent('No effect')
    expect(effect.className).not.toContain('text-amber-600')
    expect(effect.className).not.toContain('text-gauge-secondary')
    expect(screen.getByTestId('current-angle')).toHaveTextContent('S35')
  })

  test('drift that rounds to zero is still water: no angle, no current', () => {
    renderTile({ currentDriftKts: 0.04, currentDriftImpactKts: 0 })
    expect(screen.getByTestId('current-speed')).toHaveTextContent('0.0')
    expect(screen.getByTestId('current-angle')).toHaveTextContent('—')
    expect(screen.getByTestId('current-effect-label')).toHaveTextContent('No current')
    expect(screen.getByTestId('current-effect').className).not.toContain('text-amber-600')
  })

  test('shows dashes and a reason when not under way', () => {
    renderTile({ navigationState: 'anchored' })
    expect(screen.getByTestId('current-speed')).toHaveTextContent('—')
    expect(screen.getByTestId('current-angle')).toHaveTextContent('—')
    expect(screen.getByTestId('current-effect-figure')).toHaveTextContent('—')
    expect(screen.getByText('Shows under way')).toBeInTheDocument()
  })

  test.each(['sailing', 'motoring', 'under way using engine', 'UNDER_WAY_USING_ENGINE', 'UNDER_WAY_SAILING', ' Motoring '])(
    'reads when the navigation state is %s',
    (navigationState) => {
      renderTile({ navigationState })
      expect(screen.getByTestId('current-angle')).toHaveTextContent('S35')
    },
  )

  test('with no navigation state it says so instead of claiming the boat is not under way', () => {
    renderTile({ navigationState: null })
    expect(screen.getByText('No navigation state')).toBeInTheDocument()
    expect(screen.queryByText('Shows under way')).toBeNull()
    expect(screen.getByTestId('current-speed')).toHaveTextContent('—')
    expect(screen.getByTestId('current-angle')).toHaveTextContent('—')
  })

  test('without a heading the angle is a dash but the effect still shows', () => {
    renderTile({ headingTrue: null })
    expect(screen.getByTestId('current-angle')).toHaveTextContent('—')
    expect(screen.getByText('No heading')).toBeInTheDocument()
    expect(screen.getByTestId('current-effect-label')).toHaveTextContent('Helping')
    expect(screen.getByTestId('current-effect-figure').textContent).toBe('+0.4')
    expect(screen.getByTestId('current-speed')).toHaveTextContent('2.1')
  })

  test('without a drift reading everything is a dash with a reason', () => {
    renderTile({ currentDriftKts: null, currentDriftImpactKts: null })
    expect(screen.getByTestId('current-speed')).toHaveTextContent('—')
    expect(screen.getByTestId('current-angle')).toHaveTextContent('—')
    expect(screen.getByTestId('current-effect-figure')).toHaveTextContent('—')
    expect(screen.getByText('No current reading')).toBeInTheDocument()
  })

  test('without a set the angle and effect are not guessed', () => {
    renderTile({ currentSetDeg: null })
    expect(screen.getByTestId('current-angle')).toHaveTextContent('—')
    expect(screen.getByText('No current reading')).toBeInTheDocument()
  })

  // jsdom discards the cq-unit clamp itself (as gauge-tile.test.tsx notes), so
  // the tell is that each reading is the shared Readout in its fluid form,
  // which the Gauge tile also renders.
  test.each(['current-speed', 'current-angle', 'current-effect-figure'])(
    '%s is drawn by the shared fluid Readout',
    (testId) => {
      renderTile()
      const readout = screen.getByTestId(testId).closest('[data-slot="readout"]')
      expect(readout).not.toBeNull()
      expect(readout).toHaveAttribute('data-fluid', 'true')
    },
  )

  test('the units sit beside the numbers', () => {
    renderTile()
    expect(screen.getByTestId('current-speed').closest('[data-slot="readout"]')).toHaveTextContent(/2\.1\s*kt/)
  })

  test('a blocked tile puts its reason where the effect word goes, and lets it wrap', () => {
    renderTile({ navigationState: 'anchored' })
    const label = screen.getByTestId('current-effect-label')
    expect(label).toHaveTextContent('Shows under way')
    expect(label.className).not.toContain('truncate')
  })

  test('ordinary labels stay on one line', () => {
    renderTile()
    expect(screen.getByTestId('current-speed-label').className).toContain('truncate')
    expect(screen.getByTestId('current-effect-label').className).toContain('truncate')
  })

  test('no heading is said on the From row and leaves the effect word alone', () => {
    renderTile({ headingTrue: null })
    expect(screen.getByTestId('current-angle-label')).toHaveTextContent('No heading')
    expect(screen.getByTestId('current-effect-label')).toHaveTextContent('Helping')
  })

  test('a fresh feed draws no stale badge', () => {
    renderTile({ lastUpdateAgeS: 5 })
    expect(screen.queryByTestId('tile-stale-badge')).toBeNull()
  })

  test('a quiet feed outlines the tile as stale', () => {
    renderTile({ lastUpdateAgeS: 600 })
    expect(screen.getByTestId('tile-stale-badge')).toBeInTheDocument()
  })

  test.each([
    ['not under way', { navigationState: 'anchored' }],
    ['no navigation state', { navigationState: null }],
    ['no current reading', { currentDriftKts: null }],
  ])('a quiet feed draws no stale badge on a tile left blank: %s', (_name, overrides) => {
    renderTile({ lastUpdateAgeS: 600, ...overrides })
    expect(screen.queryByTestId('tile-stale-badge')).toBeNull()
  })
})
