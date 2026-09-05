import { fireEvent, render, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AnchorWatchTile } from '@/components/anchor-watch-tile'
import type { AnchorWatchResult } from '@/hooks/use-anchor-watch'
import { catenaryMethod, type RodeMethodResult, type RodePlanInput } from '@/lib/rode-plan'

// Same shallow-mock approach as anchor-imagery-toggle.test.tsx (~lines 22-37):
// the map itself is covered by its own dedicated tests, so here it's stubbed
// down to the props this tile is responsible for feeding it. scopeRecommendation
// is surfaced as data attributes (rather than rendered as the real Scope row —
// that formatting is the map's job, covered by anchor-watch-map-ui.test.tsx)
// so these tests can assert on exactly what the tile computed and handed down.
vi.mock('@/components/anchor-watch-map', () => ({
  AnchorWatchMap: (props: {
    vesselLat: number
    vesselLon: number
    scopeRecommendation: RodeMethodResult | null
  }) => (
    <div data-testid="anchor-watch-map">
      {`${props.vesselLat},${props.vesselLon}`}
      <div
        data-testid="scope-recommendation"
        data-id={props.scopeRecommendation?.id ?? ''}
        data-rode-m={props.scopeRecommendation?.recommendedRodeM ?? ''}
        data-scope-ratio={props.scopeRecommendation?.scopeRatio ?? ''}
        data-unavailable-reason={props.scopeRecommendation?.unavailableReason ?? ''}
      />
    </div>
  ),
}))

vi.mock('@/hooks/use-alarms', () => ({
  useAlarms: () => ({ alarms: [], acknowledge: vi.fn() }),
  findAnchorDragAlarm: () => null,
}))

// A plain object each test can mutate via anchorAlarmMock.silence / .unsilence
// (both vi.fn()) and reassign the booleans on — simpler than juggling
// mockReturnValueOnce across the describe blocks below, and every existing
// test keeps the pre-P0-fix "nothing going on" defaults.
const anchorAlarmMock: {
  isAlarming: boolean
  isSilenced: boolean
  isActive: boolean
  silence: ReturnType<typeof vi.fn>
  unsilence: ReturnType<typeof vi.fn>
} = {
  isAlarming: false,
  isSilenced: false,
  isActive: false,
  silence: vi.fn(),
  unsilence: vi.fn(),
}

vi.mock('@/hooks/use-anchor-alarm', () => ({
  useAnchorAlarm: () => anchorAlarmMock,
}))

vi.mock('@/lib/audio-utils', () => ({
  primeAudioContextForAlarm: vi.fn().mockResolvedValue(undefined),
}))

function baseWatch(overrides: Partial<AnchorWatchResult> = {}): AnchorWatchResult {
  return {
    anchorState: 'none',
    gnssCritical: false,
    anchorLat: null,
    anchorLon: null,
    radiusMeters: 20,
    rodeDeployedM: 0,
    seaState: 'calm',
    seabedType: 'sand',
    distanceMeters: null,
    bearingDeg: null,
    setAt: null,
    bowOffsetM: 0,
    bowOffsetApplied: false,
    bowOffsetReason: '',
    planningDepthM: null,
    planningTideHeightFt: null,
    setAnchorHere: vi.fn(),
    updatePosition: vi.fn(),
    updateRadius: vi.fn(),
    updateRodeAndConditions: vi.fn(),
    updatePlanningDepth: vi.fn(),
    clearAnchor: vi.fn(),
    ...overrides,
  }
}

function baseProps(overrides: Record<string, unknown> = {}) {
  return {
    watch: baseWatch(),
    lat: -25.1,
    lon: 152.9,
    depthMeters: 5,
    currentDriftKts: null,
    currentSetDeg: null,
    isImperial: false,
    vesselHeadingDeg: null,
    vesselTrail: () => [],
    aisVessels: [],
    aisTrails: () => new Map(),
    isDarkTheme: false,
    showImageryLayer: false,
    onImageryToggle: vi.fn(),
    showRadarEcho: false,
    onRadarEchoToggle: vi.fn(),
    onFullscreen: vi.fn(),
    placemarks: [],
    onPlacemarkCreate: vi.fn(),
    onPlacemarkRemove: vi.fn(),
    tide: null,
    windSpeedApparentKts: 15,
    maxGustKts: { '10m': null, '30m': null, '1h': null, '24h': null },
    anchorConfig: {
      bowRollerHeightM: 1,
      chainSizeMm: 10,
      chainOnboardM: 50,
      hullType: 'power_cat' as const,
      scopeMethod: 'ratio' as const,
      windageAreaM2: 20,
      gpsFromBowM: 0,
      loaM: 0,
    },
    selectedWindBandId: null,
    planningDepthM: null,
    planningTideHeightFt: null,
    lastUpdateAgeS: null,
    ...overrides,
  }
}

