/**
 * The app router against the in-memory history (ADR 0171): no window, no
 * React. The App navigation tests prove the wiring end to end; this file
 * pins the rules the router itself owns - when it pushes and when it
 * replaces, what a popstate does, how a guard holds a move and what the
 * address reads while it does.
 */
import { describe, it, expect, vi } from 'vitest'
import type { AppLocation } from '@/lib/app-location'
import {
  createAppRouter,
  memoryHistory,
  type AppRouter,
  type MemoryHistory,
  type NavigationGuard,
  type RouterContext,
} from '@/lib/app-router'

const READY: RouterContext = { firstPageId: 'p1', knownPageIds: ['p1', 'p2'], canAdmin: true, shellVisible: true }

function setup(path = '/', context: RouterContext | null = READY): { router: AppRouter; history: MemoryHistory } {
  const history = memoryHistory(path)
  const router = createAppRouter(history)
  router.start()
  if (context) router.setContext(context)
  return { router, history }
}

const forecast: AppLocation = { panel: 'forecast' }
const documentsRoot: AppLocation = { panel: 'documents', route: { view: 'browse', folderId: null, sectionId: null, documentId: null } }
const details = (id: string, folderId: string | null = null): AppLocation => ({
  panel: 'documents', route: { view: 'details', id, folderId, sectionId: null },
})
const settings = (section: 'general' | 'signalk'): AppLocation => ({ panel: 'settings', route: { view: 'section', section } })

function guard(overrides: Partial<NavigationGuard> = {}): NavigationGuard {
  return {
    owner: 'test',
    label: 'Test',
    blocks: (_from, to) => (to.panel === 'forecast' ? null : { kind: 'dirty' }),
    ...overrides,
  }
}

describe('the starting location', () => {
  it('is parsed from the address', () => {
    const { router } = setup('/documents?folder=f1')
    expect(router.getSnapshot().location).toEqual({
      panel: 'documents', route: { view: 'browse', folderId: 'f1', sectionId: null, documentId: null },
    })
  })

  it('keeps a ?document= deep link in initialLocation but not in the live location', () => {
    const { router } = setup('/documents?document=d1', null)
    expect(router.initialLocation).toEqual({
      panel: 'documents', route: { view: 'browse', folderId: null, sectionId: null, documentId: 'd1' },
    })
    expect(router.getSnapshot().location).toEqual(documentsRoot)
  })

  it('remembers the dashboard page the address names', () => {
    expect(setup('/dashboard/p2').router.getSnapshot().pageId).toBe('p2')
    expect(setup('/forecast').router.getSnapshot().pageId).toBeNull()
  })

  it('carries the wall display slug and its query options', () => {
    const { router } = setup('/display/flybridge?page=p2&preview=1', null)
    expect(router.getSnapshot().location).toEqual({
      panel: 'display', slug: 'flybridge', options: { pageId: 'p2', preview: true },
    })
  })
})

