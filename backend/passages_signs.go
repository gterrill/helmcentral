package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// Signs for the passage replay (ADR 0204). A sign is a short note, with an
// optional photo, pinned to a real instant on the track. Like a camera shot
// (ADR 0206) it belongs to a moment of time, not to a passage, so splitting,
// merging or trimming a passage never moves or loses one, and nothing here
// deletes a sign except the operator.

var errSignNotFound = errors.New("sign not found")

const (
	signAlways  = "always"
	signPassing = "passing"
	signAfter   = "after"

	signTitleMax = 80
	signNoteMax  = 280
	signShowMin  = 1
	signShowMax  = 30
	signShowDef  = 4
)

type replaySign struct {
	ID         string
	TMs        int64
	Title      string
	Note       string
	DocumentID *string
	Visibility string
	ShowS      int
	// StartsLeg marks a join: the track from here on is drawn as a new leg.
	StartsLeg bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// signJSON is a sign as the client sees it. The photo fields are filled only
// when the document is still in the library.
type signJSON struct {
	ID         string    `json:"id"`
	TMs        int64     `json:"t_ms"`
	Title      string    `json:"title"`
	Note       string    `json:"note"`
	DocumentID *string   `json:"document_id"`
	PhotoURL   *string   `json:"photo_url"`
	Visibility string    `json:"visibility"`
	ShowS      int       `json:"show_s"`
	StartsLeg  bool      `json:"starts_leg"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func createSignSchema(db *sql.DB) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS replay_signs (
			id TEXT PRIMARY KEY,
			t_ms INTEGER NOT NULL,
			title TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			document_id TEXT,
			visibility TEXT NOT NULL,
			show_s INTEGER NOT NULL,
			created_ms INTEGER NOT NULL,
			updated_ms INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_replay_signs_t ON replay_signs(t_ms)`,
	} {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("create replay signs schema: %w", err)
		}
	}
	// A join is a sign that starts a new leg. name_attempted_ms stamps the
	// last try at naming an untitled join from the track, as on passages.
	if err := ensureColumn(db, "replay_signs", "starts_leg", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return ensureColumn(db, "replay_signs", "name_attempted_ms", "INTEGER")
}

// joinTitle cuts a place name written into a join sign to the title limit, so
// a long looked-up or passage name never leaves a sign that fails validation.
func joinTitle(name string) string {
	if r := []rune(name); len(r) > signTitleMax {
		return strings.TrimSpace(string(r[:signTitleMax]))
	}
	return name
}

func validateSign(s replaySign) error {
	// Only a join may be untitled: it waits for the naming lookup.
	if n := utf8.RuneCountInString(s.Title); (n == 0 && !s.StartsLeg) || n > signTitleMax {
		return invalidf("title is required and at most %d characters", signTitleMax)
	}
	if utf8.RuneCountInString(s.Note) > signNoteMax {
		return invalidf("note must be at most %d characters", signNoteMax)
	}
	switch s.Visibility {
	case signAlways, signPassing, signAfter:
	default:
		return invalidf("visibility must be always, passing or after")
	}
	if s.ShowS < signShowMin || s.ShowS > signShowMax {
		return invalidf("show_s must be between %d and %d", signShowMin, signShowMax)
	}
	return nil
}

// signPhotoMIME is what the documents API takes for pictures.
var signPhotoMIME = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// checkSignPhoto says the document exists and is a picture.
func checkSignPhoto(id string) error {
	if globalDocumentStore == nil {
		return errors.New("document library is not running")
	}
	doc, err := globalDocumentStore.Get(id)
	if errors.Is(err, errDocumentNotFound) {
		return invalidf("document_id is not a document")
	}
	if err != nil {
		return err
	}
	if !signPhotoMIME[doc.MIME] {
		return invalidf("document_id must be a png, jpeg, gif or webp image")
	}
	return nil
}

const signColumns = `id, t_ms, title, note, document_id, visibility, show_s, starts_leg, created_ms, updated_ms`

func scanSign(row interface{ Scan(...any) error }) (replaySign, error) {
	var s replaySign
	var doc sql.NullString
	var created, updated int64
	var startsLeg int
	err := row.Scan(&s.ID, &s.TMs, &s.Title, &s.Note, &doc, &s.Visibility, &s.ShowS, &startsLeg, &created, &updated)
	s.StartsLeg = startsLeg != 0
	if doc.Valid {
		s.DocumentID = &doc.String
	}
	s.CreatedAt, s.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
	return s, err
}

