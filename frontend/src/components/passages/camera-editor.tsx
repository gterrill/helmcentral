import { Crosshair, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { CAMERA_PACES, type CameraMode, type CameraShot } from '@/lib/replay-clock'
import { cn } from '@/lib/utils'

const MODES: { id: CameraMode; label: string }[] = [
  { id: 'overview', label: 'Overview' },
  { id: 'follow', label: 'Follow' },
  { id: 'orbit', label: 'Orbit' },
]

const PACES: Record<(typeof CAMERA_PACES)[number], { label: string; name: string }> = {
  1: { label: '1×', name: 'full' },
  0.5: { label: '½×', name: 'half' },
  0.25: { label: '¼×', name: 'quarter' },
}

/** The editable fields of a shot. */
export type CameraShotChange = Partial<Pick<CameraShot, 'mode' | 'zoom' | 'pitch' | 'bearing_offset_deg' | 'orbit_deg_per_s' | 'pace'>>

interface NumberFieldProps {
  label: string
  value: number
  min: number
  max: number
  step: number
  disabled: boolean
  onCommit: (value: number) => void
}

// Takes the typed value on Enter or when the field is left; a value outside
// the range is brought to the nearest end. Nothing is sent while typing.
function NumberField({ label, value, min, max, step, disabled, onCommit }: NumberFieldProps) {
  const [draft, setDraft] = useState<string | null>(null)
  const shown = draft ?? String(Math.round(value * 100) / 100)
  const commit = () => {
    if (draft === null) return
    const n = Number(draft)
    setDraft(null)
    if (draft.trim() === '' || !Number.isFinite(n)) return
    const next = Math.max(min, Math.min(max, n))
    if (next !== value) onCommit(next)
  }
  return (
    <label className="flex items-center gap-1 text-xs text-muted-foreground">
      <span>{label}</span>
      <Input
        type="number"
        inputMode="decimal"
        aria-label={label}
        className="h-7 w-16 px-1.5 py-0 text-xs tabular-nums md:text-xs"
        min={min}
        max={max}
        step={step}
        disabled={disabled}
        value={shown}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => { if (e.key === 'Enter') commit() }}
      />
    </label>
  )
}

export interface CameraEditorProps {
  /** The shot being edited, or null when none is picked. */
  shot: CameraShot | null
  shotCount: number
  /** The map is flat: shots play as zoom and centre only. */
  flat: boolean
  busy: boolean
  error: string | null
  onAdd: () => void
  onChange: (change: CameraShotChange) => void
  onDelete: () => void
  onUseView: () => void
}

export function CameraEditor({ shot, shotCount, flat, busy, error, onAdd, onChange, onDelete, onUseView }: CameraEditorProps) {
  return (
    <div className="flex flex-col gap-2 border-t border-border p-2" data-testid="camera-editor" role="group" aria-label="Camera shots">
      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" size="sm" variant="outline" disabled={busy} onClick={onAdd}>
          <Plus className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
          Add shot here
        </Button>
        <span className="text-xs text-muted-foreground">
          {shotCount === 0 ? 'No shots: the camera frames the whole track' : `${shotCount} ${shotCount === 1 ? 'shot' : 'shots'}. Pick a mark on the scrubber to edit it`}
        </span>
        {flat && shotCount > 0 && <span className="text-xs text-muted-foreground">Flat map: shots move and zoom only</span>}
      </div>
      {shot && (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2" data-testid="camera-shot-fields">
          <span className="flex gap-1" role="group" aria-label="Shot mode">
            {MODES.map((m) => (
              <Button
                key={m.id}
                type="button"
                size="sm"
                variant={shot.mode === m.id ? 'default' : 'outline'}
                aria-pressed={shot.mode === m.id}
                disabled={busy}
                onClick={() => onChange({ mode: m.id })}
              >
                {m.label}
              </Button>
            ))}
          </span>
          {shot.mode !== 'overview' && (
            <>
              <NumberField label="Zoom" value={shot.zoom} min={0} max={22} step={0.5} disabled={busy} onCommit={(zoom) => onChange({ zoom })} />
              <NumberField label="Pitch" value={shot.pitch} min={0} max={70} step={5} disabled={busy} onCommit={(pitch) => onChange({ pitch })} />
            </>
          )}
          {shot.mode === 'follow' && (
            <NumberField label="Angle" value={shot.bearing_offset_deg} min={-180} max={180} step={15} disabled={busy} onCommit={(bearing_offset_deg) => onChange({ bearing_offset_deg })} />
          )}
          {shot.mode === 'orbit' && (
            <NumberField label="Orbit °/s" value={shot.orbit_deg_per_s} min={-90} max={90} step={1} disabled={busy} onCommit={(orbit_deg_per_s) => onChange({ orbit_deg_per_s })} />
          )}
          <span className="flex items-center gap-1" role="group" aria-label="Pace">
            <span className="text-xs text-muted-foreground">Pace</span>
            {CAMERA_PACES.map((p) => (
              <Button
                key={p}
                type="button"
                size="sm"
                variant={shot.pace === p ? 'default' : 'outline'}
                aria-pressed={shot.pace === p}
                aria-label={`Pace ${PACES[p].name}`}
                disabled={busy}
                onClick={() => onChange({ pace: p })}
              >
                {PACES[p].label}
              </Button>
            ))}
          </span>
          {shot.mode !== 'overview' && (
            <Button type="button" size="sm" variant="outline" disabled={busy} onClick={onUseView}>
              <Crosshair className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
              Use current view
            </Button>
          )}
          <Button type="button" size="sm" variant="outline" className={cn('ml-auto')} disabled={busy} onClick={onDelete}>
            <Trash2 className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
            Delete shot
          </Button>
        </div>
      )}
      {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
    </div>
  )
}