describe('navigate: push or replace', () => {
  it('replaces, not pushes, when the page list lands empty and rewrites a stale deep link', () => {
    const { router, history } = setup('/?page=old', { ...READY, firstPageId: null, knownPageIds: null })
    router.setContext({ ...READY, firstPageId: null, knownPageIds: [] })
    expect(history.entries).toEqual(['/'])
    expect(router.getSnapshot().location).toEqual({ panel: null, pageId: null })
  })

  it('replaces the stale dashboard address when the page list lands without it', () => {
    const { router, history } = setup('/dashboard/old', { ...READY, firstPageId: null, knownPageIds: null })
    router.setContext({ ...READY, firstPageId: 'p1', knownPageIds: ['p1'] })
    expect(history.entries).toHaveLength(1)
  })

  it('pushes the first move away from a dashboard opened while pages are still loading', () => {
    const { router, history } = setup('/dashboard/p2', { ...READY, firstPageId: null, knownPageIds: null })
    router.navigate(forecast)
    expect(history.entries).toEqual(['/dashboard/p2', '/forecast'])
  })

  it('pushes a new entry for a move to somewhere else', () => {
    const { router, history } = setup('/')
    router.setContext(READY) // the first write is spent on settling the opening entry
    router.navigate(forecast)
    expect(history.entries).toEqual(['/', '/forecast'])
    expect(router.getSnapshot().location).toEqual(forecast)
  })

  it('replaces the entry when asked, as a correction does', () => {
    const { router, history } = setup('/')
    router.navigate(forecast, { replace: true })
    expect(history.entries).toEqual(['/forecast'])
  })

  it('does nothing for a move to where the app already is', () => {
    const { router, history } = setup('/forecast')
    expect(router.navigate({ panel: 'forecast' })).toBe(true)
    expect(history.entries).toEqual(['/forecast'])
  })

  it('replaces for the first write, so the first click after a clean deep link does not eat its own entry', () => {
    const { router, history } = setup('/forecast', null)
    router.navigate({ panel: 'radar' })
    expect(history.entries).toEqual(['/forecast']) // not ready: location moves, the address waits
    router.setContext(READY)
    expect(history.entries).toEqual(['/radar']) // the first write rewrites the opening entry
    router.navigate(forecast)
    expect(history.entries).toEqual(['/radar', '/forecast'])
  })

  it('writes nothing until the shell is visible', () => {
    const { router, history } = setup('/settings', { ...READY, shellVisible: false, canAdmin: false })
    expect(history.entries).toEqual(['/settings'])
    router.setContext(READY)
    expect(history.entries).toEqual(['/settings'])
  })

  it('replaces rather than pushes when the entry it leaves was not canonical', () => {
    const { router, history } = setup('/dashboard/p1')
    // /dashboard/p1 is the first page spelled out: the app rewrites it to /
    expect(history.entries).toEqual(['/'])
    router.navigate(forecast)
    expect(history.entries).toEqual(['/', '/forecast'])
  })

  it('rewrites the opening entry when the first page turns out to differ from the loaded one', () => {
    const { router, history } = setup('/dashboard/p2', { ...READY, firstPageId: 'p2', knownPageIds: ['p2', 'p1'] })
    expect(history.entries).toEqual(['/'])
    router.setContext({ ...READY, firstPageId: 'p1', knownPageIds: ['p2', 'p1'] })
    expect(history.entries).toEqual(['/dashboard/p2'])
  })

  it('leaves a dashboard deep link alone while the page list is still loading', () => {
    const { history } = setup('/dashboard/zzz', { ...READY, firstPageId: null, knownPageIds: null })
    expect(history.entries).toEqual(['/dashboard/zzz'])
  })

  it('formats the New item draft as the Equipment index, without a second entry', () => {
    const { router, history } = setup('/inventory')
    router.navigate({ panel: 'inventory', route: { view: 'equipment-new', preset: null } })
    expect(history.path()).toBe('/inventory')
    expect(history.entries).toEqual(['/inventory'])
    expect(router.getSnapshot().location).toEqual({ panel: 'inventory', route: { view: 'equipment-new', preset: null } })
  })

  it('notifies subscribers on a move and not on a no-op', () => {
    const { router } = setup('/')
    const listener = vi.fn()
    router.subscribe(listener)
    router.navigate(forecast)
    expect(listener).toHaveBeenCalled()
    listener.mockClear()
    router.navigate(forecast)
    expect(listener).not.toHaveBeenCalled()
  })
})

