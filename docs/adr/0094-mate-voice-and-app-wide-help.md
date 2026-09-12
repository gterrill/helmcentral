# ADR 0094: Mate, Voice and App-Wide Help

## Status
Accepted, extends ADR 0093.

## Context

The crew wanted the assistant called Mate, and wanted to ask it a question by
voice from wherever they happened to be looking, not just from the Assistant
panel: "Hey Mate, explain how the upper atmosphere graph works" while sitting
on the Forecast panel, and an answer without navigating away.

Two facts shaped what was buildable now, and one shaped what was added
alongside the voice work.

Every browser refuses microphone access on an insecure origin. Helmcentral is
served over plain http on the boat, with no TLS code of its own, so voice
needs an https path before any speech code is useful there. The app already
reaches one: web push needed a secure context for the same reason (see
`docs/reference/configuration.md`'s Tailscale section), so the same
`https://<machine>.<tailnet>.ts.net` address the crew already opens for push
notifications is what voice uses too. Nothing new had to be stood up for
this.

The helm is a tablet, most likely Safari on an iPad, with a laptop at the nav
station; the wall kiosk has no microphone and is a display, not a control
surface.

Once the panel could take a question from anywhere, it became clear the
assistant could also answer questions about Helmcentral itself, not just the
sea, provided it could read the same operator manual a person would.

## Decision

### 1. The name is a string change only

"Mate" is what the crew sees: the sidebar label, the settings section
heading, the panel title, the system prompt's identity line ("You are Mate,
the onboard passage-planning assistant..."). Every identifier stays
`assistant`: the backend files (`assistant_prompt.go`, `assistant_handlers.go`,
`assistant_manual.go`, `assistant_tools.go`), the `/api/assistant/...` routes,
the `assistant:` settings block, the `assistant.sqlite` file. Renaming any of
those for a cosmetic change would be churn with no reader benefit.

### 2. The listener lives in the app shell, not the panel

`useMateVoice` mounts once in `App.tsx`, beside the header, rather than
inside the Assistant panel. That is what makes push-to-talk, and later "Hey
Mate", work on every panel and every dashboard page rather than only the one
place the assistant used to live. The kiosk shell never mounts it: no
microphone, and a wall display is nothing to talk to.

### 3. A right-hand sheet over the current page

A voice question, or the header's "Ask Mate" button, opens a sheet on the
right of whatever page is behind it, holding the same thread component
(`AssistantThread`) the full Assistant panel uses, extracted once so neither
copy can drift from the other. The forecast, or whatever panel was open,
stays visible behind the answer. The full panel remains for a longer working
session; the sheet is the quick channel that does not require leaving the
page you were already looking at.

The sheet holds exactly one thread: the current conversation, plus the
composer, nothing more. It keeps appending to that conversation - the most
recently updated one - across as many opens and closes as the operator
likes, and there is no timer or staleness check that starts a new one on
its own. Whether a run of questions belongs together is the operator's
call, not a clock's: only the sheet header's "New conversation" button
starts a fresh thread, and its "Open in Mate" button hands the current one
to the full panel (`onOpenPanel`, closing the sheet) for whenever a quick
question turns into a longer session that wants the conversation list and
history the panel keeps and the sheet deliberately does not.

### 4. A question carries what is on screen and whether it was spoken

The POST that asks a question can carry a `screen` object naming the panel,
the settings section, or the dashboard page the operator was looking at. The
backend turns that into one sentence at a fixed point in the system prompt:
"The operator is looking at the Forecast panel." A question can also carry
`spoken: true`, when it came from a microphone tap or a wake word rather than
typing. That adds one instruction to the same turn's prompt: end the answer
with a `## Spoken summary` heading followed by at most three sentences,
something short enough to read aloud. Neither field changes the prompt at all
when absent, which is the ordinary case for a typed question.

### 5. The operator manual is staged, embedded and served like the frontend

`docs/features`, `docs/how-to` and `docs/reference` are copied into
`backend/manual` by exactly the mechanism `backend/dist` already gets the
built frontend from: the Makefile's `manual-stage` target, the Dockerfile,
`.goreleaser.yaml`, and the dev compose stack's bind mounts for hot reload.
`backend/assistant_manual.go` embeds that tree with `go:embed all:manual`,
loads it once at startup, and lists every page's id and title in the system
prompt so the model knows what it can ask for before it asks. `read_manual`
returns one page, or one section of a page by its heading, and quotes it
back rather than paraphrasing. A build that skipped staging, a bare
`go build` with no `make manual-stage` run first, does not serve an empty
manual silently: `read_manual` reports "the manual is not embedded in this
build (run make manual-stage)" so the gap is visible immediately rather than
discovered as a suspiciously thin answer later.

### 6. Browser speech APIs, not a server-side recognizer

`hooks/use-speech-input.ts` wraps the browser's own `SpeechRecognition`
(`webkitSpeechRecognition` on Safari and Chrome), language `en-AU`. This
needs no key and no new dependency, and it is what the helm tablet already
does for dictation elsewhere. Recognition quality then depends on the
browser: Safari uses Apple's on-device dictation on recent hardware, Chrome
sends audio to Google's servers unless an on-device option is available, and
Firefox has no implementation at all. That difference is real and is said
plainly in the docs rather than hidden behind one blanket claim about how
voice works.

### 7. Read-aloud through `speechSynthesis`, primed from the tap

`hooks/use-speech-output.ts` wraps `window.speechSynthesis`, preferring an
`en-AU` voice, then `en-GB`, then whatever the platform defaults to. iOS
Safari only allows synthesis to start from a user gesture, and the actual
reply arrives later from a network callback, which is not one; a silent
priming utterance is spoken from the same tap that starts push-to-talk so the
real speech later in that same interaction chain is allowed to play.

### 8. Push-to-talk from the header and Alt+M

A microphone button sits in the header next to the vessel status bar,
visible whenever voice input is switched on and the session can write.
`Alt+M` does the same from anywhere in the shell except while typing into a
field, and Escape cancels a listening session. Both are wired once in
`App.tsx`, not per panel, matching where the listener itself lives.

### 9. "Hey Mate" as a browser-only experiment, off by default

`useMateVoice` can also run `SpeechRecognition` continuously: restarted
whenever the browser stops it on its own (Chrome after a silence, Safari
after roughly a minute), watching every final transcript for a leading wake
phrase. A bare "Hey Mate" with nothing after it arms an eight-second window
for the question that follows, rather than requiring the whole sentence in
one breath. Listening pauses whenever the tab is hidden, since an engine room
or a pocket is exactly where a continuous microphone would otherwise burn
battery and stream audio for nothing, and it stops trying to restart itself
entirely after the browser reports the microphone blocked or absent, rather
than retrying against a permission that was just refused. The switch that
turns this on is off by default, and its own description in Settings names
what it costs rather than only what it does.

### 10. Three settings switches, not one

Settings → Mate → Voice has three switches: "Voice input" (push-to-talk),
"Read replies aloud" (the spoken summary read back), and "Listen for Hey
Mate" (the wake-word experiment). Each is its own switch because each carries
a cost the others do not: voice input needs https, read-aloud interrupts
whoever else is in the cabin, and the wake word costs battery and, in
Chrome, a standing stream to Google.

### Rejected

**A Picovoice-style on-device wake-word engine, for now.** Accurate and low
power, since no audio leaves the device until the keyword fires, but it
needs a licence key living in the browser and a new dependency for a feature
still being evaluated. Recorded as the fallback if the browser-only "Hey
Mate" experiment disappoints once it has had time on the boat.

**Server-side speech recognition.** Another paid key and a large dependency
for something the browsers already reaching this app already do themselves.

**Voice on the kiosk.** The wall display has no microphone and is a display,
not a control surface; there is nothing to listen with and no one addressing
it as "Mate" from across the saloon.

**Voice control of equipment.** Mate stays read-only. Asking it a question is
one thing; having it start or stop something aboard by voice is a different
feature with a different risk, and it was never on the table here.

**A physical button.** A Bluetooth media button or foot switch mapped to
push-to-talk would be the most reliable trigger at a helm with engines
running, cheaper to add than the wake word once push-to-talk exists at all.
Recorded as the cheap fallback, not built in this pass.

## Consequences

- "Hey Mate" costs battery whenever it is switched on, and on Chrome it
  streams audio to Google continuously rather than only when a question is
  actually asked. False triggers from ordinary cockpit conversation are an
  expected cost of this design, not a bug to chase down, and the switch's own
  description says so rather than promising quiet.
- A spoken reply is the three-sentence summary, never a substitute for the
  written briefing: the full answer still renders exactly as it did before
  voice existed, and the spoken summary is additional, not instead.
- The manual now doubles as Mate's help system. An edit to a `docs/features`,
  `docs/how-to` or `docs/reference` page changes what Mate says the next time
  someone asks about that feature, which makes documentation accuracy a
  correctness property of the assistant's answers, not only of the docs
  themselves.
- `read_manual` adds one extra round to the tool-calling loop whenever it
  fires, and that round costs real money on top of the question itself:
  measured on 2026-09-12 against the reference model, asking Mate to explain
  the upper-air chart took one `read_manual` round, 9,476 tokens end to end,
  and cost $0.036, roughly three to four cents more than the same question
  would have cost without it.

## Related

- ADR 0093 (onboard assistant over OpenRouter): the assistant this ADR names
  Mate and extends with voice and the manual tool.
- ADR 0074 (deep links): the routing this ADR's `screen` context reads to
  know what panel, settings section or dashboard page the operator is on.
- ADR 0040 (SignalK delegated authentication): the write-tier role a voice
  question still has to pass, the same as a typed one.
- ADR 0089 (kiosk feed is a page flag): the shell that never mounts
  `useMateVoice` at all.
- ADR 0023 (encrypted secrets store): the OpenRouter key voice questions
  still spend, unchanged by anything in this ADR.
