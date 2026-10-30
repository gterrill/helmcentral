import { useCallback, useEffect, useRef } from 'react'

import { useAppLocation, useAppRouter, useNavigate } from '@/hooks/use-app-location'
import type { InventoryList, InventoryRoute } from '@/lib/app-location'
import type { AppRouter, NavigateOptions } from '@/lib/app-router'
import type { InventorySectionId } from '@/components/inventory/inventory-nav'

export interface InventoryRouteApi {
  route: InventoryRoute
  /** Goes to a view of Inventory, replacing the whole slice. */
  go: (view: InventoryRoute, options?: NavigateOptions) => boolean
  /** Up one level: an editor returns to its index, a bin or location page to
   * the Locations index, the Decks list to Locations, a deck to the Decks
   * list. An index has nothing above it, so nothing happens there. */
  close: (options?: NavigateOptions) => boolean
  /** The nav: a section's index. Locations returns to whichever of Table or
   * Plan was last showing. */
  goSection: (section: InventorySectionId, options?: NavigateOptions) => boolean
  /** The Locations index as the operator last had it, Table or Plan; `plan`
   * forces one of the two (the deck tab is still the last one used). `list`
   * is the table's search and sort, which the Plan view has none of. */
  locationsHome: (plan?: boolean, list?: InventoryList) => InventoryRoute
}

function liveRoute(router: AppRouter): InventoryRoute {
  const location = router.getSnapshot().location
  if (location.panel === 'inventory') return location.route
  return router.resume('inventory').route
}

/** Inventory's slice of the app location (ADR 0171). */
export function useInventoryRoute(): InventoryRouteApi {
  const router = useAppRouter()
  const location = useAppLocation()
  const navigate = useNavigate()
  // `location` is the subscription: it re-renders the caller on every move.
  const route = location.panel === 'inventory' ? location.route : liveRoute(router)

  // The Table/Plan choice, and the deck tab open in Plan, belong to the
  // Locations index but survive opening a location, a bin or the Decks list
  // and coming back, so a "back to Locations" lands where the operator left.
  // They are not in the address of those pages, which is why they are kept here.
  const locationsIndex = useRef<{ plan: boolean; deckId: string | null }>({ plan: false, deckId: null })
  useEffect(() => {
    if (route.view === 'plan') locationsIndex.current = { plan: true, deckId: route.deckId }
    else if (route.view === 'index' && route.section === 'locations') {
      locationsIndex.current = { plan: false, deckId: locationsIndex.current.deckId }
    }
  }, [route])

  const go = useCallback(
    (view: InventoryRoute, options?: NavigateOptions) => navigate({ panel: 'inventory', route: view }, options),
    [navigate],
  )
  const locationsHome = useCallback((plan: boolean = locationsIndex.current.plan, list?: InventoryList): InventoryRoute => (
    plan
      ? { view: 'plan', deckId: locationsIndex.current.deckId, ...(list ? { list } : {}) }
      : { view: 'index', section: 'locations', ...(list ? { list } : {}) }
  ), [])
  const goSection = useCallback((section: InventorySectionId, options?: NavigateOptions) => go(
    section === 'locations' ? locationsHome() : { view: 'index', section },
    options,
  ), [go, locationsHome])
  const close = useCallback((options?: NavigateOptions) => {
    const current = liveRoute(router)
    switch (current.view) {
      // Back from a record returns to the index with the filter, search and
      // sort the record was opened from.
      case 'equipment':
        return go({ view: 'index', section: 'equipment', ...(current.list ? { list: current.list } : {}) }, options)
      case 'equipment-new':
        return go({ view: 'index', section: 'equipment' }, options)
      case 'location':
        return go(locationsHome(undefined, current.list), options)
      case 'bin':
      case 'decks':
        return go(locationsHome(), options)
      case 'deck':
        return go({ view: 'decks', ...(current.list ? { list: current.list } : {}) }, options)
      default:
        return true
    }
  }, [go, locationsHome, router])

  return { route, go, close, goSection, locationsHome }
}
