import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

// ADR 0142: two (or more) short fields side by side - Manufacturer + Model,
// Serial + Quantity - instead of each stacking on its own full-width line.
//
// Keyed off the width of the container the row sits in, not the viewport, so
// the same markup is two columns in a wide main column and one column in the
// Details aside or on a phone. ui/field's own `orientation="responsive"` is
// not this (it puts a label BESIDE its control, and keys off an
// `@container/field-group` ancestor nothing in this app provides, which is
// why the pairs it was wrapping never went side by side).
//
// The outer @container element and the grid are two elements on purpose: a
// container cannot be styled by a query against itself. `grid-cols-2` is
// `repeat(2, minmax(0, 1fr))`, so a long value truncates in its own column
// instead of forcing the row wider than its card.

export interface FormRowProps {
  children: ReactNode
  className?: string
}

export function FormRow({ children, className }: FormRowProps) {
  return (
    <div data-slot="form-row" className={cn('@container w-full', className)}>
      <div className="grid gap-4 @md:grid-cols-2">{children}</div>
    </div>
  )
}
