import { describe, it, expect, vi } from 'vitest'
import { useState } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { AssistantSection } from '@/components/settings/sections/assistant-section'
import { SecretsStatusProvider } from '@/components/settings/secrets-status-context'
import {
  buildRegularSettingsPatch,
  draftsEqual,
  hydrateDraftFromSettings,
  initialRegularSettingsDraft,
  type RegularSettingsDraft,
} from '@/components/settings/settings-draft'
import type { SettingsPayload } from '@/hooks/use-settings-form'

// ADR 0093: the Assistant settings section and the settings-draft plumbing
// that carries its three fields (enabled, model, notes) through
// hydrate/save, mirroring settings-mayara-section.test.tsx.

function renderSection(overrides: Partial<RegularSettingsDraft> = {}) {
  const draftStates: RegularSettingsDraft[] = []

  function Harness() {
    const [draft, setDraft] = useState<RegularSettingsDraft>({ ...initialRegularSettingsDraft, ...overrides })
    draftStates.push(draft)
    return (
      <SecretsStatusProvider>
        <AssistantSection
          draft={draft}
          onChange={(patch) => setDraft((previous) => ({ ...previous, ...patch }))}
        />
      </SecretsStatusProvider>
    )
  }

  render(<Harness />)
  return { latestDraft: () => draftStates[draftStates.length - 1] }
}

