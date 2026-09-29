import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { AnchorAdjustActions, type AnchorAdjustActionsProps } from '@/components/anchor-adjust-actions'

// ADR 0143: Cancel/Set renamed to Escape/Save, and the whole bottom-bar
// wrapper is gone — this is just the safety notices plus the two buttons,
// reused as a map overlay and, unstyled the same way, inline in the
// no-WebGL2 fallback.

const baseProps: AnchorAdjustActionsProps = {
  isImperial: false,
  atMin: false,
  atMax: false,
  radiusM: 20,
  maxDisabledReason: 'Chain onboard + boat length',
  warningActive: false,
  confirmingSet: false,
  committing: false,
  onCancel: () => undefined,
  onSave: () => undefined,
}

describe('AnchorAdjustActions buttons', () => {
  it('renders Cancel and Save', () => {
    render(<AnchorAdjustActions {...baseProps} />)
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument()
  })

  it('Cancel calls onCancel', () => {
    const onCancel = vi.fn()
    render(<AnchorAdjustActions {...baseProps} onCancel={onCancel} />)
    screen.getByRole('button', { name: 'Cancel' }).click()
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('Save calls onSave', () => {
    const onSave = vi.fn()
    render(<AnchorAdjustActions {...baseProps} onSave={onSave} />)
    screen.getByRole('button', { name: 'Save' }).click()
    expect(onSave).toHaveBeenCalledTimes(1)
  })

  it('Save becomes "Save anyway" and disables nothing while the warning is armed for a second tap', () => {
    render(<AnchorAdjustActions {...baseProps} warningActive confirmingSet />)
    expect(screen.getByRole('button', { name: 'Save anyway' })).toBeInTheDocument()
  })

  it('Save is disabled while committing', () => {
    render(<AnchorAdjustActions {...baseProps} committing />)
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })
})

describe('AnchorAdjustActions disabled-bound reasons', () => {
  it('shows no reasons line when nothing is disabled', () => {
    render(<AnchorAdjustActions {...baseProps} />)
    expect(screen.queryByTestId('anchor-adjust-reasons')).toBeNull()
  })

  it('names the minimum when at the floor', () => {
    render(<AnchorAdjustActions {...baseProps} radiusM={5} atMin />)
    expect(screen.getByTestId('anchor-adjust-reasons')).toHaveTextContent('Minimum 5 m')
  })

  it('names the ceiling reason when at the maximum', () => {
    render(<AnchorAdjustActions {...baseProps} atMax maxDisabledReason="Chain onboard, without boat length" />)
    expect(screen.getByTestId('anchor-adjust-reasons')).toHaveTextContent('Maximum: Chain onboard, without boat length')
  })
})

describe('AnchorAdjustActions above-maximum notice', () => {
  it('shows nothing when the committed radius was not above the maximum', () => {
    render(<AnchorAdjustActions {...baseProps} />)
    expect(screen.queryByText(/above the maximum/)).not.toBeInTheDocument()
  })

  it('names the original radius and says it was above the maximum', () => {
    render(<AnchorAdjustActions {...baseProps} aboveMaxOriginalRadiusM={180} />)
    expect(screen.getByText('Was 180 m, above the maximum')).toBeInTheDocument()
  })

  it('follows the imperial unit setting', () => {
    render(<AnchorAdjustActions {...baseProps} isImperial aboveMaxOriginalRadiusM={100} />)
    expect(screen.getByText('Was 328 ft, above the maximum')).toBeInTheDocument()
  })
})

describe('AnchorAdjustActions warning and no-fix notice', () => {
  it('shows the warning banner', () => {
    render(<AnchorAdjustActions {...baseProps} warningActive />)
    expect(screen.getByTestId('anchor-adjust-warning')).toHaveTextContent('Alarm would sound now')
  })

  it('shows the no-fix line instead of the warning', () => {
    render(<AnchorAdjustActions {...baseProps} noFixNotice />)
    expect(screen.getByText("No GPS fix: can't check the boat against this circle")).toBeInTheDocument()
    expect(screen.queryByTestId('anchor-adjust-warning')).not.toBeInTheDocument()
  })

  it('shows the no-map notice', () => {
    render(<AnchorAdjustActions {...baseProps} noMapNotice />)
    expect(screen.getByText('Moving the anchor needs the map')).toBeInTheDocument()
  })
})

describe('AnchorAdjustActions variants', () => {
  it('the overlay variant carries a dark scrim wrapper', () => {
    render(<AnchorAdjustActions {...baseProps} />)
    expect(screen.getByTestId('anchor-adjust-actions').className).toMatch(/bg-black\/80/)
  })

  it('the panel variant carries no dark scrim wrapper', () => {
    render(<AnchorAdjustActions {...baseProps} variant="panel" />)
    expect(screen.getByTestId('anchor-adjust-actions').className).not.toMatch(/bg-black/)
  })
})
