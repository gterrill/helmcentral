import { vi } from 'vitest'

import type { SettingsPayload, UseSettingsFormResult } from '@/hooks/use-settings-form'

// One place that builds a useSettingsForm() result for tests that mock the
// hook. The object literal is typed as the full UseSettingsFormResult with no
// optional fields, so adding a field to the hook is a compile error here and
// not a silent `undefined` in every mock.
export function settingsFormMock(
  save: UseSettingsFormResult['save'],
  settings: SettingsPayload = {},
): UseSettingsFormResult {
  return {
    settings,
    setSettings: vi.fn(),
    loading: false,
    loaded: true,
    saving: false,
    error: null,
    save,
  }
}
