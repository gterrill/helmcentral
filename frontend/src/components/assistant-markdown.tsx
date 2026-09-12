import { lazy, Suspense } from 'react'

// The kiosk (WPE WebKit 2.38.5, Safari 16.0 era) loads this module graph as
// part of the entry chunk even though the kiosk route never mounts
// AssistantThread - a single statically-imported react-markdown pulled its
// whole dependency tree, including mdast-util-gfm-autolink-literal's regex
// lookbehind literal, into that chunk, which is enough to make a pre-16.4
// Safari throw a SyntaxError parsing the bundle before anything runs. Behind
// a dynamic import, that graph only loads (and only has to parse) in a
// browser that actually renders a Mate reply.
const AssistantMarkdownImpl = lazy(() => import('./assistant-markdown-impl'))

interface AssistantMarkdownProps {
  content: string
}

export function AssistantMarkdown({ content }: AssistantMarkdownProps) {
  return (
    <Suspense fallback={<div className="min-w-0 text-sm leading-relaxed text-muted-foreground" />}>
      <AssistantMarkdownImpl content={content} />
    </Suspense>
  )
}
