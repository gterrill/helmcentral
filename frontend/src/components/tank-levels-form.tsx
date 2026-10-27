import { memo } from 'react'

import { FormSection } from '@/components/patterns'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { useTankLevelsFormContext } from '@/components/settings/tank-levels-context'
import { TANK_DEFAULT_KINDS, isLowBadKind } from '@/lib/tank-levels'

/**
 * The warn and alarm level each kind of tank starts from (ADR 0214). They
 * colour the Tanks tile and the fuel bars and raise the tank alarms. A tank can
 * keep its own on its equipment page.
 *
 * Presentation only: the draft and the save belong to TankLevelsProvider, so
 * this block is saved by the settings page's own Save bar.
 */
export const TankLevelsForm = memo(function TankLevelsForm() {
  const { draft, setLevel, errors, loadError } = useTankLevelsFormContext()

  return (
    <FormSection
      title="Tank levels"
      description="Where a tank turns amber (warn) and red (alarm), as a percentage of full. The Tanks tile, the fuel bars and the alarms all use these. A tank can use its own levels on its equipment page."
    >
      {loadError && <p className="text-xs text-destructive">{loadError}</p>}
      {draft === null && !loadError && (
        <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">Loading…</p>
      )}
      {draft !== null &&
        TANK_DEFAULT_KINDS.map(({ kind, label }) => {
          const error = errors[kind]
          return (
            <div key={kind} className="flex flex-col gap-2">
              <p className="text-sm font-medium">{label}</p>
              <div className="grid gap-3 sm:grid-cols-2">
                <Field data-invalid={error ? true : undefined}>
                  <FieldLabel htmlFor={`tank-levels-${kind}-warn`}>Warn at</FieldLabel>
                  <div className="flex items-center gap-2">
                    <Input
                      id={`tank-levels-${kind}-warn`}
                      type="number" inputMode="decimal" step="any" min={0} max={100}
                      value={draft[kind].warn}
                      onChange={(e) => setLevel(kind, 'warn', e.target.value)}
                    />
                    <span className="text-xs text-muted-foreground">%</span>
                  </div>
                </Field>
                <Field data-invalid={error ? true : undefined}>
                  <FieldLabel htmlFor={`tank-levels-${kind}-alarm`}>Alarm at</FieldLabel>
                  <div className="flex items-center gap-2">
                    <Input
                      id={`tank-levels-${kind}-alarm`}
                      type="number" inputMode="decimal" step="any" min={0} max={100}
                      value={draft[kind].alarm}
                      onChange={(e) => setLevel(kind, 'alarm', e.target.value)}
                    />
                    <span className="text-xs text-muted-foreground">%</span>
                  </div>
                </Field>
              </div>
              <FieldDescription>
                {isLowBadKind(kind) ? 'Raised when the level falls below it.' : 'Raised when the level rises above it.'}
              </FieldDescription>
              {error && <FieldError>{error}</FieldError>}
            </div>
          )
        })}
    </FormSection>
  )
})
