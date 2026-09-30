import { render, screen, fireEvent } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { AnchorAdjustRadiusToolbar, type AnchorAdjustRadiusToolbarProps } from '@/components/anchor-adjust-radius-toolbar'

// ADR 0143: the radius stepper is now a small −/readout/+ toolbar, reused
// both inside AnchorWatchMap's own control column and inline in the
// no-WebGL2 fallback (no more hero readout + chips — those lived in the
// bottom bar this replaces, anchor-adjust-bar.tsx).

const baseProps: AnchorAdjustRadiusToolbarProps = {
  radiusM: 20,
  isImperial: false,
  atMin: false,
  atMax: false,
  maxDisabledReason: 'Chain onboard + boat length',
  stepM: 1,
  onStepRadius: () => undefined,
}

describe('AnchorAdjustRadiusToolbar readout', () => {
  it('shows the rounded whole-unit radius apart from its unit, like the other KPI readouts', () => {
    render(<AnchorAdjustRadiusToolbar {...baseProps} radiusM={24.4} />)
    expect(screen.getByTestId('anchor-adjust-radius-value')).toHaveTextContent(/^24$/)
    expect(screen.getByTestId('anchor-adjust-radius-unit')).toHaveTextContent(/^m$/)
  })

  it('shows feet under imperial', () => {
    render(<AnchorAdjustRadiusToolbar {...baseProps} radiusM={24} isImperial />)
    expect(screen.getByTestId('anchor-adjust-radius-value')).toHaveTextContent(/^79$/)
    expect(screen.getByTestId('anchor-adjust-radius-unit')).toHaveTextContent(/^ft$/)
  })
})

describe('AnchorAdjustRadiusToolbar stepping', () => {
  it('steps up once on a keyboard-originated click (detail 0)', () => {
    const onStepRadius = vi.fn()
    render(<AnchorAdjustRadiusToolbar {...baseProps} onStepRadius={onStepRadius} />)
    fireEvent.click(screen.getByRole('button', { name: 'Increase radius' }), { detail: 0 })
    expect(onStepRadius).toHaveBeenCalledTimes(1)
    expect(onStepRadius).toHaveBeenCalledWith(1)
  })

  it('steps down once on a keyboard-originated click (detail 0)', () => {
    const onStepRadius = vi.fn()
    render(<AnchorAdjustRadiusToolbar {...baseProps} onStepRadius={onStepRadius} />)
    fireEvent.click(screen.getByRole('button', { name: 'Decrease radius' }), { detail: 0 })
    expect(onStepRadius).toHaveBeenCalledTimes(1)
    expect(onStepRadius).toHaveBeenCalledWith(-1)
  })

  it('does not double-step on a pointer-originated click (detail >= 1) — onPointerDown already stepped', () => {
    const onStepRadius = vi.fn()
    render(<AnchorAdjustRadiusToolbar {...baseProps} onStepRadius={onStepRadius} />)
    fireEvent.click(screen.getByRole('button', { name: 'Increase radius' }), { detail: 1 })
    expect(onStepRadius).not.toHaveBeenCalled()
  })

  it('a single pointer press-and-release steps once immediately', () => {
    const onStepRadius = vi.fn()
    render(<AnchorAdjustRadiusToolbar {...baseProps} onStepRadius={onStepRadius} />)
    const increment = screen.getByRole('button', { name: 'Increase radius' })
    fireEvent.pointerDown(increment)
    fireEvent.pointerUp(increment)
    expect(onStepRadius).toHaveBeenCalledTimes(1)
    expect(onStepRadius).toHaveBeenCalledWith(1)
  })
})

describe('AnchorAdjustRadiusToolbar disabled at the bounds', () => {
  it('disables − at the minimum, with a title reason', () => {
    render(<AnchorAdjustRadiusToolbar {...baseProps} radiusM={5} atMin />)
    const decrement = screen.getByRole('button', { name: 'Decrease radius' })
    expect(decrement).toBeDisabled()
    expect(decrement).toHaveAttribute('title', 'Minimum 5 m')
  })

  it('disables + at the maximum, with a title reason', () => {
    render(<AnchorAdjustRadiusToolbar {...baseProps} atMax maxDisabledReason="Chain onboard, without boat length" />)
    const increment = screen.getByRole('button', { name: 'Increase radius' })
    expect(increment).toBeDisabled()
    expect(increment).toHaveAttribute('title', 'Chain onboard, without boat length')
  })
})

describe('AnchorAdjustRadiusToolbar variants', () => {
  it('defaults to the overlay (dark scrim) styling', () => {
    render(<AnchorAdjustRadiusToolbar {...baseProps} />)
    expect(screen.getByTestId('anchor-adjust-radius-toolbar').className).toMatch(/bg-black\/65/)
  })

  it('switches to themed panel styling when asked', () => {
    render(<AnchorAdjustRadiusToolbar {...baseProps} variant="panel" />)
    expect(screen.getByTestId('anchor-adjust-radius-toolbar').className).toMatch(/bg-card/)
  })
})
