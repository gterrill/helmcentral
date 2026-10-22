import { createBooleanPreference } from '@/lib/boolean-preference'

// Whether the passage replay map shows the terrain in 3D. On unless turned
// off, under its own key (separate from the satellite imagery choice).
const replay3d = createBooleanPreference('passages.replay3d', true)

export const load3dPreference = replay3d.load
export const save3dPreference = replay3d.save
export const use3dPreference = replay3d.use