// ListSigns returns the signs with from <= t_ms <= to, oldest first. A zero
// from or to leaves that end open.
func (s *passageStore) ListSigns(from, to int64) ([]replaySign, error) {
	q := `SELECT ` + signColumns + ` FROM replay_signs WHERE 1=1`
	var args []any
	if from != 0 {
		q += ` AND t_ms >= ?`
		args = append(args, from)
	}
	if to != 0 {
		q += ` AND t_ms <= ?`
		args = append(args, to)
	}
	rows, err := s.db.Query(q+` ORDER BY t_ms, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []replaySign{}
	for rows.Next() {
		sg, err := scanSign(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sg)
	}
	return out, rows.Err()
}

func (s *passageStore) CreateSign(in replaySign) (replaySign, error) {
	in.Title, in.Note = strings.TrimSpace(in.Title), strings.TrimSpace(in.Note)
	if err := validateSign(in); err != nil {
		return replaySign{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return replaySign{}, err
	}
	defer tx.Rollback()
	if err := timeInsideAPassage(tx, in.TMs); err != nil {
		return replaySign{}, err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	in.ID, in.CreatedAt, in.UpdatedAt = uuid.NewString(), now, now
	if _, err := tx.Exec(`INSERT INTO replay_signs (`+signColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		in.ID, in.TMs, in.Title, in.Note, in.DocumentID, in.Visibility, in.ShowS, boolInt(in.StartsLeg), now.UnixMilli(), now.UnixMilli()); err != nil {
		return replaySign{}, fmt.Errorf("insert sign: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return in, err
	}
	// An untitled join is waiting for a name; get it named promptly.
	if in.StartsLeg && in.Title == "" && passageNamesWake != nil {
		passageNamesWake()
	}
	return in, nil
}

// signPatch carries the fields a PATCH may change; nil means untouched.
// DocumentIDSet says document_id was present, and a nil DocumentID with it
// set means remove the photo.
type signPatch struct {
	TMs           *int64
	Title         *string
	Note          *string
	DocumentID    *string
	DocumentIDSet bool
	Visibility    *string
	ShowS         *int
	StartsLeg     *bool
}

func (s *passageStore) PatchSign(id string, p signPatch) (replaySign, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return replaySign{}, err
	}
	defer tx.Rollback()
	sg, err := scanSign(tx.QueryRow(`SELECT `+signColumns+` FROM replay_signs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return sg, errSignNotFound
	}
	if err != nil {
		return sg, err
	}
	if p.TMs != nil {
		sg.TMs = *p.TMs
	}
	if p.Title != nil {
		sg.Title = strings.TrimSpace(*p.Title)
	}
	if p.Note != nil {
		sg.Note = strings.TrimSpace(*p.Note)
	}
	if p.DocumentIDSet {
		sg.DocumentID = p.DocumentID
	}
	if p.Visibility != nil {
		sg.Visibility = *p.Visibility
	}
	if p.ShowS != nil {
		sg.ShowS = *p.ShowS
	}
	if p.StartsLeg != nil {
		sg.StartsLeg = *p.StartsLeg
	}
	if err := validateSign(sg); err != nil {
		return sg, err
	}
	if p.TMs != nil {
		if err := timeInsideAPassage(tx, sg.TMs); err != nil {
			return sg, err
		}
	}
	sg.UpdatedAt = time.Now().UTC().Truncate(time.Millisecond)
	untitledJoin := sg.StartsLeg && sg.Title == ""
	if _, err := tx.Exec(`UPDATE replay_signs SET t_ms=?, title=?, note=?, document_id=?, visibility=?, show_s=?, starts_leg=?, updated_ms=?,
		name_attempted_ms = CASE WHEN ? THEN NULL ELSE name_attempted_ms END WHERE id=?`,
		sg.TMs, sg.Title, sg.Note, sg.DocumentID, sg.Visibility, sg.ShowS, boolInt(sg.StartsLeg), sg.UpdatedAt.UnixMilli(), untitledJoin, id); err != nil {
		return sg, fmt.Errorf("update sign: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return sg, err
	}
	if untitledJoin && passageNamesWake != nil {
		passageNamesWake()
	}
	return sg, nil
}

func (s *passageStore) DeleteSign(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM replay_signs WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errSignNotFound
	}
	return tx.Commit()
}

// ── handlers ─────────────────────────────────────────────────────────────

func signErrorResponse(c echo.Context, err error) error {
	if errors.Is(err, errSignNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	return passageErrorResponse(c, err)
}

// toSignJSON adds the photo, but only while its document is still in the
// library. exists is memoised per request by the caller.
func toSignJSON(sg replaySign, exists func(id string) (bool, error)) (signJSON, error) {
	out := signJSON{
		ID: sg.ID, TMs: sg.TMs, Title: sg.Title, Note: sg.Note, Visibility: sg.Visibility,
		ShowS: sg.ShowS, StartsLeg: sg.StartsLeg, CreatedAt: sg.CreatedAt, UpdatedAt: sg.UpdatedAt,
	}
	if sg.DocumentID == nil {
		return out, nil
	}
	ok, err := exists(*sg.DocumentID)
	if err != nil {
		return out, err
	}
	if ok {
		id, url := *sg.DocumentID, "/api/documents/"+*sg.DocumentID+"/content"
		out.DocumentID, out.PhotoURL = &id, &url
	}
	return out, nil
}

func documentExists(id string) (bool, error) {
	if globalDocumentStore == nil {
		return false, errors.New("document library is not running")
	}
	_, err := globalDocumentStore.Get(id)
	if errors.Is(err, errDocumentNotFound) {
		return false, nil
	}
	return err == nil, err
}

// listSignsHandler is GET /api/passages/signs?from=&to= (Unix ms).
func listSignsHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	from, err := queryMs(c, "from")
	if err != nil {
		return passageErrorResponse(c, err)
	}
	to, err := queryMs(c, "to")
	if err != nil {
		return passageErrorResponse(c, err)
	}
	signs, err := store.ListSigns(from, to)
	if err != nil {
		return passageErrorResponse(c, err)
	}
	seen := map[string]bool{}
	exists := func(id string) (bool, error) {
		if v, ok := seen[id]; ok {
			return v, nil
		}
		v, err := documentExists(id)
		if err == nil {
			seen[id] = v
		}
		return v, err
	}
	out := make([]signJSON, len(signs))
	for i, sg := range signs {
		if out[i], err = toSignJSON(sg, exists); err != nil {
			return passageErrorResponse(c, err)
		}
	}
	return c.JSON(http.StatusOK, map[string]any{"signs": out})
}

func signResponse(c echo.Context, status int, sg replaySign) error {
	out, err := toSignJSON(sg, documentExists)
	if err != nil {
		return passageErrorResponse(c, err)
	}
	return c.JSON(status, map[string]any{"sign": out})
}

// createSignHandler is POST /api/passages/signs.
func createSignHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	var body struct {
		TMs        *int64  `json:"t_ms"`
		Title      *string `json:"title"`
		Note       string  `json:"note"`
		DocumentID *string `json:"document_id"`
		Visibility *string `json:"visibility"`
		ShowS      *int    `json:"show_s"`
		StartsLeg  bool    `json:"starts_leg"`
	}
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	// A join may leave the title out; the naming lookup fills it.
	if body.TMs == nil || (body.Title == nil && !body.StartsLeg) {
		return passageErrorResponse(c, invalidf("t_ms and title are required"))
	}
	in := replaySign{TMs: *body.TMs, Note: body.Note, DocumentID: body.DocumentID, Visibility: signPassing, ShowS: signShowDef, StartsLeg: body.StartsLeg}
	if body.Title != nil {
		in.Title = *body.Title
	}
	if body.Visibility != nil {
		in.Visibility = *body.Visibility
	}
	if body.ShowS != nil {
		in.ShowS = *body.ShowS
	}
	if in.DocumentID != nil {
		if err := checkSignPhoto(*in.DocumentID); err != nil {
			return signErrorResponse(c, err)
		}
	}
	sg, err := store.CreateSign(in)
	if err != nil {
		return signErrorResponse(c, err)
	}
	return signResponse(c, http.StatusCreated, sg)
}

