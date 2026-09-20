import type { MouseEvent } from 'react'
import ReactMarkdown, { type Components, type ExtraProps } from 'react-markdown'
import remarkGfm from 'remark-gfm'

import { assistantMarkdownComponents } from '@/components/assistant-markdown-impl'
import { apiBaseUrl } from '@/config/api'
import { slugifyHeading } from '@/lib/markdown-links'
import { resolveNoteHref, type NoteLink } from '@/lib/note-links'

// Reads the plain text out of a heading's hast node - identical to
// help-markdown-impl.tsx's own headingText, duplicated rather than shared
// for the same reason that file gives: react-markdown v10 always passes
// `node` (toJsxRuntime's hardcoded `passNode: true`), and this is a small
// enough hast walk that owning a second copy here is cheaper than exporting
// a help-sheet internal into a module that, per ADR 0116, must not import
// anything from the help tree at all (the wrong dependency direction - a
// note is not help content).
function headingText(node: unknown): string {
  if (node && typeof node === 'object') {
    const record = node as { type?: unknown; value?: unknown; children?: unknown }
    if (record.type === 'text' && typeof record.value === 'string') return record.value
    if (Array.isArray(record.children)) return record.children.map(headingText).join('')
  }
  return ''
}

// Same four-heading style table as help-markdown-impl.tsx, copied rather
// than imported so a note reads in the exact same visual language as a help
// page and a Mate reply (ADR 0093's prose language) without this module
// reaching into help-markdown-impl.tsx for it - see the module comment on
// why that import direction is the wrong one for a notes file.
const HEADING_STYLE: Record<'h1' | 'h2' | 'h3' | 'h4', { tag: 'h3' | 'h4'; className: string }> = {
  h1: { tag: 'h3', className: 'mb-2 mt-6 text-lg font-semibold leading-snug text-foreground first:mt-0' },
  h2: { tag: 'h3', className: 'mb-2 mt-6 text-base font-semibold leading-snug text-foreground first:mt-0' },
  h3: { tag: 'h4', className: 'mb-1.5 mt-5 text-sm font-semibold leading-snug text-foreground first:mt-0' },
  h4: { tag: 'h4', className: 'mb-1.5 mt-5 text-sm font-semibold leading-snug text-foreground first:mt-0' },
}

function headingComponent(level: keyof typeof HEADING_STYLE) {
  const { tag: Tag, className } = HEADING_STYLE[level]
  return function Heading({ node, children }: React.PropsWithChildren<ExtraProps>) {
    return (
      <Tag id={slugifyHeading(headingText(node))} className={className}>
        {children}
      </Tag>
    )
  }
}

interface NoteMarkdownImplProps {
  content: string
  onNavigate: (link: NoteLink) => void
}

/**
 * Renders one note's body (plan "Notes and the Boat's Manual" §5/§6, ADR
 * 0116), reusing assistant-markdown-impl.tsx's whole visual language the
 * same way help-markdown-impl.tsx does, plus the two things a note needs
 * that a help page and a chat reply don't: headings carry a slug id (a long
 * note's own table of contents, computed at read time, reuses these - a
 * later phase), and an `img` is permitted for exactly one src shape.
 *
 * Trust boundary, stated once here because it is the whole reason this file
 * exists as a second renderer rather than a flag on the shared one:
 * help-markdown-impl.tsx and assistant-markdown-impl.tsx both keep
 * `disallowedElements={['img']}` and MUST NOT change - their content (a
 * shipped help page, a Mate reply) has no equivalent of `hc-doc:`'s
 * anchored-UUID-only src, so any `img` there is still an attacker (or a
 * hallucinating model) picking a host the boat makes an outbound request
 * to. A note's `hc-doc:<uuid>` carries no host, path or query at all -
 * resolveNoteHref (note-links.ts) validates the UUID shape before it is
 * ever concatenated into a real URL - so only THAT one src shape is
 * rewritten into a real `<img>`. Everything else, including a perfectly
 * ordinary `https://` image URL, renders as its alt text in a muted span:
 * never an `<img>` (no outbound request) and never a link either (nothing
 * for a stray click to follow).
 */
export default function NoteMarkdownImpl({ content, onNavigate }: NoteMarkdownImplProps) {
  const components: Components = {
    ...assistantMarkdownComponents,
    h1: headingComponent('h1'),
    h2: headingComponent('h2'),
    h3: headingComponent('h3'),
    h4: headingComponent('h4'),
    img: ({ src, alt }) => {
      const link = src ? resolveNoteHref(src) : { kind: 'unsafe' as const }
      if (link.kind === 'document') {
        return (
          <img
            src={`${apiBaseUrl}/api/documents/${link.id}/content`}
            alt={alt ?? ''}
            className="max-w-full rounded-md border border-border"
          />
        )
      }
      // Not a valid hc-doc: reference: no img, no link, no outbound
      // request - the alt text is what survives (note-links.ts's own doc
      // comment on NoteLink calls this out as the deliberate outcome for
      // "unsafe", and it is exactly as true for "this wasn't hc-doc: at
      // all", e.g. a plain https:// image URL, which a note author has no
      // legitimate reason to embed - every real photo goes through the
      // Add photo upload flow (a later phase) and comes back as hc-doc:).
      return <span className="text-muted-foreground">{alt}</span>
    },
    a: ({ children, href }) => {
      if (!href) return <a>{children}</a>
      const link = resolveNoteHref(href)

      if (link.kind === 'external') {
        return (
          <a href={link.url} target="_blank" rel="noreferrer" className="text-primary underline underline-offset-2">
            {children}
          </a>
        )
      }

      if (link.kind === 'unsafe') {
        // Nothing safe to render as a real anchor - the visible label
        // survives as plain text (note-links.ts's own "plain text" outcome
        // for `unsafe`), never a clickable link and never a bare href.
        return <span>{children}</span>
      }

      return (
        <a
          href={href}
          className="text-primary underline underline-offset-2"
          onClick={(event: MouseEvent<HTMLAnchorElement>) => {
            event.preventDefault()
            onNavigate(link)
          }}
        >
          {children}
        </a>
      )
    },
  }

  return (
    <div className="min-w-0 text-sm leading-relaxed text-foreground">
      {/* react-markdown's own default urlTransform sanitises any URL whose
          scheme it doesn't recognise (https?/ircs?/mailto/xmpp) down to an
          empty string before a component ever sees it - which would silently
          neuter both hc-note: and hc-doc: before resolveNoteHref got a
          chance to validate them at all. Disabled here (identity function)
          because this file already does the real, stricter validation via
          resolveNoteHref for both `href` and `src` above - an allow-list
          purpose-built for exactly these two pseudo-schemes, not react-
          markdown's generic one. Safe precisely because every branch above
          that renders a real `href`/`src` into the DOM has already gone
          through that check: `external` only for resolveNoteHref's own
          SAFE_SCHEME allow-list, `document`'s img src is rebuilt from a
          validated UUID rather than passed through at all, and everything
          else renders as inert text with no `href`/`src` in the DOM. */}
      <ReactMarkdown remarkPlugins={[remarkGfm]} urlTransform={(value) => value} components={components}>
        {content}
      </ReactMarkdown>
    </div>
  )
}
