import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { OfflineCoastlineSection } from '@/components/settings/sections/offline-coastline-section'
import {
  buildRegularSettingsPatch,
  draftsEqual,
  hydrateDraftFromSettings,
  initialRegularSettingsDraft,
  type RegularSettingsDraft,
} from '@/components/settings/settings-draft'
import { SETTINGS_SECTIONS } from '@/components/settings/settings-nav'
import { SETTINGS_HELP_TARGETS } from '@/lib/help-links'
import { parseAppLocation } from '@/lib/app-location'
import type { OfflineCoastlineStatus } from '@/lib/offline-coastline'

const hook = vi.hoisted(() => ({
  status: undefined as unknown,
  loading: false,
  error: null as string | null,
  update: vi.fn(),
  clearCoverage: vi.fn(),
}))

vi.mock('@/hooks/use-offline-coastline', () => ({
  useOfflineCoastline: () => hook,
}))

// The map is MapLibre; its own file is not under test here.
vi.mock('@/components/offline-coastline-map', () => ({
  OfflineCoastlineMap: ({ status }: { status: OfflineCoastlineStatus }) => (
    <div data-testid="map-stub">{status.squares.length} squares</div>
  ),
}))

const NOON = (iso: string) => `${iso}T12:00:00Z`

function status(overrides: Partial<OfflineCoastlineStatus> = {}): OfflineCoastlineStatus {
  return {
    enabled: true,
    state: 'idle',
    has_fix: true,
    position: { lat: -20.1, lon: 148.9 },
    radius_nm: 200,
    squares: [],
    held: 0,
    wanted: 0,
    wanted_held: 0,
    failed: 0,
    edits_since: 0,
    squares_with_edits: 0,
    updating: 0,
    ...overrides,
  }
}

function renderSection(overrides: Partial<RegularSettingsDraft> = {}) {
  const drafts: RegularSettingsDraft[] = []
  function Harness() {
    const [draft, setDraft] = useState<RegularSettingsDraft>({ ...initialRegularSettingsDraft, ...overrides })
    drafts.push(draft)
    return <OfflineCoastlineSection draft={draft} onChange={(patch) => setDraft((p) => ({ ...p, ...patch }))} />
  }
  render(<Harness />)
  return { latestDraft: () => drafts[drafts.length - 1] }
}

const heldSquares = [
  { lat: -21, lon: 148, state: 'ready' as const, in_range: true, edits_since: 5, downloaded_at: NOON('2026-10-03') },
  { lat: -21, lon: 149, state: 'ready' as const, in_range: true, edits_since: 7, downloaded_at: NOON('2026-10-05') },
]

beforeEach(() => {
  hook.status = undefined
  hook.loading = false
  hook.error = null
  hook.update = vi.fn().mockResolvedValue(undefined)
  hook.clearCoverage = vi.fn().mockResolvedValue(undefined)
})