describe('AnchorWatchTile', () => {
  beforeEach(() => {
    anchorAlarmMock.isAlarming = false
    anchorAlarmMock.isSilenced = false
    anchorAlarmMock.isActive = false
    anchorAlarmMock.silence = vi.fn()
    anchorAlarmMock.unsilence = vi.fn()
  })

  it('renders the map when no anchor is set but a GPS fix is available', () => {
    render(<AnchorWatchTile {...baseProps()} />)

    expect(screen.getByTestId('anchor-watch-map')).toBeInTheDocument()
  })

  it('shows a No GPS fix placeholder — never a map — when position and anchor are both unavailable', () => {
    render(<AnchorWatchTile {...baseProps({ lat: null, lon: null })} />)

    expect(screen.queryByTestId('anchor-watch-map')).toBeNull()
    expect(screen.getByText('No GPS fix')).toBeInTheDocument()
  })

  it('disables Drop when there is no GPS fix to drop the anchor at', () => {
    render(<AnchorWatchTile {...baseProps({ lat: null, lon: null })} />)

    expect(screen.getByRole('button', { name: 'Drop' })).toBeDisabled()
  })

  it('still renders the map from the anchor point alone when the live GPS fix drops after the anchor is set', () => {
    render(
      <AnchorWatchTile
        {...baseProps({
          lat: null,
          lon: null,
          watch: baseWatch({ anchorState: 'set', anchorLat: -25.2, anchorLon: 152.8 }),
        })}
      />,
    )

    expect(screen.getByTestId('anchor-watch-map')).toHaveTextContent('-25.2,152.8')
  })

  it('shows Drop when no anchor is set', () => {
    render(<AnchorWatchTile {...baseProps()} />)

    expect(screen.getByRole('button', { name: 'Drop' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Raise' })).toBeNull()
  })

  it('shows Raise when an anchor is set, and confirming calls watch.clearAnchor', () => {
    const clearAnchor = vi.fn()
    render(
      <AnchorWatchTile
        {...baseProps({
          watch: baseWatch({ anchorState: 'set', anchorLat: -25.1, anchorLon: 152.9, clearAnchor }),
        })}
      />,
    )

    expect(screen.queryByRole('button', { name: 'Drop' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Raise' }))

    const dialog = screen.getByRole('alertdialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Raise' }))

    expect(clearAnchor).toHaveBeenCalledTimes(1)
  })

  // The recommendation itself now renders in the map's metric overlay (ADR
  // 0059 §3) — that formatting, including the imperial conversion, is
  // covered by anchor-watch-map-ui.test.tsx. These tests cover what remains
  // the tile's job: computing the right RodeMethodResult and handing it to
  // the map via scopeRecommendation.
  describe('scope recommendation', () => {
    it('computes the ratio-method rode figure for a normal (< 20kt) wind', () => {
      render(
        <AnchorWatchTile
          {...baseProps({
            depthMeters: 5,
            // 12kt apparent seeds the 10-15 band, which plans at its top
            // (15kt) — clear of the ratio method's 20kt threshold.
            windSpeedApparentKts: 12,
            anchorConfig: { ...baseProps().anchorConfig, bowRollerHeightM: 1, scopeMethod: 'ratio' },
          })}
        />,
      )

      // (5 + 1) * 5 = 30, ratio 5:1 below the 20kt threshold.
      const scope = screen.getByTestId('scope-recommendation')
      expect(scope.dataset.id).toBe('ratio')
      expect(Number(scope.dataset.rodeM)).toBeCloseTo(30, 6)
      expect(scope.dataset.scopeRatio).toBe('5')
    })

    it('computes the ratio-method rode figure for a strong (>= 20kt) wind', () => {
      render(
        <AnchorWatchTile
          {...baseProps({
            depthMeters: 5,
            windSpeedApparentKts: 25,
            anchorConfig: { ...baseProps().anchorConfig, bowRollerHeightM: 1, scopeMethod: 'ratio' },
          })}
        />,
      )

      // (5 + 1) * 7 = 42, ratio 7:1 at/above the 20kt threshold.
      const scope = screen.getByTestId('scope-recommendation')
      expect(scope.dataset.id).toBe('ratio')
      expect(Number(scope.dataset.rodeM)).toBeCloseTo(42, 6)
      expect(scope.dataset.scopeRatio).toBe('7')
    })

    // Proves the tile's Scope row now shares the same band selection as the
    // Rode Planner (frontend/src/components/anchor-rode-planner.tsx) rather
    // than seeding its own raw wind — an explicit stronger band must win
    // over a calm live gust, the same rule resolvePlanningWindBand applies
    // for the planner's own dropdown.
    it('follows the shared selectedWindBandId even when the live gust is calm', () => {
      render(
        <AnchorWatchTile
          {...baseProps({
            depthMeters: 5,
            maxGustKts: { '10m': null, '30m': null, '1h': 3, '24h': null }, // calm — seeds the 0-10 band alone
            windSpeedApparentKts: null,
            selectedWindBandId: '20-25',
            anchorConfig: { ...baseProps().anchorConfig, bowRollerHeightM: 1, scopeMethod: 'ratio' },
          })}
        />,
      )

      // The explicit 20-25 band plans at 25kt regardless of the calm 3kt
      // gust: (5 + 1) * 7 = 42, ratio 7:1, at/over the 20kt threshold.
      const scope = screen.getByTestId('scope-recommendation')
      expect(Number(scope.dataset.rodeM)).toBeCloseTo(42, 6)
      expect(scope.dataset.scopeRatio).toBe('7')
    })

    it('computes the catenary-method figure instead when scopeMethod is catenary', () => {
      const anchorConfig = {
        ...baseProps().anchorConfig,
        scopeMethod: 'catenary' as const,
        bowRollerHeightM: 1,
      }
      const input: RodePlanInput = {
        depth: { depthM: 5, tideHeightFt: null },
        isAnchored: false,
        bowRollerHeightM: anchorConfig.bowRollerHeightM,
        tide: null,
        // 12kt apparent (the wind fed to the tile below) seeds the 10-15
        // band, which plans at its top — 15kt, not the raw 12kt reading.
        windKts: 15,
        seaState: 'calm',
        seabedType: 'sand',
        chainSizeMm: anchorConfig.chainSizeMm,
        chainOnboardM: anchorConfig.chainOnboardM,
        windageAreaM2: anchorConfig.windageAreaM2,
        hullType: anchorConfig.hullType,
      }
      const expected = catenaryMethod(input)
      expect(expected).not.toBeNull()

      render(<AnchorWatchTile {...baseProps({ depthMeters: 5, windSpeedApparentKts: 12, anchorConfig })} />)

      const scope = screen.getByTestId('scope-recommendation')
      expect(scope.dataset.id).toBe('catenary')
      expect(Number(scope.dataset.rodeM)).toBeCloseTo(expected!.recommendedRodeM, 6)
    })

    it('passes the literal unavailable reason — never a bare dash — when depth is missing', () => {
      render(<AnchorWatchTile {...baseProps({ depthMeters: null })} />)

      expect(screen.getByTestId('scope-recommendation').dataset.unavailableReason).toBe('no depth reading')
    })

    it('never renders a pass/fail scope badge (ADR 0047 retired it)', () => {
      render(<AnchorWatchTile {...baseProps()} />)

      expect(screen.queryByText(/Scope OK/)).toBeNull()
      expect(screen.queryByText(/Scope Low/)).toBeNull()
      expect(screen.queryByText(/Scope Insufficient/)).toBeNull()
    })
  })

  describe('banners', () => {
    it('never prompts to set a watch from the SignalK navigation state', () => {
      render(<AnchorWatchTile {...baseProps({ watch: baseWatch({ anchorState: 'none' }) })} />)

      expect(screen.queryByText(/set anchor watch\?/i)).toBeNull()
    })

    it('shows the gnssCritical banner only once a watch is active', () => {
      const { rerender } = render(
        <AnchorWatchTile {...baseProps({ watch: baseWatch({ anchorState: 'none', gnssCritical: true }) })} />,
      )
      expect(screen.queryByText(/GPS signal degraded/)).toBeNull()

      rerender(
        <AnchorWatchTile
          {...baseProps({
            watch: baseWatch({ anchorState: 'set', anchorLat: -25.1, anchorLon: 152.9, gnssCritical: true }),
          })}
        />,
      )
      expect(screen.getByText(/GPS signal degraded/)).toBeInTheDocument()
    })
  })

  // P0: a silenced drag used to render nothing anywhere on this tile — the
  // red strip was gated on isAlarming alone, which silencing flips straight
  // to false with no replacement. These tests cover both the surviving red
  // strip and the amber one that must now stand in for it once silenced.
  describe('drag alarm strip', () => {
    it('shows a red, audible strip naming the live distance and radius, and Silence stops it', () => {
      anchorAlarmMock.isAlarming = true
      anchorAlarmMock.isActive = true
      render(
        <AnchorWatchTile
          {...baseProps({
            watch: baseWatch({
              anchorState: 'dragging',
              anchorLat: -25.1,
              anchorLon: 152.9,
              distanceMeters: 38,
              radiusMeters: 30,
            }),
          })}
        />,
      )

      const strip = screen.getByTestId('drag-alarm-strip')
      expect(strip).toHaveAttribute('role', 'alert')
      expect(strip.textContent).toContain('38')
      expect(strip.textContent).toContain('30')
      // The old copy said only "ALARM", with no indication of how far or
      // past what radius.
      expect(strip.textContent).not.toBe('ALARM')
      expect(screen.queryByTestId('drag-silenced-strip')).toBeNull()

      fireEvent.click(within(strip).getByRole('button', { name: /silence/i }))
      expect(anchorAlarmMock.silence).toHaveBeenCalledTimes(1)
    })

    it('shows a persistent amber strip — never nothing — once the drag is silenced', () => {
      anchorAlarmMock.isSilenced = true
      anchorAlarmMock.isActive = true
      render(
        <AnchorWatchTile
          {...baseProps({
            watch: baseWatch({
              anchorState: 'dragging',
              anchorLat: -25.1,
              anchorLon: 152.9,
              distanceMeters: 38,
              radiusMeters: 30,
            }),
          })}
        />,
      )

      expect(screen.queryByTestId('drag-alarm-strip')).toBeNull()
      const strip = screen.getByTestId('drag-silenced-strip')
      expect(strip).toHaveAttribute('role', 'alert')
      expect(strip.textContent).toContain('38')
      expect(strip.textContent).toContain('30')
      expect(strip.textContent?.toLowerCase()).toContain('silenced')

      fireEvent.click(within(strip).getByRole('button', { name: /unsilence/i }))
      expect(anchorAlarmMock.unsilence).toHaveBeenCalledTimes(1)
    })

    it('renders neither strip when there is no drag alarm at all', () => {
      render(<AnchorWatchTile {...baseProps({ watch: baseWatch({ anchorState: 'set', anchorLat: -25.1, anchorLon: 152.9 }) })} />)

      expect(screen.queryByTestId('drag-alarm-strip')).toBeNull()
      expect(screen.queryByTestId('drag-silenced-strip')).toBeNull()
    })
  })

  // Design critique item 4: the operator must be able to answer "is the
  // boat where I left it" from a hero readout above the map, not by parsing
  // the map's own overlay panel.
  describe('distance KPI stack', () => {
    it('promotes distance-vs-radius above the map once anchored', () => {
      render(
        <AnchorWatchTile
          {...baseProps({
            watch: baseWatch({
              anchorState: 'set',
              anchorLat: -25.1,
              anchorLon: 152.9,
              distanceMeters: 12,
              radiusMeters: 20,
            }),
          })}
        />,
      )

      const kpi = screen.getByTestId('anchor-distance-kpi')
      expect(kpi.textContent).toContain('Distance')
      expect(kpi.textContent).toContain('12')
      expect(kpi.textContent).toContain('20')
    })

    it('converts to feet when the host is set to imperial units', () => {
      render(
        <AnchorWatchTile
          {...baseProps({
            isImperial: true,
            watch: baseWatch({
              anchorState: 'set',
              anchorLat: -25.1,
              anchorLon: 152.9,
              distanceMeters: 10,
              radiusMeters: 20,
            }),
          })}
        />,
      )

      const kpi = screen.getByTestId('anchor-distance-kpi')
      // 10m -> 33ft, 20m -> 66ft (rounded).
      expect(kpi.textContent).toContain('33')
      expect(kpi.textContent).toContain('66')
      expect(kpi.textContent).toContain('ft')
    })

    it('does not render the KPI stack — not even as dashes — when no anchor is set', () => {
      render(<AnchorWatchTile {...baseProps({ watch: baseWatch({ anchorState: 'none' }) })} />)

      expect(screen.queryByTestId('anchor-distance-kpi')).toBeNull()
    })
  })

  describe('staleness', () => {
    it('marks the tile stale once the feed age passes the threshold', () => {
      render(<AnchorWatchTile {...baseProps({ lastUpdateAgeS: 500 })} />)

      const badge = screen.getByTestId('tile-stale-badge')
      expect(badge.textContent).toContain('8m')
    })

    it('does not mark the tile stale for a fresh age', () => {
      render(<AnchorWatchTile {...baseProps({ lastUpdateAgeS: 5 })} />)

      expect(screen.queryByTestId('tile-stale-badge')).toBeNull()
    })

    it('does not fabricate staleness when the age is unknown (null)', () => {
      render(<AnchorWatchTile {...baseProps({ lastUpdateAgeS: null })} />)

      expect(screen.queryByTestId('tile-stale-badge')).toBeNull()
    })
  })
})
