import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { TankLevelsForm } from '@/components/tank-levels-form'
import { TankLevelsProvider, useTankLevelsFormContext } from '@/components/settings/tank-levels-context'

const saveMock = vi.hoisted(() => vi.fn().mockResolvedValue(undefined))

vi.mock('@/hooks/use-tank-levels', () => {
  const defaults = {
    fuel: { warn: 20, alarm: 10 },
    water: { warn: 20, alarm: 10 },
    waste: { warn: 80, alarm: 90 },
  }
  return {
    useTankLevelDefaults: () => ({ defaults, error: null }),
    saveTankLevelDefaults: saveMock,
  }
})

function Probe() {
  const { dirty, save, reset } = useTankLevelsFormContext()
  return (
    <div>
      <span data-testid="dirty">{String(dirty)}</span>
      <button onClick={() => void save().catch(() => undefined)}>save</button>
      <button onClick={reset}>reset</button>
    </div>
  )
}

function renderForm() {
  return render(
    <TankLevelsProvider>
      <TankLevelsForm />
      <Probe />
    </TankLevelsProvider>,
  )
}

describe('Tank levels settings block', () => {
  beforeEach(() => saveMock.mockClear())

  it('shows each kind with the levels the server holds', () => {
    renderForm()
    expect(screen.getByLabelText('Warn at', { selector: '#tank-levels-fuel-warn' })).toHaveValue(20)
    expect(screen.getByLabelText('Alarm at', { selector: '#tank-levels-waste-alarm' })).toHaveValue(90)
    expect(screen.getByText('Raised when the level rises above it.')).toBeInTheDocument()
  })

  it('has no Save button of its own: an edit makes the page dirty and saving writes it once', async () => {
    renderForm()
    expect(screen.queryByRole('button', { name: /^save/i, hidden: false })?.textContent).toBe('save') // the probe's, not the form's
    expect(screen.getByTestId('dirty')).toHaveTextContent('false')
    fireEvent.change(document.querySelector('#tank-levels-fuel-warn') as HTMLInputElement, { target: { value: '25' } })
    expect(screen.getByTestId('dirty')).toHaveTextContent('true')
    fireEvent.click(screen.getByText('save'))
    await waitFor(() => expect(saveMock).toHaveBeenCalledTimes(1))
    expect(saveMock.mock.calls[0][0].fuel).toEqual({ warn: 25, alarm: 10 })
    await waitFor(() => expect(screen.getByTestId('dirty')).toHaveTextContent('false'))
  })

  it('names a backwards pair and refuses to save it', async () => {
    renderForm()
    fireEvent.change(document.querySelector('#tank-levels-fuel-warn') as HTMLInputElement, { target: { value: '5' } })
    expect(screen.getByText(/alarm level must be lower than the warn level/i)).toBeInTheDocument()
    fireEvent.click(screen.getByText('save'))
    await Promise.resolve()
    expect(saveMock).not.toHaveBeenCalled()
  })

  it('puts the draft back on reset', () => {
    renderForm()
    const warn = document.querySelector('#tank-levels-water-warn') as HTMLInputElement
    fireEvent.change(warn, { target: { value: '33' } })
    fireEvent.click(screen.getByText('reset'))
    expect(warn).toHaveValue(20)
    expect(screen.getByTestId('dirty')).toHaveTextContent('false')
  })
})
