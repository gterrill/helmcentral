import { act, fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import type { AnchorPlacemark } from '@/hooks/use-anchor-placemarks'

vi.mock('maplibre-gl', () => ({ default: {} }))

// Where the mock map reports the next click as having landed.
let nextMapClickLngLat = { lat: 0, lng: 0 }

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(
      (
        { children, onClick }: { children?: React.ReactNode; onClick?: (e: { lngLat: { lat: number; lng: number } }) => void },
        ref: React.Ref<unknown>,
      ) => {
        // MapLibre listens for clicks itself, on an element that contains
        // every Marker. So clicking a marker's button fires the button's
        // React handler AND then the map's click handler, and React's
        // stopPropagation cannot stop the latter — the component has to
        // suppress it explicitly. Listening on document reproduces that
        // ordering: React dispatches first, this fires after, regardless of
        // stopPropagation inside the React tree.
        const rootRef = React.useRef<HTMLDivElement | null>(null)
        React.useEffect(() => {
          const handler = (e: MouseEvent) => {
            if (rootRef.current?.contains(e.target as Node)) {
              onClick?.({ lngLat: nextMapClickLngLat })
            }
          }
          document.addEventListener('click', handler)
          return () => document.removeEventListener('click', handler)
        })
        React.useImperativeHandle(ref, () => ({
          getCanvas: () => ({ style: { cursor: 'grab' } }),
          getZoom: () => 14,
          easeTo: vi.fn(),
          project: () => ({ x: 0, y: 0 }),
        }))
        return <div data-testid="map-root" ref={rootRef}>{children}</div>
      },
    ),
    Marker: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    Source: ({ children, id }: { children?: React.ReactNode; id: string }) => <div data-testid={`source-${id}`}>{children}</div>,
    Layer: ({ id }: { id: string }) => <div data-testid={`layer-${id}`} />,
  }
})

// Vessel at the origin of these fixtures; the bombie sits ~100m north of it.
const VESSEL_LAT = -25.2939
const VESSEL_LON = 152.9103
const BOMBIE_LAT = -25.29300
const BOMBIE_LON = 152.9103
// Somewhere else entirely — where a stray map click would land a pin.
const STRAY_LAT = -25.2950
const STRAY_LON = 152.9150

function renderMap(overrides: Partial<React.ComponentProps<typeof AnchorWatchMap>> = {}) {
  return render(
    <AnchorWatchMap
      vesselLat={VESSEL_LAT}
      vesselLon={VESSEL_LON}
      vesselHeadingDeg={265}
      anchorLat={VESSEL_LAT}
      anchorLon={VESSEL_LON}
      radiusMeters={34}
      depthMeters={3.2}
      currentDriftKts={0.1}
      currentSetDeg={120}
      distanceMeters={10}
      bearingDeg={80}
      isImperial={false}
      vesselTrail={() => []}
      aisVessels={[]}
      aisTrails={() => new Map()}
      isDarkTheme={false}
      onAnchorReposition={() => undefined}
      onRadiusChange={() => undefined}
      onClearAnchor={() => undefined}
      {...overrides}
    />,
  )
}

// Clicks open water at the given point.
function clickMapAt(lat: number, lon: number) {
  nextMapClickLngLat = { lat, lng: lon }
  fireEvent.click(screen.getByTestId('map-root'))
}

// Clicks a control inside the map, with the map reporting the click as
// landing on `strayLat/strayLon` — as it does in the real component, where
// maplibre reports wherever the pointer was.
function clickInMapAt(element: HTMLElement, strayLat: number, strayLon: number) {
  nextMapClickLngLat = { lat: strayLat, lng: strayLon }
  fireEvent.click(element)
}

