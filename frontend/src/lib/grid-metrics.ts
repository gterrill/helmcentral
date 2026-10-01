// RGL's row geometry. Shared with the narrow CSS grid below, which derives each tile's
// minimum height from the same numbers so a tile keeps roughly its authored proportions.
export const GRID_COLUMNS = 12
export const GRID_ROW_HEIGHT = 32
export const GRID_MARGIN = 16

/**
 * The vertical margin a wall-display page uses instead of GRID_MARGIN. The
 * 1920x360 strip is one tile-row tall at the ordinary 48px step (32px row +
 * 16px margin), which gives a stacked pair of tiles seven rows to split
 * between them and no split that feeds both. At 8px the same strip holds
 * nine rows, which is what lets Forecast sit above Sea State without either
 * losing its content. Only pages flagged for the wall take it (App.tsx), so
 * the helm and nav boards keep the step they were authored against.
 */
export const WALL_ROW_MARGIN = 8

/**
 * How far each tile is inset from the top of its grid cell. The title pill is
 * centred on the card's top border, so it overhangs the card by 12px; the
 * inset (pt-2 on the wrapper that holds the tile) moves the card down 8px so
 * the pill clears the tile above by margin + 8 - 12: 12px on the 16px board,
 * 4px on a wall page's 8px margin. Row pitch is unchanged, so saved layouts
 * do not move.
 */
export const TILE_INSET_H = 8

/** What a row count is worth in pixels: n rows and the n-1 margins between them. */
export function gridPixelHeight(rows: number, rowMargin: number = GRID_MARGIN): number {
  return rows * GRID_ROW_HEIGHT + Math.max(0, rows - 1) * rowMargin
}

/** The fewest whole rows that will hold a given height. */
export function rowsForHeight(px: number, rowMargin: number = GRID_MARGIN): number {
  return Math.ceil((px + rowMargin) / (GRID_ROW_HEIGHT + rowMargin))
}
