import { Palette } from 'lucide-react'

export interface SkinnablePage {
  id: string
  name: string
  skin?: 'default' | 'instrument'
}

interface PageSkinSelectProps {
  page: SkinnablePage | null
  onSetSkin: (id: string, skin: 'default' | 'instrument') => void
}

/**
 * The active page's skin (ADR 0060), as a layout control.
 *
 * Sits with Add Widget rather than in the page-switcher popover it started in:
 * the skin repaints the whole board, so it belongs where an operator already is
 * when they are deciding what the page looks like, not three clicks away under
 * the list of pages. The trade is that it now lives in layout mode, which is
 * desktop-only — the same gate Add Widget and the grid itself are behind.
 */
export function PageSkinSelect({ page, onSetSkin }: PageSkinSelectProps) {
  if (!page) return null

  return (
    // A label wrapping the select, so the whole chip is the hit target and the
    // border can respond to focus inside it. Styled as the sibling of the Add
    // Widget trigger, because that is what it is.
    <label className="inline-flex w-fit items-center gap-1.5 rounded-md border border-border bg-background/70 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.1em] text-muted-foreground transition-colors focus-within:border-primary/40 hover:border-primary/40 hover:text-primary">
      <Palette className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
      {/* The name says which page, not just which setting: the board carries
          several and this edits whichever one is on screen. */}
      <select
        aria-label={`Skin for ${page.name}`}
        className="cursor-pointer bg-transparent text-xs font-semibold uppercase tracking-[0.1em] outline-none"
        value={page.skin ?? 'default'}
        onChange={(e) => onSetSkin(page.id, e.target.value as 'default' | 'instrument')}
      >
        <option value="default">Theme skin</option>
        <option value="instrument">Instrument skin</option>
      </select>
    </label>
  )
}
