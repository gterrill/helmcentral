# ADR 0141: One Business Database

## Status

Accepted (2026-09-28). Builds on [ADR 0106](0106-documents-in-the-binary.md)
(`documents.sqlite` and its "backup unit is one db file" reasoning), [ADR
0093](0093-onboard-assistant-over-openrouter.md) (`assistant.sqlite`, Mate's
conversation history), [ADR 0044](0044-nearby-vessel-encounter-confirmation.md)
(`nearby-contacts.sqlite`, the AIS sighting log) and
[ADR 0024](0024-plugin-descriptions-and-allowlist-overrides.md) (plugin
overrides). The alarm log, notification queue and web push subscriptions
have no ADR of their own; they are covered here.

## Context

`documents.sqlite` is a historical name. It started as the document
library's own database (ADR 0106) and has since grown to carry notes,
the equipment registry, inventory, and maintenance rules and service
history - none of which are "documents" in any sense a filename should keep
implying. It is Helmcentral's actual business database, the one holding
everything an operator would call "my boat's records," and it has outgrown
the name it launched with.

That business data is split across three separate files today -
`documents.sqlite`, `assistant.sqlite`, `nearby-contacts.sqlite` - opened
through three separate connections, backed up as three separate units, with
no relationship between them the database itself can see or enforce. Mate's
own conversation history already crosses that boundary: a
`message_attachments` row in `assistant.sqlite` names a `documents.sqlite`
row by id, and has done since attachments shipped, with nothing but
application code ever checking that the id it names is real. Three files
that already reference each other are not really three independent stores;
they are one logical database that happens to be split into three `.sqlite`
files for no reason connected to what any of them hold.

Three more single-file stores sit beside them for reasons that don't hold up
under the same question: `alarm-log.sqlite` (the alarm occurrence history and
delivery queue), `webpush-subscriptions.sqlite` (registered push devices) and
`plugin_overrides.sqlite` (per-plugin allowlist overrides and
plugin-declared config values). Each was originally its own file for a
locally reasonable but never load-bearing reason - see Decision 2 below for
what those were - and each is Helmcentral's own operating state in exactly
the sense `documents.sqlite`, `assistant.sqlite` and `nearby-contacts.sqlite`
are, just smaller. Six files for one conceptual database is the same
un-motivated split this ADR was written to close, just with three more
files than it first named.

## Decision

### 1. `helmcentral.sqlite` is the one business database

Documents, notes, the equipment registry, inventory, maintenance rules and
service history, checklists (all of `documentStore`), Mate's conversations
(`assistantStore`), the AIS sighting log (`nearbyContactStore`), the alarm
occurrence log and notification queue (`alarmLogStore`), registered web push
devices (`webPushSubscriptionStore`), and per-plugin allowlist overrides and
config values (`pluginOverridesStore`) all move into one file,
`helmcentral.sqlite`, resolved by a single environment variable,
`HELMCENTRAL_DB_PATH` (default `data/helmcentral.sqlite`).
`DOCUMENTS_DB_PATH`, `ASSISTANT_DB_PATH`, `NEARBY_CONTACTS_DB_PATH`,
`ALARM_LOG_DB`, `WEBPUSH_DB_PATH` and `PLUGIN_OVERRIDES_DB_PATH` are removed
outright, with no compatibility alias for any of the six - this codebase's
standing rule against speculative fallbacks applies to environment
variables as much as to anything else. `DOCUMENTS_DIR` (the folder of
hash-named file bytes ADR 0106 keeps separate from the database) is
unaffected; only the database file itself moves.

Each store keeps its own `*sql.DB` handle, its own mutex, and its own
`CREATE TABLE IF NOT EXISTS` schema code - nothing about how the six stores
talk to their tables changes, only which file those tables live in. No
foreign key is added from `message_attachments.document_id` to `documents`
in this change. The reference has existed in practice since attachments
shipped; making it a declared constraint is left for later, once the six
stores have been sharing a file long enough to be confident about what
enforcing it would actually catch.

### 2. Secrets, sessions and the tile cache stay exactly where they are

Only these three now stay separate, for three different reasons:

- **Secrets** (`secrets.sqlite`) pairs with its own key file
  (`secrets.key`); losing that pair together is what makes every stored
  credential unrecoverable, a recovery story specific to this one store
  that has no business being entangled with anything else.
- **Sessions** (`sessions.sqlite`) are throwaway - nobody needs to back up
  who is currently logged in.
