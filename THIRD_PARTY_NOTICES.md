# Third-party notices

Helmcentral includes the work of others. This file records the parts that ask
for a notice. Each project's own licence applies to its own code and data.

## Time zone boundaries

Helmcentral finds the boat's time zone from its position (see
`docs/reference/boat-time.md`). It uses three pieces:

- **timezone-boundary-builder** data, carried in `github.com/ringsaturn/tzf-dist`.
  Copyright OpenStreetMap contributors. The boundaries are available under the
  [Open Database License (ODbL) 1.0](https://opendatacommons.org/licenses/odbl/1-0/).
  The data is built from OpenStreetMap by
  [timezone-boundary-builder](https://github.com/evansiroky/timezone-boundary-builder)
  and includes ocean zones. Helmcentral queries it and doesn't change it. Anyone
  who changes the boundary data and shares the result has to share it under the
  same licence.
- **tzf** (`github.com/ringsaturn/tzf`), the library that reads the boundaries.
  MIT licence, copyright (c) 2022 ringsaturn.
- **tzf-dist** (`github.com/ringsaturn/tzf-dist`), the packaged data. Code under
  the MIT licence, copyright (c) 2026 Han Xiao. Data under the ODbL as above.

tzf bundles further MIT and BSD code, listed in its own NOTICE file:
`github.com/ringsaturn/orb` (Paul Mach), `github.com/tidwall/geoindex` and
`github.com/tidwall/rtree` (Josh Baker), a polyline decoder (Tom Payne,
BSD-2-Clause), and code from the Go project (The Go Authors, BSD-3-Clause).
