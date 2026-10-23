package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// Camera shots for the passage replay (ADR 0206). A shot belongs to a moment
// of real time, not to a passage, so splitting, merging or trimming a passage
// never moves or loses one.

var errCameraShotNotFound = errors.New("camera shot not found")

const (
	cameraModeOverview = "overview"
	cameraModeFollow   = "follow"
	cameraModeOrbit    = "orbit"

	cameraZoomMax      = 22.0
	cameraPitchMax     = 70.0
	cameraOffsetMax    = 180.0
	cameraOrbitRateMax = 90.0
)

type cameraShot struct {
	ID               string    `json:"id"`
	TMs              int64     `json:"t_ms"`
	Mode             string    `json:"mode"`
	Zoom             float64   `json:"zoom"`
	Pitch            float64   `json:"pitch"`
	BearingOffsetDeg float64   `json:"bearing_offset_deg"`
	OrbitDegPerS     float64   `json:"orbit_deg_per_s"`
	Pace             float64   `json:"pace"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func createCameraShotSchema(db *sql.DB) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS replay_camera_shots (
			id TEXT PRIMARY KEY,
			t_ms INTEGER NOT NULL,
			mode TEXT NOT NULL,
			zoom REAL NOT NULL,
			pitch REAL NOT NULL,
			bearing_offset_deg REAL NOT NULL DEFAULT 0,
			orbit_deg_per_s REAL NOT NULL DEFAULT 0,
			pace REAL NOT NULL DEFAULT 1,
			created_ms INTEGER NOT NULL,
			updated_ms INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_replay_camera_shots_t ON replay_camera_shots(t_ms)`,
	} {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("create replay camera schema: %w", err)
		}
	}
	return nil
}

func validateCameraShot(s cameraShot) error {
	switch s.Mode {
	case cameraModeOverview, cameraModeFollow, cameraModeOrbit:
	default:
		return invalidf("mode must be overview, follow or orbit")
	}
	if !(s.Zoom >= 0 && s.Zoom <= cameraZoomMax) {
		return invalidf("zoom must be between 0 and %g", cameraZoomMax)
	}
	if !(s.Pitch >= 0 && s.Pitch <= cameraPitchMax) {
		return invalidf("pitch must be between 0 and %g", cameraPitchMax)
	}
	if !(s.BearingOffsetDeg >= -cameraOffsetMax && s.BearingOffsetDeg <= cameraOffsetMax) {
		return invalidf("bearing_offset_deg must be between -%g and %g", cameraOffsetMax, cameraOffsetMax)
	}
	if !(s.OrbitDegPerS >= -cameraOrbitRateMax && s.OrbitDegPerS <= cameraOrbitRateMax) {
		return invalidf("orbit_deg_per_s must be between -%g and %g", cameraOrbitRateMax, cameraOrbitRateMax)
	}
	if s.Pace != 1 && s.Pace != 0.5 && s.Pace != 0.25 {
		return invalidf("pace must be 1, 0.5 or 0.25")
	}
	return nil
}

