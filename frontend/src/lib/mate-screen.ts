import type { AppLocation } from '@/lib/app-location'
import type { AssistantScreenContext } from '@/hooks/use-assistant-chat'

// ADR 0093 voice phase: turns the shell's current location into the
// `screen` context sent with a Mate question, so a reply can open with
// "The operator is looking at the Forecast panel" instead of the model
// guessing from the question text alone. Pure and App-free (same reasoning
// as app-location.ts): AppLocation only carries a dashboard page's id, not
// its name, so the active page's name is threaded in separately rather than
// making this helper reach into the dashboard-pages hook itself.
export function screenContextFor(location: AppLocation, pageName: string | null): AssistantScreenContext {
  if (location.panel === null) {
    return pageName ? { page: pageName } : {}
  }

  if (location.panel === 'settings') {
    return { panel: 'settings', section: location.section ?? 'general' }
  }

  return { panel: location.panel }
}
