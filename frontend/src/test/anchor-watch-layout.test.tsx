import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { AnchorWatchDrawer } from '@/components/anchor-watch-drawer'
import { AnchorSaveDepth } from '@/components/anchor-save-depth'
import { SidebarProvider } from '@/components/ui/sidebar'

// The low-water verdict comes from the server (ADR 0272); this file is not about it.
vi.mock('@/hooks/use-anchor-low-water', () => ({ useAnchorLowWater: () => ({ lowWater: null, workedOutAt: null, failed: false }) }))

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(({ children }: { children?: React.ReactNode }, ref) => {
      React.useImperativeHandle(ref, () => ({
        getCanvas: () => ({ style: { cursor: 'grab' } }),
        getZoom: () => 14,
        easeTo: () => undefined,
      }))
      return <div data-testid="map-root">{children}</div>
    }),
    Marker: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    Source: ({ children, id }: { children?: React.ReactNode; id: string }) => <div data-testid={`source-${id}`}>{children}</div>,
    Layer: ({ id }: { id: string }) => <div data-testid={`layer-${id}`} />,
  }
})
vi.mock('@/hooks/use-saved-places', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-saved-places')>()),
  useSavedPlaces: () => ({ places: [], loading: false, error: null, refresh: vi.fn(), add: vi.fn(), change: vi.fn(), remove: vi.fn() }),
}))
vi.mock('@/hooks/use-vessel-state', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-vessel-state')>()),
  useVesselStateSelector: (select: (state: { timeZone: string | null }) => unknown) => select({ timeZone: 'Australia/Brisbane' }),
}))
vi.mock('@/hooks/use-app-config', () => ({ useAppConfig: () => ({ ui: { distanceUnits: 'metric' } }) }))

const watch = {
  active: true,
  anchorLat: -20,
  anchorLon: 149,
  setAt: '2026-11-01T20:00:00Z',
  placeName: 'Butterfly Bay',
  datumDepthM: 4.2 as number | null,
  datumDepthReason: '',
  planningDepthSource: 'sounder' as const,
}

const baseProps = {
  vesselLat: -25.2939,
  vesselLon: 152.9103,
  vesselHeadingDeg: 45,
  anchorLat: -25.2938,
  anchorLon: 152.9102,
  anchorSetAt: '2026-08-20T06:30:00Z',
  anchorStateKnown: true,
  radiusMeters: 20,
  depthMeters: 3.2,
  depthLastUpdateAgeS: null,
  currentDriftKts: null,
  currentSetDeg: null,
  distanceMeters: 10,
  bearingDeg: 80,
  vesselTrail: [],
  aisVessels: [],
  aisTrails: new Map(),
  isDarkTheme: false,
  showImageryLayer: false,
  onImageryToggle: () => undefined,
  onRadiusChange: async () => undefined,
  adjustAnchor: async () => undefined,
  onClearAnchor: () => undefined,
  isImperial: false,
  onDropAnchor: () => undefined,
  canDrop: true,
  anchorState: 'set' as const,
  rodeDeployedM: 0,
  seaState: 'calm' as const,
  seabedType: 'sand' as const,
  windSpeedApparentKts: null,
  maxGustKts: { '10m': null, '30m': null, '1h': null, '24h': null },
  tide: null,
  anchorConfig: {
    bowRollerHeightM: 1, chainSizeMm: 10, chainOnboardM: 50, hullType: 'power_cat' as const, scopeMethod: 'ratio' as const,
    windageAreaM2: 20, gpsFromBowM: 2, loaM: 10, minClearanceAtLowM: 0.5,
  },
  vesselLengthOverallM: null,
  hasGpsFix: true,
  windBandId: null,
  onWindBandChange: () => undefined,
  onUpdateRodeAndConditions: async () => undefined,
  planningDepthM: null,
  planningTideHeightFt: null,
  onPlanningDepthChange: () => Promise.resolve(),
  bowOffsetApplied: true,
  bowOffsetM: 2,
  bowOffsetReason: '',
}

function renderDrawer(w = watch) {
  return render(
    <SidebarProvider>
      <AnchorWatchDrawer {...baseProps} saveDepth={<AnchorSaveDepth watch={w} />} />
    </SidebarProvider>,
  )
}

beforeEach(() => localStorage.clear())

describe('Anchor Watch layout: save depth lives in the rode planner', () => {
  it('keeps the planner rail last in the row, with no save-depth child beside it', () => {
    renderDrawer()
    const strip = screen.getByRole('button', { name: /expand rode planner/i }).parentElement as HTMLElement
    const row = strip.parentElement as HTMLElement
    expect(row.lastElementChild).toBe(strip)
    expect(row.children).toHaveLength(2)
    expect(screen.queryByTestId('anchor-save-depth')).toBeNull()
  })

  it('shows the save-depth line at the foot of the expanded planner', () => {
    renderDrawer()
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))
    const line = screen.getByTestId('anchor-save-depth')
    expect(line).toHaveTextContent('4.2 m at datum, from the sounder')
    const apply = screen.getByRole('button', { name: /^set alarm/i })
    expect(apply.compareDocumentPosition(line) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Save as a saved place' })).toHaveTextContent('Save…')
  })

  it('leaves no empty band in the planner when there is no active watch', () => {
    renderDrawer({ ...watch, active: false })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))
    expect(screen.queryByTestId('anchor-save-depth')).toBeNull()
    const footer = screen.getByRole('button', { name: /^set alarm/i }).parentElement as HTMLElement
    const band = footer.nextElementSibling as HTMLElement
    expect(band).not.toBeNull()
    expect(band).toBeEmptyDOMElement()
    expect(band).toHaveClass('empty:hidden')
  })
})
