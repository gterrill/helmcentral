# ADR 0151: Vessel Settings Save Through the Settings Page

## Status

Accepted

## Context

ADR 0142 moved the Settings page onto one `SaveBar` and left the Vessel
section's two forms alone, because each wrote to its own endpoint:

- **Engines and house bank** saved with "Save Vessel Settings" to
  `POST /api/vessel`, which wrote the `vessel:` block of `settings.yaml` and
  seeded the anomaly alarm rules.
- **Particulars** saved with "Save particulars" to `PUT /api/vessel/particulars`,
  a row in SQLite that the YachtWave import wizard (ADR 0150) also writes.

That left two extra buttons on a page whose other sections had none, and edits
in either form were invisible to the page's dirty signal: the navigation guard
did not warn about them, "Save and Continue" did not save them, and Discard did
not undo them.

## Decision

1. **Engines and house bank go through `POST /api/settings`.** They already
   live in `settings.yaml`, so there was never a reason for a second endpoint
   writing the same file. `settingsPayload` gains a `vessel` block, and the
   regular settings draft gains the engine rows and house bank. `GET
   /api/settings` returns the block.
2. **An absent `vessel` leaves the stored block alone.** Every other section
   is rewritten wholesale from the payload, but the vessel block is a pointer
   on the backend: `nil` means "not mentioned", an explicit empty block means
   "cleared". The frontend draft carries `vessel: null` until the server's
   block has been read, and the patch omits it in that state, so a settings
   fetch that fell back cannot lead to a save that wipes the setup.
   A vessel block that cannot be parsed does not fail the whole read: `GET
   /api/settings` still returns every other setting, leaves `vessel` out and
   sets `vessel_error` to the reason, and the Vessel section shows that
   message. Failing the read would send the page back to defaults, and its
   next save would write those defaults over the stored settings.
3. **Seeding stays where it was, after the write.** A save that carries the
   block runs `seedAnomalyRules`. A seeding failure is logged, not returned:
   the settings are already written, every sub-set is idempotent behind its
   own marker, and startup runs the same call again. Returning an error would
   tell the operator a save failed that had not.
4. **Particulars stay in SQLite on their own endpoint, and join the Save bar
   as a fourth draft**, the way alarm transports did. A
   `VesselParticularsProvider` mounted for the whole page holds the draft and
   saved snapshot, counts toward `dirty`, saves in the same Save with its own
   error slot, and resets on Discard. Its snapshot moves as soon as its own
   request succeeds, so Discard after a partial failure does not roll back
   behind the server. A refusal that names a field is shown at that field as
   well as in the bar.
5. **"Apply gauge zones" stays an immediate action.** It adds a tile to a
   dashboard page; it reads the linked item's profile (already written to the
   inventory item by the linker) and the instance, and never the vessel block,
   so it works from unsaved draft state and needs no saved link.
6. **`POST /api/vessel` and `GET /api/vessel` are retired.** `GET
   /api/vessel/candidates` stays: it is the live picker data, not settings.
   The candidates hook re-reads when a save lands, since the detector status
   lines are computed from the saved block.

## Consequences

- The Vessel section has no buttons that save. Editing there shows the bar,
  trips the navigation guard, and is undone by Discard.
- One Save now fans out to up to four requests: settings (with `vessel`),
  secrets, alarm transports and particulars. Each failure has its own slot in
  the bar.
- Opening Settings fetches the particulars record whichever section is
  showing, one more small GET, for the same reason transports are fetched.
- A script that POSTed to `/api/vessel` must post the `vessel` block to
  `/api/settings` instead.

## Rejected

- **Particulars in `settings.yaml`.** They are records about the boat, written
  by import as well as by hand, with a stamp and field-level validation. A
  settings file is the wrong home for them and the import would have to write
  YAML.
- **All settings into SQLite.** It would make the bar's one request honest,
  but ADR 0141's consolidation left configuration in `settings.yaml` on
  purpose, and the rewrite is out of proportion to two forms.
- **Keeping the vessel endpoint and merging only the button.** The page would
  still send a separate request for data that sits in the file it is already
  writing.
