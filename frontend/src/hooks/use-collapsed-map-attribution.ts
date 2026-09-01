import { useCallback, useRef } from 'react'
import type { RefObject } from 'react'
import type { MapRef } from 'react-map-gl/maplibre'

const ATTRIBUTION_SELECTOR = '.maplibregl-ctrl-attrib'
const ATTRIBUTION_BUTTON_SELECTOR = '.maplibregl-ctrl-attrib-button'
const EXPANDED_CLASS = 'maplibregl-compact-show'

/**
 * Keeps MapLibre's attribution control showing its compact "i" icon instead
 * of the full credit line.
 *
 * `attributionControl={{ compact: true }}` is not enough on its own. Reading
 * MapLibre's own `_updateCompact`: the first time the attribution becomes
 * non-empty, the control has neither the compact nor the empty class yet, so
 * it adds `maplibregl-compact` AND `maplibregl-compact-show` together and
 * renders fully expanded. From then on it stays expanded until something
 * collapses it - which on a real map is the first drag or pinch (MapLibre's
 * own `_updateCompactMinimize`), and never at all if you only ever zoom with
 * the on-screen buttons. That is the "it's an icon sometimes and a long
 * strip of text other times" behaviour: it depends entirely on whether you
 * happened to touch the map yet.
 *
 * This cannot be done in CSS. The auto-expanded state and the state after a
 * deliberate tap on the icon are byte-for-byte identical - `open` and
 * `maplibregl-compact-show` are always both present or both absent
 * (confirmed against a live map), so no selector can tell "MapLibre opened
 * this" from "the operator opened this".
 *
 * So it is done by hand, once per settle, and stops permanently as soon as
 * the operator touches the control themselves. Re-collapsing under someone
 * who just tapped the icon to read the credits would be worse than the
 * problem being fixed.
 *
 * Returns a callback to wire to the map's `onIdle`, by which point the
 * attribution has populated and MapLibre has had its chance to expand it.
 */
export function useCollapsedMapAttribution(mapRef: RefObject<MapRef | null>) {
  const userToggledRef = useRef(false)
  const listenerBoundRef = useRef(false)

  return useCallback(() => {
    if (userToggledRef.current) return

    const container = mapRef.current?.getMap()?.getContainer()
    if (!container) return

    // Delegated, so it survives MapLibre rebuilding the control's innards
    // on a style reload. Bound here rather than in an effect because the
    // map container does not exist until the map has mounted.
    if (!listenerBoundRef.current) {
      listenerBoundRef.current = true
      container.addEventListener('click', (event) => {
        const target = event.target as Element | null
        if (target?.closest(ATTRIBUTION_BUTTON_SELECTOR)) {
          userToggledRef.current = true
        }
      })
    }

    const attribution = container.querySelector(ATTRIBUTION_SELECTOR)
    if (!attribution || !attribution.classList.contains(EXPANDED_CLASS)) return

    // Both, together: MapLibre keys its own toggle off the class, and the
    // <details> element keys its native rendering off the attribute.
    attribution.classList.remove(EXPANDED_CLASS)
    attribution.removeAttribute('open')
  }, [mapRef])
}
