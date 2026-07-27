import { render, screen, fireEvent } from '@testing-library/react'
import { beforeEach, describe, expect, test } from 'vitest'

import { WindTile } from '@/components/wind-tile'
import type { GustWindow } from '@/lib/gust-windows'

const gustValues: Record<GustWindow, number> = {
  '10m': 7.1,
  '30m': 9.2,
  '1h': 12.3,
  '24h': 18.4,
}

const baseProps = {
  headingTrue: 90,
  windAngleApparentDeg: 45,
  windSide: 'port' as const,
  windAngleRelativeDeg: 45,
  windSpeedApparentKts: 12,
  currentSetDeg: null,
  currentDriftKts: null,
  currentDriftImpactKts: null,
  maxGustKts: gustValues,
}

// WindTile renders WindGaugeCluster twice (mobile + desktop canvases), so
// every "MAX GUST" button/text always appears twice. The two clusters are
// rendered in a fixed JSX order (mobile cluster's left card, mobile's right
// card, then desktop's left card, desktop's right card), so among the 4
// "max gust" buttons returned in DOM order, even indices (0, 2) are always
// the left card and odd indices (1, 3) are always the right card.
function getLeftButtons() {
  return screen.getAllByRole('button', { name: /max gust/i }).filter((_, i) => i % 2 === 0)
}

function getRightButtons() {
  return screen.getAllByRole('button', { name: /max gust/i }).filter((_, i) => i % 2 === 1)
}

beforeEach(() => {
  localStorage.clear()
})

describe('WindTile MAX GUST cards', () => {
  test('render their default windows and values on mount with no localStorage seeded', () => {
    render(<WindTile {...baseProps} />)

    expect(screen.getAllByText('MAX GUST 10M')).toHaveLength(2)
    expect(screen.getAllByText('MAX GUST 1HR')).toHaveLength(2)
    expect(screen.getAllByText('7.1', { exact: false })).toHaveLength(2)
    expect(screen.getAllByText('12.3', { exact: false })).toHaveLength(2)

    expect(getLeftButtons()).toHaveLength(2)
    expect(getRightButtons()).toHaveLength(2)
  })

  test('clicking the left card cycles 10m -> 30m -> 1h -> 24h -> 10m, updating title and value each step', () => {
    render(<WindTile {...baseProps} />)

    fireEvent.click(getLeftButtons()[0])
    expect(screen.getAllByText('MAX GUST 30M')).toHaveLength(2)
    expect(screen.getAllByText('9.2', { exact: false })).toHaveLength(2)

    // The right card still sits on its own default (1h), so once the left
    // card also reaches 1h there are 4 matching elements total (2 per card).
    fireEvent.click(getLeftButtons()[0])
    expect(screen.getAllByText('MAX GUST 1HR')).toHaveLength(4)
    expect(screen.getAllByText('12.3', { exact: false })).toHaveLength(4)

    fireEvent.click(getLeftButtons()[0])
    expect(screen.getAllByText('MAX GUST 24HR')).toHaveLength(2)
    expect(screen.getAllByText('18.4', { exact: false })).toHaveLength(2)

    fireEvent.click(getLeftButtons()[0])
    expect(screen.getAllByText('MAX GUST 10M')).toHaveLength(2)
    expect(screen.getAllByText('7.1', { exact: false })).toHaveLength(2)
  })

  test('the two cards cycle fully independently', () => {
    render(<WindTile {...baseProps} />)

    // Click the left card three times (10m -> 30m -> 1h -> 24h). The right
    // card must stay on its untouched default the whole time.
    fireEvent.click(getLeftButtons()[0])
    fireEvent.click(getLeftButtons()[0])
    fireEvent.click(getLeftButtons()[0])
    expect(screen.getAllByText('MAX GUST 24HR')).toHaveLength(2)
    expect(screen.getAllByText('MAX GUST 1HR')).toHaveLength(2)
    expect(screen.getAllByText('12.3', { exact: false })).toHaveLength(2)

    // Now click the right card once and confirm the left card (still 24h) is unaffected.
    fireEvent.click(getRightButtons()[0])
    expect(screen.getAllByText('MAX GUST 24HR')).toHaveLength(4) // left card + right card both now read 24hr
    expect(screen.getAllByText('18.4', { exact: false })).toHaveLength(4)
  })

  test('persists the new selection to localStorage under windTile.gustWindow.left/right', () => {
    render(<WindTile {...baseProps} />)

    fireEvent.click(getLeftButtons()[0])
    expect(localStorage.getItem('windTile.gustWindow.left')).toBe('30m')

    fireEvent.click(getRightButtons()[0])
    expect(localStorage.getItem('windTile.gustWindow.right')).toBe('24h')
  })

  test('restores a valid pre-seeded localStorage selection on mount', () => {
    localStorage.setItem('windTile.gustWindow.right', '24h')

    render(<WindTile {...baseProps} />)

    expect(screen.getAllByText('MAX GUST 24HR')).toHaveLength(2)
    expect(screen.getAllByText('18.4', { exact: false })).toHaveLength(2)
    // Left card is unaffected and keeps its own default.
    expect(screen.getAllByText('MAX GUST 10M')).toHaveLength(2)
  })

  test('falls back to the default window for an invalid/corrupt localStorage value, without throwing', () => {
    localStorage.setItem('windTile.gustWindow.left', 'garbage')

    expect(() => render(<WindTile {...baseProps} />)).not.toThrow()

    expect(screen.getAllByText('MAX GUST 10M')).toHaveLength(2)
    expect(screen.getAllByText('7.1', { exact: false })).toHaveLength(2)
  })

  test('cards are focusable, native buttons that activate on Enter and Space', () => {
    render(<WindTile {...baseProps} />)

    const button = getLeftButtons()[0]
    button.focus()
    expect(button).toHaveFocus()

    // Native <button> elements turn an Enter keydown into a click; jsdom
    // doesn't perform that default action itself, so the click is fired
    // explicitly here to stand in for what a real browser does.
    fireEvent.keyDown(button, { key: 'Enter', code: 'Enter' })
    fireEvent.click(button)
    expect(screen.getAllByText('MAX GUST 30M')).toHaveLength(2)

    fireEvent.keyDown(button, { key: ' ', code: 'Space' })
    fireEvent.click(button)
    // The right card defaults to 1h too, so both cards now read MAX GUST 1HR.
    expect(screen.getAllByText('MAX GUST 1HR')).toHaveLength(4)
  })
})
