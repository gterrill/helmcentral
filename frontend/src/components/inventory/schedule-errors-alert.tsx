import type { MaintenanceScheduleError } from '@/hooks/use-maintenance'

/**
 * Items whose equipment profile is missing or invalid: their service jobs are
 * not listed from any stored copy, so a list of services (the Service schedule,
 * the Jobs page) says so rather than looking complete.
 */
export function ScheduleErrorsAlert({
  errors,
  onOpenEquipment,
}: {
  errors: MaintenanceScheduleError[]
  onOpenEquipment: (id: string) => void
}) {
  if (errors.length === 0) return null
  return (
    <div role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
      <p>
        {errors.length === 1 ? 'One item has' : `${errors.length} items have`} a profile that is missing or
        invalid, so {errors.length === 1 ? 'its' : 'their'} service jobs are not listed:
      </p>
      <ul className="mt-1 flex flex-col gap-0.5 text-xs">
        {errors.map((e) => (
          <li key={e.equipment_id} className="truncate">
            <button type="button" onClick={() => onOpenEquipment(e.equipment_id)} className="font-medium underline">
              {e.equipment_name}
            </button>
            {e.profile_id ? ` (profile: ${e.profile_id})` : ''}: {e.error}
          </li>
        ))}
      </ul>
    </div>
  )
}
