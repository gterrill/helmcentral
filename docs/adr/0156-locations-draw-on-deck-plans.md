# ADR 0156: Locations Draw on Deck Plans

## Status

Accepted

## Context

Locations are a two-level list (ADR 0065 §4, ADR 0123): a zone is an area of
the boat, a bin is a coded container filed in one zone. The list says that the
spare impeller is in bin ENG-03 in the Engine room, which is enough for the
person who set the bins up and not for anyone else. Crew, a delivery skipper or
the owner six months later still have to find the engine room hatch and then
the third bin.

A plan of each deck with the zones outlined and the bins pinned answers "where"
at a glance. Most boats have a general arrangement drawing from the builder,
and a photo of one is enough to draw on.

## Decision

1. **A deck is a new record above zones.** `inventory_decks` holds `id`, `name`
   (unique, case-insensitive), `sort_index` and a nullable `plan_document_id`.
   Decks are ordered by `sort_index`, assigned as the next number on create,
   then by name. The plan image is an ordinary document, uploaded through the
   same intake as equipment photos (ADR 0127): JPEG or PNG only. The browser
   downscales plans to 3000 px on the long side, not the 1600 px used for
   photos, because a plan has fine detail. A PDF drawing is exported to an
   image by the operator; reading PDF pages is out of scope.
2. **A zone lies on at most one deck.** `inventory_zones` gains a nullable
   `deck_id` and a nullable `polygon`, set together or not at all: a zone is
   on the plan with an outline, or it is not on the plan. A zone that spans two
   decks is two zones. Zones not on a plan keep working exactly as before;
   nothing about the plan is required.
3. **A bin is a pin, not a shape.** `inventory_bins` gains nullable `pin_x` and
   `pin_y`, set together. A bin's deck is its zone's deck, so a pin is only
   allowed when the bin's zone is on a plan, and taking a zone off the plan
   clears its bins' pins in the same transaction. The server does not require
   a pin to fall inside its zone's outline; outlines are drawn loosely and a
   locker can sit on a zone's edge.
4. **Coordinates are fractions of the plan image, 0 to 1**, with x across and y
   down. A polygon is 3 to 64 points, stored as JSON `[[x,y],...]`. Replacing a
   plan image keeps every outline and pin; the editor warns when the new
   image's aspect ratio differs from the old one by more than 2%, since the
   shapes will then no longer line up.
5. **The layout of a deck saves as one atomic request.**
   `PUT /api/inventory/decks/:id/layout` takes
   `{zones:[{id, polygon}], bins:[{id, x, y}]}` and replaces the deck's whole
   layout in one transaction: listed zones are placed on this deck (moving
   them off another deck, whose pins for them are then the ones in this
   request), zones previously on this deck and not listed come off the plan,
   and pins of bins in listed zones that are not themselves listed are
   cleared. A listed bin whose zone is not listed is a 400. The editor holds
   the layout as a draft and saves it through the Save bar, so a half-drawn
   outline never reaches the server.
6. **Deleting a deck takes its zones off the plan**, it does not refuse. The
   confirmation names how many zones lose their outline. The plan document
   stays in Documents, as photos do when their equipment is deleted.
7. **The plan is SVG, not a canvas library.** The plan image sits in an `<svg>`
   with a `viewBox` in the image's own pixels; zones are `<polygon>` elements
   and bins are `<circle>` pins. Each zone and pin is a real link with an
   accessible name ("Engine room, 6 bins", "Bin ENG-03"), takes keyboard focus,
   and is coloured from theme tokens, so day, night and Auto need nothing
   extra. Labels hold a constant on-screen size at any plan width. Konva was
   considered: its strength is canvas performance with thousands of shapes, a
   boat has tens, and on a canvas every accessibility and theming property
   above has to be rebuilt by hand. If the editor outgrows hand-written
   pointer handling (snapping, undo, splitting shapes), react-konva can
   replace the editor alone, loaded lazily.
8. **Where the plan appears.**
   - Locations gets a **Table / Plan** switch. Plan shows one tab per deck;
     tapping a zone opens its location, tapping a pin opens its bin.
   - A **Decks** list and a deck page (name, plan image, the drawing surface)
     sit under Locations, built from the pattern library (ADR 0142).
   - The location page and the bin page show a small plan with the zone
     highlighted and, on the bin page, the bin's pin marked.
   - The drawing surface and its pointer code load lazily, so the wall kiosk
     never parses them.

## Out of scope

- Reading a deck plan out of a PDF page.
- Zoom and pan on the drawing surface. The plan draws at the page width; a
  follow-up adds zoom if bins on a large boat are too close to place by finger.
- Mate linking to the plan, and Stocktake walking zone by zone on it.
- Reordering decks. They list in the order they were created; `sort_index`
  is there so a later change can add reordering without a migration.

## Consequences

- The zones and bins JSON gain `deck_id`, `polygon` and `pin`; clients that
  ignore unknown fields are unaffected.
- A plan is a document, so it appears in Documents and is indexed like any
  other image.
