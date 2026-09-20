/**
 * Table-driven tests for resolveNoteHref (ADR 0116). Pure module, no React -
 * mirrors app-location.test.ts and help-links.test.ts's resolveHelpHref
 * suite in shape.
 */
import { describe, it, expect } from 'vitest'
import { resolveNoteHref, type NoteLink } from '@/lib/note-links'
import { slugifyHeading as slugifyFromHelpLinks } from '@/lib/help-links'
import { slugifyHeading as slugifyFromMarkdownLinks } from '@/lib/markdown-links'

// A genuine v4 UUID, matching ADR 0116's own worked frontmatter example -
// version nibble "4", variant nibble "9".
const UUID = '0f3b1c2e-8a4d-4f21-9c33-1d2e3f4a5b6c'

const cases: Array<[string, string, NoteLink]> = [
  // Bare #hash -> an anchor on the note being viewed.
  ['a bare #hash', '#nearby-map', { kind: 'anchor', hash: 'nearby-map' }],
  ['a bare #hash with no heading text', '#', { kind: 'anchor', hash: '' }],

  // hc-note: -> another note, with or without a heading hash.
  ['hc-note: with no hash', `hc-note:${UUID}`, { kind: 'note', id: UUID }],
  [
    'hc-note: with a hash',
    `hc-note:${UUID}#warm-start`,
    { kind: 'note', id: UUID, hash: 'warm-start' },
  ],

  // hc-doc: -> a document (almost always a photo per ADR 0116).
  ['hc-doc: with a valid uuid', `hc-doc:${UUID}`, { kind: 'document', id: UUID }],

  // Allow-listed schemes -> external, href unchanged.
  ['an https URL', 'https://wazero.io/', { kind: 'external', url: 'https://wazero.io/' }],
  ['an http URL', 'http://example.com/x', { kind: 'external', url: 'http://example.com/x' }],
  [
    'a mailto URL',
    'mailto:skipper@example.com',
    { kind: 'external', url: 'mailto:skipper@example.com' },
  ],

  // Outside the allow-list, including a same-origin relative path: nothing
  // renderable. tel: is on it, for a contact note's phone number.
  ['a relative path', 'photo.jpg', { kind: 'unsafe' }],
  ['a tel: URL', 'tel:+642112345', { kind: 'external', url: 'tel:+642112345' }],

  // Rejects: nothing here may resolve to a usable link.
  ['a javascript: URL', 'javascript:alert(1)', { kind: 'unsafe' }],
  ['a data: URL', 'data:text/html,<script>alert(1)</script>', { kind: 'unsafe' }],
  ['a vbscript: URL', 'vbscript:msgbox(1)', { kind: 'unsafe' }],
  ['a protocol-relative URL', '//evil.example/x', { kind: 'unsafe' }],
  ['hc-note: with a non-UUID', 'hc-note:not-a-uuid', { kind: 'unsafe' }],
  [
    'hc-doc: with uppercase hex',
    `hc-doc:${UUID.toUpperCase()}`,
    { kind: 'unsafe' },
  ],
  ['an empty string', '', { kind: 'unsafe' }],

  // Bonus: whitespace/control characters inside the scheme are a known
  // javascript: bypass ("java\tscript:" still reads as "javascript:" to a
  // browser) - resolveNoteHref strips them before the safety check, so this
  // must not slip through as external.
  ['a javascript: URL with a tab hiding the scheme', 'java\tscript:alert(1)', { kind: 'unsafe' }],
]

describe('resolveNoteHref', () => {
  it.each(cases)('resolves %s correctly', (_description, href, expected) => {
    expect(resolveNoteHref(href)).toEqual(expected)
  })
})

describe('slugifyHeading re-export', () => {
  it('behaves identically whether imported from help-links or markdown-links', () => {
    for (const text of ['The kiosk feed', 'Battery & Power', '2. Configure it in Helmcentral']) {
      expect(slugifyFromHelpLinks(text)).toBe(slugifyFromMarkdownLinks(text))
    }
  })
})

// Added on review: the catch-all used to pass any non-dangerous href through
// as `external`, which made a relative link like `foo.md` renderable and
// would have navigated the dashboard to a path that does not exist. The
// resolver now runs an allow-list, so these pin both halves of that change.
describe('resolveNoteHref - scheme allow-list', () => {
  it.each([
    ['tel:+15615559999', 'tel: is allowed, because contact is a note type'],
    ['TEL:+15615559999', 'scheme matching is case-insensitive'],
    ['HTTPS://example.com/x', 'so is https'],
  ])('allows %s (%s)', (href) => {
    expect(resolveNoteHref(href)).toEqual({ kind: 'external', url: href })
  })

  it.each([
    ['foo.md', 'a relative path would navigate the app to nowhere'],
    ['../sibling.md', 'so would a parent-relative one'],
    ['/api/documents', 'and an absolute same-origin path'],
    ['ftp://example.com/x', 'an unrecognised scheme is refused, not passed through'],
    ['file:///etc/passwd', 'file: is not on the allow-list'],
  ])('refuses %s (%s)', (href) => {
    expect(resolveNoteHref(href)).toEqual({ kind: 'unsafe' })
  })
})
