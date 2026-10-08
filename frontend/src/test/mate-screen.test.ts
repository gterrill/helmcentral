import { describe, it, expect } from 'vitest'
import { screenContextFor } from '@/lib/mate-screen'

// ADR 0093 voice phase: screenContextFor turns the shell's current
// AppLocation (plus the active dashboard page's name, which AppLocation
// doesn't carry) into the `screen` object sent with a Mate question.

describe('screenContextFor', () => {
  it('names a plain panel, such as Forecast', () => {
    expect(screenContextFor({ panel: 'forecast' }, null)).toEqual({ panel: 'forecast' })
  })

  it('names the Assistant panel itself', () => {
    expect(screenContextFor({ panel: 'assistant' }, null)).toEqual({ panel: 'assistant' })
  })

  it('names the settings section', () => {
    expect(screenContextFor({ panel: 'settings', section: 'anchor-watch' }, null)).toEqual({
      panel: 'settings',
      section: 'anchor-watch',
    })
  })

  it('defaults the settings section to general when the location omits it', () => {
    expect(screenContextFor({ panel: 'settings' }, null)).toEqual({ panel: 'settings', section: 'general' })
  })

  it('names the active dashboard page when on the dashboard', () => {
    expect(screenContextFor({ panel: null }, 'Anchored')).toEqual({ page: 'Anchored' })
  })

  it('returns an empty context on the dashboard before any page name is known', () => {
    expect(screenContextFor({ panel: null }, null)).toEqual({})
  })

  it('names the document open on its Details page', () => {
    expect(screenContextFor({ panel: 'documents', documentEditId: 'doc-1' }, null, 'Tide tables')).toEqual({
      panel: 'documents',
      document_id: 'doc-1',
      document_title: 'Tide tables',
    })
  })

  it('sends the document id alone while its title has not loaded', () => {
    expect(screenContextFor({ panel: 'documents', documentEditId: 'doc-1' }, null, null)).toEqual({
      panel: 'documents',
      document_id: 'doc-1',
    })
  })

  it('does not name a document on the ordinary Documents listing', () => {
    expect(screenContextFor({ panel: 'documents' }, null, 'Stale title')).toEqual({ panel: 'documents' })
  })
})
