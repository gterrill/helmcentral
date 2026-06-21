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
