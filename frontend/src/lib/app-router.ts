import {
  formatAppLocation,
  isCanonicalAppPath,
  parseAppLocation,
  type AppLocation,
  type LocationContext,
  type PanelId,
} from '@/lib/app-location'

// ADR 0171: the one owner of the app location. App.tsx used to mirror the
// location into sixteen useStates, translate URL to state in one place, state
// to URL in a second and Back/Forward in a third. This module holds the
// location as a single value, writes it to history by one set of rules and
// asks the registered navigation guards before it moves.
//
// No React in here: a router is a plain object with subscribe/getSnapshot, so
// React reads it through useSyncExternalStore (hooks/use-app-location.ts) and
// the tests drive it against the in-memory history below.

/** Where the router reads and writes the address. */
export interface HistoryAdapter {
  /** The current pathname plus search, as parseAppLocation takes it. */
  path(): string
  push(path: string): void
  replace(path: string): void
  /** Calls `onPop` when the operator moves through history (Back/Forward). */
  listen(onPop: () => void): () => void
}

/** window.history and popstate. */
export function browserHistory(): HistoryAdapter {
  return {
    path: () => (globalThis.location?.pathname ?? '/') + (globalThis.location?.search ?? ''),
    push: (path) => window.history.pushState(null, '', path),
    replace: (path) => window.history.replaceState(null, '', path),
    listen: (onPop) => {
      window.addEventListener('popstate', onPop)
      return () => window.removeEventListener('popstate', onPop)
    },
  }
}

export interface MemoryHistory extends HistoryAdapter {
  /** Moves through the entries the way the browser's Back/Forward do, then notifies. */
  go(delta: number): void
  back(): void
  forward(): void
  readonly entries: readonly string[]
  readonly index: number
}

/** A history that never touches the window, for tests. */
export function memoryHistory(initialPath = '/'): MemoryHistory {
  const entries = [initialPath]
  let index = 0
  const listeners = new Set<() => void>()
  const go = (delta: number) => {
    const next = index + delta
    if (next < 0 || next >= entries.length) return
    index = next
    for (const listener of [...listeners]) listener()
  }
  return {
    path: () => entries[index],
    push: (path) => {
      entries.splice(index + 1)
      entries.push(path)
      index = entries.length - 1
    },
    replace: (path) => {
      entries[index] = path
    },
    listen: (onPop) => {
      listeners.add(onPop)
      return () => listeners.delete(onPop)
    },
    go,
    back: () => go(-1),
    forward: () => go(1),
    get entries() { return entries },
    get index() { return index },
  }
}

/** Why a move is held: unsaved edits, or in-progress inventory work that has nothing to save. */
export type NavigationGuardKind = 'dirty' | 'stocktake-work' | 'quick-add-work'

/**
 * A panel's say in whether the location may change right now. It decides from
 * the move itself and carries its own save, so "Save and Continue" needs
 * nothing from App.
 */
export interface NavigationGuardVerdict {
  kind: NavigationGuardKind
  /** Wording for a work kind, when the guard has something more specific to say. */
  detail?: string | null
}

export interface NavigationGuard {
  /** Names the guard, for the sidebar's unsaved dot and for tests. */
  owner: string
  /** The page named in "You have unsaved changes on the {label} page". */
  label: string
  /** Whether this move is held, and with which prompt: one answer, so the
   * kind and its wording cannot disagree. */
  blocks: (from: AppLocation, to: AppLocation) => NavigationGuardVerdict | null
  save?: () => Promise<void>
}

/** A move a guard is holding while the operator decides. */
export interface PendingNavigation {
  target: AppLocation
  replace: boolean
  /** Captured when the move was held, not read live: a save finishing under
   * the dialog must not change what it says. */
  kind: NavigationGuardKind
  label: string
  detail: string | null
  save: (() => Promise<void>) | null
}

export interface RouterSnapshot {
  location: AppLocation
  /** The dashboard page last shown. It outlives the dashboard being on screen,
   * so a panel opened from page 3 returns to page 3. */
  pageId: string | null
  pending: PendingNavigation | null
  /** The owners of the guards registered right now. */
  guardOwners: readonly string[]
}

export interface RouterContext extends LocationContext {
  /** False while auth is still resolving or a sign-in is pending: the router
   * leaves the address bar alone until the shell is real. */
  shellVisible: boolean
}

