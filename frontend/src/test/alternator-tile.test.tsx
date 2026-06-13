import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import { AlternatorTile } from '@/components/alternator-tile'

const mockPort = {
  currentA: 50.5,
  voltageV: 13.8,
  powerW: 696.9,
  temperatureC: 85,
}

const mockStarboard = {
  currentA: 48.2,
  voltageV: 14.1,
  powerW: 679.6,
  temperatureC: 92,
}

const emptyData = {
  currentA: null,
  voltageV: null,
  powerW: null,
  temperatureC: null,
}

test('renders "Engines not running" when enginesRunning is false', () => {
  render(<AlternatorTile port={emptyData} starboard={emptyData} enginesRunning={false} />)
  expect(screen.getByText('Engines not running')).toBeInTheDocument()
  expect(screen.queryByText('Port')).not.toBeInTheDocument()
  expect(screen.queryByText('Starboard')).not.toBeInTheDocument()
})

test('renders port and starboard data when enginesRunning is true', () => {
  render(<AlternatorTile port={mockPort} starboard={mockStarboard} enginesRunning={true} />)
  expect(screen.getByText('Port')).toBeInTheDocument()
  expect(screen.getByText('Starboard')).toBeInTheDocument()
  
  // Port checks
  expect(screen.getByText('50.5')).toBeInTheDocument() // Current
  expect(screen.getByText('13.8')).toBeInTheDocument() // Voltage
  expect(screen.getByText('697')).toBeInTheDocument()  // Power rounded
  expect(screen.getByText('85°')).toBeInTheDocument() // Temp

  // Starboard checks
  expect(screen.getByText('48.2')).toBeInTheDocument()
  expect(screen.getByText('14.1')).toBeInTheDocument()
  expect(screen.getByText('680')).toBeInTheDocument() // Power rounded
  expect(screen.getByText('92°')).toBeInTheDocument()
})

test('renders empty states when enginesRunning is true but data is null', () => {
  render(<AlternatorTile port={emptyData} starboard={emptyData} enginesRunning={true} />)
  expect(screen.getByText('Port')).toBeInTheDocument()
  
  // Power defaults to '—'
  const dashes = screen.getAllByText('—')
  expect(dashes.length).toBeGreaterThan(0)
  
  // Temperature section should not be rendered if null
  expect(screen.queryByText('Temp')).not.toBeInTheDocument()
})
