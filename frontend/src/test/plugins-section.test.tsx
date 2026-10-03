import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'

import { PluginsSection } from '@/components/settings/sections/plugins-section'
import { SettingsFormProvider } from '@/components/settings/settings-form-context'
import { SecretsStatusProvider } from '@/components/settings/secrets-status-context'
import { initialRegularSettingsDraft } from '@/components/settings/settings-draft'
import { useForecastWarningsProviders } from '@/hooks/use-forecast-warnings-providers'
import { usePlaceNameProviders } from '@/hooks/use-place-name-providers'
import { usePOIProviders } from '@/hooks/use-poi-providers'
import { useTideProviders } from '@/hooks/use-tide-providers'
import { useWaveProviders } from '@/hooks/use-wave-providers'
import { useWeatherProviders } from '@/hooks/use-weather-providers'

vi.mock('@/hooks/use-tide-providers')
vi.mock('@/hooks/use-weather-providers')
vi.mock('@/hooks/use-wave-providers')
vi.mock('@/hooks/use-poi-providers')
vi.mock('@/hooks/use-forecast-warnings-providers')
vi.mock('@/hooks/use-place-name-providers')

const mockedUseTideProviders = vi.mocked(useTideProviders)
const mockedUseWeatherProviders = vi.mocked(useWeatherProviders)
const mockedUseWaveProviders = vi.mocked(useWaveProviders)
const mockedUsePOIProviders = vi.mocked(usePOIProviders)
const mockedUseForecastWarningsProviders = vi.mocked(useForecastWarningsProviders)
const mockedUsePlaceNameProviders = vi.mocked(usePlaceNameProviders)

// Each plugin kind is its own headed section; this returns the card that
// carries the given h2.
function sectionNamed(name: string): HTMLElement {
  const heading = screen.getByRole('heading', { level: 2, name })
  const card = heading.closest('[data-slot="card"]')
  if (!(card instanceof HTMLElement)) throw new Error(`no section card for ${name}`)
  return card
}

function renderSection() {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => ({ ok: true, json: async () => ({ ui: {} }) })),
  )
  render(
    <SettingsFormProvider>
      <SecretsStatusProvider>
        <PluginsSection draft={initialRegularSettingsDraft} onChange={() => {}} />
      </SecretsStatusProvider>
    </SettingsFormProvider>,
  )
}

function mockNoProviders() {
  mockedUseTideProviders.mockReturnValue({ providers: [], loading: false, error: null })
  mockedUseWeatherProviders.mockReturnValue({ providers: [], loading: false, error: null })
  mockedUseWaveProviders.mockReturnValue({ providers: [], loading: false, error: null })
  mockedUseForecastWarningsProviders.mockReturnValue({ providers: [], loading: false, error: null })
  mockedUsePOIProviders.mockReturnValue({ providers: [], loading: false, error: null })
  mockedUsePlaceNameProviders.mockReturnValue({ providers: [], loading: false, error: null })
}

describe('PluginsSection layout', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('shows every plugin kind as its own section, in the fixed order, with no tabs', () => {
    mockNoProviders()
    renderSection()

    expect(screen.getByRole('heading', { level: 1, name: 'Plugins' })).toBeTruthy()
    expect(screen.queryByRole('tablist')).toBeNull()
    expect(screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent)).toEqual([
      'Weather',
      'Wave',
      'Tide',
      'Forecast Warnings',
      'Place names',
      'Nearby',
    ])
  })

  it('says what each plugin kind affects', () => {
    mockNoProviders()
    renderSection()

    expect(within(sectionNamed('Weather')).getByText(/forecast/i)).toBeTruthy()
    expect(within(sectionNamed('Tide')).getByText(/rode planner/i)).toBeTruthy()
    expect(within(sectionNamed('Nearby')).getByText(/nearby tile/i)).toBeTruthy()
  })

  it('keeps the tide station fields inside the Tide section', () => {
    mockNoProviders()
    renderSection()

    expect(within(sectionNamed('Tide')).getByLabelText('Tide station id')).toBeTruthy()
  })

  it('shows a failed provider list as a failure, not as nothing installed', () => {
    mockNoProviders()
    mockedUseWeatherProviders.mockReturnValue({ providers: [], loading: false, error: 'HTTP 500' })
    renderSection()

    const weather = sectionNamed('Weather')
    expect(within(weather).getByText(/could not load weather plugins: HTTP 500/i)).toBeTruthy()
    expect(within(weather).queryByText(/no weather plugin installed/i)).toBeNull()
  })

  it('does not claim a place-names plugin is missing, since place names come from a points-of-interest plugin', () => {
    mockNoProviders()
    renderSection()

    expect(within(sectionNamed('Place names')).getByText(/no installed plugin can look up place names/i)).toBeTruthy()
  })

  it('says so when no plugin of a kind is installed, instead of an empty section', () => {
    mockNoProviders()
    renderSection()

    expect(within(sectionNamed('Tide')).getByText(/no tide plugin installed/i)).toBeTruthy()
  })
})

