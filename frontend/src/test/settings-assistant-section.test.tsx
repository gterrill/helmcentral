import { describe, it, expect, vi } from 'vitest'
import { useState } from 'react'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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

const enabledBaseline: RegularSettingsDraft = { ...initialRegularSettingsDraft, assistantEnabled: true }

function renderSection(overrides: Partial<RegularSettingsDraft> = {}) {
  // Mate is enabled by default here: every other section is hidden while it is off.
  const draftStates: RegularSettingsDraft[] = []

  function Harness() {
    const [draft, setDraft] = useState<RegularSettingsDraft>({ ...initialRegularSettingsDraft, assistantEnabled: true, ...overrides })
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

async function chooseNewModel() {
  const option = await screen.findByRole('option', { name: 'Choose a new model...' })
  fireEvent.pointerDown(option)
  fireEvent.pointerUp(option)
  fireEvent.click(option)
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
    const { latestDraft } = renderSection({ assistantEnabled: false })
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
    expect(draftsEqual(latestDraft(), enabledBaseline)).toBe(false)

    vi.unstubAllGlobals()
  })

  it('shows Auto group fields when OpenRouter Auto is enabled', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ models: [] }) }))
    renderSection({ assistantModel: 'openrouter/auto' })

    expect(screen.getByLabelText('Mate Auto cost tier')).toBeInTheDocument()
    expect(screen.getByLabelText('Mate allowed models')).toBeInTheDocument()
    expect(screen.getByLabelText('Mate excluded models')).toBeInTheDocument()

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

  it('filters model catalog rows by name or id in the dialog', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          models: [
            { id: 'z-ai/glm-5.3', name: 'Z.ai: GLM 5.3', price: 0.001, created_at: '2026-01-01T00:00:00Z' },
            { id: 'anthropic/claude-sonnet-4.5', name: 'Claude Sonnet 4.5', price: 0.002, created_at: '2026-01-01T00:00:00Z' },
          ],
          page: { total_pages: 1 },
        }),
      }),
    )

    renderSection({ assistantModel: 'openrouter/auto' })

    fireEvent.click(screen.getByLabelText('Manage Auto model filters'))
    await screen.findByText('Manage Auto model filters')

    fireEvent.change(screen.getByLabelText('Search model catalog'), {
      target: { value: 'glm 5.3' },
    })

    await waitFor(() => {
      expect(screen.getByText('Z.ai: GLM 5.3')).toBeInTheDocument()
      expect(screen.queryByText('Claude Sonnet 4.5')).not.toBeInTheDocument()
    })

    fireEvent.change(screen.getByLabelText('Search model catalog'), {
      target: { value: 'anthropic/claude-sonnet-4.5' },
    })

    await waitFor(() => {
      expect(screen.getByText('Claude Sonnet 4.5')).toBeInTheDocument()
      expect(screen.queryByText('Z.ai: GLM 5.3')).not.toBeInTheDocument()
    })

    fireEvent.click(screen.getByLabelText('Clear model catalog search'))

    await waitFor(() => {
      expect(screen.getByText('Z.ai: GLM 5.3')).toBeInTheDocument()
      expect(screen.getByText('Claude Sonnet 4.5')).toBeInTheDocument()
    })

    vi.unstubAllGlobals()
  })

  it('shows allowed and excluded lists without clear buttons, then the cost tier', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ models: [] }) }))
    renderSection({
      assistantModel: 'openrouter/auto',
      assistantAllowedModels: ['anthropic/*'],
      assistantExcludedModels: ['openai/gpt-4o-mini'],
    })

    expect(screen.queryByLabelText('Clear allowed models')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Clear excluded models')).not.toBeInTheDocument()
    expect(screen.queryByText(/Managed from the model catalog/)).not.toBeInTheDocument()

    const excluded = screen.getByLabelText('Mate excluded models')
    const costTier = screen.getByLabelText('Mate Auto cost tier')
    expect(excluded.compareDocumentPosition(costTier) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(costTier).toHaveTextContent('No cost cap')
    expect(screen.getByText('Cost cap')).toBeInTheDocument()

    vi.unstubAllGlobals()
  })

  it('renders allowed and excluded models as chips with short names and removes the right one', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ models: [] }) }))
    const { latestDraft } = renderSection({
      assistantModel: 'openrouter/auto',
      assistantAllowedModels: ['anthropic/claude-sonnet-4.5', 'openai/gpt-5'],
      assistantExcludedModels: ['openai/gpt-4o-mini'],
    })

    const allowed = screen.getByLabelText('Mate allowed models')
    expect(within(allowed).getByText('claude-sonnet-4.5')).toHaveAttribute('title', 'anthropic/claude-sonnet-4.5')
    expect(within(allowed).getByText('gpt-5')).toBeInTheDocument()
    const excluded = screen.getByLabelText('Mate excluded models')
    expect(within(excluded).getByText('gpt-4o-mini')).toBeInTheDocument()

    fireEvent.click(screen.getByLabelText('Remove anthropic/claude-sonnet-4.5 from allowed models'))
    expect(latestDraft().assistantAllowedModels).toEqual(['openai/gpt-5'])
    expect(latestDraft().assistantExcludedModels).toEqual(['openai/gpt-4o-mini'])

    fireEvent.click(screen.getByLabelText('Remove openai/gpt-4o-mini from excluded models'))
    expect(latestDraft().assistantExcludedModels).toEqual([])
    expect(latestDraft().assistantAllowedModels).toEqual(['openai/gpt-5'])

    vi.unstubAllGlobals()
  })

  it('shows empty-state copy when no models are allowed or excluded', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ models: [] }) }))
    renderSection({ assistantModel: 'openrouter/auto' })

    expect(within(screen.getByLabelText('Mate allowed models')).getByText('Any tool-capable model')).toBeInTheDocument()
    expect(within(screen.getByLabelText('Mate excluded models')).getByText('None')).toBeInTheDocument()
    expect(screen.getByLabelText('Manage Auto model filters')).toHaveTextContent('Manage')

    vi.unstubAllGlobals()
  })

  it('capitalises the selected cost cap label', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ models: [] }) }))
    renderSection({
      assistantModel: 'openrouter/auto',
      assistantCostTier: 'xhigh' as RegularSettingsDraft['assistantCostTier'],
    })

    expect(screen.getByLabelText('Mate Auto cost tier')).toHaveTextContent('XHigh')

    vi.unstubAllGlobals()
  })

  it('accepts a typed model id in the Choose a model dialog and shows it in the model select', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ models: [], page: { total_pages: 1 } }) }))
    const { latestDraft } = renderSection({ assistantModel: 'anthropic/claude-sonnet-4.5' })

    fireEvent.click(screen.getByLabelText('Mate model'))
    await chooseNewModel()
    await screen.findByText('Choose a model')

    expect(screen.getByLabelText('Model ID')).toHaveAttribute('id')
    expect(screen.getByRole('button', { name: 'Use' })).toBeDisabled()

    fireEvent.change(screen.getByLabelText('Model ID'), { target: { value: ' typesafe/jev-router ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Use' }))

    expect(latestDraft().assistantModel).toBe('typesafe/jev-router')
    await waitFor(() => expect(screen.queryByText('Choose a model')).not.toBeInTheDocument())
    expect(screen.getByLabelText('Mate model')).toHaveTextContent('typesafe/jev-router')

    fireEvent.click(screen.getByLabelText('Mate model'))
    expect(await screen.findByRole('option', { name: 'typesafe/jev-router' })).toBeInTheDocument()

    vi.unstubAllGlobals()
  })

  it('rejects an invalid typed model id and leaves the draft alone', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ models: [], page: { total_pages: 1 } }) }))
    const { latestDraft } = renderSection({ assistantModel: 'anthropic/claude-sonnet-4.5' })

    fireEvent.click(screen.getByLabelText('Mate model'))
    await chooseNewModel()
    await screen.findByText('Choose a model')

    fireEvent.change(screen.getByLabelText('Model ID'), { target: { value: 'not a model' } })
    fireEvent.click(screen.getByRole('button', { name: 'Use' }))

    expect(screen.getByText('Enter an OpenRouter model ID like provider/model.')).toBeInTheDocument()
    expect(latestDraft().assistantModel).toBe('anthropic/claude-sonnet-4.5')
    expect(screen.getByText('Choose a model')).toBeInTheDocument()

    vi.unstubAllGlobals()
  })

  it('does not offer the typed model id form in Auto mode', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ models: [], page: { total_pages: 1 } }) }))
    renderSection({ assistantModel: 'openrouter/auto' })

    fireEvent.click(screen.getByLabelText('Manage Auto model filters'))
    await screen.findByText('Include or exclude tool-capable models for OpenRouter Auto.')

    expect(screen.queryByLabelText('Model ID')).not.toBeInTheDocument()

    vi.unstubAllGlobals()
  })

  it('typing standing notes marks the form dirty', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    const { latestDraft } = renderSection()
    expect(draftsEqual(latestDraft(), enabledBaseline)).toBe(true)

    fireEvent.change(screen.getByLabelText('Mate standing notes'), {
      target: { value: 'Queenfish on a rising tide at Tongue Bay.' },
    })

    expect(draftsEqual(latestDraft(), enabledBaseline)).toBe(false)

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
    expect(draftsEqual(latestDraft(), enabledBaseline)).toBe(false)

    vi.unstubAllGlobals()
  })

  it('toggling Read replies aloud marks the form dirty', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    const { latestDraft } = renderSection()

    fireEvent.click(screen.getByLabelText('Read replies aloud'))

    expect(latestDraft().assistantReadAloud).toBe(true)
    expect(draftsEqual(latestDraft(), enabledBaseline)).toBe(false)

    vi.unstubAllGlobals()
  })

  it('toggling Listen for Hey Mate marks the form dirty', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    const { latestDraft } = renderSection()

    fireEvent.click(screen.getByLabelText('Listen for Hey Mate'))

    expect(latestDraft().assistantWakeWord).toBe(true)
    expect(draftsEqual(latestDraft(), enabledBaseline)).toBe(false)

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
      document_model: '',
      notes: 'Queenfish on a rising tide.',
      allowed_models: [],
      excluded_models: [],
      cost_tier: '',
      voice_input: false,
      read_aloud: false,
      wake_word: false,
      web_search: false,
      web_search_model: 'google/gemini-2.5-flash-lite',
      routines_enabled: false,
      routines_daily_cap_usd: 1,
      passage_limits: expect.objectContaining({ daylight_arrival: false }),
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

  it('toggling Web search marks the form dirty and defaults off', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    const { latestDraft } = renderSection()

    expect(latestDraft().assistantWebSearch).toBe(false)
    fireEvent.click(screen.getByLabelText('Web search'))

    expect(latestDraft().assistantWebSearch).toBe(true)
    expect(draftsEqual(latestDraft(), enabledBaseline)).toBe(false)
    expect(buildRegularSettingsPatch(latestDraft()).assistant?.web_search).toBe(true)
    expect(hydrateDraftFromSettings({ assistant: { web_search: true } }).assistantWebSearch).toBe(true)
    expect(hydrateDraftFromSettings({}).assistantWebSearch).toBe(false)

    vi.unstubAllGlobals()
  })

  it('shows the Search model picker only while Web search is on', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    renderSection()

    expect(screen.queryByLabelText('Search model')).not.toBeInTheDocument()
    fireEvent.click(screen.getByLabelText('Web search'))

    expect(screen.getByLabelText('Search model')).toHaveTextContent('google/gemini-2.5-flash-lite')
    expect(screen.getByText(/a cheap model is fine/)).toBeInTheDocument()

    vi.unstubAllGlobals()
  })

  it('selecting a catalog row sets the search model, from an unfiltered catalog', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        models: [{ id: 'openai/gpt-4o-mini', name: 'GPT-4o mini', price: 0.0001, created_at: '2026-01-01T00:00:00Z' }],
        page: { total_pages: 1 },
      }),
    })
    vi.stubGlobal('fetch', fetchMock)
    const { latestDraft } = renderSection({ assistantWebSearch: true })

    fireEvent.click(screen.getByLabelText('Search model'))
    await chooseNewModel()
    fireEvent.click(await screen.findByRole('button', { name: 'Select' }))

    expect(latestDraft().assistantWebSearchModel).toBe('openai/gpt-4o-mini')
    expect(buildRegularSettingsPatch(latestDraft()).assistant?.web_search_model).toBe('openai/gpt-4o-mini')
    const urls = fetchMock.mock.calls.map((call) => String(call[0]))
    expect(urls.some((url) => url.includes('capability=all'))).toBe(true)
    expect(hydrateDraftFromSettings({ assistant: { web_search_model: 'x/y' } }).assistantWebSearchModel).toBe('x/y')
    expect(hydrateDraftFromSettings({}).assistantWebSearchModel).toBe('google/gemini-2.5-flash-lite')

    vi.unstubAllGlobals()
  })

  it('renders a Document indexing section with a picker and helper text', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    renderSection()

    expect(screen.getByText('Document indexing')).toBeInTheDocument()
    const trigger = screen.getByLabelText('Document indexing model')
    expect(trigger).toHaveAttribute('id', 'assistant-document-model')
    expect(trigger).toHaveTextContent('Default (google/gemini-2.5-flash)')
    expect(screen.getByText(/pick a cheap model that can read images/)).toBeInTheDocument()

    vi.unstubAllGlobals()
  })

  it('opens the image-capable catalog from the document indexing picker', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ models: [], page: { total_pages: 1 } }) })
    vi.stubGlobal('fetch', fetchMock)
    renderSection()

    fireEvent.click(screen.getByLabelText('Document indexing model'))
    await chooseNewModel()

    expect(await screen.findByText('Models that can read images, with server-side sorting and pagination.')).toBeInTheDocument()
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    const urls = fetchMock.mock.calls.map((call) => String(call[0]))
    expect(urls.some((url) => url.startsWith('/api/assistant/models?') && url.includes('capability=images'))).toBe(true)
    expect(urls.some((url) => url.includes('capability=tools'))).toBe(false)

    vi.unstubAllGlobals()
  })

  it('selecting a catalog row sets the document indexing model', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          models: [{ id: 'google/gemini-2.5-flash-lite', name: 'Gemini 2.5 Flash Lite', price: 0.0001, created_at: '2026-01-01T00:00:00Z' }],
          page: { total_pages: 1 },
        }),
      }),
    )
    const { latestDraft } = renderSection()

    fireEvent.click(screen.getByLabelText('Document indexing model'))
    await chooseNewModel()
    fireEvent.click(await screen.findByRole('button', { name: 'Select' }))

    expect(latestDraft().assistantDocumentModel).toBe('google/gemini-2.5-flash-lite')
    expect(latestDraft().assistantModel).toBe(initialRegularSettingsDraft.assistantModel)
    await waitFor(() => expect(screen.getByLabelText('Document indexing model')).toHaveTextContent('google/gemini-2.5-flash-lite'))

    vi.unstubAllGlobals()
  })

  it('returns the document indexing model to the backend default', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ models: [], page: { total_pages: 1 } }) }))
    const { latestDraft } = renderSection({ assistantDocumentModel: 'qwen/qwen2.5-vl-72b-instruct' })

    fireEvent.click(screen.getByLabelText('Document indexing model'))
    const option = await screen.findByRole('option', { name: 'Default (google/gemini-2.5-flash)' })
    fireEvent.pointerDown(option)
    fireEvent.pointerUp(option)
    fireEvent.click(option)

    await waitFor(() => expect(latestDraft().assistantDocumentModel).toBe(''))

    vi.unstubAllGlobals()
  })

  it('accepts a custom model id for document indexing and keeps its recents separate', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ models: [], page: { total_pages: 1 } }) }))
    window.localStorage.clear()
    const { latestDraft } = renderSection()

    fireEvent.click(screen.getByLabelText('Document indexing model'))
    await chooseNewModel()
    await screen.findByText('Choose a model')
    fireEvent.change(screen.getByLabelText('Model ID'), { target: { value: 'qwen/qwen2.5-vl-72b-instruct' } })
    fireEvent.click(screen.getByRole('button', { name: 'Use' }))

    expect(latestDraft().assistantDocumentModel).toBe('qwen/qwen2.5-vl-72b-instruct')
    expect(window.localStorage.getItem('assistant.recent-document-models')).toContain('qwen/qwen2.5-vl-72b-instruct')
    expect(window.localStorage.getItem('assistant.recent-models')).toBeNull()

    vi.unstubAllGlobals()
  })
})

