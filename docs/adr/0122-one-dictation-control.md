# ADR 0122: One Dictation Control

## Status

Accepted (2026-09-22).

## Context

Helmcentral had two microphones that looked almost identical and did
different things. The header's "Talk to Mate" button (App.tsx, ADR 0093/
0094) is push-to-talk: tap it, speak, and the transcript sends itself to
Mate the moment recognition ends. The note capture sheet
(components/documents/note-capture-sheet.tsx, ADR 0119) had its own
microphone in the footer, away from the textarea, that appended whatever it
heard to the box and never sent anything - dictation, not a question. The
Mate chat composer (components/assistant-thread.tsx) had no microphone at
all, so a skipper who wanted to add a follow-up by voice while typing had no
way to.

Both mic buttons used the same idle affordance: a ghost icon that turned
`text-primary` while listening. At the helm in direct sun, or on deck with
wet hands, that colour shift is close to invisible - a low-contrast tint on
a small glyph is exactly what DESIGN.md's micro-typography rules warn against
for text, and the same physics apply to an icon. The only other signal was a
`title` tooltip, which never shows on a touchscreen at all. An operator had
no reliable way to tell, at a glance, whether a microphone was live.

Separately, `hooks/use-speech-input.ts`'s only way to end a session was
`stop()`, which aborts the recognizer outright. That is correct for Escape
or a cancel, where the operator wants out and whatever was half-said doesn't
matter. It is wrong for an operator tapping the mic to say "I'm done
talking": abort() discards whatever phrase the browser was still in the
middle of recognising, so the last few words of a dictation could vanish
right when the operator stopped to look at what they'd said.

Wake mode ("Hey Mate," `hooks/use-mate-voice.ts`) already had to solve "only
one recognizer at a time" once, for push-to-talk taking over from a running
wake-word session - both live inside the same hook and share one
`useSpeechInput` instance, so starting push-to-talk's session simply aborts
wake's. Adding a microphone to two more fields, in two components that have
no reach into `use-mate-voice.ts` at all, reopened the same problem from
outside: a dictating field and a continuous wake-word listener are two
independent `SpeechRecognition` sessions fighting over one microphone.

## Decision

### One button asks, one button dictates