export interface NavigateOptions {
  /** Rewrite the current history entry instead of adding one. Set by a change
   * that corrects the location rather than going somewhere new. */
  replace?: boolean
  /** Skip the guards. For a move that has already settled what they protect
   * (a create or delete that succeeded, losing admin access). */
  force?: boolean
}

export type ResumablePanel = Exclude<PanelId, 'display'> | null

export interface AppRouter {
  /** The location the app was loaded with, as parsed. Unlike the live
   * location it still carries a `?document=` deep link. */
  readonly initialLocation: AppLocation
  subscribe: (listener: () => void) => () => void
  getSnapshot: () => RouterSnapshot
  /** Starts listening for Back/Forward. Returns the stop function. */
  start: () => () => void
  setContext: (context: RouterContext) => void
  /** Moves to `target`. Returns false when a guard held it; the held move is
   * in the snapshot's `pending`. */
  navigate: (target: AppLocation, options?: NavigateOptions) => boolean
  /** The location a panel was last left at, or its start page. */
  resume: {
    <P extends NonNullable<ResumablePanel>>(panel: P): Extract<AppLocation, { panel: P }>
    (panel: ResumablePanel): AppLocation
  }
  /** Changes the remembered dashboard page. On the dashboard that is a move
   * (a push unless `replace`); anywhere else, including the wall display, it
   * touches neither history nor the location. */
  setPageId: (pageId: string | null, options?: { replace?: boolean }) => void
  registerGuard: (read: () => NavigationGuard | null) => () => void
  /** Whether any panel holds work worth stopping for. */
  hasGuards: () => boolean
  /** Discard: makes the held move, skipping the guards. A dialog passes the
   * move it is showing, so closing it first cannot lose the move. */
  confirmPending: (held?: PendingNavigation) => void
  cancelPending: () => void
  /** Saves through the guard that held the move, then makes it. Rejects with
   * the save's error and keeps the move held. */
  saveAndContinue: () => Promise<void>
}

function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (typeof a !== 'object' || typeof b !== 'object' || a === null || b === null) return false
  const left = a as Record<string, unknown>
  const right = b as Record<string, unknown>
  const keys = Object.keys(left)
  if (keys.length !== Object.keys(right).length) return false
  return keys.every((key) => key in right && deepEqual(left[key], right[key]))
}

function memoryKey(location: AppLocation): string | null {
  if (location.panel === 'display') return null
  return location.panel ?? 'dashboard'
}

function startLocation(panel: ResumablePanel): AppLocation {
  switch (panel) {
    case null: return { panel: null, pageId: null }
    case 'assistant': return { panel: 'assistant', conversationId: null }
    case 'settings': return { panel: 'settings', route: { view: 'section', section: 'general' } }
    case 'documents': return { panel: 'documents', route: { view: 'browse', folderId: null, sectionId: null, documentId: null } }
    case 'inventory': return { panel: 'inventory', route: { view: 'index', section: 'equipment' } }
    case 'wall-displays': return { panel: 'wall-displays', route: { view: 'index' } }
    case 'passages': return { panel: 'passages', route: { view: 'index', filter: { kind: 'all' } } }
    case 'watches': return { panel: 'watches', route: { view: 'index' } }
    default: return { panel }
  }
}

// The viewer deep link (`?document=`) is read once, from the location the app
// was loaded with. It is not part of where the app is, so the live location
// never holds it and the bar drops it on the first write.
function adopt(parsed: AppLocation): AppLocation {
  if (parsed.panel === 'documents' && parsed.route.view === 'browse' && parsed.route.documentId !== null) {
    return { panel: 'documents', route: { ...parsed.route, documentId: null } }
  }
  return parsed
}

