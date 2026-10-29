import { settingsSectionOf, type AppLocation } from '@/lib/app-location'
import type { AssistantScreenContext } from '@/hooks/use-assistant-chat'

// ADR 0093 voice phase: turns the shell's current location into the
// `screen` context sent with a Mate question, so a reply can open with
// "The operator is looking at the Forecast panel" instead of the model
// guessing from the question text alone. Pure and App-free (same reasoning
// as app-location.ts): AppLocation only carries a dashboard page's id, not
// its name, so the active page's name is threaded in separately rather than
// making this helper reach into the dashboard-pages hook itself.
export function screenContextFor(location: AppLocation, pageName: string | null, documentTitle?: string | null): AssistantScreenContext | undefined {
  // On the Mate panel the screen is Mate itself, which says nothing about
  // what a question is about.
  if (location.panel === 'assistant') return undefined

  if (location.panel === null) {
    return pageName ? { page: pageName } : {}
  }

  if (location.panel === 'settings') {
    return { panel: 'settings', section: settingsSectionOf(location.route) }
  }

  // ADR 0167: a document open on its Details page is what a question asked
  // from the Mate sheet most likely means by "this".
  if (location.panel === 'documents' && location.route.view === 'details') {
    return {
      panel: 'documents',
      document_id: location.route.id,
      ...(documentTitle ? { document_title: documentTitle } : {}),
    }
  }

  return { panel: location.panel }
}

/** The alarm a turn is about (ADR 0226): the card's id and title. */
export interface MateAlarmContext {
  id: string
  title: string
}

// Layers the alarm the operator tapped "Ask Mate about this" on over the screen
// they are on. The backend reads alarm_id and tells Mate to call
// get_alarm_context with it; the title only names the alarm in that sentence.
export function withAlarmContext(screen: AssistantScreenContext | undefined, alarm: MateAlarmContext | null): AssistantScreenContext | undefined {
  if (!alarm) return screen
  return { ...screen, alarm_id: alarm.id, alarm_title: alarm.title }
}

/** The watch run a turn is about (ADR 0227): the run's id and its watch's name. */
export interface MateWatchRunContext {
  id: string
  name: string
}

// Layers the run the operator tapped "Ask Mate about this run" on over the
// screen they are on. The backend reads watch_run_id and tells Mate to read
// that run; the name only names the watch in that sentence.
export function withWatchRunContext(screen: AssistantScreenContext | undefined, run: MateWatchRunContext | null): AssistantScreenContext | undefined {
  if (!run) return screen
  return { ...screen, watch_run_id: run.id, watch_name: run.name }
}
