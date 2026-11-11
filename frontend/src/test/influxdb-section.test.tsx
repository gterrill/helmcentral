import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'

import { InfluxdbSection } from '@/components/settings/sections/influxdb-section'
import { initialRegularSettingsDraft } from '@/components/settings/settings-draft'
import { resetQueryStore } from '@/lib/api-query'

// The token group needs the secrets provider and is not what this is about.
vi.mock('@/components/settings/secret-field-group', () => ({ SecretFieldGroup: () => null }))

// ADR 0276: the optional one-minute bucket the learned normal reads.

describe('InfluxdbSection one-minute bucket', () => {
  beforeEach(() => {
    resetQueryStore()
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ configured: false, note: 'No learned normal: InfluxDB is not set up.', subsystems: [] }) })),
    )
  })

  it('has an optional field that says what blank does', () => {
    render(<InfluxdbSection draft={initialRegularSettingsDraft} onChange={() => {}} />)
    const field = screen.getByLabelText('One-minute bucket (optional)')
    expect(field).toHaveValue('')
    expect(
      screen.getByText("Leave blank to roll up the main bucket. Helmcentral learns the usual readings of each engine and the house bank from it."),
    ).toBeInTheDocument()
  })

  it('reports the typed bucket through onChange', () => {
    const onChange = vi.fn()
    render(<InfluxdbSection draft={{ ...initialRegularSettingsDraft, influxdbMinuteBucket: 'a' }} onChange={onChange} />)
    fireEvent.change(screen.getByLabelText('One-minute bucket (optional)'), { target: { value: 'my_1m' } })
    expect(onChange).toHaveBeenCalledWith({ influxdbMinuteBucket: 'my_1m' })
  })

  it('names the learned normal in the switch label and shows its status', async () => {
    render(<InfluxdbSection draft={initialRegularSettingsDraft} onChange={() => {}} />)
    expect(screen.getByText(/to learn the usual readings of each engine and the house bank/)).toBeInTheDocument()
    expect(await screen.findByText('No learned normal: InfluxDB is not set up.')).toBeInTheDocument()
  })
})
