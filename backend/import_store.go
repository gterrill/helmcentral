package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// importSchema creates the vessel particulars row and the two tables an
// import leaves behind. All three are new, so CREATE TABLE IF NOT EXISTS is
// the whole story on every boat's database.
var importSchema = []string{
	// vessel_particulars is one row, id pinned to 1: the things about the boat
	// that SignalK does not publish. Name, MMSI, call sign, length, beam, draft
	// and air height are deliberately not here; the boat's own data feed is
	// their source. Text columns hold "" for "not set", the two numeric ones
	// are NULL, so a blank form field and an unset value are the same thing
	// and a zero is never mistaken for a reading.
	`CREATE TABLE IF NOT EXISTS vessel_particulars (
		id              INTEGER PRIMARY KEY CHECK (id = 1),
		builder         TEXT NOT NULL DEFAULT '',
		model           TEXT NOT NULL DEFAULT '',
		year            INTEGER CHECK (year IS NULL OR (year >= 1800 AND year <= 2200)),
		hin             TEXT NOT NULL DEFAULT '',
		flag            TEXT NOT NULL DEFAULT '',
		hailing_port    TEXT NOT NULL DEFAULT '',
		hull_type       TEXT NOT NULL DEFAULT '',
		hull_material   TEXT NOT NULL DEFAULT '',
		displacement_kg REAL CHECK (displacement_kg IS NULL OR displacement_kg >= 0),
		shore_power     TEXT NOT NULL DEFAULT '',
		system_voltage  TEXT NOT NULL DEFAULT '',
		registration    TEXT NOT NULL DEFAULT '',
		imo             TEXT NOT NULL DEFAULT '',
		epirb_id        TEXT NOT NULL DEFAULT '',
		date_acquired   TEXT NOT NULL DEFAULT '',
		loa_m           REAL CHECK (loa_m IS NULL OR loa_m >= 0),
		beam_m          REAL CHECK (beam_m IS NULL OR beam_m >= 0),
		owner_name      TEXT NOT NULL DEFAULT '',
		owner_phone     TEXT NOT NULL DEFAULT '',
		owner_email     TEXT NOT NULL DEFAULT '',
		insurer         TEXT NOT NULL DEFAULT '',
		policy_number   TEXT NOT NULL DEFAULT '',
		home_marina     TEXT NOT NULL DEFAULT '',
		berth           TEXT NOT NULL DEFAULT '',
		storm_delegate  TEXT NOT NULL DEFAULT '',
		updated_at      INTEGER NOT NULL
	)`,

	// import_runs is one upload of an export file and everything decided about
	// it: the parsed payload (staged_json) and the operator's choices
	// (decisions_json) live on the server so the wizard resumes where it left
	// off on another device. file_sha256 is the uploaded bytes' hash, kept so
	// a run can say which file it came from. A run is draft until commit or
	// abandon; nothing in it touches the registry before commit.
	`CREATE TABLE IF NOT EXISTS import_runs (
		id             TEXT PRIMARY KEY,
		source         TEXT NOT NULL,
		source_label   TEXT NOT NULL DEFAULT '',
		file_sha256    TEXT NOT NULL,
		status         TEXT NOT NULL CHECK (status IN ('draft','committed','abandoned')),
		staged_json    TEXT NOT NULL,
		decisions_json TEXT NOT NULL DEFAULT '{}',
		created_at     INTEGER NOT NULL,
		updated_at     INTEGER NOT NULL,
		committed_at   INTEGER
	)`,
	`CREATE INDEX IF NOT EXISTS import_runs_status ON import_runs (status, created_at DESC)`,

	// import_records remembers what an import created or matched, keyed by the
	// source's own natural-key hash (the export has no ids). It is what makes a
	// second import of a newer export say "already imported" instead of
	// creating everything twice. The unique index is the whole rule: one
	// source record lands in one target kind once. CASCADE from the run so a
	// deleted run takes its records with it.
	`CREATE TABLE IF NOT EXISTS import_records (
		run_id       TEXT NOT NULL REFERENCES import_runs(id) ON DELETE CASCADE,
		source       TEXT NOT NULL,
		external_key TEXT NOT NULL,
		target_kind  TEXT NOT NULL,
		target_id    TEXT NOT NULL,
		created_at   INTEGER NOT NULL
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS import_records_source_key ON import_records (source, external_key, target_kind)`,
	`CREATE INDEX IF NOT EXISTS import_records_run ON import_records (run_id)`,
}

