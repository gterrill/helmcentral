# ADR 0223: Water routing bounds the queue, not the corners

## Status

Accepted (2026-10-28). Amends [ADR 0216](0216-passage-distance-is-routed-over-water.md):
the router no longer refuses a corridor for holding more than a fixed number of
corners. It refuses a search for holding too many candidate edges at once, or
for running too long.

## Context

Asked "How far is Goldsmith Island from here?" with the boat at Coral Sea
Marina, Airlie Beach, Mate answered that the shoreline was "too intricate to
route in one go (2707 corners); plan it in shorter legs". The run is 34 nm
straight and 37 nm by water, and it crosses the Whitsundays, which is exactly
the case the router exists for.

The refusal came from `waterRouteMaxNodes = 2000`. ADR 0216 says the limit was
3000 corners, but the code never allowed more than 2000. The cap was a stand-in for
memory. A* in `routeOverWater` queued one untested candidate edge to every
unsettled node each time a node settled, so the queue grew with settled nodes
times corners, and the limit was a guess at where that became dangerous.

We lifted the cap and routed seven pairs around the Whitsundays against a
fixture captured on 2026-10-28 (`testdata/water_route/whitsundays-goldsmith.json.gz`,
21.1°S to 19.9°S and 148.4°E to 149.6°E). Every route solved, in 0.2 to 0.6 s on
a laptop. The queue was the only cost that mattered:

| Route | Margin | Corners | Peak queue entries | Queue memory |
| --- | --- | --- | --- | --- |
| Marina to Goldsmith (37.35 nm) | 8 nm | 2,863 | 242,486 | 7.8 MB |
| Marina to Whitehaven (24.70 nm) | 20 nm | 3,247 | 2,283,944 | 73 MB |
| Goldsmith to Nara Inlet (34.52 nm) | 50 nm | 4,739 | 2,663,892 | 85 MB |

The corner count predicted none of this. The 4,376-corner search from the marina
to Goldsmith held 422,533 entries, a fifth of the Whitehaven search with 3,247 corners.
What mattered was how many nodes settled before the destination did. The
production box is a ZimaOS machine with a slower CPU than ours, shared with the
rest of the stack, so both memory and time count.

## Decision

**Bound the queue, because the queue is the cost.** `waterRouteMaxQueued` is
4,000,000 entries. Each is 16 bytes, so the bound is 64 MB. A search that would
hold more fails with the same fail-fast refusal as before, "too intricate to route
in one go", plus the number of candidate legs it would have to hold, and still
asks for shorter legs. It never returns a path it has not proven shortest and
never falls back to the straight line.

**Refuse a search that runs too long.** The corner cap was also the only
limit on time, and dropping it would leave none. A queue within bounds can
still take minutes, and Mate's run has a budget of three minutes for everything
else it does. `RouteOverWater` now gives the whole call, every margin and the
retry that proves the answer, `waterRouteMaxTime` of 30 seconds. The search
checks the clock at every expansion, which is a full scan of the nodes, and
not only every 256 pops. A search that outruns the limit fails with the same
"too intricate to route in one go" and the same advice, and both refusals wrap
`errWaterRouteTooIntricate`, so a caller can tell them from a coast with no way
through and from a cancelled request. A parent that cancels or expires keeps
its own error.

**Make each entry 16 bytes.** An entry is `{f float64; node, from int32}`. It
used to carry `g` as well, in two `int`s, and took 32 bytes. The cost so far is
`f` less the straight run still to go from `node`, so the search recomputes it
when it pops the entry.

**Queue a settled node's candidates a batch at a time.** The root cause was the
n candidates per settled node, not their size. A settled node now offers its
sixteen cheapest candidates, ordered by `f`. The last one carries a cursor, and
when the search pops the cursor the node offers the next batch, twice the size of
the last. The straight-line heuristic is consistent, so `f` never falls along a
path, and a later batch never holds an `f` below one already popped. The pops come
in the order they would with every candidate queued, so the path found is the same
one. A test routes random pairs across the fixture with one candidate to a batch
and with all of them in one batch. Over 600 pairs, the 580 that route agree to a
millionth of a nautical mile, and the other 20 are refused alike (an end on land).

