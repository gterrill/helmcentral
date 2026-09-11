import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'

// ADR 0093: assistant replies render as GFM markdown (a comparison table is
// the whole point of the Hook Reef use case, plus lists and links) with no
// raw HTML. react-markdown skips raw HTML elements by default; `img` is
// dropped explicitly too below, so a reply can never make the browser fetch
// an attacker-controlled URL as a side effect of rendering it.
//
// Every override below reaches for a semantic token, not a literal colour,
// so a reply repaints correctly across the light/dark/instrument themes
// the same as every other surface in the app.
const components: Components = {
  h1: ({ children }) => <h3 className="text-sm font-semibold text-foreground">{children}</h3>,
  h2: ({ children }) => <h3 className="text-sm font-semibold text-foreground">{children}</h3>,
  h3: ({ children }) => <h3 className="text-sm font-semibold text-foreground">{children}</h3>,
  h4: ({ children }) => <h3 className="text-sm font-semibold text-foreground">{children}</h3>,
  h5: ({ children }) => <h3 className="text-sm font-semibold text-foreground">{children}</h3>,
  h6: ({ children }) => <h3 className="text-sm font-semibold text-foreground">{children}</h3>,
  p: ({ children }) => <p className="text-sm text-foreground">{children}</p>,
  ul: ({ children }) => <ul className="list-disc space-y-0.5 pl-5 text-sm text-foreground">{children}</ul>,
  ol: ({ children }) => <ol className="list-decimal space-y-0.5 pl-5 text-sm text-foreground">{children}</ol>,
  li: ({ children }) => <li className="text-sm text-foreground">{children}</li>,
  a: ({ children, href }) => (
    <a href={href} target="_blank" rel="noreferrer" className="text-primary underline">
      {children}
    </a>
  ),
  code: ({ children }) => <code className="rounded bg-muted px-1 text-xs text-foreground">{children}</code>,
  pre: ({ children }) => (
    <pre className="overflow-x-auto rounded-md border border-border bg-muted p-2 text-xs text-foreground">
      {children}
    </pre>
  ),
  table: ({ children }) => (
    <div className="min-w-0 overflow-x-auto">
      <table className="w-full text-xs text-foreground">{children}</table>
    </div>
  ),
  thead: ({ children }) => <thead>{children}</thead>,
  tr: ({ children }) => <tr className="border-b border-border">{children}</tr>,
  th: ({ children }) => <th className="border border-border px-2 py-1 text-left font-semibold">{children}</th>,
  td: ({ children }) => <td className="border border-border px-2 py-1">{children}</td>,
  strong: ({ children }) => <strong className="font-semibold text-foreground">{children}</strong>,
  blockquote: ({ children }) => (
    <blockquote className="border-l-2 border-border pl-3 text-sm text-muted-foreground">{children}</blockquote>
  ),
  hr: () => <hr className="border-border" />,
}

interface AssistantMarkdownProps {
  content: string
}

export function AssistantMarkdown({ content }: AssistantMarkdownProps) {
  return (
    <ReactMarkdown remarkPlugins={[remarkGfm]} disallowedElements={['img']} components={components}>
      {content}
    </ReactMarkdown>
  )
}
