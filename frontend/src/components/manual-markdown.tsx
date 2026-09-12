import type { MouseEvent } from 'react'
import ReactMarkdown, { type Components, type ExtraProps } from 'react-markdown'
import remarkGfm from 'remark-gfm'

import { assistantMarkdownComponents } from '@/components/assistant-markdown'
import { resolveManualHref, slugifyHeading, type ManualLink } from '@/lib/manual-links'

// Reads the plain text out of a heading's hast node (react-markdown v10
// always passes `node`, regardless of any option - see toJsxRuntime's
// hardcoded `passNode: true`), by walking its children for text leaves. Duck
// typed against `unknown` rather than importing @types/hast: that package
// isn't a direct dependency of this project, only a transitive one of
// react-markdown, and this only needs two fields off it.
function headingText(node: unknown): string {
  if (node && typeof node === 'object') {
    const record = node as { type?: unknown; value?: unknown; children?: unknown }
    if (record.type === 'text' && typeof record.value === 'string') return record.value
    if (Array.isArray(record.children)) return record.children.map(headingText).join('')
  }
  return ''
}

// assistant-markdown.tsx's h1-h4 renderers only ever destructure `children`,
// so passing them an `id` prop would silently do nothing - they'd have to
// forward it themselves, which would mean spreading `node` (a hast object)
// onto the DOM element too. Cheaper and clearer to own these four elements
// here, duplicating their four style strings, than to teach a chat-reply
// renderer about ids and hast internals it otherwise has no reason to know.
const HEADING_STYLE: Record<'h1' | 'h2' | 'h3' | 'h4', { tag: 'h3' | 'h4'; className: string }> = {
  h1: { tag: 'h3', className: 'mb-2 mt-6 text-lg font-semibold leading-snug text-foreground first:mt-0' },
  h2: { tag: 'h3', className: 'mb-2 mt-6 text-base font-semibold leading-snug text-foreground first:mt-0' },
  // h3 matters as much as h2 here: every "routes/charts/radar" lookup-table
  // target lands on a ### heading (a subsection under dashboard.md's
  // "Beyond the grid"), so it needs a scrollable id exactly like a ##
  // section does.
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

interface ManualMarkdownProps {
  content: string
  /** The manual page id this content came from - relative links (e.g.
   * "alarms.md" or "../reference/configuration.md#…") are resolved against
   * it, the same way a browser resolves a relative href against the current
   * document's URL. */
  pageId: string
  onNavigate: (link: ManualLink) => void
}

/**
 * Renders one manual page's markdown (ADR 0095) in the Manual sheet, reusing
 * assistant-markdown.tsx's whole visual language (ADR 0093) - Geist Sans
 * prose, semantic tokens, the same heading/list/table rhythm - so a manual
 * page and a Mate reply read as the same product. Two things a chat reply
 * never needs: headings carry a GitHub-style slug id so the sheet can scroll
 * to a specific section, and a link is resolved against the manual tree
 * (another in-tree page, an anchor on this one, or external GitHub for
 * anything the manual doesn't stage) rather than always opening a new tab.
 */
export function ManualMarkdown({ content, pageId, onNavigate }: ManualMarkdownProps) {
  const components: Components = {
    ...assistantMarkdownComponents,
    h1: headingComponent('h1'),
    h2: headingComponent('h2'),
    h3: headingComponent('h3'),
    h4: headingComponent('h4'),
    a: ({ children, href }) => {
      if (!href) return <a>{children}</a>
      const link = resolveManualHref(href, pageId)

      if (link.kind === 'external') {
        return (
          <a href={link.url} target="_blank" rel="noreferrer" className="text-primary underline underline-offset-2">
            {children}
          </a>
        )
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
      <ReactMarkdown remarkPlugins={[remarkGfm]} disallowedElements={['img']} components={components}>
        {content}
      </ReactMarkdown>
    </div>
  )
}
