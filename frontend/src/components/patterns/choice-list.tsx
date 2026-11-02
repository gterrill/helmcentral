import { useId, type ReactNode } from 'react'

import { Checkbox } from '@/components/ui/checkbox'
import { FieldError, FieldLegend, FieldSet } from '@/components/ui/field'
import { cn } from '@/lib/utils'

// ADR 0142: a short list of choices where one or several can be picked, each
// with a title, a line saying what picking it means, and optionally something
// that appears only while it is picked. Modelled on Shopify's s-choice-list.
//
// Plain rows, no cards. The <label> holds the title and details, and its ::after
// stretches over the row so tapping anywhere there toggles the control. It sits
// beside the control rather than around it because a label wrapped around Base
// UI's Checkbox toggles it twice. `secondaryContent` and `selectedContent` sit
// outside the row so a link or field in there does not toggle. The control is
// named by the title alone and described by the details and any
// `selectedContent`; a field in `secondaryContent` has its own label, so it is
// not read out as the control's description. With `multiple`, a change of
// selection returns only drawn choices: a stored value with no row has nothing
// on screen to untick it, so it must not be sent back for ever.

export interface ChoiceListChoice<T extends string = string> {
  value: T
  label: ReactNode
  /** Always visible under the label: what picking this choice means. */
  details?: ReactNode
  /** Always visible below the row, e.g. a field whose error must not hide when the choice is off. The caller disables a field in here. */
  secondaryContent?: ReactNode
  /** Visible only while this choice is selected, e.g. a warning or a follow-up field. */
  selectedContent?: ReactNode
  disabled?: boolean
}

interface ChoiceListBase<T extends string> {
  /** The legend. */
  label: ReactNode
  /** Keeps the legend for screen readers only, when a heading above already says it. */
  labelHidden?: boolean
  choices: ChoiceListChoice<T>[]
  /** Wins over each choice's own `disabled`. */
  disabled?: boolean
  /** Above the choices. */
  error?: ReactNode
  /** Below the choices, inside the group. */
  details?: ReactNode
  /** Drawn in place of the choices when `choices` is empty. */
  empty?: ReactNode
  className?: string
  /** Merged onto the list of rows, e.g. a grid for a list of short labels in columns. */
  listClassName?: string
}

export type ChoiceListProps<T extends string = string> = ChoiceListBase<T> &
  (
    | { multiple?: false; value: T | undefined; onChange: (value: T) => void }
    | { multiple: true; value: T[]; onChange: (value: T[]) => void }
  )

export function ChoiceList<T extends string = string>(props: ChoiceListProps<T>) {
  const { label, labelHidden, choices, disabled, error, details, empty, className, listClassName } = props
  const base = useId()

  const toggle = (value: T, on: boolean) => {
    if (!props.multiple) return
    props.onChange(choices.filter((c) => (c.value === value ? on : props.value.includes(c.value))).map((c) => c.value))
  }

  const isSelected = (value: T) => (props.multiple ? props.value.includes(value) : props.value === value)

  return (
    <FieldSet data-slot="choice-list" className={className}>
      <FieldLegend variant="label" className={labelHidden ? 'sr-only' : undefined}>{label}</FieldLegend>
      {error && <FieldError className="text-xs">{error}</FieldError>}
      {choices.length === 0 ? (
        empty
      ) : (
        <div data-slot="choice-list-rows" className={cn('flex flex-col', listClassName)}>
          {choices.map((c, i) => {
            const id = `${base}-${i}`
            const titleId = `${id}-title`
            const detailsId = `${id}-details`
            const selectedId = `${id}-selected`
            const off = disabled || c.disabled
            const selected = isSelected(c.value)
            const hasDetails = Boolean(c.details)
            const showSelected = selected && Boolean(c.selectedContent)
            // On the title's first line, however far the label wraps.
            const controlOffset = hasDetails ? 'mt-1' : 'mt-3.5'
            const control = {
              id,
              disabled: off,
              'aria-labelledby': titleId,
              'aria-describedby': [hasDetails && detailsId, showSelected && selectedId].filter(Boolean).join(' ') || undefined,
            }
            return (
              <div key={c.value} className="flex min-w-0 flex-col py-1">
                <div
                  className={cn(
                    'relative flex items-start gap-3',
                    off ? 'cursor-not-allowed opacity-60' : 'cursor-pointer',
                  )}
                >
                  {props.multiple ? (
                    <Checkbox
                      {...control}
                      className={cn('z-10', controlOffset)}
                      checked={selected}
                      onCheckedChange={(on) => toggle(c.value, on)}
                    />
                  ) : (
                    <input
                      {...control}
                      type="radio"
                      name={base}
                      value={c.value}
                      checked={selected}
                      onChange={() => props.onChange(c.value)}
                      className={cn('relative z-10 size-4 shrink-0 accent-primary', controlOffset)}
                    />
                  )}
                  <label
                    htmlFor={id}
                    className={cn(
                      'flex min-h-11 min-w-0 flex-1 cursor-[inherit] flex-col justify-center gap-0.5 after:absolute after:inset-0',
                      !hasDetails && 'py-3',
                    )}
                  >
                    <span id={titleId} className="text-sm font-medium text-foreground">{c.label}</span>
                    {hasDetails && <span id={detailsId} className="text-xs text-muted-foreground">{c.details}</span>}
                  </label>
                </div>
                {c.secondaryContent ? <div className="-mt-1 pb-2 pl-7">{c.secondaryContent}</div> : null}
                {showSelected && <div id={selectedId} className={cn('pl-7', !c.secondaryContent && '-mt-1')}>{c.selectedContent}</div>}
              </div>
            )
          })}
        </div>
      )}
      {details && <div className="text-xs text-muted-foreground">{details}</div>}
    </FieldSet>
  )
}
