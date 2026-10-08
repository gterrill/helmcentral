# ADR 0167: Mate knows which document is on screen

## Status

Accepted (2026-10-08). Extends the screen context of [ADR 0093](0093-onboard-assistant-over-openrouter.md) and builds on the
document tools of [ADR 0106](0106-documents-in-the-binary.md) and the Details page of [ADR 0115](0115-document-details-live-on-a-page.md).

## Context

Asked from the Mate sheet over a document's Details page, "What period does this cover?" got the reply "Which period
are you referring to - the weather forecast, tide predictions...". The screen context sent with a question named only
the panel (`documents`), so Mate had no way to know that "this" meant the document in front of the operator.

## Decision

- The `screen` object gains `document_id` and `document_title`. The frontend sets them only when the location is the
  Details route (`/documents/<id>`); the title is reported up by the Details page once it has loaded, and is omitted
  until then.
- The server trims and caps both like the other screen fields (80 runes; document ids are shorter). A blank id means
  no document.
- `assistantScreenSentence` says the operator is viewing that document on its Details page, that "this" and "it" most
  likely mean it, and tells Mate to read it with `read_document` using the id before answering instead of asking which
  source is meant.
- The ordinary Documents listing and every other panel are unchanged.

## Consequences

- One extra sentence in the live (non-cached) part of the prompt, only on the Details page.
- A question about some other document asked from the Details page still works: the sentence says "most likely", and
  Mate can still search.
- The same Details page now has a breadcrumb of the full folder path and a Move action; neither affects the prompt.
