# ADR 0141: One Business Database

## Status

Accepted (2026-09-28). Builds on [ADR 0106](0106-documents-in-the-binary.md)
(`documents.sqlite` and its "backup unit is one db file" reasoning), [ADR
0093](0093-onboard-assistant-over-openrouter.md) (`assistant.sqlite`, Mate's
conversation history) and [ADR 0044](0044-nearby-vessel-encounter-confirmation.md)
(`nearby-contacts.sqlite`, the AIS sighting log).

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

## Decision

### 1. `helmcentral.sqlite` is the one business database

Documents, notes, the equipment registry, inventory, maintenance rules and
service history, checklists (all of `documentStore`), Mate's conversations
(`assistantStore`), and the AIS sighting log (`nearbyContactStore`) all move
into one file, `helmcentral.sqlite`, resolved by a single environment
variable, `HELMCENTRAL_DB_PATH` (default `data/helmcentral.sqlite`).
`DOCUMENTS_DB_PATH`, `ASSISTANT_DB_PATH` and `NEARBY_CONTACTS_DB_PATH` are
removed outright, with no compatibility alias for any of the three - this
codebase's standing rule against speculative fallbacks applies to
environment variables as much as to anything else. `DOCUMENTS_DIR` (the
folder of hash-named file bytes ADR 0106 keeps separate from the database)
is unaffected; only the database file itself moves.

Each store keeps its own `*sql.DB` handle, its own mutex, and its own
`CREATE TABLE IF NOT EXISTS` schema code - nothing about how the three
stores talk to their tables changes, only which file those tables live in.
No foreign key is added from `message_attachments.document_id` to
`documents` in this change. The reference has existed in practice since
attachments shipped; making it a declared constraint is left for later, once
the three stores have been sharing a file long enough to be confident about
what enforcing it would actually catch.

### 2. Secrets, sessions, the alarm log, web push, plugin overrides and the
tile cache stay exactly where they are

None of the six move, for six different reasons:

- **Secrets** (`secrets.sqlite`) pairs with its own key file
  (`secrets.key`); losing that pair together is what makes every stored
  credential unrecoverable, a recovery story specific to this one store
  that has no business being entangled with anything else.
- **Sessions** (`sessions.sqlite`) are throwaway - nobody needs to back up
  who is currently logged in.
- **The alarm log** (`alarm-log.sqlite`) is safe-to-delete by design;
  clearing alarm history is a supported operator action, unlike anything in
  the combined file.
- **Web push** (`webpush-subscriptions.sqlite`) is device registration
  config, not vessel history.
- **Plugin overrides** (`plugin_overrides.sqlite`) is config.
- **The tile cache** (`tile-cache.sqlite`) is a cache; it is reconstructed
  from Esri on a cold start and has no history worth protecting.

None of these six describe the boat. They describe this installation's own
operating state, and each already has a backup story (or deliberately
doesn't need one) that combining files would only complicate.

### 3. One shared opener, WAL for every store that uses it

`openHelmcentralDB(path string) (*sql.DB, error)` (`backend/helmcentral_db.go`)
is the one function all three stores open their own connection through: it
creates the parent directory, sets `foreign_keys(1)`, `busy_timeout(5000)`,
`journal_mode(WAL)` and `synchronous(NORMAL)`, and reads back both
`foreign_keys` and `journal_mode` to confirm they actually took effect rather
than trusting the driver's own DSN pragma parameters - the same fail-fast
check `newDocumentStore` has always applied to `foreign_keys` alone, now
applied to both pragmas that matter, for every caller.

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

It refuses rather than skips, on four different checks, each with its own
message: `helmcentral.sqlite` already existing (nothing to do, or this
already ran); `documents.sqlite` not existing (a fresh install has nothing
to migrate); a `documents.sqlite-journal` sitting next to it (the last
process to hold that file open did not shut down cleanly, and renaming it
forward would make the very next open run SQLite's own crash recovery
against it as a side effect of migration rather than a deliberate choice);
and, per table, a row count that doesn't match after copying. The first
three stop the migration before anything is touched; the fourth (and any
other failure once `documents.sqlite` has already been renamed forward -
opening the combined file, creating its tables, attaching a legacy file,
copying rows, or committing) renames `helmcentral.sqlite` straight back to
`documents.sqlite` before returning the error, so a failed run leaves the
install exactly as it found it and re-running `migrate-db`, once whatever
caused the failure is fixed, is always safe. Safety here comes from refusing
outright (or undoing the one step that already happened), not from silently
skipping a step and hoping the operator notices later.

`documents.sqlite` is renamed straight to `helmcentral.sqlite` (with its
`-wal`/`-shm` siblings, if present) and reopened through `openHelmcentralDB`,
which is what actually converts it to WAL; the assistant and nearby-contact
tables are created in it via those stores' own schema code, factored out
into `createAssistantSchema`/`createNearbyContactSchema` so the migration
and the ordinary `newAssistantStore`/`newNearbyContactStore` paths share one
copy of each. `assistant.sqlite` and `nearby-contacts.sqlite`, when present,
are `ATTACH`ed and every row copied across by an explicit column list, in
one transaction, with the row count checked table by table before it
commits. Reading a `-wal`-backed legacy file this way (`ATTACH` against the
live file, not a copy of it) is deliberate: SQLite resolves a WAL-backed
attach correctly on its own, and copying `nearby-contacts.sqlite` without
its `-wal` sidecar would silently drop whatever had not yet been
checkpointed into the main file. Neither legacy file is ever deleted: once
migrated, each is renamed to `<name>.sqlite.migrated` so an operator who
wants to double-check the numbers still has the original.

### 5. Startup refuses to run against a pre-upgrade data directory

`checkForLegacyDatabaseFiles` (`backend/helmcentral_db.go`) runs in `main()`
before any store opens anything: if `documents.sqlite`, `assistant.sqlite`
or `nearby-contacts.sqlite` still exist beside where `helmcentral.sqlite`
would go, it `log.Fatalf`s naming every file it found and the exact migrate
command to run, rather than either silently ignoring years of document,
conversation or sighting history, or starting against an empty combined
file while the real data sits untouched next to it.

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
- `docs/reference/configuration.md`'s state-paths table drops three
  variables and gains one; anything that read `ASSISTANT_DB_PATH`,
  `DOCUMENTS_DB_PATH` or `NEARBY_CONTACTS_DB_PATH` from the environment has
  to move to `HELMCENTRAL_DB_PATH` on upgrade, via the migration above.

## Alternatives considered

**Keep the AIS sighting log separate, combine only documents and Mate.**
The sighting log is the smallest and least "business record" of the three,
and folding it in was briefly considered optional. Rejected for
consistency: a fourth business-data file with its own backup story, kept
separate for no reason but that it is small, is exactly the kind of
un-motivated split this ADR exists to close. It is boat history the same
way a document or a conversation is.

**Drop WAL, keep `documentStore`'s original no-WAL backup property.**
Considered because it is the simpler backup story: one file, no siblings to
lose track of. Rejected because it would stall the five-second AIS poller
(and any Mate read) behind whatever a long document write - a bulk reindex,
a multi-page OCR pass - is doing at that moment, the exact problem WAL
exists to solve for `nearby-contacts.sqlite` today. A backup procedure that
accounts for `-wal`/`-shm` is a smaller cost than a poller that stalls.
