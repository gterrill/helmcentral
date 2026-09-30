import type { MaintenanceRule } from '@/hooks/use-maintenance'

// The provenance fields (ADR 0148) every rule view carries. The default is a
// hand rule added for the item, which is what most tests mean by "a rule".
export const ITEM_RULE_PROVENANCE = {
  source: 'item',
  profile_id: '',
  overridden_fields: [],
  not_applicable: false,
  profile_values: null,
  first_at_hours: null,
  supersedes: [],
  removed_from_profile: false,
} satisfies Pick<MaintenanceRule,
  'source' | 'profile_id' | 'overridden_fields' | 'not_applicable' | 'profile_values' | 'first_at_hours' | 'supersedes' | 'removed_from_profile'>

/** A live profile job for equipment item eq-1, e.g. makeProfileJob({ overridden_fields: ['interval_hours'] }). */
export function makeProfileJob(overrides: Partial<MaintenanceRule> = {}): MaintenanceRule {
  return {
    id: 'job:eq-1:engine-oil', equipment_id: 'eq-1', equipment_name: 'Main engine', system: 'propulsion',
    description: 'Engine oil and filter', interval_hours: 250, interval_months: 12,
    due_soon_hours: null, due_soon_months: null, fixed_due_date: '', last_done_at: '',
    last_done_hours: null, profile_service_id: 'engine-oil', procedure_note_id: '',
    ack_reason: '', acknowledged: false, created_at: null, updated_at: null,
    status: 'never_recorded', remaining_hours: null, remaining_days: null,
    hours_unknown: false, has_hour_meter_path: true, hours_as_of: null, current_hours: null,
    ...ITEM_RULE_PROVENANCE,
    source: 'profile',
    profile_id: 'cummins-qsb67-550',
    profile_values: { description: 'Engine oil and filter', interval_hours: 250, interval_months: 12 },
    ...overrides,
  }
}
