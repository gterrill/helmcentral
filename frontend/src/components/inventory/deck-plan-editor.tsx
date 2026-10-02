import { lazy, Suspense } from 'react'

import type { DeckPlanEditorProps } from './deck-plan-editor-impl'

// Same kiosk bundle-split reasoning as note-editor.tsx: the drawing surface and
// its pointer handling are only ever mounted from the deck page, so the wall
// kiosk never needs to parse them. Everything the impl exports at runtime
// stays behind this lazy boundary; only its prop types are imported here.
const loadDeckPlanEditorImpl = () => import('./deck-plan-editor-impl')
const DeckPlanEditorImpl = lazy(loadDeckPlanEditorImpl)

/** Warms the drawing surface's chunk before the deck page needs it. */
export function prefetchDeckPlanEditor(): void {
  void loadDeckPlanEditorImpl()
}

export function DeckPlanEditor(props: DeckPlanEditorProps) {
  return (
    <Suspense fallback={<p className="text-sm text-muted-foreground">Loading plan...</p>}>
      <DeckPlanEditorImpl {...props} />
    </Suspense>
  )
}