describe('anchor-watch placemarks', () => {
  it('offers a Pin button in the tooltip raised by clicking unoccupied water', () => {
    renderMap()

    clickMapAt(BOMBIE_LAT, BOMBIE_LON)

    // The tooltip reports range and bearing to the clicked point...
    expect(screen.getByTestId('pin-candidate')).toHaveTextContent('100 m')
    expect(screen.getByTestId('pin-candidate')).toHaveTextContent('0°')
    // ...and offers to make it permanent.
    expect(screen.getByRole('button', { name: /^pin$/i })).toBeInTheDocument()
  })

  it('does not let the Pin tooltip expire before it can be clicked', () => {
    vi.useFakeTimers()
    try {
      renderMap()
      clickMapAt(BOMBIE_LAT, BOMBIE_LON)
      act(() => { vi.advanceTimersByTime(10_000) })
      expect(screen.getByRole('button', { name: /^pin$/i })).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it('creates a placemark at the clicked point when Pin is pressed', () => {
    const onPlacemarkCreate = vi.fn()
    renderMap({ onPlacemarkCreate })

    clickMapAt(BOMBIE_LAT, BOMBIE_LON)
    clickInMapAt(screen.getByRole('button', { name: /^pin$/i }), STRAY_LAT, STRAY_LON)

    expect(onPlacemarkCreate).toHaveBeenCalledTimes(1)
    expect(onPlacemarkCreate).toHaveBeenCalledWith(BOMBIE_LAT, BOMBIE_LON)
    // The candidate tooltip gives way to the real placemark once pinned —
    // pressing Pin must not double as a map click that opens a fresh
    // tooltip under the button.
    expect(screen.queryByTestId('pin-candidate')).not.toBeInTheDocument()
  })

  it('dismisses the tooltip on X instead of moving it under the button', () => {
    renderMap()

    clickMapAt(BOMBIE_LAT, BOMBIE_LON)
    clickInMapAt(screen.getByRole('button', { name: /dismiss/i }), STRAY_LAT, STRAY_LON)

    expect(screen.queryByTestId('pin-candidate')).not.toBeInTheDocument()
  })

  it('keeps a placemark put when it is selected and removed', () => {
    const onPlacemarkCreate = vi.fn()
    const onPlacemarkRemove = vi.fn()
    renderMap({
      placemarks: [{ id: 'pm-1', lat: BOMBIE_LAT, lon: BOMBIE_LON, label: 'bombie', created_at: '' }],
      onPlacemarkCreate,
      onPlacemarkRemove,
    })

    clickInMapAt(screen.getByRole('button', { name: /placemark: bombie/i }), STRAY_LAT, STRAY_LON)
    expect(screen.queryByTestId('pin-candidate')).not.toBeInTheDocument()

    clickInMapAt(screen.getByRole('button', { name: /remove/i }), STRAY_LAT, STRAY_LON)
    expect(onPlacemarkRemove).toHaveBeenCalledWith('pm-1')
    expect(screen.queryByTestId('pin-candidate')).not.toBeInTheDocument()
    expect(onPlacemarkCreate).not.toHaveBeenCalled()
  })

  it('shows the live range to a placemark, and updates it as the vessel swings', () => {
    const placemarks: AnchorPlacemark[] = [
      { id: 'pm-1', lat: BOMBIE_LAT, lon: BOMBIE_LON, label: '', created_at: '' },
    ]
    const { rerender } = renderMap({ placemarks })

    expect(screen.getByTestId('placemark-pm-1')).toHaveTextContent('100 m')

    // The boat swings 50m further away; the pin's range must follow, because
    // watching that number close is the whole point of dropping it.
    rerender(
      <AnchorWatchMap
        vesselLat={-25.29435}
        vesselLon={VESSEL_LON}
        vesselHeadingDeg={265}
        anchorLat={VESSEL_LAT}
        anchorLon={VESSEL_LON}
        radiusMeters={34}
        depthMeters={3.2}
        currentDriftKts={0.1}
        currentSetDeg={120}
        distanceMeters={10}
        bearingDeg={80}
        isImperial={false}
        vesselTrail={() => []}
        aisVessels={[]}
        aisTrails={() => new Map()}
        isDarkTheme={false}
        placemarks={placemarks}
        onAnchorReposition={() => undefined}
        onRadiusChange={() => undefined}
        onClearAnchor={() => undefined}
      />,
    )

    expect(screen.getByTestId('placemark-pm-1')).toHaveTextContent('150 m')
  })

  it('removes a placemark through the button revealed by clicking it', () => {
    const onPlacemarkRemove = vi.fn()
    renderMap({
      placemarks: [{ id: 'pm-1', lat: BOMBIE_LAT, lon: BOMBIE_LON, label: 'bombie', created_at: '' }],
      onPlacemarkRemove,
    })

    expect(screen.queryByRole('button', { name: /remove/i })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /placemark: bombie/i }))
    fireEvent.click(screen.getByRole('button', { name: /remove/i }))

    expect(onPlacemarkRemove).toHaveBeenCalledWith('pm-1')
  })

  it('shows imperial ranges when the boat is set to imperial units', () => {
    renderMap({
      isImperial: true,
      placemarks: [{ id: 'pm-1', lat: BOMBIE_LAT, lon: BOMBIE_LON, label: '', created_at: '' }],
    })

    expect(screen.getByTestId('placemark-pm-1')).toHaveTextContent('328 ft')
  })
})