// timeInsideAPassage says some passage, hidden or not, spans the instant.
func timeInsideAPassage(q interface {
	QueryRow(string, ...any) *sql.Row
}, tMs int64) error {
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM passages WHERE start_ms <= ? AND end_ms >= ?`, tMs, tMs).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return invalidf("t_ms is not inside any passage")
	}
	return nil
}

const cameraShotColumns = `id, t_ms, mode, zoom, pitch, bearing_offset_deg, orbit_deg_per_s, pace, created_ms, updated_ms`

func scanCameraShot(row interface{ Scan(...any) error }) (cameraShot, error) {
	var s cameraShot
	var created, updated int64
	err := row.Scan(&s.ID, &s.TMs, &s.Mode, &s.Zoom, &s.Pitch, &s.BearingOffsetDeg, &s.OrbitDegPerS, &s.Pace, &created, &updated)
	s.CreatedAt, s.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
	return s, err
}

// ListCameraShots returns the shots with from <= t_ms <= to, oldest first. A
// zero from or to leaves that end open.
func (s *passageStore) ListCameraShots(from, to int64) ([]cameraShot, error) {
	q := `SELECT ` + cameraShotColumns + ` FROM replay_camera_shots WHERE 1=1`
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
	out := []cameraShot{}
	for rows.Next() {
		sh, err := scanCameraShot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

func (s *passageStore) CreateCameraShot(in cameraShot) (cameraShot, error) {
	if err := validateCameraShot(in); err != nil {
		return cameraShot{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return cameraShot{}, err
	}
	defer tx.Rollback()
	if err := timeInsideAPassage(tx, in.TMs); err != nil {
		return cameraShot{}, err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	in.ID, in.CreatedAt, in.UpdatedAt = uuid.NewString(), now, now
	if _, err := tx.Exec(`INSERT INTO replay_camera_shots (`+cameraShotColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		in.ID, in.TMs, in.Mode, in.Zoom, in.Pitch, in.BearingOffsetDeg, in.OrbitDegPerS, in.Pace, now.UnixMilli(), now.UnixMilli()); err != nil {
		return cameraShot{}, fmt.Errorf("insert camera shot: %w", err)
	}
	return in, tx.Commit()
}

type cameraShotPatch struct {
	TMs              *int64
	Mode             *string
	Zoom             *float64
	Pitch            *float64
	BearingOffsetDeg *float64
	OrbitDegPerS     *float64
	Pace             *float64
}

func (s *passageStore) PatchCameraShot(id string, p cameraShotPatch) (cameraShot, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return cameraShot{}, err
	}
	defer tx.Rollback()
	sh, err := scanCameraShot(tx.QueryRow(`SELECT `+cameraShotColumns+` FROM replay_camera_shots WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return sh, errCameraShotNotFound
	}
	if err != nil {
		return sh, err
	}
	if p.TMs != nil {
		sh.TMs = *p.TMs
	}
	if p.Mode != nil {
		sh.Mode = *p.Mode
	}
	if p.Zoom != nil {
		sh.Zoom = *p.Zoom
	}
	if p.Pitch != nil {
		sh.Pitch = *p.Pitch
	}
	if p.BearingOffsetDeg != nil {
		sh.BearingOffsetDeg = *p.BearingOffsetDeg
	}
	if p.OrbitDegPerS != nil {
		sh.OrbitDegPerS = *p.OrbitDegPerS
	}
	if p.Pace != nil {
		sh.Pace = *p.Pace
	}
	if err := validateCameraShot(sh); err != nil {
		return sh, err
	}
	if p.TMs != nil {
		if err := timeInsideAPassage(tx, sh.TMs); err != nil {
			return sh, err
		}
	}
	sh.UpdatedAt = time.Now().UTC().Truncate(time.Millisecond)
	if _, err := tx.Exec(`UPDATE replay_camera_shots SET t_ms=?, mode=?, zoom=?, pitch=?, bearing_offset_deg=?, orbit_deg_per_s=?, pace=?, updated_ms=? WHERE id=?`,
		sh.TMs, sh.Mode, sh.Zoom, sh.Pitch, sh.BearingOffsetDeg, sh.OrbitDegPerS, sh.Pace, sh.UpdatedAt.UnixMilli(), id); err != nil {
		return sh, fmt.Errorf("update camera shot: %w", err)
	}
	return sh, tx.Commit()
}

func (s *passageStore) DeleteCameraShot(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM replay_camera_shots WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errCameraShotNotFound
	}
	return tx.Commit()
}

// ── handlers ─────────────────────────────────────────────────────────────

func cameraShotErrorResponse(c echo.Context, err error) error {
	if errors.Is(err, errCameraShotNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	return passageErrorResponse(c, err)
}

func queryMs(c echo.Context, name string) (int64, error) {
	raw := c.QueryParam(name)
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, invalidf("%s must be whole milliseconds", name)
	}
	return v, nil
}

// listCameraShotsHandler is GET /api/passages/camera-shots?from=&to= (Unix ms).
func listCameraShotsHandler(c echo.Context) error {
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
	shots, err := store.ListCameraShots(from, to)
	if err != nil {
		return passageErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"shots": shots})
}

// createCameraShotHandler is POST /api/passages/camera-shots.
func createCameraShotHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	var body struct {
		TMs              *int64   `json:"t_ms"`
		Mode             *string  `json:"mode"`
		Zoom             *float64 `json:"zoom"`
		Pitch            *float64 `json:"pitch"`
		BearingOffsetDeg float64  `json:"bearing_offset_deg"`
		OrbitDegPerS     float64  `json:"orbit_deg_per_s"`
		Pace             *float64 `json:"pace"`
	}
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	if body.TMs == nil || body.Mode == nil || body.Zoom == nil || body.Pitch == nil || body.Pace == nil {
		return passageErrorResponse(c, invalidf("t_ms, mode, zoom, pitch and pace are required"))
	}
	sh, err := store.CreateCameraShot(cameraShot{
		TMs: *body.TMs, Mode: *body.Mode, Zoom: *body.Zoom, Pitch: *body.Pitch,
		BearingOffsetDeg: body.BearingOffsetDeg, OrbitDegPerS: body.OrbitDegPerS, Pace: *body.Pace,
	})
	if err != nil {
		return cameraShotErrorResponse(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"shot": sh})
}

// patchCameraShotHandler is PATCH /api/passages/camera-shots/:id.
func patchCameraShotHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	var body struct {
		TMs              *int64   `json:"t_ms"`
		Mode             *string  `json:"mode"`
		Zoom             *float64 `json:"zoom"`
		Pitch            *float64 `json:"pitch"`
		BearingOffsetDeg *float64 `json:"bearing_offset_deg"`
		OrbitDegPerS     *float64 `json:"orbit_deg_per_s"`
		Pace             *float64 `json:"pace"`
	}
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	sh, err := store.PatchCameraShot(c.Param("id"), cameraShotPatch(body))
	if err != nil {
		return cameraShotErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"shot": sh})
}

// deleteCameraShotHandler is DELETE /api/passages/camera-shots/:id.
func deleteCameraShotHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	if err := store.DeleteCameraShot(c.Param("id")); err != nil {
		return cameraShotErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, map[string]bool{"deleted": true})
}