**Stop the cell walk when the test has its answer.** `wrIndex.walk` kept
stepping along a segment after `clear` had already found a hit. The callback now
returns false to stop it.

Measured with the same seven routes at margins 3, 8 and 20, and 50 for two of
them, every distance matched the unbatched search to 0.01 nm. Peak queue entries
fell by 4 to 28 times, and the memory they take by 9 to 57 times, because each
entry is half the size:

| Route | Margin | Queue before | Queue after | Time before | Time after |
| --- | --- | --- | --- | --- | --- |
| Marina to Goldsmith | 8 nm | 242,486 | 55,870 | 190 ms | 118 ms |
| Marina to Whitehaven | 20 nm | 2,283,944 | 158,625 | 615 ms | 545 ms |
| Goldsmith to Nara Inlet | 50 nm | 2,663,892 | 93,315 | 597 ms | 582 ms |
| Marina to Brampton | 50 nm | 839,313 | 53,501 | 312 ms | 194 ms |

The longest search now holds under 3 MB. Time barely moved because the cost of a
search is the visibility test at each pop (200,000 to 300,000 on the hard routes),
and batching leaves the pops as they were.

## What we rejected

- **Raising the corner cap.** The queue grew with settled nodes times corners, so
  doubling the cap quadrupled the worst case, and we would be guessing again at the
  next coast that happened to be worse. The Whitehaven search held 73 MB at 3,247
  corners. A cap that admits the Goldsmith run would have admitted a run that
  fills the box.
- **Pruning to the reduced visibility graph.** The textbook rules are exact among
  polygonal obstacles: an edge must graze a corner and not cut it, and a bend must
  turn towards the land. Applied to the offset corners here, they cut queue
  entries by 20 to 40 times, and they are not exact. Nodes sit a full clearance
  off their corner and edges are only tested at half of it, so a shortest path may
  pass a corner in the gap between the two without a node at all, and on a coast
  with bends smaller than the clearance the corner a rule sees is not the shape
  the path has to clear. On 300 random pairs, 6 routed longer, by up to 0.32 nm. We
  then made the rules decline wherever the shoreline within 0.15 nm of a corner
  strays more than half a clearance from a straight line. That kept the worst
  disagreement in 300 pairs to 0.003 nm, but 1,500 pairs showed 3 routes longer by
  up to 0.16 nm, and it saved about a third of the time. A distance that is
  sometimes a little long, with no way to tell which, is the quiet substitution ADR
  0216 removed. Batching gives the same bound on memory with no such risk.
- **A grid or any-angle search.** Either would make memory a constant, but a grid
  loses the proof that the path is the shortest within the margin that
  `ExactWithinNm` relies on, and it loses fidelity in the 0.2 nm gaps between
  islands, which the clearance was chosen to keep open.
- **Routing through forced intermediate legs.** That asks the operator to choose
  waypoints because of a limit in our tool. "How far is Goldsmith?" is a question
  about a place. A plan to break it into hops is ours to make, not theirs.

## Consequences

- The Coral Sea Marina to Goldsmith run now answers: 37.35 nm by water against
  33.95 nm straight, in about 0.1 s on a laptop.
- The tool refuses a run only when its search would hold 4 million candidate legs
  at once, or run past 30 s. None of the seven routes comes within a factor of 20 of that, and a run
  that does is refused with the number, so a later change can tell a wall from a
  slow day.
- A search holds at most 64 MB for its queue, and briefly a second copy while
  the queue grows. That is the number to tune on a smaller box, and it is one
  constant.
- Time is unchanged. A route that needs 300,000 visibility tests takes about half
  a second here and longer on the boat. If that becomes the next complaint, the
  place to look is a cheaper `clear`, not the queue.
- ADR 0216 says the corridor may not hold more than 3000 corners, and the code
  held it to 2000. That limit is gone. The bounds are the queue and the clock.
- No route takes more than 30 s of the 3-minute Mate budget. The slowest search
  we measured takes 0.6 s on a laptop, so the limit is a wall for the unexpected,
  not a number the Whitsundays come near. A slow box that reaches it gets the
  same refusal as a crowded coast, and the fix is shorter legs either way.
