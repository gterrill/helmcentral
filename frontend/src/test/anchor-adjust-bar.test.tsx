import { render, screen, fireEvent } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { AnchorAdjustBar, type AnchorAdjustBarProps } from '@/components/anchor-adjust-bar'

const baseProps: AnchorAdjustBarProps = {
  radiusM: 20,
  isImperial: false,
  atMin: false,
  atMax: false,
  maxDisabledReason: 'Chain onboard + boat length',
  stepM: 1,
  onStepRadius: () => undefined,
  chips: [
    { label: 'Rode + LOA', valueM: 48, reason: null },
    { label: 'Planner swing', valueM: 55, reason: null },
  ],
  onApplyChip: () => undefined,
  warningActive: false,
  confirmingSet: false,
  committing: false,
  onCancel: () => undefined,
  onSet: () => undefined,
}

// Disabled reasons have to be readable on a touchscreen, where a hover
// title never shows.
describe('AnchorAdjustBar disabled reasons', () => {
  it('shows no reasons line when nothing is disabled', () => {
    render(<AnchorAdjustBar {...baseProps} />)
    expect(screen.queryByTestId('anchor-adjust-reasons')).toBeNull()
  })

  it('shows a disabled chip reason as visible text', () => {
    render(
      <AnchorAdjustBar
        {...baseProps}
        chips={[
          { label: 'Rode + LOA', valueM: null, reason: 'no boat length' },
          { label: 'Planner swing', valueM: 55, reason: null },
        ]}
      />,
    )
    expect(screen.getByTestId('anchor-adjust-reasons')).toHaveTextContent('Rode + LOA: no boat length')
  })

  it('shows why + is disabled at the maximum', () => {
    render(<AnchorAdjustBar {...baseProps} atMax maxDisabledReason="Chain onboard, without boat length" />)
    expect(screen.getByTestId('anchor-adjust-reasons')).toHaveTextContent('Maximum: Chain onboard, without boat length')
  })

  it('shows the minimum when − is disabled', () => {
    render(<AnchorAdjustBar {...baseProps} radiusM={5} atMin />)
    expect(screen.getByTestId('anchor-adjust-reasons')).toHaveTextContent('Minimum 5 m')
  })
})

describe('AnchorAdjustBar radius readout', () => {
  it('sets the unit apart from the figure, like the other KPI readouts', () => {
    render(<AnchorAdjustBar {...baseProps} />)
    expect(screen.getByTestId('anchor-adjust-radius-value')).toHaveTextContent(/^20$/)
    expect(screen.getByTestId('anchor-adjust-radius-unit')).toHaveTextContent(/^m$/)
  })
})

// A committed radius above the current chain+LOA ceiling (possible from
// before that ceiling existed) gets clamped down for the draft — this line
// is what tells the operator the number they see isn't the one they last
// set (code-review finding).
describe('AnchorAdjustBar above-maximum notice', () => {
  it('shows nothing when the committed radius was not above the maximum', () => {
    render(<AnchorAdjustBar {...baseProps} />)
    expect(screen.queryByText(/above the maximum/)).not.toBeInTheDocument()
  })

  it('names the original radius and says it was above the maximum', () => {
    render(<AnchorAdjustBar {...baseProps} aboveMaxOriginalRadiusM={180} />)
    expect(screen.getByText('Was 180 m, above the maximum')).toBeInTheDocument()
  })

  it('follows the imperial unit setting', () => {
    render(<AnchorAdjustBar {...baseProps} isImperial aboveMaxOriginalRadiusM={100} />)
    // 100 m * 3.28084 = 328.084 ft.
    expect(screen.getByText('Was 328 ft, above the maximum')).toBeInTheDocument()
  })
})

// No live boat position (no fix, or the GNSS gate holding one back): the
// warning can't be evaluated, so this replaces it rather than a silently
// skipped check (code-review finding).
// Code-review finding: the +/- buttons only wired pointer events, so
// keyboard activation (Enter/Space on a focused button, which the browser
// turns into a synthetic click with detail 0) did nothing — a real gap on
// the no-WebGL2 path, which has no pointer gesture at all to fall back on.
// The fix adds onClick, gated to detail === 0 so a real pointer/touch press
// (which already stepped via onPointerDown, and still fires its own click
// afterward) doesn't step a second time.
describe('AnchorAdjustBar keyboard activation of +/-', () => {
  it('steps up once on a keyboard-originated click (detail 0)', () => {
    const onStepRadius = vi.fn()
    render(<AnchorAdjustBar {...baseProps} onStepRadius={onStepRadius} />)
    fireEvent.click(screen.getByRole('button', { name: 'Increase radius' }), { detail: 0 })
    expect(onStepRadius).toHaveBeenCalledTimes(1)
    expect(onStepRadius).toHaveBeenCalledWith(1)
  })

  it('steps down once on a keyboard-originated click (detail 0)', () => {
    const onStepRadius = vi.fn()
    render(<AnchorAdjustBar {...baseProps} onStepRadius={onStepRadius} />)
    fireEvent.click(screen.getByRole('button', { name: 'Decrease radius' }), { detail: 0 })
    expect(onStepRadius).toHaveBeenCalledTimes(1)
    expect(onStepRadius).toHaveBeenCalledWith(-1)
  })

  it('does not double-step on a pointer-originated click (detail >= 1) — onPointerDown already stepped', () => {
    const onStepRadius = vi.fn()
    render(<AnchorAdjustBar {...baseProps} onStepRadius={onStepRadius} />)
    fireEvent.click(screen.getByRole('button', { name: 'Increase radius' }), { detail: 1 })
    expect(onStepRadius).not.toHaveBeenCalled()
  })
})

describe('AnchorAdjustBar no-fix notice', () => {
  it('shows nothing by default', () => {
    render(<AnchorAdjustBar {...baseProps} />)
    expect(screen.queryByText(/No GPS fix/)).not.toBeInTheDocument()
  })

  it("shows the no-fix line instead of the warning", () => {
    render(<AnchorAdjustBar {...baseProps} noFixNotice />)
    expect(screen.getByText("No GPS fix: can't check the boat against this circle")).toBeInTheDocument()
    expect(screen.queryByTestId('anchor-adjust-warning')).not.toBeInTheDocument()
  })
})