// createImportSchema is called from createDocumentsSchema, after the
// equipment rebuild, so the import tables exist on every open.
func createImportSchema(db *sql.DB) error {
	for _, stmt := range importSchema {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create import schema: %w", err)
		}
	}
	// The owner and insurance columns arrived after the table did. Single
	// operator, no installed base: a guarded ADD COLUMN is the whole of the
	// migration story (AGENTS.md), as for the document store.
	for _, stmt := range vesselParticularsAddedColumns {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("create import schema: %w", err)
		}
	}
	return nil
}

var vesselParticularsAddedColumns = []string{
	`ALTER TABLE vessel_particulars ADD COLUMN loa_m REAL CHECK (loa_m IS NULL OR loa_m >= 0)`,
	`ALTER TABLE vessel_particulars ADD COLUMN beam_m REAL CHECK (beam_m IS NULL OR beam_m >= 0)`,
	`ALTER TABLE vessel_particulars ADD COLUMN owner_name TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE vessel_particulars ADD COLUMN owner_phone TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE vessel_particulars ADD COLUMN owner_email TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE vessel_particulars ADD COLUMN insurer TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE vessel_particulars ADD COLUMN policy_number TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE vessel_particulars ADD COLUMN home_marina TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE vessel_particulars ADD COLUMN berth TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE vessel_particulars ADD COLUMN storm_delegate TEXT NOT NULL DEFAULT ''`,
}

// ── runs ─────────────────────────────────────────────────────────────────

var (
	// errImportRunNotFound is returned for an import run id that does not
	// exist.
	errImportRunNotFound = errors.New("import run not found")

	// errImportRunNotDraft is returned when something that only makes sense
	// on a draft (changing decisions, uploading a file, committing,
	// abandoning) is asked of a run that has already been committed or
	// abandoned.
	errImportRunNotDraft = errors.New("import run is no longer a draft")

	// errImportInvalid wraps every decision the operator (or a client) sent
	// that cannot be right: an unknown action, a key that is not in the
	// staged payload, a match with no target. The wrapping message says which.
	errImportInvalid = errors.New("invalid import decision")

	// errImportConflict wraps everything that stops a commit though the
	// decisions are well formed: a file nobody has decided on, an ambiguous
	// log entry without a choice, a record that is already imported, an
	// existing record a match points at that has since gone.
	errImportConflict = errors.New("import cannot be committed")
)

// importInvalidf and importConflictf build the wrapped errors.
func importInvalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errImportInvalid, fmt.Sprintf(format, args...))
}

func importConflictf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errImportConflict, fmt.Sprintf(format, args...))
}

// importRun is one upload and everything decided about it, as the API
// returns it. AlreadyImported lists the staged keys an earlier committed run
// already wrote, so the wizard can show them as such.
type importRun struct {
	ID              string          `json:"id"`
	Source          string          `json:"source"`
	SourceLabel     string          `json:"source_label"`
	FileSHA256      string          `json:"file_sha256"`
	Status          string          `json:"status"`
	Staged          stagedImport    `json:"staged"`
	Decisions       importDecisions `json:"decisions"`
	AlreadyImported []string        `json:"already_imported"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	CommittedAt     *time.Time      `json:"committed_at"`
}

// normalizeDecisions makes every map non-nil so a saved and reloaded set
// serialises as {} rather than null.
func normalizeDecisions(d importDecisions) importDecisions {
	if d.Particulars == nil {
		d.Particulars = map[string]string{}
	}
	if d.Zones == nil {
		d.Zones = map[string]importZoneDecision{}
	}
	if d.Records == nil {
		d.Records = map[string]importRecordDecision{}
	}
	if d.LogEquipment == nil {
		d.LogEquipment = map[string]importEquipmentRef{}
	}
	if d.Files == nil {
		d.Files = map[string]importFileDecision{}
	}
	return d
}

const importRunColumns = `id, source, source_label, file_sha256, status, staged_json, decisions_json, created_at, updated_at, committed_at`

func scanImportRun(row rowScanner) (importRun, error) {
	var r importRun
	var stagedJSON, decisionsJSON string
	var created, updated int64
	var committed sql.NullInt64
	if err := row.Scan(&r.ID, &r.Source, &r.SourceLabel, &r.FileSHA256, &r.Status, &stagedJSON, &decisionsJSON, &created, &updated, &committed); err != nil {
		return importRun{}, err
	}
	if err := json.Unmarshal([]byte(stagedJSON), &r.Staged); err != nil {
		return importRun{}, fmt.Errorf("scan import run: staged payload: %w", err)
	}
	if err := json.Unmarshal([]byte(decisionsJSON), &r.Decisions); err != nil {
		return importRun{}, fmt.Errorf("scan import run: decisions: %w", err)
	}
	r.Decisions = normalizeDecisions(r.Decisions)
	r.CreatedAt = time.Unix(created, 0).UTC()
	r.UpdatedAt = time.Unix(updated, 0).UTC()
	if committed.Valid {
		t := time.Unix(committed.Int64, 0).UTC()
		r.CommittedAt = &t
	}
	return r, nil
}

// importRunByID reads one run and fills AlreadyImported.
func importRunByID(q sqlQueryer, id string) (importRun, error) {
	r, err := scanImportRun(q.QueryRow(`SELECT `+importRunColumns+` FROM import_runs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return importRun{}, errImportRunNotFound
	}
	if err != nil {
		return importRun{}, fmt.Errorf("get import run: %w", err)
	}
	recorded, err := importRecordedKeys(q, r.Source)
	if err != nil {
		return importRun{}, err
	}
	r.AlreadyImported = alreadyImportedKeys(r.Staged, recorded)
	return r, nil
}

