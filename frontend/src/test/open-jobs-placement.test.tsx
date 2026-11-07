import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MaintenanceSection } from '@/components/inventory/maintenance-section'
import { MaintenanceEquipmentBlock } from '@/components/inventory/maintenance-equipment-block'
import { PROPOSAL_REFRESH } from '@/lib/assistant-proposal-actions'
import { stubApi } from '@/test/helpers/stub-api'
import { makeProfileJob } from './maintenance-fixtures'

// ADR 0269: where the Open jobs group sits. At the top of Inventory,
// Maintenance, above Overdue, and on each item's own page.

afterEach(() => { vi.unstubAllGlobals() })

const JOB = {
  id: 'job-1', equipment_id: 'eq-1', equipment_name: 'Main engine', title: 'Weeping hose clamp', notes: '',
  opened_on: '2026-11-01', before_on: '', status: 'open', closed_on: '', drop_reason: '', log_entry_id: null,
  source: 'manual', source_link: '', source_label: '', created_at: '', updated_at: '', photo_ids: [],
}

describe('Open jobs placement', () => {
  it('sits above Overdue on the Maintenance list', async () => {
    stubApi({
      'GET /api/inventory/open-jobs': { jobs: [JOB] },
      'GET /api/inventory/maintenance/rules': { rules: [makeProfileJob({ description: 'Oil change', status: 'overdue' })], removed: [], schedule_errors: [] },
    })
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    const job = await screen.findByText('Weeping hose clamp')
    const overdue = await screen.findByText('Oil change')
    expect(job.compareDocumentPosition(overdue) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.getByRole('heading', { name: /open jobs/i })).toBeInTheDocument()
  })

  it('is still there when no rule exists yet', async () => {
    stubApi({
      'GET /api/inventory/open-jobs': { jobs: [JOB] },
      'GET /api/inventory/maintenance/rules': { rules: [], removed: [], schedule_errors: [] },
    })
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    expect(await screen.findByText('Weeping hose clamp')).toBeInTheDocument()
    expect(await screen.findByText(/no maintenance rules yet/i)).toBeInTheDocument()
  })

  it('shows the items own open jobs on its page', async () => {
    const fetchMock = stubApi({
      'GET /api/inventory/open-jobs': { jobs: [JOB] },
      'GET /api/inventory/maintenance/rules': { rules: [], removed: [], schedule_errors: [] },
      'GET /api/inventory/maintenance/log': { entries: [] },
      'GET /api/equipment-profiles': { profiles: [], problems: [] },
    })
    render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterSource="" />)
    expect(await screen.findByText('Weeping hose clamp')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([u]) => String(u).includes('/api/inventory/open-jobs?equipment=eq-1'))).toBe(true)
    // On the item's own page the item's name is not repeated on each row.
    expect(screen.queryByRole('button', { name: 'Main engine' })).not.toBeInTheDocument()
  })

  it('refreshes the list after Mate adds a job', () => {
    expect(PROPOSAL_REFRESH.open_job).toBeTypeOf('function')
  })
})
