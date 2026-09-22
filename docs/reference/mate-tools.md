# What Mate can look up

[Mate](../features/assistant.md) answers most questions from the boat's own
live instrument data. Beyond that, it can reach for the lookups below, each
tied to a specific provider you configure elsewhere in Helmcentral. This page
is the detail behind the summary on Mate's own page: what each lookup draws
on, what has to be configured for it to work, and what Mate says when it
can't be done.

| Lookup | What it reads | Comes from | Needs configured | If it can't run |
| --- | --- | --- | --- | --- |
| Live vessel context | Position, heading, speed, apparent wind, current place name, marine warning in force | The dashboard's own live feed, exactly as shown right now | Nothing extra; always included | A missing reading reads as "unknown", never a guessed number |
| Place lookup | Resolves a named place to a position | Your saved route waypoints first, then the configured place-names plugin, searched within about 100 nautical miles of the boat | A place-names plugin (**Settings → Tiles → Place names**; OpenStreetMap by default) | Says the name didn't resolve |
| Forecast for a place | Wind and wave forecast for a named place, not just where you are now | The same providers the Forecast panel uses | A forecast provider configured | Names the provider that failed |
| Tide for a place | Tide predictions for a named place | The same tide provider and station catalog the dashboard uses | A tide provider and station | Says no tide provider is configured |
| Passage wind and sea angle | True wind and sea angle against a planned course | Worked out from the forecast lookup above, not estimated by a language model | A course to compare against | - |
| Passage time and fuel | Estimated time and fuel burn for a distance and speed | This vessel's own logged speed-over-ground and fuel-rate history, not a manufacturer's curve | InfluxDB | Mate reasons about the passage in general terms instead |
| What you're looking at | Which panel or dashboard page the question was asked from | The screen you're on when you ask, from the sheet's context | Nothing extra | - |
| Helmcentral's own features | How a feature works, quoting the actual page | The in-app help, the same pages the header's **?** opens | Nothing extra | - |
| The document library | Manuals, receipts, notes, photos already in the library | An attached file, or a search across the whole library | See [Documents](../features/documents.md) | - |

Place search uses the same plugin, and the same Overpass server setting when
that plugin is OpenStreetMap, as the place name shown on the position tile -
a separate choice from whichever plugin the Nearby tile uses, even though
both default to the same one. If your boat's connection can't reach the
default Overpass server, see [Configuration → Overpass](configuration.md#overpass)
for pointing it at a mirror.

A lookup that keeps failing is abandoned rather than retried forever: if one
of these fails three times while answering a single question, it stops being
called for the rest of that answer, and Mate says the lookup was unavailable
and answers with what it has.