// importRecord is one row of import_records as the defaults and the commit
// need it.
type importRecord struct {
	Kind     string
	TargetID string
}

// importRecordedKeys reads every import_records row for source, keyed by
// external key and then target kind.
func importRecordedKeys(q sqlQueryer, source string) (map[string]map[string]importRecord, error) {
	rows, err := q.Query(`SELECT external_key, target_kind, target_id FROM import_records WHERE source = ?`, source)
	if err != nil {
		return nil, fmt.Errorf("read import records: %w", err)
	}
	defer rows.Close()
	out := map[string]map[string]importRecord{}
	for rows.Next() {
		var key, kind, target string
		if err := rows.Scan(&key, &kind, &target); err != nil {
			return nil, fmt.Errorf("read import records: %w", err)
		}
		if out[key] == nil {
			out[key] = map[string]importRecord{}
		}
		out[key][kind] = importRecord{Kind: kind, TargetID: target}
	}
	return out, rows.Err()
}

// stagedKeys lists every key in a staged payload, in a stable order, with
// the key each locker's items take (binKey#index).
func stagedKeys(st stagedImport) []string {
	var keys []string
	for _, l := range st.Locations {
		keys = append(keys, l.Key)
	}
	for _, e := range st.Equipment {
		keys = append(keys, e.Key)
	}
	for _, s := range st.Spares {
		keys = append(keys, s.Key)
		for i := range s.Items {
			keys = append(keys, binItemKey(s.Key, i))
		}
	}
	for _, e := range st.LogEntries {
		keys = append(keys, e.Key)
	}
	for _, n := range st.Notes {
		keys = append(keys, n.Key)
	}
	for _, f := range st.Files {
		keys = append(keys, f.Key)
	}
	return keys
}

// binItemKey is the external key of the index-th item of a locker row.
func binItemKey(binKey string, index int) string { return fmt.Sprintf("%s#%d", binKey, index) }

// alreadyImportedKeys is the subset of a payload's keys that has an
// import_records row; never nil.
func alreadyImportedKeys(st stagedImport, recorded map[string]map[string]importRecord) []string {
	out := []string{}
	for _, k := range stagedKeys(st) {
		if len(recorded[k]) > 0 {
			out = append(out, k)
		}
	}
	return out
}

// CreateImportRun stores a new draft. The decisions must already validate
// against the payload (the defaults always do).
func (s *documentStore) CreateImportRun(source, label, fileSHA string, staged stagedImport, decisions importDecisions) (importRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	decisions = normalizeDecisions(decisions)
	if err := validateImportDecisions(staged, decisions); err != nil {
		return importRun{}, err
	}
	stagedJSON, err := json.Marshal(staged)
	if err != nil {
		return importRun{}, fmt.Errorf("create import run: marshal staged: %w", err)
	}
	decisionsJSON, err := json.Marshal(decisions)
	if err != nil {
		return importRun{}, fmt.Errorf("create import run: marshal decisions: %w", err)
	}

	id := uuid.NewString()
	now := s.now().Unix()
	if _, err := s.db.Exec(`
		INSERT INTO import_runs (id, source, source_label, file_sha256, status, staged_json, decisions_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'draft', ?, ?, ?, ?)`,
		id, source, label, fileSHA, string(stagedJSON), string(decisionsJSON), now, now); err != nil {
		return importRun{}, fmt.Errorf("create import run: %w", err)
	}
	return importRunByID(s.db, id)
}

// GetImportRun reads one run.
func (s *documentStore) GetImportRun(id string) (importRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return importRunByID(s.db, id)
}