describe('the dashboard page', () => {
  it('keeps the page last shown when the dashboard is reopened bare', () => {
    const { router } = setup('/dashboard/p2')
    router.navigate(forecast)
    router.navigate(router.resume(null))
    expect(router.getSnapshot().location).toEqual({ panel: null, pageId: 'p2' })
  })

  it('setPageId pushes on the dashboard and replaces when asked', () => {
    const { router, history } = setup('/')
    router.setPageId('p2')
    expect(history.entries).toEqual(['/', '/dashboard/p2'])
    router.setPageId('p1', { replace: true })
    expect(history.entries).toEqual(['/', '/'])
  })

  it('setPageId on another panel remembers the page and touches no history', () => {
    const { router, history } = setup('/forecast')
    const before = [...history.entries]
    router.setPageId('p2')
    expect(history.entries).toEqual(before)
    expect(router.getSnapshot().pageId).toBe('p2')
    expect(router.getSnapshot().location).toEqual(forecast)
  })

  it('on the wall display the rotation changes the page without touching history', () => {
    const { router, history } = setup('/display/flybridge', null)
    router.setContext(READY)
    router.setPageId('p2')
    router.setPageId('p1')
    expect(history.entries).toEqual(['/display/flybridge'])
    expect(router.getSnapshot().pageId).toBe('p1')
  })

  it('never writes history for the wall display, even when told to navigate', () => {
    const { router, history } = setup('/display/flybridge', null)
    router.setContext(READY)
    router.navigate({ panel: 'display', slug: 'other', options: { pageId: null, preview: false } })
    expect(history.entries).toEqual(['/display/flybridge'])
  })
})

describe('resume', () => {
  it('returns a panel to where it was left', () => {
    const { router } = setup('/')
    router.navigate(settings('signalk'))
    router.navigate(forecast)
    expect(router.resume('settings')).toEqual(settings('signalk'))
  })

  it('starts a panel never opened at its first view', () => {
    const { router } = setup('/')
    expect(router.resume('settings')).toEqual(settings('general'))
    expect(router.resume('inventory')).toEqual({ panel: 'inventory', route: { view: 'index', section: 'equipment' } })
    expect(router.resume('forecast')).toEqual(forecast)
  })

  it('always opens Mate on an empty chat', () => {
    const { router } = setup('/mate/thread-1')
    router.navigate(forecast)
    expect(router.resume('assistant')).toEqual({ panel: 'assistant', conversationId: null })
  })

  it('remembers a watch across a visit elsewhere but never an unsaved draft of one', () => {
    const { router } = setup('/')
    router.navigate({ panel: 'watches', route: { view: 'details', id: 'w-1' } })
    router.navigate({ panel: 'watches', route: { view: 'new', template: 'morning' } })
    router.navigate(forecast)
    expect(router.resume('watches')).toEqual({ panel: 'watches', route: { view: 'details', id: 'w-1' } })
  })

  it('starts Watches at its index', () => {
    const { router } = setup('/')
    expect(router.resume('watches')).toEqual({ panel: 'watches', route: { view: 'index' } })
  })

  it('remembers the Details page of Documents across a visit elsewhere', () => {
    const { router } = setup('/documents/doc-1?folder=f1')
    router.navigate(forecast)
    expect(router.resume('documents')).toEqual(details('doc-1', 'f1'))
  })
})

describe('Back and Forward', () => {
  it('applies the address it lands on', () => {
    const { router, history } = setup('/')
    router.navigate(forecast)
    history.back()
    expect(router.getSnapshot().location).toEqual({ panel: null, pageId: 'p1' })
    history.forward()
    expect(router.getSnapshot().location).toEqual(forecast)
  })

  it('does not write a second entry for an address it only applied', () => {
    const { router, history } = setup('/')
    router.navigate(forecast)
    history.back()
    expect(history.entries).toEqual(['/', '/forecast'])
    expect(history.index).toBe(0)
  })

  it('sends a dashboard entry whose page is gone to the first page, and normalises the entry', () => {
    const { router, history } = setup('/')
    router.navigate(forecast)
    history.replace('/dashboard/zzz')
    history.go(0)
    expect(router.getSnapshot().location).toEqual({ panel: null, pageId: 'p1' })
    expect(history.path()).toBe('/')
  })

  it('keeps the page on screen for a bare / while the page list is unknown, and the bar converges once it loads', () => {
    // Same as before the router: with the list unknown a bare dashboard entry
    // cannot be resolved, so the page last shown stays; the address is then
    // written as soon as the list settles.
    const { router, history } = setup('/forecast', { ...READY, knownPageIds: null })
    router.navigate({ panel: null, pageId: 'p2' })
    history.replace('/')
    history.go(0)
    expect(router.getSnapshot().location).toEqual({ panel: null, pageId: 'p2' })
    router.setContext(READY)
    expect(router.getSnapshot().location).toEqual({ panel: null, pageId: 'p2' })
    expect(history.path()).toBe('/dashboard/p2')
  })

  it('ignores Back/Forward before the shell is visible', () => {
    const { router, history } = setup('/', { ...READY, shellVisible: false })
    router.navigate(forecast)
    history.replace('/radar')
    history.go(0)
    expect(router.getSnapshot().location).toEqual(forecast)
  })

  it('ignores Back/Forward on the wall display', () => {
    const { router, history } = setup('/display/flybridge', null)
    router.setContext(READY)
    history.replace('/forecast')
    history.go(0)
    expect(router.getSnapshot().location.panel).toBe('display')
  })

  it('drops a ?document= deep link from the location it applies, and the bar follows', () => {
    const { router, history } = setup('/')
    router.navigate(forecast)
    history.replace('/documents?document=d1')
    history.go(0)
    expect(router.getSnapshot().location).toEqual(documentsRoot)
    expect(history.path()).toBe('/documents')
  })
})