describe('AssistantSection', () => {
  it('renders the enable switch, model select and standing notes by aria-label', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    renderSection()

    expect(screen.getByLabelText('Enable Mate')).toBeInTheDocument()
    expect(screen.getByLabelText('Use OpenRouter Auto')).toBeInTheDocument()
    expect(screen.getByLabelText('Mate model')).toBeInTheDocument()
    expect(screen.queryByLabelText('Mate Auto cost tier')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Mate allowed models')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Mate excluded models')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Mate standing notes')).toBeInTheDocument()

    vi.unstubAllGlobals()
  })

  it('toggling the switch marks the form dirty', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    const { latestDraft } = renderSection()
    expect(draftsEqual(latestDraft(), initialRegularSettingsDraft)).toBe(true)

    fireEvent.click(screen.getByLabelText('Enable Mate'))

    expect(draftsEqual(latestDraft(), initialRegularSettingsDraft)).toBe(false)

    vi.unstubAllGlobals()
  })

  it('toggling OpenRouter Auto sets the model to openrouter/auto', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ models: [] }) }))
    const { latestDraft } = renderSection({ assistantModel: 'anthropic/claude-sonnet-4.5' })

    fireEvent.click(screen.getByLabelText('Use OpenRouter Auto'))

    expect(latestDraft().assistantModel).toBe('openrouter/auto')
    expect(draftsEqual(latestDraft(), initialRegularSettingsDraft)).toBe(false)

    vi.unstubAllGlobals()
  })

  it('shows Auto group fields when OpenRouter Auto is enabled', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ models: [] }) }))
    renderSection({ assistantModel: 'openrouter/auto' })

    expect(screen.getByLabelText('Mate Auto cost tier')).toBeInTheDocument()
    expect(screen.getByLabelText('Mate allowed models')).toHaveAttribute('readonly')
    expect(screen.getByLabelText('Mate excluded models')).toHaveAttribute('readonly')

    vi.unstubAllGlobals()
  })

  it('uses dialog Include/Exclude actions to manage Auto model filters', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          models: [
            { id: 'anthropic/claude-sonnet-4.5', name: 'Claude Sonnet 4.5', price: 0.002, created_at: '2026-01-01T00:00:00Z' },
          ],
          page: { total_pages: 1 },
        }),
      }),
    )
    const { latestDraft } = renderSection({ assistantModel: 'openrouter/auto' })

    fireEvent.click(screen.getByLabelText('Manage Auto model filters'))

    expect(await screen.findByText('Manage Auto model filters')).toBeInTheDocument()

    fireEvent.click(await screen.findByRole('button', { name: 'Include' }))
    expect(latestDraft().assistantAllowedModels).toEqual(['anthropic/claude-sonnet-4.5'])
    expect(latestDraft().assistantExcludedModels).toEqual([])

    fireEvent.click(await screen.findByRole('button', { name: 'Exclude' }))
    expect(latestDraft().assistantAllowedModels).toEqual([])
    expect(latestDraft().assistantExcludedModels).toEqual(['anthropic/claude-sonnet-4.5'])

    vi.unstubAllGlobals()
  })

  it('clears allowed and excluded model lists with clear buttons', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ models: [] }) }))
    const { latestDraft } = renderSection({
      assistantModel: 'openrouter/auto',
      assistantAllowedModels: ['anthropic/*'],
      assistantExcludedModels: ['openai/gpt-4o-mini'],
    })

    fireEvent.click(screen.getByLabelText('Clear allowed models'))
    expect(latestDraft().assistantAllowedModels).toEqual([])

    fireEvent.click(screen.getByLabelText('Clear excluded models'))
    expect(latestDraft().assistantExcludedModels).toEqual([])

    vi.unstubAllGlobals()
  })

  it('typing standing notes marks the form dirty', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    const { latestDraft } = renderSection()
    expect(draftsEqual(latestDraft(), initialRegularSettingsDraft)).toBe(true)

    fireEvent.change(screen.getByLabelText('Mate standing notes'), {
      target: { value: 'Queenfish on a rising tide at Tongue Bay.' },
    })

    expect(draftsEqual(latestDraft(), initialRegularSettingsDraft)).toBe(false)

    vi.unstubAllGlobals()
  })

  // ADR 0093 voice phase: three switches, all off by default.
  it('renders the three voice switches by aria-label', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    renderSection()

    expect(screen.getByLabelText('Voice input')).toBeInTheDocument()
    expect(screen.getByLabelText('Read replies aloud')).toBeInTheDocument()
    expect(screen.getByLabelText('Listen for Hey Mate')).toBeInTheDocument()

    vi.unstubAllGlobals()
  })

  it('toggling Voice input marks the form dirty', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    const { latestDraft } = renderSection()

    fireEvent.click(screen.getByLabelText('Voice input'))

    expect(latestDraft().assistantVoiceInput).toBe(true)
    expect(draftsEqual(latestDraft(), initialRegularSettingsDraft)).toBe(false)

    vi.unstubAllGlobals()
  })

  it('toggling Read replies aloud marks the form dirty', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    const { latestDraft } = renderSection()

    fireEvent.click(screen.getByLabelText('Read replies aloud'))

    expect(latestDraft().assistantReadAloud).toBe(true)
    expect(draftsEqual(latestDraft(), initialRegularSettingsDraft)).toBe(false)

    vi.unstubAllGlobals()
  })

  it('toggling Listen for Hey Mate marks the form dirty', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    const { latestDraft } = renderSection()

    fireEvent.click(screen.getByLabelText('Listen for Hey Mate'))

    expect(latestDraft().assistantWakeWord).toBe(true)
    expect(draftsEqual(latestDraft(), initialRegularSettingsDraft)).toBe(false)

    vi.unstubAllGlobals()
  })
})

