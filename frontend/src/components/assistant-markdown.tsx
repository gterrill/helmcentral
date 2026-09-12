import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'

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
// Exported as assistantMarkdownComponents (ADR 0095) so manual-markdown.tsx
// can spread this same map and override only the handful of tags the Manual
// sheet needs to behave differently - heading ids for scroll targets, and
// links resolved against the manual tree instead of always opening a new
// tab - rather than maintaining a second, near-identical style sheet.
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
  a: ({ children, href }) => (
    <a href={href} target="_blank" rel="noreferrer" className="text-primary underline underline-offset-2">
      {children}
    </a>
  ),
  code: ({ children }) => (
    <code className="rounded-sm bg-muted px-1 py-0.5 font-display text-xs text-foreground">{children}</code>
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

interface AssistantMarkdownProps {
  content: string
}

export function AssistantMarkdown({ content }: AssistantMarkdownProps) {
  return (
    <div className="min-w-0 text-sm leading-relaxed text-foreground">
      <ReactMarkdown remarkPlugins={[remarkGfm]} disallowedElements={['img']} components={assistantMarkdownComponents}>
        {content}
      </ReactMarkdown>
    </div>
  )
}
