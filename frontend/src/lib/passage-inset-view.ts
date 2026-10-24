// Where the overview map sits on a passage replay and how close it looks:
// one of four corners, and a number of zoom steps wider or closer than the
// automatic fit. Saved per passage on the server; the replay, the blog embed
// and the still images all read the same two values.

export type InsetCorner = 'top-left' | 'top-right' | 'bottom-left' | 'bottom-right'

export const INSET_CORNERS: readonly { id: InsetCorner; label: string }[] = [
  { id: 'top-left', label: 'Top left' },
  { id: 'top-right', label: 'Top right' },
  { id: 'bottom-left', label: 'Bottom left' },
  { id: 'bottom-right', label: 'Bottom right' },
]

export const INSET_ZOOM_STEP_MIN = -3
export const INSET_ZOOM_STEP_MAX = 3

/** The overview never draws outside these map zoom levels, whatever the step. */
export const INSET_ZOOM_FLOOR = 0
export const INSET_ZOOM_CEILING = 12

export interface InsetView {
  corner: InsetCorner
  /** Zoom steps from the automatic fit: negative is wider, positive closer. */
  zoomOffset: number
}

/** What an unset passage shows: top right, the automatic zoom. */
export const DEFAULT_INSET_VIEW: InsetView = { corner: 'top-right', zoomOffset: 0 }

/** The automatic zoom moved by the operator's steps, one map zoom level each. */
export function offsetInsetZoom(autoZoom: number, zoomOffset: number): number {
  return Math.max(INSET_ZOOM_FLOOR, Math.min(INSET_ZOOM_CEILING, autoZoom + zoomOffset))
}

export function clampInsetStep(step: number): number {
  return Math.max(INSET_ZOOM_STEP_MIN, Math.min(INSET_ZOOM_STEP_MAX, step))
}

/** "+2", "-1" or "0": the step as the operator reads it. */
export function insetStepLabel(step: number): string {
  return step > 0 ? `+${step}` : String(step)
}
