import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ZoneConflicts } from '@/components/zone-conflicts'
import type { TileZonePlan } from '@/lib/equipment-tiles'

const plan = {
  moves: [],
  stays: [],
  conflicts: [
    { index: 0, label: 'Coolant', profileText: 'amber above 90', tileText: 'amber above 85' },
    { index: 1, label: 'Oil', profileText: 'red below 1', tileText: 'red below 2' },
  ],
} as unknown as TileZonePlan

describe('ZoneConflicts', () => {
  it('names each conflict group and leaves both radios unchecked until a choice is made', () => {
    render(<ZoneConflicts plan={plan} choices={{}} onChoose={vi.fn()} heldWithoutItem={[]} />)
    expect(screen.getAllByTestId('zone-conflict')).toHaveLength(2)
    const group = screen.getByRole('group', { name: 'Coolant: two sets of zones' })
    expect(group).toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'Oil: two sets of zones' })).toBeInTheDocument()
    for (const radio of screen.getAllByRole('radio')) expect(radio).not.toBeChecked()
  })

  it('reports the choice with the conflict index', () => {
    const onChoose = vi.fn()
    render(<ZoneConflicts plan={plan} choices={{}} onChoose={onChoose} heldWithoutItem={[]} />)
    fireEvent.click(screen.getAllByRole('radio', { name: /Use this tile's, replacing the profile's/ })[1])
    expect(onChoose).toHaveBeenCalledWith(1, 'tile')
    fireEvent.click(screen.getAllByRole('radio', { name: /Keep the profile's/ })[0])
    expect(onChoose).toHaveBeenCalledWith(0, 'profile')
  })

  it('keeps two conflicts in separate radio groups', () => {
    const { rerender } = render(<ZoneConflicts plan={plan} choices={{ 0: 'profile' }} onChoose={vi.fn()} heldWithoutItem={[]} />)
    const radios = screen.getAllByRole('radio') as HTMLInputElement[]
    expect(radios[0].name).not.toBe(radios[2].name)
    rerender(<ZoneConflicts plan={plan} choices={{ 0: 'profile', 1: 'tile' }} onChoose={vi.fn()} heldWithoutItem={[]} />)
    const after = screen.getAllByRole('radio')
    expect(after[0]).toBeChecked()
    expect(after[3]).toBeChecked()
    expect(after[1]).not.toBeChecked()
    expect(after[2]).not.toBeChecked()
  })
})
