import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { FormRow, FormSection } from '@/components/patterns'
import { STOCK_STATE_LABEL, stockState } from '@/lib/equipment-stock'
import type { EquipmentInput } from '@/hooks/use-inventory'
import { cn } from '@/lib/utils'

// The stock half of an equipment record: the supplier's part number, how many
// are on board and how many the boat should carry. "Out of stock" and "Below
// required" are read off those two numbers (lib/equipment-stock.ts) rather
// than stored, so editing either number moves the flag at once.

type SparesDraft = Pick<EquipmentInput, 'part_number' | 'quantity' | 'required_quantity'>

export interface EquipmentSparesFieldsProps {
  draft: SparesDraft
  onChange: (patch: Partial<SparesDraft>) => void
}

export function EquipmentSparesFields({ draft, onChange }: EquipmentSparesFieldsProps) {
  const state = stockState(draft.quantity, draft.required_quantity)

  return (
    <FormSection title="Stock">
      <FieldGroup>
        <FormRow>
          <Field>
            <FieldLabel htmlFor="equipment-part-number">Part number</FieldLabel>
            <Input
              id="equipment-part-number"
              value={draft.part_number}
              onChange={(e) => onChange({ part_number: e.target.value })}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="equipment-on-hand">On hand</FieldLabel>
            <Input
              id="equipment-on-hand"
              type="number"
              min={0}
              value={draft.quantity}
              onChange={(e) => onChange({ quantity: Math.max(0, Math.trunc(Number(e.target.value)) || 0) })}
            />
          </Field>
        </FormRow>
        <FormRow>
          <Field>
            <FieldLabel htmlFor="equipment-required-quantity">Required quantity</FieldLabel>
            <Input
              id="equipment-required-quantity"
              type="number"
              min={0}
              value={draft.required_quantity ?? ''}
              onChange={(e) => {
                const raw = e.target.value.trim()
                onChange({ required_quantity: raw === '' ? null : Math.max(0, Math.trunc(Number(raw)) || 0) })
              }}
            />
            <FieldDescription>How many the boat should carry. Leave blank if there is no target.</FieldDescription>
          </Field>
          <div className="flex items-end pb-2">
            {state !== null && (
              <p
                role="status"
                className={cn(
                  'text-sm font-medium',
                  state === 'out' ? 'text-red-600 dark:text-red-400' : 'text-amber-600 dark:text-amber-400',
                )}
              >
                {STOCK_STATE_LABEL[state]}
              </p>
            )}
          </div>
        </FormRow>
      </FieldGroup>
    </FormSection>
  )
}