describe('OfflineCoastlineSection status line', () => {
  it('says it is off and offers nothing to update or clear', () => {
    hook.status = status({ enabled: false, state: 'off' })
    renderSection()
    expect(screen.getByTestId('offline-coastline-status')).toHaveTextContent('Off. Nothing is downloaded or checked.')
    expect(screen.queryByRole('button', { name: 'Update' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Clear coverage' })).not.toBeInTheDocument()
    expect(screen.queryByTestId('map-stub')).not.toBeInTheDocument()
  })

  it('shows coverage, oldest date and edits since', () => {
    hook.status = status({
      squares: heldSquares, held: 31, oldest_downloaded_at: NOON('2026-10-03'), edits_since: 12, squares_with_edits: 2,
    })
    renderSection({ offlineCoastlineEnabled: true })
    expect(screen.getByTestId('offline-coastline-status')).toHaveTextContent(/Coverage: 31 squares · oldest 3 Oct( 2026)? · 12 edits since/)
  })

  it('shows download progress', () => {
    hook.status = status({ state: 'downloading', held: 7, wanted: 25, wanted_held: 7 })
    renderSection({ offlineCoastlineEnabled: true })
    expect(screen.getByTestId('offline-coastline-status')).toHaveTextContent('Downloading 7 of 25')
  })

  it('says it is waiting for a position fix', () => {
    hook.status = status({ state: 'waiting_for_fix', has_fix: false, position: undefined })
    renderSection({ offlineCoastlineEnabled: true })
    expect(screen.getByTestId('offline-coastline-status')).toHaveTextContent('Waiting for a position fix')
  })

  it('shows what the server said when it cannot run', () => {
    hook.status = status({ state: 'error', error: 'the osm-overpass plugin is not installed, so the offline coastline cannot download' })
    renderSection({ offlineCoastlineEnabled: true })
    expect(screen.getByTestId('offline-coastline-status')).toHaveTextContent('the osm-overpass plugin is not installed')
  })

  it('says so while the status loads and when it cannot be read', () => {
    hook.loading = true
    renderSection()
    expect(screen.getByText('Reading status…')).toBeInTheDocument()
  })

  it('shows a read failure rather than a blank', () => {
    hook.error = 'HTTP 503'
    renderSection()
    expect(screen.getByText(/Could not read the offline coastline status: HTTP 503/)).toBeInTheDocument()
  })

  it('reminds the operator that the switch applies on save', () => {
    hook.status = status({ enabled: false, state: 'off' })
    renderSection({ offlineCoastlineEnabled: true })
    expect(screen.getByText('Save settings to apply the switch.')).toBeInTheDocument()
  })
})

describe('OfflineCoastlineSection switch', () => {
  it('turns on through the settings draft and marks the form dirty', () => {
    hook.status = status({ enabled: false, state: 'off' })
    const { latestDraft } = renderSection()
    expect(draftsEqual(latestDraft(), initialRegularSettingsDraft)).toBe(true)
    fireEvent.click(screen.getByRole('switch', { name: 'Keep an offline coastline' }))
    expect(latestDraft().offlineCoastlineEnabled).toBe(true)
    expect(draftsEqual(latestDraft(), initialRegularSettingsDraft)).toBe(false)
    expect(buildRegularSettingsPatch(latestDraft()).offline_coastline).toEqual({ enabled: true })
  })

  it('is off by default and round-trips through the payload', () => {
    expect(initialRegularSettingsDraft.offlineCoastlineEnabled).toBe(false)
    expect(hydrateDraftFromSettings({}).offlineCoastlineEnabled).toBe(false)
    expect(hydrateDraftFromSettings({ offline_coastline: { enabled: true } }).offlineCoastlineEnabled).toBe(true)
  })
})

describe('OfflineCoastlineSection coverage', () => {
  it('shows the map and names squares that failed with the reason', () => {
    hook.status = status({
      state: 'retrying', held: 1, failed: 1, oldest_downloaded_at: NOON('2026-10-03'),
      squares: [
        heldSquares[0],
        { lat: -22, lon: 148, state: 'failed', in_range: true, edits_since: 0, error: 'overpass rate limited' },
      ],
    })
    renderSection({ offlineCoastlineEnabled: true })
    expect(screen.getByTestId('map-stub')).toHaveTextContent('2 squares')
    const failures = screen.getByTestId('offline-coastline-failures')
    expect(failures).toHaveTextContent('1 square did not download or check')
    expect(failures).toHaveTextContent('22°S 148°E')
    expect(failures).toHaveTextContent('overpass rate limited')
  })
})

describe('OfflineCoastlineSection Update', () => {
  it('appears only when there are edits since and updates the squares', async () => {
    hook.status = status({ squares: heldSquares, held: 2, edits_since: 12, squares_with_edits: 2, oldest_downloaded_at: NOON('2026-10-03') })
    renderSection({ offlineCoastlineEnabled: true })
    expect(screen.getByText(/12 edits since download in 2 squares/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Update' }))
    await waitFor(() => expect(hook.update).toHaveBeenCalledTimes(1))
  })

  it('is absent with no edits since', () => {
    hook.status = status({ squares: [{ ...heldSquares[0], edits_since: 0 }], held: 1, oldest_downloaded_at: NOON('2026-10-03') })
    renderSection({ offlineCoastlineEnabled: true })
    expect(screen.queryByRole('button', { name: 'Update' })).not.toBeInTheDocument()
  })

  it('is disabled while squares are updating', () => {
    hook.status = status({ state: 'updating', updating: 2, squares: heldSquares, held: 2, edits_since: 12, squares_with_edits: 2 })
    renderSection({ offlineCoastlineEnabled: true })
    expect(screen.getByRole('button', { name: 'Update' })).toBeDisabled()
  })

  it('shows the server error when the update is refused', async () => {
    hook.update = vi.fn().mockRejectedValue(new Error('Offline coastline is off: turn it on in Settings before updating'))
    hook.status = status({ squares: heldSquares, held: 2, edits_since: 12, squares_with_edits: 2 })
    renderSection({ offlineCoastlineEnabled: true })
    fireEvent.click(screen.getByRole('button', { name: 'Update' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Offline coastline is off')
  })
})

describe('OfflineCoastlineSection Clear coverage', () => {
  it('asks first, then clears', async () => {
    hook.status = status({ squares: heldSquares, held: 2 })
    renderSection({ offlineCoastlineEnabled: true })
    fireEvent.click(screen.getByRole('button', { name: 'Clear coverage' }))
    const dialog = await screen.findByRole('alertdialog')
    expect(hook.clearCoverage).not.toHaveBeenCalled()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Clear coverage' }))
    await waitFor(() => expect(hook.clearCoverage).toHaveBeenCalledTimes(1))
  })

  it('cancel leaves the coverage alone', async () => {
    hook.status = status({ squares: heldSquares, held: 2 })
    renderSection({ offlineCoastlineEnabled: true })
    fireEvent.click(screen.getByRole('button', { name: 'Clear coverage' }))
    const dialog = await screen.findByRole('alertdialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(hook.clearCoverage).not.toHaveBeenCalled()
  })
})

describe('Offline coastline section wiring', () => {
  it('is a settings section with its own route and help page', () => {
    expect(SETTINGS_SECTIONS.map((s) => s.id)).toContain('offline-coastline')
    expect(parseAppLocation('/settings/offline-coastline')).toEqual({
      panel: 'settings', route: { view: 'section', section: 'offline-coastline' },
    })
    expect(SETTINGS_HELP_TARGETS['offline-coastline']).toEqual({ page: 'features/offline-coastline' })
  })
})
