import { lazy, Suspense } from 'react'

import type { NoteLink } from '@/lib/note-links'

// Same kiosk bundle-split reasoning as help-markdown.tsx / assistant-markdown.tsx
// (commit 70acb53): this keeps react-markdown's module graph (and the regex
// lookbehind literal in its mdast-util-gfm-autolink-literal dependency,
// unparseable by a pre-16.4 Safari) out of the entry chunk. Notes are read
// from the Notes panel and from the Documents viewer, neither of which the
// kiosk route mounts, but a lazy import here still keeps that dependency
// graph out of every chunk the kiosk DOES load eagerly.
const NoteMarkdownImpl = lazy(() => import('./note-markdown-impl'))

interface NoteMarkdownProps {
  content: string
  onNavigate: (link: NoteLink) => void
}

export function NoteMarkdown({ content, onNavigate }: NoteMarkdownProps) {
  return (
    <Suspense fallback={<div className="min-w-0 text-sm leading-relaxed text-muted-foreground" />}>
      <NoteMarkdownImpl content={content} onNavigate={onNavigate} />
    </Suspense>
  )
}