describe('AssistantSection enable gate', () => {
  const hiddenWhenOff = [
    'Use OpenRouter Auto',
    'Mate model',
    'Document indexing model',
    'Mate standing notes',
    'Voice input',
    'Read replies aloud',
    'Listen for Hey Mate',
  ]

  it('shows only the enable switch and its disclosure while Mate is off', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    renderSection({ assistantEnabled: false, assistantModel: 'openrouter/auto' })

    expect(screen.getByLabelText('Enable Mate')).toBeInTheDocument()
    expect(screen.getByText(/Every question sends your position/)).toBeInTheDocument()
    for (const label of hiddenWhenOff) expect(screen.queryByLabelText(label)).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Mate Auto cost tier')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'OpenRouter API key' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Document indexing' })).not.toBeInTheDocument()

    vi.unstubAllGlobals()
  })

  it('shows the other settings once Mate is switched on', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    renderSection({ assistantEnabled: false })

    fireEvent.click(screen.getByLabelText('Enable Mate'))

    for (const label of hiddenWhenOff) expect(screen.getByLabelText(label)).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'OpenRouter API key' })).toBeInTheDocument()

    vi.unstubAllGlobals()
  })
})

describe('document model settings-draft plumbing', () => {
  it('sends a trimmed document_model in the patch', () => {
    const patch = buildRegularSettingsPatch({ ...initialRegularSettingsDraft, assistantDocumentModel: '  google/gemini-2.5-flash-lite ' })
    expect(patch.assistant?.document_model).toBe('google/gemini-2.5-flash-lite')
  })

  it('hydrates and compares assistantDocumentModel', () => {
    const settings: SettingsPayload = { assistant: { document_model: 'google/gemini-2.5-flash' } }
    const draft = hydrateDraftFromSettings(settings)
    expect(draft.assistantDocumentModel).toBe('google/gemini-2.5-flash')
    expect(draftsEqual(draft, { ...draft, assistantDocumentModel: 'other/model' })).toBe(false)
  })

  describe('Routines', () => {
    const routinesPayload = {
      routines: [
        { id: 'r1', name: 'Morning summary', cadence: 'daily', daily_time: '06:00', focus: 'batteries', enabled: true, conversation_id: 'c1', last_run_at: '2026-10-29T06:00:00Z', last_outcome: 'ran', last_note: 'Nothing to report.' },
        { id: 'r2', name: 'Hourly look', cadence: 'hourly', focus: 'bilge', enabled: false, conversation_id: 'c2', last_outcome: 'skipped', last_run_at: '2026-10-29T05:00:00Z', last_note: 'Skipped: routines spent $1.00 of the $1.00 daily limit' },
      ],
      max: 8,
      routines_enabled: true,
      daily_cap_usd: 1,
      spent_24h_usd: 0.4,
    }

    function stubRoutines(calls: Array<{ url: string; method: string; body?: unknown }> = []) {
      vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
        calls.push({ url, method: init?.method ?? 'GET', body: init?.body ? JSON.parse(init.body as string) : undefined })
        if (url.endsWith('/api/assistant/routines')) return { ok: true, json: async () => routinesPayload }
        if (url.includes('/api/assistant/routines/')) return { ok: true, status: 204, json: async () => ({}) }
        return { ok: true, json: async () => ({}) }
      }))
    }

    it('keeps the switch off and the limit at one dollar until changed, and saves both', () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
      const { latestDraft } = renderSection()
      expect(latestDraft().assistantRoutinesEnabled).toBe(false)
      expect(latestDraft().assistantRoutinesDailyCap).toBe('1')

      fireEvent.click(screen.getByLabelText('Let routines run on their own'))
      fireEvent.change(screen.getByLabelText('Daily limit (US dollars)'), { target: { value: '2.50' } })

      expect(draftsEqual(latestDraft(), enabledBaseline)).toBe(false)
      const patch = buildRegularSettingsPatch(latestDraft()).assistant
      expect(patch?.routines_enabled).toBe(true)
      expect(patch?.routines_daily_cap_usd).toBe(2.5)
      const hydrated = hydrateDraftFromSettings({ assistant: { routines_enabled: true, routines_daily_cap_usd: 0.25 } })
      expect(hydrated.assistantRoutinesEnabled).toBe(true)
      expect(hydrated.assistantRoutinesDailyCap).toBe('0.25')
      expect(hydrateDraftFromSettings({}).assistantRoutinesDailyCap).toBe('1')
      vi.unstubAllGlobals()
    })

    it('says why a limit outside the range cannot be saved', () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
      renderSection({ assistantRoutinesDailyCap: '25' })
      expect(screen.getByText('Daily limit: enter between $0.05 and $20.')).toBeInTheDocument()
      vi.unstubAllGlobals()
    })

    it('shows what routines spent and each routine with its last run', async () => {
      stubRoutines()
      renderSection()
      expect(await screen.findByText('Routines spent $0.40 in the last 24 hours')).toBeInTheDocument()
      const rows = await screen.findAllByTestId('routine-row')
      expect(rows).toHaveLength(2)
      expect(within(rows[0]).getByText('Daily at 06:00 (boat time)')).toBeInTheDocument()
      expect(within(rows[0]).getByText(/Nothing to report\./)).toBeInTheDocument()
      expect(within(rows[1]).getByText('Every hour')).toBeInTheDocument()
      expect(within(rows[1]).getByText(/Skipped: routines spent \$1\.00 of the \$1\.00 daily limit/)).toBeInTheDocument()
      vi.unstubAllGlobals()
    })

    it('switches a routine and deletes one after confirming', async () => {
      const calls: Array<{ url: string; method: string; body?: unknown }> = []
      stubRoutines(calls)
      renderSection()
      fireEvent.click(await screen.findByLabelText('Run Hourly look'))
      await waitFor(() => expect(calls.some((c) => c.method === 'PATCH' && c.url.endsWith('/api/assistant/routines/r2'))).toBe(true))
      expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ enabled: true })

      fireEvent.click(screen.getByLabelText('Delete Morning summary'))
      expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
      fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
      await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url.endsWith('/api/assistant/routines/r1'))).toBe(true))
      vi.unstubAllGlobals()
    })

    it('says routines are paused when a failed run\'s cost is not known', async () => {
      vi.stubGlobal('fetch', vi.fn(async (url: string) => ({
        ok: true,
        json: async () => (url.endsWith('/api/assistant/routines') ? { ...routinesPayload, paused_until: '2026-10-30T10:00:00Z' } : {}),
      })))
      renderSection()
      expect(await screen.findByTestId('routines-paused')).toHaveTextContent(/A run failed and what it cost is not known, so routines are paused until/)
      vi.unstubAllGlobals()
    })

    it.each([
      'Routines stopped: the run log could not be saved (disk full). Restart Helmcentral after fixing it.',
      'Routines are not running: assistant.routines_daily_cap_usd is 99, outside $0.05 to $20',
    ])('shows why routines are not running: %s', async (problem) => {
      vi.stubGlobal('fetch', vi.fn(async (url: string) => ({
        ok: true,
        json: async () => (url.endsWith('/api/assistant/routines') ? { ...routinesPayload, problem } : {}),
      })))
      renderSection()
      expect(await screen.findByTestId('routines-problem')).toHaveTextContent(problem)
      vi.unstubAllGlobals()
    })

    it('shows a no-position note as the latest word on a routine', async () => {
      vi.stubGlobal('fetch', vi.fn(async (url: string) => ({
        ok: true,
        json: async () => (url.endsWith('/api/assistant/routines')
          ? { ...routinesPayload, routines: [{ ...routinesPayload.routines[0], last_run_at: '2026-10-27T06:00:00Z', last_skip_at: '2026-10-29T08:01:00Z', last_outcome: 'skipped', last_note: "Skipped: no boat position, so the boat's time is not known" }] }
          : {}),
      })))
      renderSection()
      expect(await screen.findByText(/Skipped: no boat position/)).toBeInTheDocument()
      vi.unstubAllGlobals()
    })

    it('tells the operator to ask Mate when there are no routines', async () => {
      vi.stubGlobal('fetch', vi.fn(async (url: string) => ({
        ok: true,
        json: async () => (url.endsWith('/api/assistant/routines') ? { routines: [], max: 8, routines_enabled: false, daily_cap_usd: 1, spent_24h_usd: 0 } : {}),
      })))
      renderSection()
      expect(await screen.findByText(/No routines yet\. Ask Mate/)).toBeInTheDocument()
      vi.unstubAllGlobals()
    })
  })
})