describe('assistant settings-draft plumbing', () => {
  it('buildRegularSettingsPatch emits the assistant block', () => {
    const draft: RegularSettingsDraft = {
      ...initialRegularSettingsDraft,
      assistantEnabled: true,
      assistantModel: 'anthropic/claude-sonnet-4.5',
      assistantNotes: 'Queenfish on a rising tide.',
    }

    const patch = buildRegularSettingsPatch(draft)

    expect(patch.assistant).toEqual({
      enabled: true,
      model: 'anthropic/claude-sonnet-4.5',
      notes: 'Queenfish on a rising tide.',
      allowed_models: [],
      excluded_models: [],
      cost_tier: '',
      voice_input: false,
      read_aloud: false,
      wake_word: false,
    })
  })

  it('trims the model id when building the patch', () => {
    const draft: RegularSettingsDraft = {
      ...initialRegularSettingsDraft,
      assistantModel: '  anthropic/claude-sonnet-4.5  ',
    }

    const patch = buildRegularSettingsPatch(draft)

    expect(patch.assistant?.model).toBe('anthropic/claude-sonnet-4.5')
  })

  it('hydrateDraftFromSettings round-trips the assistant block', () => {
    const settings: SettingsPayload = {
      assistant: {
        enabled: true,
        model: 'anthropic/claude-opus-4',
        notes: 'Standing notes here.',
        allowed_models: ['anthropic/*', 'openai/gpt-5*'],
        excluded_models: ['openai/gpt-4o-mini'],
        cost_tier: 'high',
      },
    }

    const draft = hydrateDraftFromSettings(settings)

    expect(draft.assistantEnabled).toBe(true)
    expect(draft.assistantModel).toBe('anthropic/claude-opus-4')
    expect(draft.assistantNotes).toBe('Standing notes here.')
    expect(draft.assistantAllowedModels).toEqual(['anthropic/*', 'openai/gpt-5*'])
    expect(draft.assistantExcludedModels).toEqual(['openai/gpt-4o-mini'])
    expect(draft.assistantCostTier).toBe('high')
  })

  // A blank model on the server (nothing configured yet, or explicitly
  // cleared) must hydrate as blank, not silently acquire the default model
  // id — same reasoning as mayaraAddress.
  it('hydrates a blank model as blank', () => {
    const settings: SettingsPayload = { assistant: { enabled: false, model: '', notes: '' } }

    const draft = hydrateDraftFromSettings(settings)

    expect(draft.assistantModel).toBe('')
  })

  it('hydrates an absent assistant block to the initial defaults', () => {
    const draft = hydrateDraftFromSettings({})

    expect(draft.assistantEnabled).toBe(false)
    expect(draft.assistantModel).toBe('anthropic/claude-sonnet-4.5')
    expect(draft.assistantNotes).toBe('')
    expect(draft.assistantAllowedModels).toEqual([])
    expect(draft.assistantExcludedModels).toEqual([])
    expect(draft.assistantCostTier).toBe('')
  })

  it('trims assistant allowed/excluded model entries and cost tier when building the patch', () => {
    const draft: RegularSettingsDraft = {
      ...initialRegularSettingsDraft,
      assistantAllowedModels: ['  anthropic/*  ', ' ', 'openai/gpt-5*'],
      assistantExcludedModels: ['  openai/gpt-4o-mini  ', ''],
      assistantCostTier: '  xhigh  ' as RegularSettingsDraft['assistantCostTier'],
    }

    const patch = buildRegularSettingsPatch(draft)

    expect(patch.assistant?.allowed_models).toEqual(['anthropic/*', 'openai/gpt-5*'])
    expect(patch.assistant?.excluded_models).toEqual(['openai/gpt-4o-mini'])
    expect(patch.assistant?.cost_tier).toBe('xhigh')
  })

  // ADR 0093 voice phase: voice_input/read_aloud/wake_word, all default false.
  it('buildRegularSettingsPatch emits the voice switches', () => {
    const draft: RegularSettingsDraft = {
      ...initialRegularSettingsDraft,
      assistantVoiceInput: true,
      assistantReadAloud: true,
      assistantWakeWord: true,
    }

    const patch = buildRegularSettingsPatch(draft)

    expect(patch.assistant).toMatchObject({
      voice_input: true,
      read_aloud: true,
      wake_word: true,
    })
  })

  it('hydrateDraftFromSettings round-trips the voice switches', () => {
    const settings: SettingsPayload = {
      assistant: { voice_input: true, read_aloud: true, wake_word: true },
    }

    const draft = hydrateDraftFromSettings(settings)

    expect(draft.assistantVoiceInput).toBe(true)
    expect(draft.assistantReadAloud).toBe(true)
    expect(draft.assistantWakeWord).toBe(true)
  })

  it('hydrates the voice switches to false when the server omits them', () => {
    const draft = hydrateDraftFromSettings({})

    expect(draft.assistantVoiceInput).toBe(false)
    expect(draft.assistantReadAloud).toBe(false)
    expect(draft.assistantWakeWord).toBe(false)
  })
})
