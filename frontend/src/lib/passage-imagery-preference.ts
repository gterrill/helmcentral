import { createBooleanPreference } from '@/lib/boolean-preference'

// Whether the passage map and the still images drawn from it show satellite
// imagery. On unless turned off, under its own key (the anchor map keeps its
// own choice).
const imagery = createBooleanPreference('passages.satelliteImagery', true)

export const loadImageryPreference = imagery.load
export const saveImageryPreference = imagery.save
export const useImageryPreference = imagery.use