// The Nearby section (ui.poi_provider) added alongside the poi plugin kind
// (docs/adr/0091) - this guards the section existing and rendering the
// registered POI providers.
describe('PluginsSection Nearby', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('lists the registered POI providers under the Nearby section', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: true, json: async () => ({ ui: {} }) })),
    )
    mockedUseTideProviders.mockReturnValue({ providers: [], loading: false, error: null })
    mockedUseWeatherProviders.mockReturnValue({ providers: [], loading: false, error: null })
    mockedUseWaveProviders.mockReturnValue({ providers: [], loading: false, error: null })
    mockedUseForecastWarningsProviders.mockReturnValue({ providers: [], loading: false, error: null })
    mockedUsePOIProviders.mockReturnValue({
      providers: [
        { id: 'osm-overpass', name: 'OpenStreetMap (Overpass)', description: 'Free, keyless POI data' },
        { id: 'google-places', name: 'Google Places', description: "Google's Places API (New)" },
      ],
      loading: false,
      error: null,
    })
    mockedUsePlaceNameProviders.mockReturnValue({ providers: [], loading: false, error: null })

    render(
      <SettingsFormProvider>
        <SecretsStatusProvider>
          <PluginsSection draft={initialRegularSettingsDraft} onChange={() => {}} />
        </SecretsStatusProvider>
      </SettingsFormProvider>,
    )

    const nearby = sectionNamed('Nearby')
    await waitFor(() => {
      expect(within(nearby).getByText('OpenStreetMap (Overpass)')).toBeTruthy()
    })
    expect(within(nearby).getByText('Google Places')).toBeTruthy()
  })
})

// ui.place_name_provider (ADR 0101) - a separate section from Nearby, since
// the operator can point the two at different plugins even though both
// default to osm-overpass.
describe('PluginsSection Place names', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('lists the registered place-name providers under the Place names section and saves a selection', async () => {
    const saved: unknown[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString()
        if (url.endsWith('/api/settings') && init?.method === 'POST') {
          saved.push(JSON.parse(String(init.body)))
        }
        return { ok: true, json: async () => ({ ui: {} }) }
      }),
    )
    mockedUseTideProviders.mockReturnValue({ providers: [], loading: false, error: null })
    mockedUseWeatherProviders.mockReturnValue({ providers: [], loading: false, error: null })
    mockedUseWaveProviders.mockReturnValue({ providers: [], loading: false, error: null })
    mockedUseForecastWarningsProviders.mockReturnValue({ providers: [], loading: false, error: null })
    mockedUsePOIProviders.mockReturnValue({ providers: [], loading: false, error: null })
    mockedUsePlaceNameProviders.mockReturnValue({
      providers: [
        { id: 'osm-overpass', name: 'OpenStreetMap (Overpass)', description: 'Free, keyless place names' },
        { id: 'another-osm', name: 'Another OSM Mirror', description: 'A second place-names plugin' },
      ],
      loading: false,
      error: null,
    })

    render(
      <SettingsFormProvider>
        <SecretsStatusProvider>
          <PluginsSection draft={initialRegularSettingsDraft} onChange={() => {}} />
        </SecretsStatusProvider>
      </SettingsFormProvider>,
    )

    const placeNames = sectionNamed('Place names')
    await waitFor(() => {
      expect(within(placeNames).getByText('OpenStreetMap (Overpass)')).toBeTruthy()
    })
    expect(within(placeNames).getByText('Another OSM Mirror')).toBeTruthy()

    // osm-overpass is already the default-active card (unconfigured), so
    // activate the other one to prove a selection actually saves
    // ui.place_name_provider.
    fireEvent.click(screen.getByRole('switch', { name: /activate another osm mirror/i }))

    await waitFor(() => {
      expect(
        saved.some((body) => (body as { ui?: { place_name_provider?: string } }).ui?.place_name_provider === 'another-osm'),
      ).toBe(true)
    })
  })
})
