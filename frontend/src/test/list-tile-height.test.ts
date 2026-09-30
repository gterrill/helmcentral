import { describe, expect, test } from 'vitest'

import {
  MIN_SHRUNK_ROWS,
  bottomOfColumnIds,
  effectiveRows,
  effectiveRowsById,
  lastInStackId,
} from '@/lib/list-tile-height'
import { GRID_MARGIN, GRID_ROW_HEIGHT, gridPixelHeight } from '@/lib/grid-metrics'

const w = (id: string, x: number, y: number, ww: number, h: number) => ({ id, x, y, w: ww, h })

describe('bottomOfColumnIds', () => {
  test('a tile with nothing below it is at the bottom', () => {
    const ids = bottomOfColumnIds([w('a', 0, 0, 4, 6), w('b', 0, 6, 4, 4)])
    expect([...ids]).toEqual(['b'])
  })

  test('columns are independent: each column has its own bottom', () => {
    const ids = bottomOfColumnIds([w('a', 0, 0, 4, 6), w('b', 4, 0, 4, 9), w('c', 0, 6, 4, 3)])
    expect([...ids].sort()).toEqual(['b', 'c'])
  })

  test('a partial x-overlap below blocks', () => {
    const ids = bottomOfColumnIds([w('a', 0, 0, 4, 6), w('b', 3, 6, 4, 4)])
    expect([...ids]).toEqual(['b'])
  })

  test('a tile that only touches at the edge column does not block', () => {
    const ids = bottomOfColumnIds([w('a', 0, 0, 4, 6), w('b', 4, 6, 4, 4)])
    expect([...ids].sort()).toEqual(['a', 'b'])
  })

  test('a tile beside it but starting higher does not block', () => {
    const ids = bottomOfColumnIds([w('a', 0, 2, 4, 4), w('b', 2, 0, 4, 3)])
    expect([...ids].sort()).toEqual(['a', 'b'])
  })

  test('the hero is neither a candidate nor a blocker', () => {
    const ids = bottomOfColumnIds([w('a', 0, 0, 4, 6), w('hero', 0, 6, 4, 4)], 'hero')
    expect([...ids]).toEqual(['a'])
  })
})

describe('lastInStackId', () => {
  test('is the last tile in (y, x) order, hero excluded', () => {
    expect(lastInStackId([w('b', 4, 2, 4, 4), w('a', 0, 2, 4, 4), w('c', 0, 0, 12, 2)])).toBe('b')
    expect(lastInStackId([w('a', 0, 0, 4, 4), w('hero', 0, 8, 4, 4)], 'hero')).toBe('a')
    expect(lastInStackId([])).toBeNull()
  })
})

describe('effectiveRows', () => {
  const px = (rows: number) => gridPixelHeight(rows)

  test('uses the saved height when nothing has been measured', () => {
    expect(effectiveRows(8, undefined)).toBe(8)
  })

  test('shrinks to the rows the content needs', () => {
    expect(effectiveRows(10, px(4))).toBe(4)
    expect(effectiveRows(10, px(4) - 1)).toBe(4)
    expect(effectiveRows(10, px(4) + 1)).toBe(5)
  })

  test('never grows past the saved height', () => {
    expect(effectiveRows(5, px(9))).toBe(5)
  })

  test('never drops below the header plus one row', () => {
    expect(effectiveRows(10, 10)).toBe(MIN_SHRUNK_ROWS)
  })

  test('a saved height already below the floor is kept', () => {
    expect(effectiveRows(1, 10)).toBe(1)
  })

  test('honours the wall row margin', () => {
    const wall = 8
    expect(effectiveRows(10, 3 * GRID_ROW_HEIGHT + 2 * wall, wall)).toBe(3)
    expect(GRID_MARGIN).not.toBe(wall)
  })
})

describe('effectiveRowsById', () => {
  const widgets = [w('a', 0, 0, 4, 6), w('list', 4, 0, 4, 9)]
  const needed = { a: gridPixelHeight(2), list: gridPixelHeight(3) }

  test('shrinks only a measured bottom tile', () => {
    const rows = effectiveRowsById({ widgets, needed, editing: false, desktop: true, rowMargin: GRID_MARGIN })
    expect(rows).toEqual({ a: 2, list: 3 })
  })

  test('a tile with something below it keeps its saved height', () => {
    const rows = effectiveRowsById({
      widgets: [w('list', 0, 0, 4, 9), w('b', 0, 9, 4, 3)],
      needed: { list: gridPixelHeight(3) },
      editing: false, desktop: true, rowMargin: GRID_MARGIN,
    })
    expect(rows.list).toBe(9)
  })

  test('edit mode always uses the saved height', () => {
    const rows = effectiveRowsById({ widgets, needed, editing: true, desktop: true, rowMargin: GRID_MARGIN })
    expect(rows).toEqual({ a: 6, list: 9 })
  })

  test('on the narrow layout only the last tile in stack order shrinks', () => {
    const rows = effectiveRowsById({ widgets, needed, editing: false, desktop: false, rowMargin: GRID_MARGIN })
    expect(rows).toEqual({ a: 6, list: 3 })
  })

  test('the hero keeps its saved height', () => {
    const rows = effectiveRowsById({ widgets, needed, editing: false, desktop: true, rowMargin: GRID_MARGIN, heroId: 'a' })
    expect(rows.a).toBe(6)
  })
})
