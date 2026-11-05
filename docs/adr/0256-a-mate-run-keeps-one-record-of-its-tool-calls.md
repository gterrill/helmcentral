# ADR 0256: A Mate Run Keeps One Record Of Its Tool Calls

## Status

Accepted (2026-11-05). Amends the draft checks in ADR 0234 and ADR 0243.

## Context

The run loop kept what a run's tool calls did in two slices, `runCalls` and
`runResults`, that only agreed by index. Each draft check walked both and
worked out for itself what "the call ran" meant. Two of them got it wrong.

- **The passage check:** `checkPassageDraft` took no results at all. It set
  `assessed` for any `assess_passage_window` call, so a call that errored let a
  wind or passage-limits claim through. A `get_wind_forecast` that errored but
  had been asked for `course_deg` counted as angled in the same way. The shelter
  check next to it already handled this, which is how the gap went unseen.
- **The depth check:** `depthSourced` looked for the text `"depth_m"` in a call's
  arguments, so `{"depth_m": null}` on a call that ran sourced a depth.

## Decision

The run keeps one record (`mateRunRecord`): an entry per call, in the order
made, holding the call, its result and its error. The error is empty when the
call ran, and says why when it failed or never ran. `checkDraft` takes the
record, and a check asks it for the calls to a named tool that ran, then reads
what they returned.

- **What a check reads:** only calls that ran. An errored call tells Mate
  nothing, so it satisfies no check. A call that ran and returned nothing still
  ran.
- **A failed check can carry the fix:** `assess_passage_window` fails with the
  operator's fix in its error when a sector has no wind no-go. A failed check
  whose error names the passage limits excuses a draft that relays it. Any other
  failure leaves a limits claim wrong.
- **After a failure the send-back doesn't repeat the call:** a repeat fails the
  same way, stalls the round budget and forces a final answer with no check at
  all. The send-back says the check failed, to say it could not run, and to take
  any angle from the forecast's `rel_wind` and `rel_wave` labels or drop it.
- **How arguments and results are read:** decoded as JSON. `depth_m` sources a
  depth when a number sits under that name at any depth, as in
  `propose_changes` operations. A forecast gives the angle only when a
  `rel_wind` or `rel_wave` label came back, since the host leaves them out when
  the direction is unknown.
- **Where it is built:** once per round, from the calls and the tool messages,
  right after the round runs. Nothing else keeps results by index.

The regexes in `checkDraft` stay as they were. Only how a check reads the run
changed, so a draft that tripped a trigger before still trips it.

## Cards stay where they are

`assistantCardSink` hands a tool's card to the run under the exact result text.
It looked like a second thing to record per call, and it stays out of the
record.

- **The handlers don't know the call:** a handler receives a name and arguments
  and returns text. Putting the card on the record means every handler returns
  it, a change to all the tools that build one and to the tests that read cards
  with `takeCards`.
- **The sink works under concurrency:** calls in a round run at once, and the
  sink needs no call identity to give each its own card.
- **The record is for checks:** a card is output for the operator and has no
  part in whether a check passes.

## Rejected

- **Reading an empty result as a failure:** a call can return nothing and
  succeed, and the check that follows an error needs the error's text.
- **Passing results to `checkPassageDraft` beside the calls:** the same
  parallel slices, one more place to get the index wrong.

## Consequences

- A failed passage check now sends a wind or limits claim back, as a missing one
  did. A run where the check errored every time costs the one send-back.
- A run that passes a null `depth_m` and states a depth for an anchorage is
  sent back.
- A forecast that ran but returned no angle labels no longer excuses a wind
  judgement.
