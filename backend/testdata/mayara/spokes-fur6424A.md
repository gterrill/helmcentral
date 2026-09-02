# Spoke capture, 2026-09-01

Phase 0 of the radar overlay plan. Captured direct from mayara at
192.168.50.81:6502 over `ws://.../radars/fur6424A/spokes`, because the SignalK
plugin returns 404 for the spoke stream. Radar transmitting (`power: 2`) on the
463 m range, boat stopped.

`spokes-fur6424A.jsonl.gz` holds the first 2 frames of a 90 second capture, one
JSON record per WebSocket message with a base64 payload. Message-delimited
rather than a concatenated `.bin`, because WebSocket framing carries the
boundaries and concatenation destroys them. Two frames is 732 KB raw and 16 KB
gzipped; the full capture was 51 MB and is not worth keeping.

## Measured

| | |
| --- | --- |
| frames | 220 in 89.6 s, 2.4/s |
| spokes | 48,056, averaging 218 per frame |
| bytes | 51,130,069 |
| **rate** | **555 KiB/s = 4.54 Mbit/s** |
| gzip ratio | 45:1, so the payload is mostly zeros |

## The four questions the plan gated on

**`bearing` is always populated.** 48,056 of 48,056. So the picture is drawn
north-up directly from `Spoke.bearing` and never rotated by heading. That
retires the risk ADR 0062 line 165 recorded, where a bearing that turned out to
be bow-relative would have put every echo wrong by the heading, rotating as the
boat swung.

**`lat`/`lon` are always populated.** 48,056 of 48,056. The canvas centres on
the radar's own reported fix rather than own-ship position, which resolves the
antenna-offset caveat ADR 0062 line 163 parked with the words "until someone
wants the radar overlay to line up at 100 metres". No offset setting is needed
and none should be added.

**Every spoke is exactly 1024 bytes.** min 1024, median 1024, max 1024, despite
`hasSparseSpokes: true` in the capabilities. So no nearest-neighbour resampling
on ingest and `binCount` is a constant 1024. Worth re-checking on a longer range
before relying on it, since a capability that is advertised but unused here may
be used elsewhere.

**Bandwidth is 4.54 Mbit/s, but that number is tied to scan speed.** See below.

## Two findings the plan did not anticipate

**Only 4096 distinct angles are used, not 8192.** Every observed angle is even
and the step is exactly 2: 2, 4, 6, 8 and so on to 8192. `spokesPerRevolution`
in the capabilities reads 8192, and the wire uses half of them. The renderer's
spoke count, look-up table and stride must come from the observed angle step,
not from the advertised figure. This is the same class of error as `version`
against `apiVersion` and `is_dangerous` against `isDangerous`: a documented
value contradicted by the wire.

(7 of 48,056 spokes decoded to an odd angle. That is 0.015% and is more likely
an artefact of the throwaway analysis decoder than a real value. Worth
confirming with the production decoder rather than assuming either way.)

**The measured rate is at 7.9 rpm, which is slow for a marine radar.**
48,056 spokes over 4096 per revolution is 11.7 revolutions in 89.6 s. A
DRS4D-NXT normally turns at 24 rpm or more, and `scanSpeed` reads 2 (Auto).
Since every spoke is a full 1024 bytes, bandwidth scales linearly with rotation
rate, so **at 24 rpm the same stream would be roughly 13.6 Mbit/s**, well past
the 5 Mbit/s the plan set as the point where a verbatim relay stops being
sensible.

Do not treat 4.54 Mbit/s as the design figure. Re-measure with the antenna at
its normal speed before deciding whether the relay can pass frames through
untouched.

## Second measurement, 30 s, Doppler on

504 KiB/s = **4.13 Mbit/s**, against 4.54 in the first run. Same radar, same
463 m range, same Auto scan speed, `doppler: 1` this time rather than 0. So the
byte rate is stable at roughly 4 to 4.5 Mbit/s and Doppler colouring does not
change the volume, only the pixel values.

(The quick inline decoder used for this run reported a spoke count four times
lower than the byte rate implies, while the byte figures agreed across both
runs. The careful pass over the saved frames is the one to trust: 220 frames,
218.4 spokes each, 232 KB per frame, which reconciles exactly with 1024 bytes
per spoke plus framing. Noted because a throwaway parser disagreeing with
saved-byte analysis is worth recording rather than quietly discarding.)

## What this means for the relay

At the observed rate a verbatim relay is comfortable. The caveat in the section
above still stands: the rate is proportional to rotation speed and this antenna
is turning at about 8 rpm, so a normal 24 rpm would put it near 13 Mbit/s.

The cheap hedge, which avoids needing a Go protobuf dependency to decimate:
**the relay should drop frames per client under backpressure rather than
buffering them.** A tablet that cannot keep up then degrades to a lower frame
rate, which for a rotating radar picture is barely perceptible, instead of
stalling the fan-out or growing memory without bound. Spokes are independent and
the client accumulates them into a persistent buffer, so a dropped frame costs
one sector of one sweep and the next revolution repaints it.

Decide on server-side decimation only if a measurement at normal scan speed
actually demands it. Do not build it on this evidence.
