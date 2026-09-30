# ADR 0150: Imports Are Staged, Reviewed Page by Page, and Remember Their Source

## Status

Accepted

## Context

Operators arrive at Helmcentral with years of records in another boat
management platform. YachtWave is the first; others will follow. The records
are the same kinds Helmcentral keeps (equipment, spares, a service log, notes,
documents), but each platform shapes them its own way, and none of them hands
over clean data.

YachtWave's "Vessel Export" is an HTML report, not a data export. Measured
against the export of Pikorua (30 Sep 2026):

- **No record ids.** Nothing identifies a row except its contents, so a second
  import of a later export cannot tell a new record from one already brought
  across.
- **Files are links.** Documents and photos point at YachtWave's CDN
  (`documents.yachtwave.com`, `images.yachtwave.com`). Whether those links
  open without a YachtWave session is not something Helmcentral should find
  out by fetching them.
- **Checklists carry no steps.** Only a name, description and step count.
- **Ambiguity the file cannot resolve.** Both main engines are called "Cummins
  QSB 6.7"; six service log rows name that engine without saying which.
- **Data that is wrong as exported.** Displacement "32 kg", an item named
  "xys", duplicated rows, part numbers of "Unknown".
- **Secrets in notes.** Two notes and one bullet of a third hold passwords.
  Imported, they would be indexed, embedded and handed to Mate's model
  provider.

The export also exposed four gaps in Helmcentral's own model: no record of the
vessel's particulars beyond what SignalK publishes, no part number or required
quantity on a spare, a `quantity >= 1` constraint that cannot hold a spare
that has run out, and no memory of where a record came from.

YachtWave's tasks (priority, assignee, due date, status) are a simplified
version of the maintenance schedule. The maintenance rule store is being
reworked to reference equipment profiles (ADR 0148) at the same time.

## Decision

1. **A parser per platform, one staged shape.** Each platform's parser turns
   its export into a platform-neutral staged payload (`import_staged.go`):
   particulars, locations, equipment, spares, log entries, notes, files, and a
   list of issues. The parser fails on a file that is not the export it
   expects. It does not repair values: the 32 kg displacement is staged as
   exported and raised as an issue for the operator to apply or skip.

2. **Nothing is written until the operator commits.** An upload creates a
   draft `import_run` holding the staged payload and a default set of
   decisions. The operator walks a wizard, a page per concern, and every
   choice is saved to the run as they go, so an import can be left and
   resumed. Commit writes the whole run in one transaction, and refuses while
   anything needs a decision: an ambiguous log entry without an engine, or a
   file neither supplied nor skipped.

3. **Files are handed over by the operator, one page each.** The wizard shows
   the YachtWave link to open, and a place to paste, drop or pick the saved
   file, or skip it. Helmcentral never fetches from the platform's CDN.

4. **Records remember their source.** `import_records` maps (source,
   external key, target kind) to the Helmcentral record created or matched.
   With no ids in the export, the external key is a hash of each record's
   natural key. A re-import shows records already brought across and skips
   them by default. A `match` decision pairs a staged record with an existing
   one and writes nothing to the existing record.

5. **SignalK stays authoritative for what it publishes.** `vessel_particulars`
   stores only what SignalK does not carry: builder, model, year, HIN, flag,
   hailing port, hull, displacement, shore power, system voltage,
   registration, IMO, EPIRB id, date acquired. Name, MMSI, call sign, length,
   beam, draft and air height are shown beside the live value with a mismatch
   flag, and never written. The low-water warning keeps reading SignalK's
   draft.

6. **Spares carry a part number and a required quantity, and can run out.**
   `equipment` gains `part_number` and a nullable `required_quantity`, and
   `quantity` may be 0. SQLite cannot alter a CHECK, so the table is rebuilt
   once, detected from its own DDL, with foreign keys off on a pinned
   connection and `foreign_key_check` required clean before commit. Out of
   stock is derived (on hand below required), not stored.

7. **Password notes are flagged and skipped, and the secret is not kept.** The
   parser blanks the body of a note it flags, so the password is never stored
   in the run or served by it. The wizard shows the title, date and reason.

8. **YachtWave tasks become notes.** Each carries its priority, assignee, due
   date and status in its body. Turning one into a maintenance rule is left
   until ADR 0148's profile-referenced rules have settled.

9. **Checklists are not imported.** Without their steps there is nothing to
   run. The review page lists each one and why.

## Consequences

- A second platform is a parser and a source entry in Settings → Import; the
  run store, decisions, wizard and commit are shared.
- A re-import of a later YachtWave export picks up only what is new, as far as
  a content hash can tell. A record edited in YachtWave after the first import
  hashes differently and arrives as new; the wizard's match option is the
  answer to that, not an automatic merge.
- Crew, cruise log, general log, readings and expenses have no home yet. The
  review page reports a non-empty section of any of them as not imported.
- The equipment table rebuild runs on the first start after upgrade and fails
  the start, loudly, on a database that already holds a dangling reference.

## Rejected

- **Parsing in the browser.** Quicker to write, but the parser is the part
  that must be tested against real exports, and a Go test against a redacted
  fixture is where that belongs.
- **Fetching the CDN links server side.** It would need the operator's
  YachtWave session, or rely on the links being public, which is not ours to
  assume.
- **A tasks table and page.** It would duplicate the maintenance schedule in a
  weaker form.
- **Writing YachtWave's particulars into SignalK, or preferring them over it.**
  SignalK is where the draft the low-water warning uses comes from; a second
  source of truth for the same figure would make that warning's answer depend
  on which import ran last.
