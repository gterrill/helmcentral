import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import { GeneratorTile } from '@/components/generator-tile'

const baseProps = {
  generatorState: 'running',
  generatorManualStart: true,
  generatorManualStartTimer: 3600, // 1 hour remaining
  generatorRunningByCondition: null,
  generatorRuntime: 600,
  generatorRealPowerW: 3000,
  generatorConfigured: true,
}

test('shows a projected battery percentage at the end of the timer while charging', () => {
  render(
    <GeneratorTile
      {...baseProps}
      batterySocPercent={70}
      batteryRatePercentPerHour={10}
    />,
  )

  expect(screen.getByText('Battery at Finish')).toBeInTheDocument()
  expect(screen.getByText('80%')).toBeInTheDocument()
})

test('clamps the projected battery percentage at 100', () => {
  render(
    <GeneratorTile
      {...baseProps}
      generatorManualStartTimer={3600 * 5}
      batterySocPercent={90}
      batteryRatePercentPerHour={10}
    />,
  )

  expect(screen.getByText('Battery at Finish')).toBeInTheDocument()
  expect(screen.getByText('100%')).toBeInTheDocument()
})

test('omits the forecast when the battery is discharging', () => {
  render(
    <GeneratorTile
      {...baseProps}
      batterySocPercent={70}
      batteryRatePercentPerHour={-2}
    />,
  )

  expect(screen.queryByText('Battery at Finish')).not.toBeInTheDocument()
})

test('omits the forecast when battery data is unavailable', () => {
  render(
    <GeneratorTile
      {...baseProps}
      batterySocPercent={null}
      batteryRatePercentPerHour={null}
    />,
  )

  expect(screen.queryByText('Battery at Finish')).not.toBeInTheDocument()
})

test('omits the forecast when there is no manual-start timer running', () => {
  render(
    <GeneratorTile
      {...baseProps}
      generatorManualStart={false}
      generatorManualStartTimer={0}
      batterySocPercent={70}
      batteryRatePercentPerHour={10}
    />,
  )

  expect(screen.queryByText('Battery at Finish')).not.toBeInTheDocument()
  expect(screen.queryByText('Timer Remaining')).not.toBeInTheDocument()
})

test('treats real power output as running even when generatorState has not caught up (e.g. started manually at the panel)', () => {
  render(
    <GeneratorTile
      {...baseProps}
      generatorState={null}
      generatorRealPowerW={3000}
      batterySocPercent={null}
      batteryRatePercentPerHour={null}
    />,
  )

  expect(screen.getByRole('button', { name: 'Stop' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Start' })).not.toBeInTheDocument()
})

test('does not treat zero output as running when generatorState also says stopped', () => {
  render(
    <GeneratorTile
      {...baseProps}
      generatorState={null}
      generatorRealPowerW={0}
      batterySocPercent={null}
      batteryRatePercentPerHour={null}
    />,
  )

  expect(screen.getByRole('button', { name: 'Start' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Stop' })).not.toBeInTheDocument()
})

// Role-gating (ADR 0040 §frontend): below readwrite, Start/Stop must be
// disabled rather than hidden — cosmetic only, the server is the actual
// enforcement point.
test('disables Start when readOnly', () => {
  render(
    <GeneratorTile
      {...baseProps}
      generatorState={null}
      generatorRealPowerW={0}
      batterySocPercent={null}
      batteryRatePercentPerHour={null}
      readOnly
    />,
  )

  expect(screen.getByRole('button', { name: 'Start' })).toBeDisabled()
})

test('disables Stop when readOnly', () => {
  render(
    <GeneratorTile
      {...baseProps}
      generatorRealPowerW={3000}
      batterySocPercent={null}
      batteryRatePercentPerHour={null}
      readOnly
    />,
  )

  expect(screen.getByRole('button', { name: 'Stop' })).toBeDisabled()
})

test('Start is enabled when not readOnly', () => {
  render(
    <GeneratorTile
      {...baseProps}
      generatorState={null}
      generatorRealPowerW={0}
      batterySocPercent={null}
      batteryRatePercentPerHour={null}
    />,
  )

  expect(screen.getByRole('button', { name: 'Start' })).not.toBeDisabled()
})

test('says the generator is not set up, and offers no Start or Stop, when none is chosen', () => {
  render(
    <GeneratorTile
      {...baseProps}
      generatorConfigured={false}
      generatorState={null}
      generatorRealPowerW={null}
      batterySocPercent={70}
      batteryRatePercentPerHour={10}
    />,
  )

  expect(screen.getByText(/not set up/i)).toBeInTheDocument()
  expect(screen.getByText(/Settings/)).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Start' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Stop' })).not.toBeInTheDocument()
})

test('shows neither the not-set-up message nor Start and Stop while it is not yet known whether a generator is set up', () => {
  render(<GeneratorTile {...baseProps} generatorConfigured={null} batterySocPercent={null} batteryRatePercentPerHour={null} />)

  expect(screen.queryByText(/not set up/i)).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Start' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Stop' })).not.toBeInTheDocument()
})

test('shows -- for power when the generator AC input reports nothing, not 0 W', () => {
  render(<GeneratorTile {...baseProps} generatorState="stopped" generatorRealPowerW={null} batterySocPercent={null} batteryRatePercentPerHour={null} />)

  const power = screen.getByText('Power').nextElementSibling as HTMLElement
  expect(power).toHaveTextContent('--')
  expect(power).not.toHaveTextContent('0')
})

test('shows 0 W for a measured zero', () => {
  render(<GeneratorTile {...baseProps} generatorState="stopped" generatorRealPowerW={0} batterySocPercent={null} batteryRatePercentPerHour={null} />)

  expect(screen.getByText('Power').nextElementSibling).toHaveTextContent('0W')
})
