import { File as FileIcon, FileImage, FileSpreadsheet, FileText, FileX2, StickyNote } from 'lucide-react'
import type { ReactNode } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'

import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useDocumentCitation } from '@/hooks/use-document-citation'
import { citationIconKind, parseDocumentCitationHref, type CitationIconKind } from '@/lib/document-citation'

// ADR 0093: assistant replies render as GFM markdown (a comparison table is
// the whole point of the Hook Reef use case, plus lists and links) with no
// raw HTML. react-markdown skips raw HTML elements by default; `img` is
// dropped explicitly too below, so a reply can never make the browser fetch
// an attacker-controlled URL as a side effect of rendering it.
//
// A reply is read like a briefing, not scanned like a tile, so the block
// elements carry their own vertical rhythm: more space above a heading than
// below it, a paragraph's worth between blocks, and a wider gap around a
// rule, which is how the model separates sections. Tailwind's preflight
// zeroes every margin, so without these the sections run together.
//
// Every override below reaches for a semantic token, not a literal colour,
// so a reply repaints correctly across the light/dark/instrument themes
// the same as every other surface in the app.
//
// Exported as assistantMarkdownComponents (ADR 0095) so help-markdown.tsx
// can spread this same map and override only the handful of tags the Help
// sheet needs to behave differently - heading ids for scroll targets, and
// links resolved against the help tree instead of always opening a new
// tab - rather than maintaining a second, near-identical style sheet.
// Mate UI cycle: document sources as icons. Mate cites a document as
// `[Title](/documents?document=<id>)` (assistant_prompt.go's citation
// guidance) instead of the old free-text parenthetical mention - the `a`
// override below recognises that exact link shape (parseDocumentCitationHref)
// and swaps the visible text for a small icon matching the document's kind,
// with the link's own text (the title, optionally "Title › Section") as its
// tooltip/accessible name. An ordinary link (any href that isn't a citation)
// still renders exactly as it did before, further down this file.
const CITATION_ICONS: Record<CitationIconKind, typeof FileText> = {
  pdf: FileText,
  spreadsheet: FileSpreadsheet,
  image: FileImage,
  note: StickyNote,
  file: FileIcon,
}

// A citation's link text is always plain prose Mate wrote itself (the title,
// optionally "Title › Section" - assistant_prompt.go's own citation
// guidance never asks for nested markdown there), but react-markdown still
// hands `children` through as a ReactNode tree rather than a bare string, so
// this walks it down to the text the tooltip/aria-label actually needs.
function citationLinkText(children: ReactNode): string {
  if (typeof children === 'string') return children
  if (typeof children === 'number') return String(children)
  if (Array.isArray(children)) return children.map(citationLinkText).join('')
  if (children !== null && typeof children === 'object' && 'props' in children) {
    const props = (children as { props?: { children?: ReactNode } }).props
    return citationLinkText(props?.children ?? null)
  }
  return ''
}

// Shared by both branches below (found and broken) - the dense cockpit
// sizing this renders at inline with body text (h-3.5 w-3.5, AGENTS.md's
// "individual metric containers must share identical inner padding" spirit
// applied to an inline glyph rather than a card).
const CITATION_LINK_CLASS =
  'inline-flex h-5 w-5 shrink-0 items-center justify-center align-text-bottom rounded-xs hover:bg-muted'

/**
 * One document citation link, rendered as an icon button rather than visible
 * text: `label` (the markdown link's own text, e.g. "Equipment List ›
 * Navigation") is both the tooltip content and the accessible name, and the
 * icon matches the document's kind once useDocumentCitation resolves it. Tap
 * or click always navigates (the anchor's own href, in-app, same tab, no
 * lookup required to do that much) - the tooltip is a hover/focus-only
 * bonus, not the only way to learn what this points to, since aria-label
 * already carries the same text for a screen reader and touch device alike.
 *
 * An unknown or deleted id (`not-found`) - or a lookup that simply failed
 * (`error`) - renders a visibly muted, broken-file icon rather than
 * disappearing or silently keeping the wrong icon: AGENTS.md's fallback
 * policy applied to a UI affordance, not just a data path.
 */
function DocumentCitationLink({ id, href, label }: { id: string; href: string; label: string }) {
  const citation = useDocumentCitation(id)

  if (citation.status === 'not-found' || citation.status === 'error') {
    const title = citation.status === 'not-found' ? 'Document not found' : 'Could not check this document'
    return (
      <Tooltip>
        <TooltipTrigger
          render={
            <a href={href} aria-label={title} className={`${CITATION_LINK_CLASS} text-muted-foreground/60`}>
              <FileX2 className="h-3.5 w-3.5" aria-hidden="true" />
            </a>
          }
        />
        <TooltipContent>{title}</TooltipContent>
      </Tooltip>
    )
  }

  // 'loading' has no kind yet - a neutral placeholder icon rather than
  // guessing, with the title (already known from the link text itself, no
  // fetch required for that part) shown immediately rather than waiting on
  // the lookup just to name what this points to.
  const kind: CitationIconKind = citation.status === 'ok' ? citationIconKind({ mime: citation.mime ?? '', kind: citation.kind ?? '' }) : 'file'
  const Icon = CITATION_ICONS[kind]
  const tooltipTitle = label !== '' ? label : citation.title ?? 'Document'

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <a href={href} aria-label={tooltipTitle} className={`${CITATION_LINK_CLASS} text-primary`}>
            <Icon className="h-3.5 w-3.5" aria-hidden="true" />
          </a>
        }
      />
      <TooltipContent>{tooltipTitle}</TooltipContent>
    </Tooltip>
  )
}

