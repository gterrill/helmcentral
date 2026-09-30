// A spare is short when fewer are on board than the boat should carry. The
// server stores only the two numbers; "out of stock" and "below required"
// are read off them here, so there is one rule and nothing to keep in step.
// No required number means no stocking target, and nothing to be short of.

export type StockState = 'out' | 'below' | null

export function stockState(onHand: number, required: number | null): StockState {
  if (required === null || onHand >= required) return null
  return onHand <= 0 ? 'out' : 'below'
}

export const STOCK_STATE_LABEL: Record<Exclude<StockState, null>, string> = {
  out: 'Out of stock',
  below: 'Below required',
}
