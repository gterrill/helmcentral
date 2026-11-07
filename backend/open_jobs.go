package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Open jobs (ADR 0269): a known problem that is not fixed yet ("shower pump
// leaks at the outlet"). A one-off record with an open / done / dropped
// status. It is not a maintenance rule (a rule repeats and has no done column;
// ADR 0138) and not an alarm (nothing is raised, and a job is never counted
// as one). The code calls it an open job and never a "job" alone: a "profile
// job" (ADR 0148) is a recurring service an equipment profile supplies.
//
// Done writes one line to the service log and links it; Dropped writes none.
// Both are commands taking an open transaction, the way maintenance_commands.go
// does, so the HTTP handlers and Mate's changeset run the same code.

var (
	errOpenJobNotFound      = errors.New("open job not found")
	errOpenJobClosed        = errors.New("this job is already closed")
	errOpenJobPhotoNotFound = errors.New("photo not found on this job")
)

const (
	openJobOpen    = "open"
	openJobDone    = "done"
	openJobDropped = "dropped"
	// openJobStatusAny is a list filter only: every status.
	openJobStatusAny = "any"

	openJobSourceManual        = "manual"
	openJobSourceMate          = "mate"
	openJobSourceWatchFinding  = "watch_finding"
	openJobSourceAlarm         = "alarm"
	openJobSourceChecklistLine = "checklist_line"

	openJobTitleMax = 200
	openJobNotesMax = 4000
	openJobLabelMax = 200
	openJobLinkMax  = 500
)

var validOpenJobSources = map[string]bool{
	openJobSourceManual: true, openJobSourceMate: true, openJobSourceWatchFinding: true,
	openJobSourceAlarm: true, openJobSourceChecklistLine: true,
}

// openJob is one row of open_jobs with its item's name and photo ids. Dates
// are YYYY-MM-DD, "" when unset. The age of a job is not stored: it is the
// operator's date minus OpenedOn, so each reader works it out against the
// date it was given.
type openJob struct {
	ID            string  `json:"id"`
	EquipmentID   *string `json:"equipment_id"`
	EquipmentName string  `json:"equipment_name"`
	Title         string  `json:"title"`
	Notes         string  `json:"notes"`
	OpenedOn      string  `json:"opened_on"`
	BeforeOn      string  `json:"before_on"`
	Status        string  `json:"status"`
	ClosedOn      string  `json:"closed_on"`
	DropReason    string  `json:"drop_reason"`
	// LogEntryID is the service log line Done wrote.
	LogEntryID *string `json:"log_entry_id"`
	Source     string  `json:"source"`
	// SourceLink is an in-app path back to where the job came from, "" when
	// there is none; SourceLabel names it for the operator.
	SourceLink  string    `json:"source_link"`
	SourceLabel string    `json:"source_label"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	PhotoIDs    []string  `json:"photo_ids"`
}

// openJobRequest is the editable shape a create or an edit takes. Source,
// SourceLink and SourceLabel are read on create only; an edit never moves
// where a job came from.
type openJobRequest struct {
	EquipmentID *string `json:"equipment_id"`
	Title       string  `json:"title"`
	Notes       string  `json:"notes"`
	BeforeOn    string  `json:"before_on"`
	Source      string  `json:"source"`
	SourceLink  string  `json:"source_link"`
	SourceLabel string  `json:"source_label"`
}

// openJobFilter is ListOpenJobs' input. Status "" lists the open jobs;
// openJobStatusAny lists every status. IncludeStored false leaves out the
// jobs of items in storage, the way the Maintenance list leaves out their
// rules. System narrows to one system's gear and leaves out jobs with no item.
type openJobFilter struct {
	EquipmentID   string
	System        string
	Status        string
	IncludeStored bool
}

// openJobAgeDays is the whole days from the day a job opened to today, never
// negative. An unreadable date counts as 0 rather than a made-up age.
func openJobAgeDays(openedOn string, today time.Time) int {
	opened, err := time.Parse("2006-01-02", openedOn)
	if err != nil {
		return 0
	}
	days := int(today.Sub(opened).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

// ── validation ──────────────────────────────────────────────────────────

func validateOpenJobRequest(req openJobRequest, create bool) (openJobRequest, *inventoryValidationError) {
	req.Title = strings.TrimSpace(req.Title)
	req.Notes = strings.TrimSpace(req.Notes)
	req.BeforeOn = strings.TrimSpace(req.BeforeOn)
	req.EquipmentID = trimStringPtr(req.EquipmentID)
	if req.Title == "" {
		return req, &inventoryValidationError{Field: "title", Message: "title is required"}
	}
	if utf8.RuneCountInString(req.Title) > openJobTitleMax {
		return req, &inventoryValidationError{Field: "title", Message: fmt.Sprintf("title is at most %d characters", openJobTitleMax)}
	}
	if utf8.RuneCountInString(req.Notes) > openJobNotesMax {
		return req, &inventoryValidationError{Field: "notes", Message: fmt.Sprintf("notes are at most %d characters", openJobNotesMax)}
	}
	if req.BeforeOn != "" {
		if _, err := time.Parse("2006-01-02", req.BeforeOn); err != nil {
			return req, &inventoryValidationError{Field: "before_on", Message: "before_on must be blank or a real date, YYYY-MM-DD"}
		}
	}
	if !create {
		return req, nil
	}

	req.Source = strings.TrimSpace(req.Source)
	if req.Source == "" {
		req.Source = openJobSourceManual
	}
	if !validOpenJobSources[req.Source] {
		return req, &inventoryValidationError{Field: "source", Message: "source must be manual, mate, watch_finding, alarm or checklist_line"}
	}
	req.SourceLink = strings.TrimSpace(req.SourceLink)
	req.SourceLabel = strings.TrimSpace(req.SourceLabel)
	if req.SourceLink != "" {
		// The link is followed by the browser as a page of this app. Only a
		// path that stays on this origin is kept: no scheme, no host, and no
		// protocol-relative "//host".
		if req.Source == openJobSourceManual {
			return req, &inventoryValidationError{Field: "source_link", Message: "a job made by hand has no link back"}
		}
		if len(req.SourceLink) > openJobLinkMax || !strings.HasPrefix(req.SourceLink, "/") ||
			strings.HasPrefix(req.SourceLink, "//") || strings.ContainsAny(req.SourceLink, "\\ \t\r\n") {
			return req, &inventoryValidationError{Field: "source_link", Message: "source_link must be a path inside Helmcentral, such as /alarms"}
		}
	}
	if utf8.RuneCountInString(req.SourceLabel) > openJobLabelMax {
		return req, &inventoryValidationError{Field: "source_label", Message: fmt.Sprintf("source_label is at most %d characters", openJobLabelMax)}
	}
	return req, nil
}

// ── reads ───────────────────────────────────────────────────────────────

const openJobSelect = `SELECT j.id, j.equipment_id, COALESCE(e.name, ''), j.title, j.notes, j.opened_on, j.before_on,
	j.status, j.closed_on, j.drop_reason, j.log_entry_id, j.source, j.source_link, j.source_label, j.created_at, j.updated_at
	FROM open_jobs j LEFT JOIN equipment e ON e.id = j.equipment_id`

func scanOpenJob(row rowScanner) (openJob, error) {
	var j openJob
	var equipmentID, logEntryID sql.NullString
	var createdAt, updatedAt int64
	if err := row.Scan(&j.ID, &equipmentID, &j.EquipmentName, &j.Title, &j.Notes, &j.OpenedOn, &j.BeforeOn,
		&j.Status, &j.ClosedOn, &j.DropReason, &logEntryID, &j.Source, &j.SourceLink, &j.SourceLabel, &createdAt, &updatedAt); err != nil {
		return openJob{}, err
	}
	if equipmentID.Valid {
		v := equipmentID.String
		j.EquipmentID = &v
	}
	if logEntryID.Valid {
		v := logEntryID.String
		j.LogEntryID = &v
	}
	j.CreatedAt = time.Unix(createdAt, 0).UTC()
	j.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	j.PhotoIDs = []string{}
	return j, nil
}

// attachOpenJobPhotos fills PhotoIDs on every job with one query.
func attachOpenJobPhotos(q sqlQueryer, jobs []openJob) error {
	if len(jobs) == 0 {
		return nil
	}
	index := make(map[string]int, len(jobs))
	args := make([]any, len(jobs))
	marks := make([]string, len(jobs))
	for i, j := range jobs {
		index[j.ID] = i
		args[i] = j.ID
		marks[i] = "?"
	}
	rows, err := q.Query(`SELECT job_id, document_id FROM open_job_photos WHERE job_id IN (`+strings.Join(marks, ",")+`)
		ORDER BY job_id, sort_index, document_id`, args...)
	if err != nil {
		return fmt.Errorf("open job photos: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var jobID, documentID string
		if err := rows.Scan(&jobID, &documentID); err != nil {
			return fmt.Errorf("open job photos: scan: %w", err)
		}
		i := index[jobID]
		jobs[i].PhotoIDs = append(jobs[i].PhotoIDs, documentID)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("open job photos: %w", err)
	}
	return nil
}

func openJobByID(q sqlQueryer, id string) (openJob, error) {
	j, err := scanOpenJob(q.QueryRow(openJobSelect+` WHERE j.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return openJob{}, errOpenJobNotFound
	}
	if err != nil {
		return openJob{}, fmt.Errorf("get open job: %w", err)
	}
	jobs := []openJob{j}
	if err := attachOpenJobPhotos(q, jobs); err != nil {
		return openJob{}, err
	}
	return jobs[0], nil
}