export function createAppRouter(history: HistoryAdapter): AppRouter {
  const initialLocation = parseAppLocation(history.path())
  const location = adopt(initialLocation)
  let snapshot: RouterSnapshot = {
    location,
    pageId: location.panel === null ? location.pageId : null,
    pending: null,
    guardOwners: [],
  }
  const listeners = new Set<() => void>()
  const guards = new Set<() => NavigationGuard | null>()
  const memory = new Map<string, AppLocation>()
  const startKey = memoryKey(location)
  if (startKey !== null) memory.set(startKey, location)

  let context: RouterContext = { firstPageId: null, knownPageIds: null, canAdmin: true, shellVisible: false }
  // Whether the address has been reconciled with the location yet. The first
  // write rewrites the entry the app was loaded at rather than adding one, or
  // the first click after a clean deep link would replace its own entry.
  let initialised = false
  let previousFirstPageId: string | null = null
  // Held moves the operator has withdrawn: cancelled, or replaced by another
  // held move. Saving a guard can itself navigate (a New item lands on the
  // record it created), which clears `pending` without anyone withdrawing
  // the move, so "no longer pending" cannot stand for "abandoned".
  const withdrawn = new WeakSet<PendingNavigation>()
  // The held move whose guard is saving right now. A move the operator makes
  // meanwhile (Back, a page switch, a navigation nothing holds) replaces it;
  // only a forced move is the save's own (a create landing on its new record,
  // which is also why the force exists), so that one leaves it standing.
  let saving: PendingNavigation | null = null
  let activeSave: { pending: PendingNavigation; done: Promise<void> } | null = null
  function operatorMoved() {
    if (saving) withdrawn.add(saving)
  }

  function emit(next: Partial<RouterSnapshot>) {
    snapshot = { ...snapshot, ...next }
    for (const listener of [...listeners]) listener()
  }

  function refreshGuardOwners() {
    const owners: string[] = []
    for (const read of guards) {
      const guard = read()
      if (guard) owners.push(guard.owner)
    }
    emit({ guardOwners: owners })
  }

  // The one writer of the address. Push vs replace is decided here: a change
  // that corrects the location (`replaceRequested`), the first write, the
  // first page changing under a settled location, or a bar that was not
  // canonical to begin with all rewrite the entry; everything else is a new
  // place to go Back from.
  function writeAddress(replaceRequested: boolean) {
    if (!context.shellVisible) return
    const { location: current } = snapshot
    // /display/<slug> owns its own query string and never navigates anywhere
    // else: writing history for it would fight the device's fixed address for
    // no benefit to a screen with no back button.
    if (current.panel === 'display') return
    // Page structure not yet known: leave whatever deep link brought us here
    // alone rather than guessing at a page id that might still turn out valid.
    // The entry is still the one the app was loaded at, so it counts as
    // recorded: a move away before the list arrives must push, or Back would
    // leave the app.
    if (current.panel === null && context.knownPageIds === null) {
      initialised = true
      return
    }

    const first = !initialised
    initialised = true
    const firstPageChanged = previousFirstPageId !== context.firstPageId
    previousFirstPageId = context.firstPageId

    const next = formatAppLocation(current, context)
    const path = history.path()
    if (next === path) return
    const replace = replaceRequested || first || firstPageChanged || !isCanonicalAppPath(path, context)
    if (replace) history.replace(next)
    else history.push(next)
  }

  function commit(target: AppLocation, replaceRequested: boolean) {
    const next: AppLocation = target.panel === null
      ? { panel: null, pageId: target.pageId ?? snapshot.pageId }
      : target
    const key = memoryKey(next)
    // An unsaved watch has no address to come back to, so it is not remembered.
    const isDraft = next.panel === 'watches' && next.route.view === 'new'
    if (key !== null && !isDraft) memory.set(key, next)
    const pageId = next.panel === null ? next.pageId : snapshot.pageId
    snapshot = { ...snapshot, location: next, pageId, pending: null }
    writeAddress(replaceRequested)
    emit({})
  }

  function findBlock(from: AppLocation, to: AppLocation): PendingNavigation | null {
    for (const read of guards) {
      // A panel that has just stopped holding work may not have unregistered
      // yet (that happens in an effect), so a guard can read as gone.
      const guard = read()
      if (guard === null) continue
      const verdict = guard.blocks(from, to)
      if (verdict !== null) {
        return {
          target: to,
          replace: false,
          kind: verdict.kind,
          label: guard.label,
          detail: verdict.detail ?? null,
          save: guard.save ?? null,
        }
      }
    }
    return null
  }

  function navigate(target: AppLocation, options: NavigateOptions = {}): boolean {
    const resolved: AppLocation = target.panel === null
      ? { panel: null, pageId: target.pageId ?? snapshot.pageId }
      : target
    if (deepEqual(resolved, snapshot.location)) return true
    if (!options.force) {
      const held = findBlock(snapshot.location, resolved)
      if (held) {
        if (snapshot.pending) withdrawn.add(snapshot.pending)
        emit({ pending: { ...held, replace: options.replace === true } })
        return false
      }
    }
    // A replace is the app correcting where it already is (a panel following
    // a rename or a folder), not the operator going somewhere.
    if (!options.force && !options.replace) operatorMoved()
    commit(resolved, options.replace === true)
    return true
  }

  function handlePop() {
    if (!context.shellVisible) return
    if (snapshot.location.panel === 'display') return
    const path = history.path()
    let parsed = adopt(parseAppLocation(path))
    // A Back/Forward onto a path that is not canonical (a deleted page, a
    // Settings link without admin) is normalised in place first, so the entry
    // the operator lands on is one the app would have written itself.
    if (!isCanonicalAppPath(path, context)) {
      history.replace(formatAppLocation(parsed, context))
    }
    // A dashboard entry names a page the list may no longer hold. The page
    // list decides: an unknown or absent id is the first page.
    if (parsed.panel === null && context.knownPageIds !== null) {
      const known = parsed.pageId != null && context.knownPageIds.includes(parsed.pageId)
      parsed = { panel: null, pageId: known ? parsed.pageId : context.firstPageId }
    }
    const held = deepEqual(parsed, snapshot.location) ? null : findBlock(snapshot.location, parsed)
    if (held) {
      // Held: the browser has already moved off the page that is still on
      // screen, so write that page's address back as a new entry. The bar has
      // to agree with what is rendered while the dialog is up.
      if (snapshot.pending) withdrawn.add(snapshot.pending)
      emit({ pending: held })
      history.push(formatAppLocation(snapshot.location, context))
      return
    }
    operatorMoved()
    commit(parsed, false)
  }

  return {
    initialLocation,
    subscribe(listener) {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    getSnapshot: () => snapshot,
    start: () => history.listen(handlePop),
    setContext(next) {
      const unchanged = next.shellVisible === context.shellVisible
        && next.canAdmin === context.canAdmin
        && next.firstPageId === context.firstPageId
        && (next.knownPageIds === context.knownPageIds
          || (next.knownPageIds !== null && context.knownPageIds !== null
            && next.knownPageIds.length === context.knownPageIds.length
            && next.knownPageIds.every((id, i) => id === context.knownPageIds![i])))
      if (unchanged) return
      context = next
      writeAddress(false)
    },
    navigate,
    resume: ((panel: ResumablePanel) => {
      const key = panel ?? 'dashboard'
      const remembered = memory.get(key)
      // Mate always opens on an empty chat: the sidebar's Mate entry would
      // otherwise reopen whatever thread was last picked, forever. The
      // dashboard is the page last shown, which `navigate` fills in.
      if (panel === 'assistant' || panel === null || remembered === undefined) return startLocation(panel)
      return remembered
    }) as AppRouter['resume'],
    setPageId(pageId, options = {}) {
      if (snapshot.location.panel === null) {
        if (snapshot.location.pageId === pageId) return
        const next: AppLocation = { panel: null, pageId }
        if (!options.replace) operatorMoved()
        snapshot = { ...snapshot, location: next, pageId, pending: null }
        memory.set('dashboard', next)
        writeAddress(options.replace === true)
        emit({})
        return
      }
      if (snapshot.pageId === pageId) return
      emit({ pageId })
    },
    registerGuard(read) {
      guards.add(read)
      refreshGuardOwners()
      return () => {
        guards.delete(read)
        refreshGuardOwners()
      }
    },
    hasGuards() {
      for (const read of guards) if (read() !== null) return true
      return false
    },
    confirmPending(held) {
      const pending = held ?? snapshot.pending
      if (!pending) return
      commit(pending.target, pending.replace)
    },
    cancelPending() {
      if (!snapshot.pending) return
      withdrawn.add(snapshot.pending)
      emit({ pending: null })
    },
    async saveAndContinue() {
      const { pending } = snapshot
      if (!pending) return
      // A second press while the save for this same move runs joins it: one
      // save, one decision about the move.
      if (activeSave && activeSave.pending === pending) return activeSave.done
      const done = runSave(pending)
      activeSave = { pending, done }
      try {
        await done
      } finally {
        if (activeSave?.done === done) activeSave = null
      }
    },
  }

  async function runSave(pending: PendingNavigation): Promise<void> {
    saving = pending
    try {
      await pending.save?.()
    } finally {
      saving = null
    }
    // Cancelled, replaced by another held move, or overtaken by a move the
    // operator made elsewhere while the save ran: only the save stands. A
    // move the save itself cleared by navigating is still wanted.
    if (withdrawn.has(pending)) return
    commit(pending.target, pending.replace)
  }
}
