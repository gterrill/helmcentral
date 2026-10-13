import { inventorySectionOf, type AppLocation, type InventoryRoute } from '@/lib/app-location'

// ADR 0171: the questions Inventory's navigation guards ask, as pure functions.
// These were inventoryEditorClosedBy and inventoryWorkClosedBy in
// app-location.ts, asked by App's popstate handler. Each owner of unsaved
// state (the editors, Stocktake, the bin's quick-add) registers its own guard
// and asks the one that fits it, with the route it is showing; every move (a
// click as much as a Back press) goes through them.

/**
 * Would moving to `target` take the editor that holds a draft off screen?
 *
 * The editors are the Equipment editor, the location page (a rename draft)
 * and the deck page (a name and layout draft). Comparing ids alone is not
 * enough, and got this wrong twice over: a "New item" draft has no id, so
 * Back out of one to another inventory section compared nothing to nothing,
 * saw no change, and discarded the draft with no prompt at all. A saved
 * record open in the editor had the same hole whenever the section changed
 * without the id doing so.
 *
 * So the question is asked the other way round - is an editor showing, and
 * does `target` still show that same editor? - which needs no special case
 * for the draft and none for a section switch. A draft has no URL that can
 * represent it, so any other location at all closes it; a saved record
 * survives only a target naming that same record.
 */
export function inventoryEditorClosedBy(route: InventoryRoute, target: AppLocation): boolean {
  switch (route.view) {
    case 'deck':
      return !(target.panel === 'inventory' && target.route.view === 'deck' && target.route.id === route.id)
    case 'location':
      return !(target.panel === 'inventory' && target.route.view === 'location' && target.route.id === route.id)
    case 'equipment':
      return !(target.panel === 'inventory' && target.route.view === 'equipment' && target.route.id === route.id)
    case 'equipment-new':
      return true
    default:
      return false
  }
}

/**
 * Would moving to `target` take Stocktake or the bin page (whichever holds
 * quick-add work or scans) off screen? The counterpart of
 * inventoryEditorClosedBy for the work that has no draft to save: leaving the
 * Inventory panel, leaving Stocktake, or the bin page closing or changing bin.
 */
export function inventoryWorkClosedBy(route: InventoryRoute, target: AppLocation): boolean {
  const stocktake = inventorySectionOf(route) === 'stocktake'
  if (!stocktake && route.view !== 'bin') return false
  if (target.panel !== 'inventory') return true
  if (stocktake) return inventorySectionOf(target.route) !== 'stocktake'
  return !(target.route.view === 'bin' && route.view === 'bin' && target.route.code === route.code)
}
