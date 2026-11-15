import { createBooleanPreference } from '@/lib/boolean-preference'

// Whether the Forecast page shows its plans sidebar. Open unless the operator
// hid it; each screen keeps its own choice.
export const forecastPlansOpen = createBooleanPreference('forecast.plans.open', true)

export const loadForecastPlansOpen = forecastPlansOpen.load
export const saveForecastPlansOpen = forecastPlansOpen.save
export const useForecastPlansOpen = forecastPlansOpen.use