describe('guards', () => {
  it('holds a move the guard blocks, and leaves the location and the bar alone', () => {
    const { router, history } = setup('/settings')
    router.registerGuard(() => guard({ owner: 'settings', label: 'Settings' }))
    expect(router.navigate({ panel: 'radar' })).toBe(false)
    expect(router.getSnapshot().location).toEqual(settings('general'))
    expect(history.path()).toBe('/settings')
    expect(router.getSnapshot().pending).toMatchObject({ kind: 'dirty', label: 'Settings', target: { panel: 'radar' } })
  })

  it('lets through a move the guard does not block', () => {
    const { router } = setup('/settings')
    router.registerGuard(() => guard())
    expect(router.navigate(forecast)).toBe(true)
    expect(router.getSnapshot().pending).toBeNull()
  })

  it('asks the guard with the move itself: where from, where to', () => {
    const { router } = setup('/settings')
    const blocks = vi.fn(() => null)
    router.registerGuard(() => guard({ blocks }))
    router.navigate(forecast)
    expect(blocks).toHaveBeenCalledWith(settings('general'), forecast)
  })

  it('force skips the guards', () => {
    const { router } = setup('/settings')
    router.registerGuard(() => guard())
    expect(router.navigate({ panel: 'radar' }, { force: true })).toBe(true)
    expect(router.getSnapshot().location).toEqual({ panel: 'radar' })
  })

  it('cancel drops the held move', () => {
    const { router } = setup('/settings')
    router.registerGuard(() => guard())
    router.navigate({ panel: 'radar' })
    router.cancelPending()
    expect(router.getSnapshot().pending).toBeNull()
    expect(router.getSnapshot().location).toEqual(settings('general'))
  })

  it('discard makes the held move, as a new entry', () => {
    const { router, history } = setup('/settings')
    router.registerGuard(() => guard())
    router.navigate({ panel: 'radar' })
    router.confirmPending()
    expect(router.getSnapshot().location).toEqual({ panel: 'radar' })
    expect(router.getSnapshot().pending).toBeNull()
    expect(history.entries).toEqual(['/settings', '/radar'])
  })

  it('captures the wording when the move is held, not when the dialog reads it', () => {
    const { router } = setup('/inventory/bins/LAZ-02')
    let detail: string | null = 'one photo is still queued'
    router.registerGuard(() => guard({ blocks: () => ({ kind: 'quick-add-work', detail }) }))
    router.navigate(forecast)
    detail = null
    expect(router.getSnapshot().pending).toMatchObject({ kind: 'quick-add-work', detail: 'one photo is still queued' })
  })

  it('Save and Continue saves through the guard, then makes the move', async () => {
    const { router } = setup('/settings')
    const save = vi.fn().mockResolvedValue(undefined)
    router.registerGuard(() => guard({ save }))
    router.navigate({ panel: 'radar' })
    await router.saveAndContinue()
    expect(save).toHaveBeenCalledTimes(1)
    expect(router.getSnapshot().location).toEqual({ panel: 'radar' })
    expect(router.getSnapshot().pending).toBeNull()
  })

  it('Save and Continue does not make a move that was cancelled during the save', async () => {
    const { router } = setup('/settings')
    let finish: () => void = () => {}
    const save = vi.fn(() => new Promise<void>((resolve) => { finish = resolve }))
    router.registerGuard(() => guard({ save }))
    router.navigate({ panel: 'radar' })
    const saving = router.saveAndContinue()
    router.cancelPending()
    finish()
    await saving
    expect(router.getSnapshot().location).toEqual(settings('general'))
  })

  it('Save and Continue still makes the move when the save itself navigated', async () => {
    const { router } = setup('/settings')
    // A New item's save lands on the created record with a forced move, which
    // clears the held one; the operator still asked to go to Radar.
    const save = vi.fn(async () => { router.navigate(forecast, { force: true }) })
    router.registerGuard(() => guard({ save }))
    router.navigate({ panel: 'radar' })
    await router.saveAndContinue()
    expect(router.getSnapshot().location).toEqual({ panel: 'radar' })
  })

  describe('Save and Continue when the operator moves elsewhere during the save', () => {
    function holdWithSlowSave(path = '/settings') {
      const ctx = setup(path)
      let finish: () => void = () => {}
      const save = vi.fn(() => new Promise<void>((resolve) => { finish = resolve }))
      ctx.router.registerGuard(() => guard({ save }))
      return { ...ctx, save, finish: () => finish() }
    }

    it('keeps the operator where Back took them', async () => {
      const { router, history } = setup('/forecast')
      router.navigate(settings('general'))
      let finish: () => void = () => {}
      router.registerGuard(() => guard({ save: () => new Promise<void>((resolve) => { finish = resolve }) }))
      router.navigate({ panel: 'radar' })
      const saving = router.saveAndContinue()
      history.back()
      finish()
      await saving
      expect(router.getSnapshot().location).toEqual(forecast)
    })

    it('does not count a replace-correction as the operator moving', async () => {
      const { router } = setup('/settings')
      let finish: () => void = () => {}
      router.registerGuard(() => guard({
        blocks: (_from, to) => (to.panel === 'radar' ? { kind: 'dirty' } : null),
        save: () => new Promise<void>((resolve) => { finish = resolve }),
      }))
      router.navigate({ panel: 'radar' })
      const saving = router.saveAndContinue()
      // e.g. a panel rewriting its own address while the save runs
      router.navigate(settings('signalk'), { replace: true })
      finish()
      await saving
      expect(router.getSnapshot().location).toEqual({ panel: 'radar' })
    })

    it('runs one save for a double-click, and the move still respects a Back made meanwhile', async () => {
      const { router, history } = setup('/forecast')
      router.navigate(settings('general'))
      let finish: () => void = () => {}
      const save = vi.fn(() => new Promise<void>((resolve) => { finish = resolve }))
      router.registerGuard(() => guard({ save }))
      router.navigate({ panel: 'radar' })
      const first = router.saveAndContinue()
      const second = router.saveAndContinue()
      history.back()
      finish()
      await Promise.all([first, second])
      expect(save).toHaveBeenCalledTimes(1)
      expect(router.getSnapshot().location).toEqual(forecast)
    })

    it('keeps a move the guard no longer holds', async () => {
      const { router, finish } = holdWithSlowSave()
      router.navigate({ panel: 'radar' })
      const saving = router.saveAndContinue()
      router.navigate(forecast)
      finish()
      await saving
      expect(router.getSnapshot().location).toEqual(forecast)
    })
  })

  it('Save and Continue drops a move replaced by another held move during the save', async () => {
    const { router } = setup('/settings')
    let finish: () => void = () => {}
    const save = vi.fn(() => new Promise<void>((resolve) => { finish = resolve }))
    router.registerGuard(() => guard({ save }))
    router.navigate({ panel: 'radar' })
    const saving = router.saveAndContinue()
    router.navigate({ panel: 'assistant', conversationId: null })
    finish()
    await saving
    expect(router.getSnapshot().location).toEqual(settings('general'))
    expect(router.getSnapshot().pending?.target).toEqual({ panel: 'assistant', conversationId: null })
  })

  it('Save and Continue stays put, still holding the move, when the save fails', async () => {
    const { router } = setup('/settings')
    router.registerGuard(() => guard({ save: vi.fn().mockRejectedValue(new Error('title already used')) }))
    router.navigate({ panel: 'radar' })
    await expect(router.saveAndContinue()).rejects.toThrow('title already used')
    expect(router.getSnapshot().location).toEqual(settings('general'))
    expect(router.getSnapshot().pending).not.toBeNull()
  })

  it('stops holding once the guard is unregistered', () => {
    const { router } = setup('/settings')
    const unregister = router.registerGuard(() => guard())
    expect(router.hasGuards()).toBe(true)
    unregister()
    expect(router.hasGuards()).toBe(false)
    expect(router.navigate({ panel: 'radar' })).toBe(true)
  })

  it('lists the owners of the registered guards', () => {
    const { router } = setup('/settings')
    const unregister = router.registerGuard(() => guard({ owner: 'settings' }))
    expect(router.getSnapshot().guardOwners).toEqual(['settings'])
    unregister()
    expect(router.getSnapshot().guardOwners).toEqual([])
  })

  it('treats a guard that has just stopped holding work as absent', () => {
    const { router } = setup('/settings')
    router.registerGuard(() => null)
    expect(router.hasGuards()).toBe(false)
    expect(router.navigate({ panel: 'radar' })).toBe(true)
  })
})

