import { createContext, useCallback, useContext, useEffect, type ReactNode, type RefObject } from 'react'

/**
 * How a tile tells the board what height its content needs. The board gives
 * each tile a scope; a tile outside one (the hero row, a settings preview, a
 * test) reports into nothing.
 */
type Report = (px: number | null) => void

const TileHeightContext = createContext<Report | null>(null)

export function TileHeightScope({ id, onReport, children }: { id: string; onReport: (id: string, px: number | null) => void; children: ReactNode }) {
  const report = useCallback<Report>((px) => onReport(id, px), [id, onReport])
  return <TileHeightContext.Provider value={report}>{children}</TileHeightContext.Provider>
}

/** Reports a height in px, or null to withdraw it. Cleans up on unmount. */
export function useReportTileHeight(px: number | null) {
  const report = useContext(TileHeightContext)
  useEffect(() => {
    report?.(px)
  }, [report, px])
  useEffect(() => () => report?.(null), [report])
}

/**
 * Measures the height a tile needs to show `contentRef` whole: the card's
 * chrome (border, padding, header) plus the content at its natural height.
 *
 * The chrome is the card's height less the stretched content area's, which
 * does not depend on how tall the card currently is, and the content is the
 * unstretched block inside it, so shrinking the card cannot change the answer.
 * offsetHeight rather than getBoundingClientRect: layout units are unaffected
 * by the wall display's scale transform.
 */
export function useMeasureTileHeight(contentRef: RefObject<HTMLElement | null>, enabled: boolean) {
  const report = useContext(TileHeightContext)

  const measure = useCallback(() => {
    const el = contentRef.current
    const area = el?.parentElement
    const card = el?.closest<HTMLElement>('[data-slot="card"]')
    if (!el || !area || !card) return null
    return Math.ceil(card.offsetHeight - area.offsetHeight + el.offsetHeight)
  }, [contentRef])

  useEffect(() => {
    if (!enabled || !report) return
    const el = contentRef.current
    const card = el?.closest<HTMLElement>('[data-slot="card"]')
    if (!el || !card) return
    const update = () => report(measure())
    update()
    const observer = new ResizeObserver(update)
    observer.observe(el)
    observer.observe(card)
    return () => {
      observer.disconnect()
      report(null)
    }
  }, [enabled, report, contentRef, measure])
}