// MergeImportDecisions folds patch into the run's decisions, map entry by map
// entry: an entry the patch names replaces the stored one, an entry it does
// not name stays. The whole result is validated and, if anything is wrong,
// nothing is saved. A file entry's document_id is never taken from a patch -
// it is set only by an upload (SetImportFileDocument), so a stale client can
// not wipe a file the operator already handed over.
func (s *documentStore) MergeImportDecisions(id string, patch importDecisions) (importRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return importRun{}, fmt.Errorf("merge import decisions: begin: %w", err)
	}
	defer tx.Rollback()

	run, err := importRunByID(tx, id)
	if err != nil {
		return importRun{}, err
	}
	if run.Status != importStatusDraft {
		return importRun{}, errImportRunNotDraft
	}

	d := run.Decisions
	for k, v := range patch.Particulars {
		d.Particulars[k] = v
	}
	for k, v := range patch.Zones {
		d.Zones[k] = v
	}
	for k, v := range patch.Records {
		d.Records[k] = v
	}
	for k, v := range patch.LogEquipment {
		d.LogEquipment[k] = v
	}
	for k, v := range patch.Files {
		v.DocumentID = d.Files[k].DocumentID
		d.Files[k] = v
	}

	if err := validateImportDecisions(run.Staged, d); err != nil {
		return importRun{}, err
	}
	if err := checkImportMatchTargets(tx, run.Staged, d, importInvalidf); err != nil {
		return importRun{}, err
	}
	if err := saveImportDecisionsTx(tx, id, d, s.now()); err != nil {
		return importRun{}, err
	}
	out, err := importRunByID(tx, id)
	if err != nil {
		return importRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return importRun{}, fmt.Errorf("merge import decisions: commit: %w", err)
	}
	return out, nil
}

// SetImportFileDocument records the stored document the operator handed over
// for one staged file, and clears its skipped flag.
func (s *documentStore) SetImportFileDocument(id, fileKey, documentID string) (importRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return importRun{}, fmt.Errorf("set import file: begin: %w", err)
	}
	defer tx.Rollback()

	run, err := importRunByID(tx, id)
	if err != nil {
		return importRun{}, err
	}
	if run.Status != importStatusDraft {
		return importRun{}, errImportRunNotDraft
	}
	known := false
	for _, f := range run.Staged.Files {
		if f.Key == fileKey {
			known = true
		}
	}
	if !known {
		return importRun{}, importInvalidf("no file %q in this export", fileKey)
	}
	ok, err := rowExists(tx, `SELECT 1 FROM documents WHERE id = ?`, documentID)
	if err != nil {
		return importRun{}, fmt.Errorf("set import file: check document: %w", err)
	}
	if !ok {
		return importRun{}, errDocumentNotFound
	}

	d := run.Decisions
	fd := d.Files[fileKey]
	fd.DocumentID = documentID
	fd.Skipped = false
	d.Files[fileKey] = fd
	if err := saveImportDecisionsTx(tx, id, d, s.now()); err != nil {
		return importRun{}, err
	}
	out, err := importRunByID(tx, id)
	if err != nil {
		return importRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return importRun{}, fmt.Errorf("set import file: commit: %w", err)
	}
	return out, nil
}

func saveImportDecisionsTx(tx *sql.Tx, id string, d importDecisions, now time.Time) error {
	b, err := json.Marshal(normalizeDecisions(d))
	if err != nil {
		return fmt.Errorf("save import decisions: marshal: %w", err)
	}
	if _, err := tx.Exec(`UPDATE import_runs SET decisions_json = ?, updated_at = ? WHERE id = ?`, string(b), now.Unix(), id); err != nil {
		return fmt.Errorf("save import decisions: %w", err)
	}
	return nil
}

// AbandonImportRun marks a draft abandoned. Its staged payload stays (the
// run is a record of what was uploaded); nothing it would have written
// exists. Files already stored for it stay in the document library.
func (s *documentStore) AbandonImportRun(id string) (importRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return importRun{}, fmt.Errorf("abandon import run: begin: %w", err)
	}
	defer tx.Rollback()

	run, err := importRunByID(tx, id)
	if err != nil {
		return importRun{}, err
	}
	if run.Status != importStatusDraft {
		return importRun{}, errImportRunNotDraft
	}
	if _, err := tx.Exec(`UPDATE import_runs SET status = 'abandoned', updated_at = ? WHERE id = ?`, s.now().Unix(), id); err != nil {
		return importRun{}, fmt.Errorf("abandon import run: %w", err)
	}
	out, err := importRunByID(tx, id)
	if err != nil {
		return importRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return importRun{}, fmt.Errorf("abandon import run: commit: %w", err)
	}
	return out, nil
}
