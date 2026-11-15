import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { HandHoursDialog } from '@/components/inventory/maintenance-hand-hours-dialog'
import { MaintenanceEquipmentBlock } from '@/components/inventory/maintenance-equipment-block'
import { OpenJobDoneDialog } from '@/components/inventory/open-job-dialogs'
import type { OpenJob } from '@/hooks/use-open-jobs'

// The server judges a maintenance date against the boat's calendar day, so the
// dates these forms start on, and the latest one they allow, are the boat's
// day, never the device's. A phone one zone ahead of the boat would otherwise
// offer a date the server refuses. With the boat's day unknown there is no
// default and no cap: the operator types a date, and the server has the last
// word.

let boatDay: string | null = '2026-11-16'
vi.mock('@/hooks/use-vessel-clock', () => ({ useBoatDayKey: () => boatDay }))

const completeOpenJob = vi.fn()
vi.mock('@/hooks/use-inventory', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/hooks/use-inventory')>()
  return { ...actual, useEquipment: () => ({ items: [], loading: false, error: null, refresh: vi.fn() }) }
})
vi.mock('@/hooks/use-open-jobs', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/hooks/use-open-jobs')>()
  return { ...actual, completeOpenJob: (...args: unknown[]) => completeOpenJob(...args) }
})

const job: OpenJob = {
  id: 'job-1', equipment_id: 'eq-1', equipment_name: 'Port engine', title: 'Oil pressure reads low', notes: '',
  opened_on: '2026-11-03', before_on: '', status: 'open', closed_on: '', drop_reason: '', log_entry_id: null,
  source: 'manual', source_link: '', source_label: '', created_at: '', updated_at: '', photo_ids: [],
}

const fetchMock = vi.fn()
function jsonResponse(status: number, body: unknown) {
  return Promise.resolve({ ok: status >= 200 && status < 300, status, json: async () => body } as Response)
}

beforeEach(() => {
  boatDay = '2026-11-16'
  completeOpenJob.mockReset()
  completeOpenJob.mockResolvedValue({ job, entry: {} })
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
  // The device is already on the 17th while the boat is still on the 16th.
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 10, 17, 9, 0, 0))
})

describe('HandHoursDialog on the boat day', () => {
  it('starts on the boat day and allows nothing later', () => {
    render(<HandHoursDialog equipmentName="Watermaker" open onCancel={vi.fn()} onSave={vi.fn()} />)
    const date = screen.getByLabelText('Date read')
    expect(date).toHaveValue('2026-11-16')
    expect(date).toHaveAttribute('max', '2026-11-16')
  })

  it('leaves the date blank, and Save held, until the boat day is known, then fills it', () => {
    boatDay = null
    const { rerender } = render(<HandHoursDialog equipmentName="Watermaker" open onCancel={vi.fn()} onSave={vi.fn()} />)
    const date = screen.getByLabelText('Date read')
    expect(date).toHaveValue('')
    expect(date).not.toHaveAttribute('max')
    fireEvent.change(screen.getByLabelText('Gauge reading (hours)'), { target: { value: '5' } })
    expect(screen.getByRole('button', { name: 'Save reading' })).toBeDisabled()

    boatDay = '2026-11-16'
    rerender(<HandHoursDialog equipmentName="Watermaker" open onCancel={vi.fn()} onSave={vi.fn()} />)
    expect(screen.getByLabelText('Date read')).toHaveValue('2026-11-16')
    expect(screen.getByRole('button', { name: 'Save reading' })).toBeEnabled()
  })

  it('holds Save for a date typed before the boat day was known that turns out to be later', () => {
    boatDay = null
    const { rerender } = render(<HandHoursDialog equipmentName="Watermaker" open onCancel={vi.fn()} onSave={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Gauge reading (hours)'), { target: { value: '5' } })
    fireEvent.change(screen.getByLabelText('Date read'), { target: { value: '2026-11-17' } })
    expect(screen.getByRole('button', { name: 'Save reading' })).toBeEnabled()
    boatDay = '2026-11-16'
    rerender(<HandHoursDialog equipmentName="Watermaker" open onCancel={vi.fn()} onSave={vi.fn()} />)
    expect(screen.getByRole('button', { name: 'Save reading' })).toBeDisabled()
  })

  it('keeps a date the operator chose when the boat day turns', () => {
    const { rerender } = render(<HandHoursDialog equipmentName="Watermaker" open onCancel={vi.fn()} onSave={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Date read'), { target: { value: '2026-11-10' } })
    boatDay = '2026-11-17'
    rerender(<HandHoursDialog equipmentName="Watermaker" open onCancel={vi.fn()} onSave={vi.fn()} />)
    expect(screen.getByLabelText('Date read')).toHaveValue('2026-11-10')
  })

  it('sends the boat day when saved untouched', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    render(<HandHoursDialog equipmentName="Watermaker" open onCancel={vi.fn()} onSave={onSave} />)
    fireEvent.change(screen.getByLabelText('Gauge reading (hours)'), { target: { value: '120' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save reading' }))
    await waitFor(() => expect(onSave).toHaveBeenCalledWith({ read_on: '2026-11-16', gauge: 120, note: '' }))
  })
})

describe('OpenJobDoneDialog on the boat day', () => {
  it('starts the date on the boat day', async () => {
    render(<OpenJobDoneDialog job={job} onClose={vi.fn()} />)
    expect(screen.getByLabelText('Date')).toHaveValue('2026-11-16')
    fireEvent.click(screen.getByRole('button', { name: 'Mark done' }))
    await waitFor(() => expect(completeOpenJob).toHaveBeenCalledTimes(1))
    expect(completeOpenJob.mock.calls[0][1]).toMatchObject({ performed_at: '2026-11-16' })
  })

  it('leaves the date blank and holds Mark done while the boat day is unknown', () => {
    boatDay = null
    render(<OpenJobDoneDialog job={job} onClose={vi.fn()} />)
    expect(screen.getByLabelText('Date')).toHaveValue('')
    expect(screen.getByRole('button', { name: 'Mark done' })).toBeDisabled()
  })
})

describe('MaintenanceEquipmentBlock meter replacement on the boat day', () => {
  function mockSchedule() {
    fetchMock.mockImplementation((url: string) => {
      const u = String(url)
      if (u.includes('/api/equipment-profiles')) return jsonResponse(200, { profiles: [], problems: [] })
      if (u.includes('/maintenance/rules')) return jsonResponse(200, { rules: [], removed: [], schedule_errors: [] })
      if (u.includes('/maintenance/log')) return jsonResponse(200, { entries: [] })
      if (u.includes('/maintenance/meter-resets')) return jsonResponse(200, { resets: [] })
      return jsonResponse(200, {})
    })
  }

  it('starts the replacement date on the boat day', async () => {
    mockSchedule()
    render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterSource="propulsion.main.runTime" />)
    fireEvent.click(await screen.findByRole('button', { name: /Record meter replacement/ }))
    expect(screen.getByLabelText('Date')).toHaveValue('2026-11-16')
  })

  it('leaves it blank and holds Save while the boat day is unknown', async () => {
    boatDay = null
    mockSchedule()
    render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterSource="propulsion.main.runTime" />)
    fireEvent.click(await screen.findByRole('button', { name: /Record meter replacement/ }))
    expect(screen.getByLabelText('Date')).toHaveValue('')
    fireEvent.change(screen.getByLabelText('Old reading'), { target: { value: '100' } })
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })
})