- **The tile cache** (`tile-cache.sqlite`) is a cache; it is reconstructed
  from Esri on a cold start and has no history worth protecting.

None of these three describe the boat. They describe this installation's
own operating state, and each already has a backup story (or deliberately
doesn't need one) that combining files would only complicate.

The alarm log, web push and plugin overrides don't share that shape, even
though each was its own file for a locally reasonable-sounding reason:

- **The alarm log** (`alarm-log.sqlite`) was kept separate so that deleting
  the file was a supported way to clear alarm history. That reasoning
  stops working the moment the alarm log is a table in the same file as
  every other business record - deleting `helmcentral.sqlite` to clear
  alarm history would take the document library, Mate's conversations and
  the AIS sighting log with it. Clearing alarm history moves from a file
  op to a table op: `DropLogOlderThan` (`backend/alarm_log_store.go`)
  already prunes rows past a year old on an ongoing basis; a full manual
  clear is now `DELETE FROM alarm_log` against `helmcentral.sqlite`
  instead of `rm alarm-log.sqlite`.
- **Web push** (`webpush-subscriptions.sqlite`) was kept out of
  `alarm-log.sqlite` specifically so that deleting *that* file never
  disconnected a phone - a reason that named the alarm log, not a reason to
  stay out of the combined business database once the alarm log joins it
  too.
- **Plugin overrides** (`plugin_overrides.sqlite`) had no stated reason to
  be separate beyond having shipped as its own file; it is config in
  exactly the sense the equipment registry and maintenance rules already
  in `documentStore` are.

The alarm log, web push and plugin overrides describe this installation's
own operating state, not the boat - exactly the category Decision 1 already
collects into `helmcentral.sqlite`, and exactly what `documents.sqlite`,
`assistant.sqlite` and `nearby-contacts.sqlite` were before this ADR moved
them in. Keeping any of the three out for a reason that only made sense
while they sat next to a file being deleted is the same un-motivated split
this ADR exists to close.

### 3. One shared opener, WAL for every store that uses it

`openHelmcentralDB(path string) (*sql.DB, error)` (`backend/helmcentral_db.go`)
is the one function all six stores open their own connection through: it
creates the parent directory, sets `foreign_keys(1)`, `busy_timeout(5000)`,
`journal_mode(WAL)` and `synchronous(NORMAL)`, and reads back both
`foreign_keys` and `journal_mode` to confirm they actually took effect rather
than trusting the driver's own DSN pragma parameters - the same fail-fast
check `newDocumentStore` has always applied to `foreign_keys` alone, now
applied to both pragmas that matter, for every caller. `alarmLogStore`,
`webPushSubscriptionStore` and `pluginOverridesStore` had no pragmas of
their own worth mentioning before this - a bare `sql.Open` plus
`SetMaxOpenConns(1)` - so for them this is a straightforward upgrade rather
than a change in backup properties the way it is for `documentStore` below.

WAL is new for `documentStore`: ADR 0106 deliberately ran with no WAL so the
backup unit stayed exactly one `.sqlite` file plus one folder of hash-named
files. `nearbyContactStore` had already been running WAL with
`synchronous(NORMAL)` on its own file, for the opposite reason - so a
five-second poll tick's write never stalls a concurrent read. Combining the
files means picking one answer, and WAL is it: a long document write (a
bulk reindex, an OCR pass) must never stall the AIS poller or Mate's own
reads, and WAL is what lets a reader proceed without waiting on a writer at
all. `SetMaxOpenConns(1)` per store's own handle still makes
`database/sql` queue that ONE store's own concurrent writers, since
modernc/sqlite surfaces a second writer as "database is locked" rather than
waiting; WAL is what then lets a *different* store's one-connection pool
read the same file without waiting on that queue either.

### 4. Migration is a one-off, operator-run subcommand, never automatic

`migrate-db` (`runMigrateDBCommand`, `backend/helmcentral_db.go`) is the only
way this move happens. `main()` checks `os.Args[1] == "migrate-db"` before
doing anything else and, if it matches, runs the migration and exits with
its result - there is no other flag on this binary, so a plain `os.Args`
check is enough. It never runs at ordinary startup: it renames files and
copies rows, which has to be a deliberate action an operator takes once, not
something that could fire again on a normal restart.

It refuses rather than skips on two checks that stop the migration before
anything is touched: `helmcentral.sqlite` already existing (nothing to do,
or this already ran), and none of the six legacy files existing at all (a
fresh install has nothing to migrate). `documents.sqlite` itself is not
required to exist - see the branch below - so its absence alone is no
longer one of these checks; only the complete absence of all six is. A
`documents.sqlite-journal` sitting next to an existing `documents.sqlite`
(the last process to hold that file open did not shut down cleanly, and
renaming it forward would make the very next open run SQLite's own crash
recovery against it as a side effect of migration rather than a deliberate
choice) stops the migration the same way, but only applies when
`documents.sqlite` exists. Any failure from there on - opening the combined
file, creating its tables, attaching a legacy file, copying rows, or
committing, including a per-table row count that doesn't match after
copying - undoes whatever this run did to `helmcentralPath` before
returning the error (see the two branches below for what "undo" means in
each), so a failed run leaves the install exactly as it found it and
re-running `migrate-db`, once whatever caused the failure is fixed, is
always safe. Safety here comes from refusing outright (or undoing what
already happened), not from silently skipping a step and hoping the
operator notices later.

Which of two branches runs (`migrateWithDocumentsFile` /
`migrateWithoutDocumentsFile`, `backend/helmcentral_db.go`) depends on
whether `documents.sqlite` exists:

- **`documents.sqlite` exists.** It is renamed straight to
  `helmcentral.sqlite` (with its `-wal`/`-shm` siblings, if present) and
  reopened through `openHelmcentralDB`, which is what actually converts it
  to WAL. A failure from here on renames `helmcentral.sqlite` straight back
  to `documents.sqlite`.
- **`documents.sqlite` does not exist, but at least one of the other five
  legacy files does.** An install can be old enough to predate the document
  store (ADR 0106) entirely - Mate, the AIS sighting log, alarms, web push
  or plugin overrides configured with no document library ever used -
  and refusing outright here used to leave that install unable to either
  start (`checkForLegacyDatabaseFiles`, below) or migrate. Instead,
  `helmcentral.sqlite` is created fresh through `openHelmcentralDB` and
  given the documents schema directly (`createDocumentsSchema`,
  `documents_store.go`, factored out of `newDocumentStore` the same way as
  the other five stores' schema functions) so the file is complete even
  though no legacy `documents.sqlite` ever fed it a row. A failure from
  here on removes the `helmcentral.sqlite` this branch created (and any
  `-wal`/`-shm` it picked up) outright, rather than renaming it back - there
  is no `documents.sqlite` to rename it back to, and leaving a half-created
  file at the resolved path would strand a rerun exactly as a half-migrated
  one would.

Both branches then reach the same step: the other five stores' tables are
created via each store's own schema code, factored out into
`createAssistantSchema`/`createNearbyContactSchema`/`createAlarmLogSchema`/
`createWebPushSchema`/`createPluginOverridesSchema` so the migration and
each store's ordinary `new*Store` path share one copy of each.
`assistant.sqlite`, `nearby-contacts.sqlite`, `alarm-log.sqlite`,
`webpush-subscriptions.sqlite` and `plugin_overrides.sqlite`, when present,
are each `ATTACH`ed and every row copied across by an explicit column list,
all within the same one transaction, with the row count checked table by
table before it commits. Reading a `-wal`-backed legacy file this way
(`ATTACH` against the live file, not a copy of it) is deliberate: SQLite
resolves a WAL-backed attach correctly on its own, and copying a legacy
file without its `-wal` sidecar would silently drop whatever had not yet
been checkpointed into the main file. None of the five legacy files is ever
deleted: once migrated, each is renamed to `<name>.sqlite.migrated` so an
operator who wants to double-check the numbers still has the original -
this rename only happens after the transaction above has committed, so it
runs the same way regardless of which branch created `helmcentral.sqlite`.

Two of the five carry a schema wrinkle a straight `ATTACH`-and-copy doesn't
handle on its own, because each added something to its schema after it
first shipped: `notification_queue.rule_id` (`alarm_log_store.go`'s own
lazy `ALTER TABLE`, applied to every database on open) and
`plugin_config_values` (a second table `plugin_overrides_store.go` creates
alongside `plugin_overrides`, added later). A legacy file that was never
reopened since either shipped has neither - the live store patches both on
its own next open, but `ATTACH` reads the raw file as it sits on disk,
bypassing that entirely. `migrateAttachedLegacyData` applies the identical
`ALTER TABLE`/`CREATE TABLE IF NOT EXISTS` directly to the attached legacy
file before copying from it, so an old-enough file still migrates cleanly
instead of failing on "no such column" or "no such table".

### 5. Startup refuses to run against a pre-upgrade data directory

`checkForLegacyDatabaseFiles` (`backend/helmcentral_db.go`) runs in `main()`
before any store opens anything: if any of `documents.sqlite`,
`assistant.sqlite`, `nearby-contacts.sqlite`, `alarm-log.sqlite`,
`webpush-subscriptions.sqlite` or `plugin_overrides.sqlite` still exist
beside where `helmcentral.sqlite` would go, it `log.Fatalf`s naming every
file it found and the exact migrate command to run, rather than either
silently ignoring years of document, conversation, sighting, alarm,
device-registration or plugin-config history, or starting against an empty
combined file while the real data sits untouched next to it.

Under Docker Compose this failure happens almost immediately on every
restart - the service crash-loops until the operator runs the migration -
which means `docker exec` cannot reliably reach the container between
restarts to run it there. The message instead names
`docker compose run --rm helmcentral /app/helmcentral migrate-db`: a fresh,
one-off container against the same `backend-data` volume, run whether or not
the crash-looping service is currently up between restarts, followed by
`docker compose up -d helmcentral` once it reports success. A native install
stops the service, runs `/usr/local/bin/helmcentral migrate-db` directly,
then starts it again.

## Consequences

- The whole file runs in WAL now, which supersedes ADR 0106's "the backup
  unit is exactly one db file": a plain copy of `helmcentral.sqlite` taken
  while Helmcentral is running can be a torn read if it misses the current
  `-wal` contents. Back it up with the service stopped, or with SQLite's own
  online backup (`sqlite3 helmcentral.sqlite ".backup /path/to/backup.sqlite"`
  or `VACUUM INTO`) while it runs - never separate the `-wal`/`-shm` files
  from the main one while Helmcentral is live.
- A declared foreign key from `message_attachments.document_id` to
  `documents` becomes possible for the first time - the two tables finally
  share a connection and a `foreign_keys` pragma that is actually
  enforced - but is deliberately left for later rather than added in the
  same change that makes it possible.
- `docs/reference/configuration.md`'s state-paths table drops six variables
  and gains one; anything that read `ASSISTANT_DB_PATH`, `DOCUMENTS_DB_PATH`,
  `NEARBY_CONTACTS_DB_PATH`, `ALARM_LOG_DB`, `WEBPUSH_DB_PATH` or
  `PLUGIN_OVERRIDES_DB_PATH` from the environment has to move to
  `HELMCENTRAL_DB_PATH` on upgrade, via the migration above.
- Clearing alarm history stops being a file operation. Before this change,
  an operator (or a support script) who wanted to wipe alarm history could
  stop Helmcentral, delete `alarm-log.sqlite`, and start it again - the
  store's own `CREATE TABLE IF NOT EXISTS` recreated an empty one on next
  open. That is no longer available: `alarm_log` and `notification_queue`
  are tables inside `helmcentral.sqlite` now, and deleting the file to
  clear them would delete every other business record with it. The
  equivalent is now a table-level clear (`DELETE FROM alarm_log`, or
  waiting on `DropLogOlderThan`'s existing year-long retention) rather than
  a file-level one.

## Alternatives considered

**Keep the AIS sighting log separate, combine only documents and Mate.**
The sighting log is the smallest and least "business record" of the three,
and folding it in was briefly considered optional. Rejected for
consistency: a fourth business-data file with its own backup story, kept
separate for no reason but that it is small, is exactly the kind of
un-motivated split this ADR exists to close. It is boat history the same
way a document or a conversation is.

**Leave the alarm log, web push and plugin overrides where they were, fold
in only documents/assistant/nearby-contacts.** This was the shape this ADR
first shipped in. Rejected on reflection for the same reason the sighting
log wasn't left out: none of the three reasons originally given (Decision 2
above) survive being looked at directly - "safe to delete" stops being true
the moment the file also holds everything else, "kept out of the alarm log"
named a file rather than a property once that file joins the combined
database too, and "shipped as its own file" was never a reason at all. Six
small, unmotivated splits are the same problem as three.

**Drop WAL, keep `documentStore`'s original no-WAL backup property.**
Considered because it is the simpler backup story: one file, no siblings to
lose track of. Rejected because it would stall the five-second AIS poller
(and any Mate read) behind whatever a long document write - a bulk reindex,
a multi-page OCR pass - is doing at that moment, the exact problem WAL
exists to solve for `nearby-contacts.sqlite` today. A backup procedure that
accounts for `-wal`/`-shm` is a smaller cost than a poller that stalls.
