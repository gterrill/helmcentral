import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { SignalKSettingsPanel } from '@/components/signalk-settings-panel'

// Guards against a regression of the bug where the tide-provider select
// silently pre-filled with "Storm Glass" (a paid, keyless-unfriendly
// provider) before settings ever loaded, and would then get written back to
// settings.yaml on the next unrelated save even though nothing was actually
// configured. The select must show no provider selected until either the
// server reports one configured, or the operator picks one themselves.
describe('SignalKSettingsPanel tide provider default', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (url.includes('/api/tide-providers')) {
          return { ok: true, json: async () => [{ id: 'bom', name: 'Bureau of Meteorology' }, { id: 'stormglass', name: 'Storm Glass' }] }
        }
        if (url.includes('/api/weather-providers')) {
          return { ok: true, json: async () => [{ id: 'open-meteo', name: 'Open-Meteo' }] }
        }
        if (url.includes('/api/wave-providers')) {
          return { ok: true, json: async () => [{ id: 'open-meteo-marine', name: 'Open-Meteo Marine' }] }
        }
        if (url.includes('/api/settings/signalk')) {
          return { ok: true, json: async () => ({ address: 'localhost', port: 3000 }) }
        }
        if (url.includes('/api/settings')) {
          // No tide_provider configured at all - the exact scenario the bug
          // mishandled.
          return { ok: true, json: async () => ({ ui: {} }) }
        }
        return { ok: false, json: async () => ({}) }
      }),
    )
  })

  it('shows no tide provider pre-selected when none is configured', async () => {
    render(<SignalKSettingsPanel />)

    await waitFor(() => {
      expect(screen.getByLabelText('Tide provider')).toBeTruthy()
    })

    expect(screen.queryByText('Storm Glass')).toBeNull()
  })
})