export const assistantMarkdownComponents: Components = {
  // One visible step above h2 (impeccable critique 2026-09-12, P3) - the two
  // used to render identically, so a reply using both levels had no
  // hierarchy between them.
  h1: ({ children }) => (
    <h3 className="mb-2 mt-6 text-lg font-semibold leading-snug text-foreground first:mt-0">{children}</h3>
  ),
  h2: ({ children }) => (
    <h3 className="mb-2 mt-6 text-base font-semibold leading-snug text-foreground first:mt-0">{children}</h3>
  ),
  h3: ({ children }) => (
    <h4 className="mb-1.5 mt-5 text-sm font-semibold leading-snug text-foreground first:mt-0">{children}</h4>
  ),
  h4: ({ children }) => (
    <h4 className="mb-1.5 mt-5 text-sm font-semibold leading-snug text-foreground first:mt-0">{children}</h4>
  ),
  h5: ({ children }) => (
    <h4 className="mb-1.5 mt-5 text-sm font-semibold leading-snug text-foreground first:mt-0">{children}</h4>
  ),
  h6: ({ children }) => (
    <h4 className="mb-1.5 mt-5 text-sm font-semibold leading-snug text-foreground first:mt-0">{children}</h4>
  ),
  p: ({ children }) => <p className="mb-3 text-foreground last:mb-0">{children}</p>,
  ul: ({ children }) => <ul className="mb-3 list-disc space-y-1 pl-5 text-foreground last:mb-0">{children}</ul>,
  ol: ({ children }) => <ol className="mb-3 list-decimal space-y-1 pl-5 text-foreground last:mb-0">{children}</ol>,
  li: ({ children }) => <li className="pl-1 text-foreground">{children}</li>,
  a: ({ children, href }) => {
    const citationId = href ? parseDocumentCitationHref(href) : null
    if (citationId !== null && href) {
      return <DocumentCitationLink id={citationId} href={href} label={citationLinkText(children)} />
    }
    return (
      <a href={href} target="_blank" rel="noreferrer" className="text-primary underline underline-offset-2">
        {children}
      </a>
    )
  },
  code: ({ children }) => (
    <code className="rounded-xs bg-muted px-1 py-0.5 font-display text-xs text-foreground">{children}</code>
  ),
  pre: ({ children }) => (
    <pre className="mb-3 overflow-x-auto rounded-md border border-border bg-muted p-3 font-display text-xs leading-relaxed text-foreground last:mb-0">
      {children}
    </pre>
  ),
  table: ({ children }) => (
    <div className="mb-4 min-w-0 overflow-x-auto last:mb-0">
      <table className="w-full border-collapse text-xs text-foreground">{children}</table>
    </div>
  ),
  thead: ({ children }) => <thead className="text-muted-foreground">{children}</thead>,
  tr: ({ children }) => <tr className="border-b border-border">{children}</tr>,
  th: ({ children }) => (
    <th className="px-2 py-1.5 text-left align-bottom font-medium uppercase tracking-[0.08em]">{children}</th>
  ),
  td: ({ children }) => <td className="px-2 py-1.5 align-top tabular-nums">{children}</td>,
  strong: ({ children }) => <strong className="font-semibold text-foreground">{children}</strong>,
  blockquote: ({ children }) => (
    <blockquote className="mb-3 border-l border-border pl-3 text-muted-foreground last:mb-0">{children}</blockquote>
  ),
  hr: () => <hr className="my-5 border-border" />,
}

interface AssistantMarkdownImplProps {
  content: string
}

// Default export (not the named AssistantMarkdown export assistant-markdown.tsx
// used to carry directly): React.lazy() needs a component reachable as a
// module's default export, which is what assistant-markdown.tsx now lazily
// imports this file for. See that file's own comment for why - this is the
// half of the split that actually pulls in react-markdown/remark-gfm and,
// through it, mdast-util-gfm-autolink-literal's regex lookbehind literal
// that a pre-16.4 Safari kiosk browser can't even parse.
export default function AssistantMarkdownImpl({ content }: AssistantMarkdownImplProps) {
  return (
    <div className="min-w-0 text-sm leading-relaxed text-foreground">
      <ReactMarkdown remarkPlugins={[remarkGfm]} disallowedElements={['img']} components={assistantMarkdownComponents}>
        {content}
      </ReactMarkdown>
    </div>
  )
}
