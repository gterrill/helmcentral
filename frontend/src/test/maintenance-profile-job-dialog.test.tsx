import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MaintenanceRuleDialog } from '@/components/inventory/maintenance-rule-dialog'
import { makeProfileJob } from './maintenance-fixtures'

// ADR 0148: a profile job is edited through overrides. Each value shows the
// profile's own figure, an edit becomes an override, an overridden value can
// be reset, and there is no Delete.

const fetchMock = vi.fn()
function jsonResponse(status: number, body: unknown) {
  return Promise.resolve({ ok: status >= 200 && status < 300, status, json: async () => body } as Response)
}

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})

function renderDialog(rule = makeProfileJob(), extra: { onCancel?: () => void; onRuleChanged?: () => void } = {}) {
  fetchMock.mockImplementation((url: string) => {
    if (String(url).includes('/api/inventory/equipment')) return jsonResponse(200, { items: [] })
    return jsonResponse(200, { rule })
  })
  return render(
    <MaintenanceRuleDialog
      rule={rule}
      open
      onCancel={extra.onCancel ?? (() => {})}
      onSave={async () => rule}
      onDelete={async () => {}}
      onRuleChanged={extra.onRuleChanged}
    />,
  )
}

function overrideCall() {
  return fetchMock.mock.calls.find(([u, init]) => String(u).includes('/overrides') && init?.method === 'PUT')
}

describe('MaintenanceRuleDialog for a profile job', () => {
  it('shows the profile value under each field and has no Delete', async () => {
    renderDialog()
    expect(await screen.findByRole('heading', { name: 'Edit profile job' })).toBeInTheDocument()
    expect(screen.getByText('Profile: Engine oil and filter')).toBeInTheDocument()
    expect(screen.getByText('Profile: 250 h')).toBeInTheDocument()
    expect(screen.getByText('Profile: 12 months')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^delete$/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /reset to profile/i })).not.toBeInTheDocument()
  })

  it('sends only the edited field as an override', async () => {
    const onCancel = vi.fn()
    const onRuleChanged = vi.fn()
    renderDialog(makeProfileJob(), { onCancel, onRuleChanged })

    fireEvent.change(await screen.findByLabelText('Every (hours)'), { target: { value: '100' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(overrideCall()).toBeDefined())
    expect(JSON.parse(overrideCall()![1].body)).toEqual({ interval_hours: 100 })
    await waitFor(() => expect(onCancel).toHaveBeenCalled())
    expect(onRuleChanged).toHaveBeenCalled()
  })

  it('clearing an interval overrides it to none (null)', async () => {
    renderDialog()
    fireEvent.change(await screen.findByLabelText('Every (months)'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(overrideCall()).toBeDefined())
    expect(JSON.parse(overrideCall()![1].body)).toEqual({ interval_months: null })
  })

  it('offers Reset to profile on an overridden field and calls the reset endpoint', async () => {
    const rule = makeProfileJob({ interval_hours: 100, overridden_fields: ['interval_hours'] })
    renderDialog(rule)

    fireEvent.click(await screen.findByRole('button', { name: 'Reset hours interval to profile' }))

    await waitFor(() => expect(fetchMock.mock.calls.some(([u, init]) =>
      String(u).includes('/overrides/interval_hours') && init?.method === 'DELETE')).toBe(true))
    // Only the one overridden field offers a reset.
    expect(screen.getAllByRole('button', { name: /to profile$/ })).toHaveLength(1)
  })

  it('marks the job not applicable to this item', async () => {
    renderDialog()
    fireEvent.click(await screen.findByRole('switch', { name: 'Not applicable to this item' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(overrideCall()).toBeDefined())
    expect(JSON.parse(overrideCall()![1].body)).toEqual({ not_applicable: true })
  })

  it('edits due-soon and the fixed date through the same endpoint, null to clear', async () => {
    renderDialog(makeProfileJob({ due_soon_hours: 25 }))
    fireEvent.change(await screen.findByLabelText('Due soon within (hours)'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Or a fixed due date'), { target: { value: '2027-01-31' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(overrideCall()).toBeDefined())
    expect(JSON.parse(overrideCall()![1].body)).toEqual({ due_soon_hours: null, fixed_due_date: '2027-01-31' })
  })
})
