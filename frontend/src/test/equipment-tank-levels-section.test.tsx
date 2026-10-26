import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { EquipmentTankLevelsSection } from '@/components/inventory/equipment-tank-levels-section'
import type { EquipmentInput } from '@/hooks/use-inventory'
import { BLANK_DRAFT } from '@/hooks/use-inventory'

vi.mock('@/hooks/use-tank-levels', () => ({
  useTankLevelDefaults: () => ({
    defaults: { fuel: { warn: 20, alarm: 10 }, water: { warn: 20, alarm: 10 }, waste: { warn: 80, alarm: 90 } },
    error: null,
  }),
}))

function Harness({ source, initial, canWrite = true }: { source: string; initial?: Partial<EquipmentInput>; canWrite?: boolean }) {
  const [draft, setDraft] = useState<EquipmentInput>({ ...BLANK_DRAFT, signalk_source: source, ...initial })
  return (
    <>
      <EquipmentTankLevelsSection draft={draft} setDraft={setDraft} canWrite={canWrite} />
      <pre data-testid="draft">{JSON.stringify({ w: draft.tank_warn_percent, a: draft.tank_alarm_percent, off: draft.tank_alarms_off })}</pre>
    </>
  )
}

const draftOf = () => JSON.parse(screen.getByTestId('draft').textContent ?? '{}')

describe('Tank levels on the equipment page', () => {
  it('shows the kind default as the placeholder when a level is blank', () => {
    render(<Harness source="tanks.fuel.0" />)
    expect(screen.getByLabelText('Warn at')).toHaveAttribute('placeholder', '20')
    expect(screen.getByLabelText('Alarm at')).toHaveAttribute('placeholder', '10')
    expect(screen.getByLabelText('Warn at')).toHaveValue(null)
  })

  it('uses the waste defaults for a holding tank', () => {
    render(<Harness source="tanks.blackWater.1" />)
    expect(screen.getByLabelText('Warn at')).toHaveAttribute('placeholder', '80')
    expect(screen.getByLabelText('Alarm at')).toHaveAttribute('placeholder', '90')
  })

  it('stores a typed level, and blank again means the default (0)', () => {
    render(<Harness source="tanks.fuel.0" />)
    fireEvent.change(screen.getByLabelText('Warn at'), { target: { value: '30' } })
    expect(draftOf().w).toBe(30)
    fireEvent.change(screen.getByLabelText('Warn at'), { target: { value: '' } })
    expect(draftOf().w).toBe(0)
  })

  it('switches the alarms off without touching the levels', () => {
    render(<Harness source="tanks.fuel.0" initial={{ tank_warn_percent: 25 }} />)
    fireEvent.click(screen.getByRole('switch', { name: 'Alarms off' }))
    expect(draftOf()).toEqual({ w: 25, a: 0, off: true })
    expect(screen.getByText(/still shows this tank's level and colours/i)).toBeInTheDocument()
  })

  it('names a backwards pair, counting a blank as the default', () => {
    render(<Harness source="tanks.fuel.0" />)
    fireEvent.change(screen.getByLabelText('Warn at'), { target: { value: '8' } })
    expect(screen.getByRole('alert')).toHaveTextContent(/alarm level must be lower than the warn level/i)
  })

  it('names a level outside 0-100', () => {
    render(<Harness source="tanks.fuel.0" />)
    fireEvent.change(screen.getByLabelText('Alarm at'), { target: { value: '150' } })
    expect(screen.getByRole('alert')).toHaveTextContent(/between 0 and 100/i)
  })

  it('renders nothing for an item that is not a tank', () => {
    render(<Harness source="propulsion.port" />)
    expect(screen.queryByText('Tank levels')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Warn at')).not.toBeInTheDocument()
  })

  it('is read-only without write access', () => {
    render(<Harness source="tanks.fuel.0" canWrite={false} />)
    expect(screen.getByLabelText('Warn at')).toBeDisabled()
    expect(screen.getByRole('switch', { name: 'Alarms off' })).toHaveAttribute('aria-disabled', 'true')
  })
})
