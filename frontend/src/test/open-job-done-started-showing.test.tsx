import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { OpenJobDoneDialog } from '@/components/inventory/open-job-dialogs'
import type { OpenJob } from '@/hooks/use-open-jobs'

// ADR 0276: closing an open job as a repair can say when the fault started
// showing. The job's open date is the best first guess, and the operator can
// change or clear it.

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

describe('OpenJobDoneDialog: Started showing', () => {
  beforeEach(() => {
    completeOpenJob.mockReset()
    completeOpenJob.mockResolvedValue({ job, entry: {} })
  })

  it('asks for it on a repair, filled with the day the job was opened, and says what it is for', () => {
    render(<OpenJobDoneDialog job={job} onClose={vi.fn()} />)
    expect(screen.getByLabelText('Started showing')).toHaveValue('2026-11-03')
    expect(screen.getByText(/On an engine, the house bank or what charges it, readings from then until this repair are left out of what counts as normal/)).toBeInTheDocument()
  })

  it('sends the date, or blank once it is cleared', async () => {
    render(<OpenJobDoneDialog job={job} onClose={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Date'), { target: { value: '2026-11-10' } })
    fireEvent.click(screen.getByRole('button', { name: 'Mark done' }))
    await waitFor(() => expect(completeOpenJob).toHaveBeenCalledTimes(1))
    expect(completeOpenJob.mock.calls[0][1]).toMatchObject({ kind: 'repair', started_at: '2026-11-03' })
  })

  it('sends it blank when cleared', async () => {
    render(<OpenJobDoneDialog job={job} onClose={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Started showing'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Mark done' }))
    await waitFor(() => expect(completeOpenJob).toHaveBeenCalledTimes(1))
    expect(completeOpenJob.mock.calls[0][1]).toMatchObject({ started_at: '' })
  })

  it('says so and holds Mark done when the date is earlier than Started showing', () => {
    render(<OpenJobDoneDialog job={job} onClose={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Date'), { target: { value: '2026-11-01' } })
    expect(screen.getByRole('alert')).toHaveTextContent("Started showing can't be after the repair date.")
    expect(screen.getByRole('button', { name: 'Mark done' })).toBeDisabled()
    // Moving either date to make sense again lets it through.
    fireEvent.change(screen.getByLabelText('Started showing'), { target: { value: '2026-10-30' } })
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.getByRole('button', { name: 'Mark done' })).toBeEnabled()
  })
})
