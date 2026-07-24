import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { ProviderGroup } from '@/components/settings/provider-group'
import { SettingsFormProvider } from '@/components/settings/settings-form-context'
import { SecretsStatusProvider } from '@/components/settings/secrets-status-context'

// Guards against a regression of the bug where a provider select/card
// silently pre-selected "Storm Glass" (a paid, keyless-unfriendly
// provider) before settings ever loaded, and would then get written back
// to settings.yaml on the next unrelated save even though nothing was
// actually configured. Unlike weather/wave/forecast-warnings (which do
// have an "active if unset" default), tide must show NO provider active
// until either the server reports one configured, or the operator picks
// one themselves.
describe('ProviderGroup tide provider default', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows no tide provider active when none is configured', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: true, json: async () => ({ ui: {} }) })),
    )

    render(
      <SettingsFormProvider>
        <SecretsStatusProvider>
          <ProviderGroup
            type="tide"
            providers={[
              { id: 'bom', name: 'Bureau of Meteorology', description: 'Australian tide data' },
              { id: 'stormglass', name: 'Storm Glass', description: 'Paid worldwide tide data' },
            ]}
          />
        </SecretsStatusProvider>
      </SettingsFormProvider>,
    )

    await waitFor(() => {
      expect(screen.getByText('Bureau of Meteorology')).toBeTruthy()
    })

    expect(screen.queryByText('Active')).toBeNull()
    expect(screen.getAllByText('Inactive')).toHaveLength(2)
  })

  it('defaults weather to open-meteo active when unconfigured', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: true, json: async () => ({ ui: {} }) })),
    )

    render(
      <SettingsFormProvider>
        <SecretsStatusProvider>
          <ProviderGroup
            type="weather"
            providers={[
              { id: 'open-meteo', name: 'Open-Meteo', description: 'Free worldwide weather data' },
              { id: 'weatherkit', name: 'WeatherKit', description: "Apple's weather API" },
            ]}
          />
        </SecretsStatusProvider>
      </SettingsFormProvider>,
    )

    await waitFor(() => {
      expect(screen.getByText('Active')).toBeTruthy()
    })

    const openMeteoSwitch = screen.getByRole('switch', { name: /activate open-meteo/i })
    expect(openMeteoSwitch).toHaveAttribute('aria-disabled', 'true')
  })
})