The header mic keeps doing exactly what it did: push-to-talk, sends by
itself, the only voice control in the app that does. Every other
microphone - the Mate composer, the note capture sheet - dictates: it
appends to the field it lives in and never sends or submits anything. The
operator's own tap on Send or Capture is still what sends. This is a naming
and placement rule as much as a behavioural one: a mic *inside* a text field
dictates into that field; a mic that is not inside a field (the header's)
is the one that acts on its own.

### `finish()` alongside `stop()`

`use-speech-input.ts` gains `finish()`: it calls the recognizer's own
`stop()`, not `abort()`. A real recognizer still delivers whatever phrase it
was mid-recognising as one last final result before firing `end`, so
`listening` clears the ordinary way - from that `end` event - rather than
being cleared out from under it. `stop()` (the existing method) keeps its
behaviour unchanged: abort immediately, discard whatever's in flight,
`listening` clears synchronously. Escape and cancel call `stop()`. Tapping
the mic to end a dictation calls `finish()`. They are not interchangeable,
and the hook's own comments say so next to each method.

### `components/dictation.tsx`: one implementation, two fields

`useDictation` wraps `useSpeechInput` and owns append semantics: a final
result is skipped if it's empty after trimming, otherwise it's joined onto
the field's current value with a single space if the field already has text.
Both call sites pass their own `setState` function straight in - no field
state lives inside the hook.

`<DictateButton>` is sized and placed to sit beside a composer's other
`InputGroupButton`s (Paperclip, Send): ghost and a plain `Mic` icon while
idle, filled `variant="default"` (the same treatment Send already has) with
a `Square` icon while listening, so "this is recording" is a solid colour
change, not a tint. No speech API at all: renders nothing, the same rule the
note sheet's old mic already followed. An insecure origin: still rendered,
disabled, `MicOff`.

`<DictationStatus>` is a visible, `aria-live="polite"` line inside the
field's own block-end addon: "Listening…" until the first interim words
land, then the interim transcript itself, in `text-sm text-foreground` -
sized to actually read in daylight, not micro-text, because this is content
the operator has to read back, not a label.

`<DictationError>` renders the same `role="alert"`, `text-[11px]
text-destructive` line the composer already uses for upload errors,
directly under the field. An insecure-context reason renders in the same
spot, muted rather than red, because unlike a real recognition error it's a
standing fact about how the app was opened, not something that just failed.
Both cases exist because a `title` tooltip is not a channel a touchscreen
operator can read.

Escape while dictating is scoped to the field
(`event.stopPropagation()` in the field's own `onKeyDown`), not a window
listener - the note capture sheet is itself a Sheet that closes on Escape,
and a dictating operator hitting Escape means "stop dictating," not "close
the sheet out from under me."

### `lib/voice-arbiter.ts`: a claim, not a context

A dictating field and `use-mate-voice.ts`'s wake-word listener are two
independent `useSpeechInput` instances in two different component subtrees,
so they have no ref or state in common to coordinate through the way
push-to-talk and wake mode already do inside one hook. `voice-arbiter.ts` is
a small module-level, ref-counted claim: `claimVoice()` returns a release
function, `getVoiceClaimSnapshot()` reads whether anything currently holds
one, `subscribeVoiceClaim()` notifies on a transition. `useDictation` claims
it before it starts its own recognition, and releases once its own
recognition stops (finish, cancel, or the recognizer's own end/error).
`use-mate-voice.ts` subscribes directly and pauses wake mode's recognition
the moment a claim appears, resuming once it lifts - the same pause/resume
shape the hook already uses for a hidden tab, applied to a second cause.

Claiming happens before the recognizer starts, and the subscriber's own
reaction runs synchronously inside that same call: `notify()` calls every
subscriber directly, in the same call stack as `claimVoice()`, not through a
state update and a later render. A caller that claims and then immediately
starts its own recognizer - which is exactly what `useDictation`'s `start()`
does - needs wake mode already stopped by the time it does that, not on
whatever render happens to come next. An earlier build of this claim read it
reactively (`useSyncExternalStore` driving a `useEffect`), which let the two
recognizers overlap for the gap between the claim and the next render; this
was caught in review and fixed before anything shipped.

A plain module singleton, not a React context: there is exactly one
`useMateVoice` instance for the app's lifetime (App.tsx mounts it once, ADR
0094), and any number of dictating fields that mount and unmount with
whatever sheet or composer is open. A context would only buy indirection for
a relationship that already has one fixed listener. Ref-counted rather than
a boolean so two overlapping claims - unlikely, but not ruled out if a
second field mounts before the first's cleanup runs - resolve correctly:
the claim only actually lifts once every claimant has released.

### Push-to-talk wins

Claiming handles a dictating field taking the microphone away from wake
mode. The other direction - the operator tapping push-to-talk while a field
is still dictating - is a separate signal, `preemptVoice()`/
`subscribeVoicePreempt()` on the same module. Push-to-talk is a deliberate,
explicit action: the operator pressed a button (or Alt+M) meaning to talk to
Mate right now, so it does not wait its turn behind a field that happens to
be listening. `pushToTalk()` calls `preemptVoice()` before it starts its own
recognition; `useDictation` subscribes and stops (and releases its claim)
when preempted, the same way it stops on Escape or a tap on its own mic. A
no-op in the ordinary case where nothing is dictating.

### Header mic: hide, don't grey out, for no API

`unsupportedReason === 'no-api'` now hides the header's push-to-talk button
entirely, matching the rule `DictateButton` already follows - a disabled
button with a tooltip nobody can read on a touchscreen looked broken rather
than gated. An insecure origin still shows the button, disabled, with
`MicOff` and a `title`: unlike a field's mic, the header sits in a
non-touch-first toolbar next to other titled buttons, and [Talk to
Mate](../how-to/talk-to-mate.md) already walks through reaching the app over
https. Alt+M and Escape are unchanged, and still work even while the button
is hidden - the keyboard shortcut has no visibility to hide.

## Rejected

**Making the header mic dictate into whatever field has focus, instead of
building two separate controls.** Sending by voice from anywhere in the app
is the entire reason push-to-talk exists (ADR 0094); collapsing it into
dictation would mean either losing hands-free sending or making an ordinary
field's mic sometimes send without warning, depending on focus the operator
might not be thinking about. Two controls with two clearly different
behaviours beat one control with a hidden mode.

**A React context for the recognizer claim.** Considered and rejected in
the Decision section above - a context adds provider wiring for a
relationship (`use-mate-voice.ts`'s one running instance, and any number of
transient dictation claimants) that a module singleton already expresses
correctly with less code.

## Consequences

- `use-speech-input.ts` gains `finish()`; every existing caller of `stop()`
  is unaffected, since `stop()`'s own behaviour did not change.
- `components/dictation.tsx` is a new shared module. Both
  `note-capture-sheet.tsx` and `assistant-thread.tsx` import it rather than
  calling `useSpeechInput` directly, which is a change to the note sheet's
  own implementation - the `Auto` type default, local classification and
  everything else ADR 0119 decided about that sheet are unaffected.
- `lib/voice-arbiter.ts` is a new dependency of `use-mate-voice.ts`. Wake
  mode now has a third reason to pause besides "not desired" and "tab
  hidden" - a dictating field claiming the microphone elsewhere in the app.
- The note capture sheet's mic moved from a separate row below the textarea
  into the textarea's own field (an `InputGroup`, the same shape the Mate
  composer already used), and its labels changed from "Dictate a note"/
  "Stop dictation" to "Dictate"/"Stop dictation" to match the composer's.
  `docs/features/notes-and-the-manual.md` and
  `docs/how-to/write-the-boats-manual.md` describe the button's presence and
  behaviour, not its former row, so no wording there needed to change beyond
  what was already accurate.
- `docs/features/assistant.md` and `docs/how-to/talk-to-mate.md` now
  distinguish the header's asking mic from the composer's dictating one, and
  describe the header mic as absent (not disabled) on a browser with no
  speech recognition.
- `lib/voice-arbiter.ts` also carries `preemptVoice()`/
  `subscribeVoicePreempt()`, a one-way signal separate from the claim
  itself, so push-to-talk can win over a field that is still dictating (see
  "Push-to-talk wins" above) instead of starting a second recognition
  alongside it.

## Related

- [ADR 0093](0093-onboard-assistant-over-openrouter.md): the assistant this
  voice work sits on top of.
- [ADR 0094](0094-mate-voice-and-app-wide-help.md): push-to-talk, "Hey
  Mate," and why `useMateVoice` mounts once in the app shell rather than per
  panel - the ADR this one extends with a second, independent recognizer
  that has to coexist with the first.
- [ADR 0119](0119-capture-is-an-action-not-a-place.md): built the note
  capture sheet's original mic, in the footer, which this ADR moves inside
  the field and relabels.
- [ADR 0121](0121-notes-are-created-in-documents.md): the most recent change
  to `note-capture-sheet.tsx` before this one - capture's *entry point*
  moved into Documents; this ADR changes what's inside the sheet itself.
