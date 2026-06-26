import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { SatChartsDrawer } from '@/components/sat-charts-drawer'
import type { SatChart } from '@/hooks/use-sat-charts'

const chartA: SatChart = {
  id: 'a',
  name: 'Reef A',
  bounds: [150, -25, 151, -24],
  minzoom: 10,
  maxzoom: 18,
  format: 'png',
  size_bytes: 2_500_000,
}

function renderDrawer(overrides: Partial<Parameters<typeof SatChartsDrawer>[0]> = {}) {
  return render(
    <SatChartsDrawer
      charts={[]}
      loading={false}
      error={null}
      uploadChart={vi.fn()}
      deleteChart={vi.fn()}
      {...overrides}
    />,
  )
}

describe('SatChartsDrawer', () => {
  it('shows an empty-state message when no charts are uploaded', () => {
    renderDrawer()
    expect(screen.getByText(/No satellite charts uploaded yet/)).toBeInTheDocument()
  })

  it('renders one list item per chart with name and bounds/zoom/size summary', () => {
    renderDrawer({ charts: [chartA] })
    expect(screen.getByText('Reef A')).toBeInTheDocument()
    expect(screen.getByText(/zoom 10–18/)).toBeInTheDocument()
    expect(screen.getByText(/2\.4 MB/)).toBeInTheDocument()
  })

  it('shows the loading message while charts are loading', () => {
    renderDrawer({ loading: true })
    expect(screen.getByText(/Loading charts/)).toBeInTheDocument()
  })

  it('shows the hook error message when present', () => {
    renderDrawer({ error: 'failed to fetch' })
    expect(screen.getByText('failed to fetch')).toBeInTheDocument()
  })

  it('selecting a file calls uploadChart with that file', async () => {
    const uploadChart = vi.fn().mockResolvedValue({ ...chartA })
    renderDrawer({ uploadChart })

    const file = new File(['fake-mbtiles-bytes'], 'reef.mbtiles')
    const input = screen.getByTestId('sat-chart-file-input') as HTMLInputElement
    await fireEvent.change(input, { target: { files: [file] } })

    expect(uploadChart).toHaveBeenCalledWith(file)
  })

  it('shows an error message when uploadChart resolves to null', async () => {
    const uploadChart = vi.fn().mockResolvedValue(null)
    renderDrawer({ uploadChart })

    const file = new File(['not-mbtiles'], 'bad.mbtiles')
    const input = screen.getByTestId('sat-chart-file-input') as HTMLInputElement
    await fireEvent.change(input, { target: { files: [file] } })

    expect(await screen.findByText(/Upload failed/)).toBeInTheDocument()
  })

  it('clicking delete calls deleteChart for the right chart', () => {
    const deleteChart = vi.fn()
    renderDrawer({ charts: [chartA], deleteChart })

    fireEvent.click(screen.getByRole('button', { name: 'Delete Reef A' }))

    expect(deleteChart).toHaveBeenCalledWith('a')
  })
})
