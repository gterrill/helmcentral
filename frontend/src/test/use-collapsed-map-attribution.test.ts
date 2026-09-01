import { describe, expect, it } from 'vitest'
import { renderHook } from '@testing-library/react'
import { useCollapsedMapAttribution } from '@/hooks/use-collapsed-map-attribution'

/**
 * Builds a stand-in for MapLibre's attribution control in the state it
 * leaves itself in after auto-expanding: both the expanded class and the
 * `open` attribute set. Those two always move together (verified against a
 * live map), which is exactly why this cannot be fixed in CSS - a control
 * the user deliberately expanded is indistinguishable from one MapLibre
 * expanded on its own.
 */
function buildMap(opts: { expanded: boolean } = { expanded: true }) {
  const container = document.createElement('div')
  const attrib = document.createElement('details')
  attrib.className = 'maplibregl-ctrl maplibregl-ctrl-attrib maplibregl-compact'
  if (opts.expanded) {
    attrib.classList.add('maplibregl-compact-show')
    attrib.setAttribute('open', '')
  }
  const button = document.createElement('summary')
  button.className = 'maplibregl-ctrl-attrib-button'
  attrib.appendChild(button)
  container.appendChild(attrib)
  document.body.appendChild(container)

  return {
    attrib,
    button,
    ref: { current: { getMap: () => ({ getContainer: () => container }) } },
  }
}

function isExpanded(el: HTMLElement) {
  return el.classList.contains('maplibregl-compact-show')
}

describe('useCollapsedMapAttribution', () => {
  it('collapses an auto-expanded attribution control to the icon', () => {
    const { attrib, ref } = buildMap()
    expect(isExpanded(attrib)).toBe(true)

    const { result } = renderHook(() => useCollapsedMapAttribution(ref as never))
    result.current()

    expect(isExpanded(attrib)).toBe(false)
    expect(attrib.hasAttribute('open')).toBe(false)
  })

  it('leaves an already-collapsed control alone', () => {
    const { attrib, ref } = buildMap({ expanded: false })
    const { result } = renderHook(() => useCollapsedMapAttribution(ref as never))
    result.current()
    expect(isExpanded(attrib)).toBe(false)
  })

  // The whole point of tracking user intent: MapLibre re-runs its own
  // expand logic on attribution changes (a source appearing, a style
  // reload), and re-collapsing underneath someone who just tapped the icon
  // to read the credits would be worse than the original problem.
  it('stops collapsing once the user has toggled the control themselves', () => {
    const { attrib, button, ref } = buildMap()
    const { result } = renderHook(() => useCollapsedMapAttribution(ref as never))

    // First settle: collapses, as normal.
    result.current()
    expect(isExpanded(attrib)).toBe(false)

    // User taps the icon; MapLibre expands it.
    button.click()
    attrib.classList.add('maplibregl-compact-show')
    attrib.setAttribute('open', '')

    // A later settle must not undo that.
    result.current()
    expect(isExpanded(attrib)).toBe(true)
  })

  it('does not throw before the map exists', () => {
    const ref = { current: null }
    const { result } = renderHook(() => useCollapsedMapAttribution(ref as never))
    expect(() => result.current()).not.toThrow()
  })
})
