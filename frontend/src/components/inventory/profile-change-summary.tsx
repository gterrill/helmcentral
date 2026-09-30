import type { MaintenanceProfileChangePreview } from '@/hooks/use-maintenance'

// ADR 0148: what switching an item's equipment profile does to its service
// schedule, in the operator's words. Shown under the Profile select and again
// in the confirmation before the change is saved.

export function profileChangeIsEmpty(preview: MaintenanceProfileChangePreview): boolean {
  return preview.kept.length === 0 && preview.leaving.length === 0 && preview.new.length === 0
}

function Group({ title, note, entries }: { title: string; note: string; entries: { service_id: string; description: string }[] }) {
  if (entries.length === 0) return null
  return (
    <div className="flex flex-col gap-1">
      <p className="text-xs font-medium text-foreground">{title}</p>
      <p className="text-xs text-muted-foreground">{note}</p>
      <ul className="flex flex-col gap-0.5 text-sm">
        {entries.map((entry) => (
          <li key={entry.service_id} className="truncate">{entry.description || entry.service_id}</li>
        ))}
      </ul>
    </div>
  )
}

export function ProfileChangeSummary({ preview }: { preview: MaintenanceProfileChangePreview }) {
  if (profileChangeIsEmpty(preview)) return null
  return (
    <div className="flex flex-col gap-3 rounded-md border border-border bg-muted/30 p-3">
      <Group
        title="Stay on the schedule"
        note="Same job in both profiles. Last done, acknowledgements and your edits carry over."
        entries={preview.kept}
      />
      <Group
        title="Leave the schedule"
        note="Not in the new profile. They are never due, and their history stays under No longer in the profile until you delete it."
        entries={preview.leaving}
      />
      <Group
        title="Join the schedule"
        note="New jobs from the new profile. They start as never recorded."
        entries={preview.new}
      />
    </div>
  )
}