describe('Back and Forward against a guard', () => {
  it('holds the move, and rolls the bar back to the page still on screen', () => {
    const { router, history } = setup('/documents/doc-1')
    router.navigate(forecast) // leave Details, then come back to it
    router.navigate(details('doc-1'), { force: true })
    router.registerGuard(() => guard({ blocks: (_from, to) => (to.panel === 'documents' && to.route.view === 'details' ? null : { kind: 'dirty' }) }))
    history.replace('/documents')
    history.go(0)
    expect(router.getSnapshot().location).toEqual(details('doc-1'))
    expect(router.getSnapshot().pending).toMatchObject({ kind: 'dirty', target: documentsRoot })
    // Written back as a new entry, with the one formatter.
    expect(history.path()).toBe('/documents/doc-1')
  })

  it('Discard then completes the Back: the bar goes to where Back was headed', () => {
    const { router, history } = setup('/documents/doc-1')
    router.registerGuard(() => guard({ blocks: (_from, to) => (to.panel === 'documents' && to.route.view === 'details' ? null : { kind: 'dirty' }) }))
    history.replace('/documents')
    history.go(0)
    router.confirmPending()
    expect(router.getSnapshot().location).toEqual(documentsRoot)
    expect(history.path()).toBe('/documents')
  })

  it('Cancel leaves the page and the rolled-back bar as they are', () => {
    const { router, history } = setup('/documents/doc-1')
    router.registerGuard(() => guard({ blocks: () => ({ kind: 'dirty' }) }))
    history.replace('/documents')
    history.go(0)
    router.cancelPending()
    expect(router.getSnapshot().location).toEqual(details('doc-1'))
    expect(history.path()).toBe('/documents/doc-1')
  })

  it('rolls back with the section a manual had open, which the old rollback left out', () => {
    const { router, history } = setup('/documents?folder=f1&section=s1')
    router.registerGuard(() => guard({ blocks: () => ({ kind: 'dirty' }) }))
    history.replace('/forecast')
    history.go(0)
    expect(history.path()).toBe('/documents?folder=f1&section=s1')
    expect(router.getSnapshot().location.panel).toBe('documents')
  })
})