// patchSignHandler is PATCH /api/passages/signs/:id. "document_id": null
// removes the photo.
func patchSignHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	var body struct {
		TMs        *int64          `json:"t_ms"`
		Title      *string         `json:"title"`
		Note       *string         `json:"note"`
		DocumentID json.RawMessage `json:"document_id"`
		Visibility *string         `json:"visibility"`
		ShowS      *int            `json:"show_s"`
		StartsLeg  *bool           `json:"starts_leg"`
	}
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	patch := signPatch{TMs: body.TMs, Title: body.Title, Note: body.Note, Visibility: body.Visibility, ShowS: body.ShowS, StartsLeg: body.StartsLeg}
	if body.DocumentID != nil {
		patch.DocumentIDSet = true
		if !bytes.Equal(bytes.TrimSpace(body.DocumentID), []byte("null")) {
			var id string
			if err := json.Unmarshal(body.DocumentID, &id); err != nil {
				return passageErrorResponse(c, invalidf("document_id must be a string or null"))
			}
			if err := checkSignPhoto(id); err != nil {
				return signErrorResponse(c, err)
			}
			patch.DocumentID = &id
		}
	}
	sg, err := store.PatchSign(c.Param("id"), patch)
	if err != nil {
		return signErrorResponse(c, err)
	}
	return signResponse(c, http.StatusOK, sg)
}

// deleteSignHandler is DELETE /api/passages/signs/:id.
func deleteSignHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	if err := store.DeleteSign(c.Param("id")); err != nil {
		return signErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, map[string]bool{"deleted": true})
}
