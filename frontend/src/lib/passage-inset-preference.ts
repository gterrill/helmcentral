import { createBooleanPreference } from '@/lib/boolean-preference'

// Whether the passage replay shows the small overview map. One choice for the
// replay and for what the export carries.
const inset = createBooleanPreference('passages.insetMap', false)

export const loadInsetPreference = inset.load
export const saveInsetPreference = inset.save
export const useInsetPreference = inset.use
