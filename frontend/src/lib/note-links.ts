// ADR 0116: links inside note markdown. Pure and React-free, like
// help-links.ts and app-location.ts - it turns a markdown href into where
// the note viewer should send the operator, with no react-markdown, no DOM
// API and nothing that isn't testable as plain data.
//
// A note's own pseudo-schemes, `hc-note:<uuid>` and `hc-doc:<uuid>`, carry no
// host, no path and no query - only a UUID, matched against an anchored
// pattern before it is ever used to build a real request. That is what keeps
// `![Fuel manifold](hc-doc:<uuid>)` from being a string an attacker (or a
// hallucinating Mate) can turn into an outbound request to a host of their
// choosing; see ADR 0116's "Photographs and links carry no host" and
// ADR 0111's threat model, which this mirrors for the same reason.

/**
 * Where a link found inside a rendered note's markdown should go.
 *
 * `unsafe` is the fourth variant promised by that host-smuggling concern
 * above: a plain "don't render this" outcome for anything that could
 * execute or leave the origin unexpectedly (a `javascript:`/`data:`/
 * `vbscript:` scheme, a protocol-relative `//host` URL, or garbage after our
 * own `hc-note:`/`hc-doc:` prefix). It carries no `url` field on purpose -
 * unlike `external`, there is no string here a careless caller could still
 * interpolate into an `href` or `src`. A caller that switches on `kind` has
 * nothing to render for `unsafe` but plain text (or the link's own visible
 * label, the markdown-link equivalent of ADR 0116's "photo missing"
 * placeholder), which is the point.
 */
export type NoteLink =
  | { kind: 'anchor'; hash: string }
  | { kind: 'note'; id: string; hash?: string }
  | { kind: 'document'; id: string }
  | { kind: 'external'; url: string }
  | { kind: 'unsafe' }

// Anchored, lowercase-hex, exact v4 shape: 8-4-4-4-12 hex digits, the third
// group's leading nibble fixed to "4" (the version) and the fourth group's
// leading nibble restricted to 8/9/a/b (the variant). No lookbehind -
// check-entry-chunk.mjs fails the production build on one, because the
// wall-display kiosk's WebKit (ADR 0088/0110) can't parse that syntax, and
// this module ships in the same entry chunk help-links.ts does.
const UUID = '[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}'
const NOTE_HREF = new RegExp(`^hc-note:(${UUID})(#(.+))?$`)
const DOC_HREF = new RegExp(`^hc-doc:(${UUID})$`)

// The three schemes that run script or load an arbitrarily large payload
// directly from the href itself, rather than merely naming a destination -
// see the type doc above. Checked case-insensitively and after stripping
// whitespace (below), because both are real bypasses of a naive prefix
// check, not hypothetical ones.
const DANGEROUS_SCHEME = /^(javascript|data|vbscript):/

// The only schemes a note may link out with. See resolveNoteHref's comment
// on why this is an allow-list.
const SAFE_SCHEME = /^(https?:|mailto:|tel:)/i

// Browsers strip ASCII tab/newline/CR from a URL before parsing its scheme,
// so "java\tscript:alert(1)" reads as "javascript:" to the address bar even
// though `href.startsWith('javascript:')` would miss it. Used only to decide
// whether href is safe; a safe href is still returned to the caller exactly
// as written, never this normalised form - "everything else resolves to
// external with the href passed through unchanged" means unchanged.
function normalizeForSafetyCheck(href: string): string {
  return href.replace(/[\t\n\r]/g, '').trim().toLowerCase()
}

// True for anything that could execute or leave the origin unexpectedly: an
// empty (or whitespace-only) href, a dangerous scheme, or a protocol-relative
// `//host` URL, which a browser resolves against the current protocol to a
// different host without ever spelling out "http(s):".
function isUnsafe(href: string): boolean {
  const normalized = normalizeForSafetyCheck(href)
  return normalized === '' || DANGEROUS_SCHEME.test(normalized) || normalized.startsWith('//')
}

// Resolves a markdown link's `href`, found inside a note's body, to where the
// note viewer should send the operator: another note, a document (almost
// always a photo, per ADR 0116), an anchor on the same note, an external URL,
// or nothing renderable at all.
export function resolveNoteHref(href: string): NoteLink {
  // hc-note:/hc-doc: are ours - anything after the prefix that isn't exactly
  // one of our own UUIDs is refused outright rather than falling through to
  // the generic external case below. A pseudo-scheme no browser recognises
  // is not a useful external link even when it isn't a dangerous one, and
  // silently rendering "hc-note:not-a-uuid" as though it were a real href
  // would hide the authoring mistake instead of surfacing it.
  if (href.startsWith('hc-note:')) {
    const match = NOTE_HREF.exec(href)
    if (!match) return { kind: 'unsafe' }
    const [, id, , hash] = match
    return hash ? { kind: 'note', id, hash } : { kind: 'note', id }
  }

  if (href.startsWith('hc-doc:')) {
    const match = DOC_HREF.exec(href)
    return match ? { kind: 'document', id: match[1] } : { kind: 'unsafe' }
  }

  if (href.startsWith('#')) return { kind: 'anchor', hash: href.slice(1) }

  // An allow-list, not a deny-list, and the difference is the whole point:
  // a deny-list stays safe only for as long as every dangerous scheme is
  // remembered and kept current, while an allow-list fails safe by
  // construction for whatever nobody thought of. Same reasoning as ADR
  // 0106's documentInlineMIMEAllowList.
  //
  // tel: earns its place because `contact` is a first-class note type - a
  // yard manager's number should be tappable from a helm touchscreen, not
  // copied out by hand. A relative href (`foo.md`, `../x`) is deliberately
  // NOT here: it is same-origin, so not dangerous, but it would navigate the
  // dashboard away to a path that does not exist. Refusing it surfaces the
  // authoring mistake instead of shipping a dead link.
  if (SAFE_SCHEME.test(href) && !isUnsafe(href)) return { kind: 'external', url: href }

  return { kind: 'unsafe' }
}