func listOpenJobs(q sqlQueryer, f openJobFilter) ([]openJob, error) {
	query := openJobSelect + ` WHERE 1=1`
	var args []any
	switch f.Status {
	case "", openJobOpen:
		query += ` AND j.status = 'open'`
	case openJobStatusAny:
	default:
		query += ` AND j.status = ?`
		args = append(args, f.Status)
	}
	if f.EquipmentID != "" {
		query += ` AND j.equipment_id = ?`
		args = append(args, f.EquipmentID)
	}
	if f.System != "" {
		query += ` AND e.system = ?`
		args = append(args, f.System)
	}
	if !f.IncludeStored {
		query += ` AND (j.equipment_id IS NULL OR e.status != 'stored')`
	}
	query += ` ORDER BY j.opened_on, j.created_at, j.id`
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list open jobs: %w", err)
	}
	defer rows.Close()
	jobs := []openJob{}
	for rows.Next() {
		j, err := scanOpenJob(rows)
		if err != nil {
			return nil, fmt.Errorf("list open jobs: scan: %w", err)
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list open jobs: %w", err)
	}
	rows.Close()
	if err := attachOpenJobPhotos(q, jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (s *documentStore) GetOpenJob(id string) (openJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return openJobByID(s.db, id)
}

func (s *documentStore) ListOpenJobs(f openJobFilter) ([]openJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return listOpenJobs(s.db, f)
}

// ── commands ────────────────────────────────────────────────────────────

func requireOpenJobEquipment(tx *sql.Tx, id *string) error {
	if id == nil {
		return nil
	}
	ok, err := rowExists(tx, `SELECT 1 FROM equipment WHERE id = ?`, *id)
	if err != nil {
		return fmt.Errorf("check equipment: %w", err)
	}
	if !ok {
		return errEquipmentNotFound
	}
	return nil
}

// openJobForClose reads a job that must still be open.
func openJobForClose(tx *sql.Tx, id string) (openJob, error) {
	job, err := openJobByID(tx, id)
	if err != nil {
		return openJob{}, err
	}
	if job.Status != openJobOpen {
		return openJob{}, errOpenJobClosed
	}
	return job, nil
}

// cmdCreateOpenJob opens a job on today's date (the operator's, never the
// server's).
func cmdCreateOpenJob(tx *sql.Tx, now, today time.Time, req openJobRequest) (openJob, error) {
	req, verr := validateOpenJobRequest(req, true)
	if verr != nil {
		return openJob{}, verr
	}
	if err := requireOpenJobEquipment(tx, req.EquipmentID); err != nil {
		return openJob{}, err
	}
	id := uuid.NewString()
	if _, err := tx.Exec(`
		INSERT INTO open_jobs (id, equipment_id, title, notes, opened_on, before_on, status, closed_on, drop_reason, log_entry_id,
			source, source_link, source_label, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'open', '', '', NULL, ?, ?, ?, ?, ?)`,
		id, nullableString(req.EquipmentID), req.Title, req.Notes, today.Format("2006-01-02"), req.BeforeOn,
		req.Source, req.SourceLink, req.SourceLabel, now.Unix(), now.Unix(),
	); err != nil {
		return openJob{}, fmt.Errorf("create open job: %w", err)
	}
	return openJobByID(tx, id)
}

// cmdUpdateOpenJob replaces an open job's item, title, notes and before-date.
// Where it came from, when it opened and its photos are not touched.
func cmdUpdateOpenJob(tx *sql.Tx, now time.Time, id string, req openJobRequest) (openJob, error) {
	req, verr := validateOpenJobRequest(req, false)
	if verr != nil {
		return openJob{}, verr
	}
	if _, err := openJobForClose(tx, id); err != nil {
		return openJob{}, err
	}
	if err := requireOpenJobEquipment(tx, req.EquipmentID); err != nil {
		return openJob{}, err
	}
	if _, err := tx.Exec(`UPDATE open_jobs SET equipment_id = ?, title = ?, notes = ?, before_on = ?, updated_at = ? WHERE id = ?`,
		nullableString(req.EquipmentID), req.Title, req.Notes, req.BeforeOn, now.Unix(), id); err != nil {
		return openJob{}, fmt.Errorf("update open job: %w", err)
	}
	return openJobByID(tx, id)
}

// cmdDropOpenJob closes a job without a service log line. The reason is
// optional.
func cmdDropOpenJob(tx *sql.Tx, now, today time.Time, id, reason string) (openJob, error) {
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > openJobNotesMax {
		return openJob{}, &inventoryValidationError{Field: "reason", Message: fmt.Sprintf("reason is at most %d characters", openJobNotesMax)}
	}
	if _, err := openJobForClose(tx, id); err != nil {
		return openJob{}, err
	}
	if _, err := tx.Exec(`UPDATE open_jobs SET status = 'dropped', closed_on = ?, drop_reason = ?, updated_at = ? WHERE id = ?`,
		today.Format("2006-01-02"), reason, now.Unix(), id); err != nil {
		return openJob{}, fmt.Errorf("drop open job: %w", err)
	}
	return openJobByID(tx, id)
}

// cmdCompleteOpenJob writes the one service log line for a finished job and
// closes the job on the line's date. The line is a repair unless the form
// says otherwise, on the job's item, with the job's title when no notes are
// given. Gauge hours are converted the way every log line's are. The job's
// photos are shown on the line too. A refusal anywhere leaves the job open.
func cmdCompleteOpenJob(tx *sql.Tx, now, today time.Time, id string, req maintenanceLogEntryRequest) (openJob, maintenanceLogEntry, error) {
	in, verr := validateMaintenanceLogEntryCore(req)
	if verr != nil {
		return openJob{}, maintenanceLogEntry{}, verr
	}
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = "repair"
	}
	if !validMaintenanceLogKinds[kind] {
		return openJob{}, maintenanceLogEntry{}, &inventoryValidationError{Field: "kind", Message: "kind must be maintenance, repair or improvement"}
	}
	job, err := openJobForClose(tx, id)
	if err != nil {
		return openJob{}, maintenanceLogEntry{}, err
	}

	// The line's date can be the day the job opened, today, or any day
	// between; never before it was known, never a day that has not come.
	if in.PerformedAt < job.OpenedOn {
		return openJob{}, maintenanceLogEntry{}, &inventoryValidationError{Field: "performed_at", Message: "performed_at cannot be before the day the job was opened, " + job.OpenedOn}
	}
	if in.PerformedAt > today.Format("2006-01-02") {
		return openJob{}, maintenanceLogEntry{}, &inventoryValidationError{Field: "performed_at", Message: "performed_at cannot be in the future"}
	}

	in.Kind = kind
	in.EquipmentID = job.EquipmentID
	if in.Description == "" {
		in.Description = job.Title
	}
	if in.Hours != nil {
		if job.EquipmentID == nil {
			return openJob{}, maintenanceLogEntry{}, &inventoryValidationError{Field: "hours", Message: "a job with no item has no hour meter to take a reading from"}
		}
		performedAt, parseErr := time.Parse("2006-01-02", in.PerformedAt)
		if parseErr != nil {
			return openJob{}, maintenanceLogEntry{}, fmt.Errorf("parse performed_at: %w", parseErr)
		}
		trueHours, err := convertGaugeHoursToTrueTx(tx, *job.EquipmentID, *in.Hours, performedAt)
		if err != nil {
			return openJob{}, maintenanceLogEntry{}, err
		}
		in.Hours = &trueHours
	}
	if err := checkMaintenancePartsExistTx(tx, in.Parts); err != nil {
		return openJob{}, maintenanceLogEntry{}, err
	}

	entry, err := insertMaintenanceLogEntryTx(tx, now, in)
	if err != nil {
		return openJob{}, maintenanceLogEntry{}, err
	}
	if _, err := tx.Exec(`INSERT INTO maintenance_log_photos (log_entry_id, document_id, sort_index, created_at)
		SELECT ?, document_id, sort_index, ? FROM open_job_photos WHERE job_id = ?`, entry.ID, now.Unix(), id); err != nil {
		return openJob{}, maintenanceLogEntry{}, fmt.Errorf("complete open job: share photos: %w", err)
	}
	if _, err := tx.Exec(`UPDATE open_jobs SET status = 'done', closed_on = ?, log_entry_id = ?, updated_at = ? WHERE id = ?`,
		in.PerformedAt, entry.ID, now.Unix(), id); err != nil {
		return openJob{}, maintenanceLogEntry{}, fmt.Errorf("complete open job: close: %w", err)
	}
	entry, err = maintenanceLogEntryByID(tx, entry.ID)
	if err != nil {
		return openJob{}, maintenanceLogEntry{}, err
	}
	closed, err := openJobByID(tx, id)
	if err != nil {
		return openJob{}, maintenanceLogEntry{}, err
	}
	return closed, entry, nil
}

// ── store entry points ──────────────────────────────────────────────────

func (s *documentStore) CreateOpenJobCmd(today time.Time, req openJobRequest) (openJob, error) {
	return runCmd(s, "create open job", func(tx *sql.Tx, now time.Time) (openJob, error) {
		return cmdCreateOpenJob(tx, now, today, req)
	})
}

func (s *documentStore) UpdateOpenJobCmd(id string, req openJobRequest) (openJob, error) {
	return runCmd(s, "update open job", func(tx *sql.Tx, now time.Time) (openJob, error) {
		return cmdUpdateOpenJob(tx, now, id, req)
	})
}

func (s *documentStore) DropOpenJobCmd(today time.Time, id, reason string) (openJob, error) {
	return runCmd(s, "drop open job", func(tx *sql.Tx, now time.Time) (openJob, error) {
		return cmdDropOpenJob(tx, now, today, id, reason)
	})
}

type completedOpenJob struct {
	job   openJob
	entry maintenanceLogEntry
}

func (s *documentStore) CompleteOpenJobCmd(today time.Time, id string, req maintenanceLogEntryRequest) (openJob, maintenanceLogEntry, error) {
	done, err := runCmd(s, "complete open job", func(tx *sql.Tx, now time.Time) (completedOpenJob, error) {
		job, entry, err := cmdCompleteOpenJob(tx, now, today, id, req)
		return completedOpenJob{job, entry}, err
	})
	return done.job, done.entry, err
}

// ── photos ──────────────────────────────────────────────────────────────
// Mirrors the service log's photos (maintenance_store.go): an append-ordered
// strip, unlink only here, deleting the document itself is the handler's call.

// AddOpenJobPhoto links documentID to the job at the end of its photo order.
// Only a job that is still open takes photos; once closed, its photos are the
// ones it was closed with.
func (s *documentStore) AddOpenJobPhoto(jobID, documentID string) error {
	return s.RunTx("add open job photo", func(tx *sql.Tx, now time.Time) error {
		if _, err := openJobForClose(tx, jobID); err != nil {
			return err
		}
		already, err := rowExists(tx, `SELECT 1 FROM open_job_photos WHERE job_id = ? AND document_id = ?`, jobID, documentID)
		if err != nil {
			return fmt.Errorf("add open job photo: check existing: %w", err)
		}
		if already {
			return nil
		}
		var maxSort sql.NullInt64
		if err := tx.QueryRow(`SELECT MAX(sort_index) FROM open_job_photos WHERE job_id = ?`, jobID).Scan(&maxSort); err != nil {
			return fmt.Errorf("add open job photo: max sort: %w", err)
		}
		next := 0
		if maxSort.Valid {
			next = int(maxSort.Int64) + 1
		}
		if _, err := tx.Exec(`INSERT INTO open_job_photos (job_id, document_id, sort_index, created_at) VALUES (?, ?, ?, ?)`,
			jobID, documentID, next, now.Unix()); err != nil {
			return fmt.Errorf("add open job photo: link: %w", err)
		}
		return nil
	})
}

// RemoveOpenJobPhoto unlinks documentID from the job.
func (s *documentStore) RemoveOpenJobPhoto(jobID, documentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`DELETE FROM open_job_photos WHERE job_id = ? AND document_id = ?`, jobID, documentID)
	if err != nil {
		return fmt.Errorf("remove open job photo: %w", err)
	}
	return checkRowsAffected(res, errOpenJobPhotoNotFound)
}

// OpenJobPhotoDeletableAsOrphan is true when nothing else (an item's photo
// strip, a log line, another job) still shows the document.
func (s *documentStore) OpenJobPhotoDeletableAsOrphan(documentID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return rowExists(s.db, `
		SELECT 1 FROM documents d
		WHERE d.id = ?
		AND `+documentDeletableAsOrphanPhotoClause+`
		AND `+documentHasNoEquipmentPhotoLinkClause+`
		AND `+documentHasNoMaintenanceLogPhotoLinkClause, documentID)
}
