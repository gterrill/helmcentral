package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// The overview map's corner and zoom for a replay (ADR 0209). Like camera
// shots (ADR 0206) the setting belongs to real time: the replay's range, from
// the first passage's start to the last passage's end. A passage and a cruise
// that starts with it are different ranges and keep separate settings, and
// renames and edits that leave both ends alone do not disturb one. Nothing saved means the defaults: top
// right, the automatic zoom.

const (
	insetDefaultCorner = "top-right"
	insetZoomOffsetMax = 3
)

var insetCorners = map[string]bool{"top-left": true, "top-right": true, "bottom-left": true, "bottom-right": true}

type insetView struct {
	FromMs     int64  `json:"from_ms"`
	ToMs       int64  `json:"to_ms"`
	Corner     string `json:"corner"`
	ZoomOffset int    `json:"zoom_offset"`
}

func createInsetViewSchema(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS replay_inset_views (
		from_ms INTEGER NOT NULL,
		to_ms INTEGER NOT NULL,
		corner TEXT NOT NULL,
		zoom_offset INTEGER NOT NULL,
		updated_ms INTEGER NOT NULL,
		PRIMARY KEY (from_ms, to_ms)
	)`); err != nil {
		return fmt.Errorf("create replay inset schema: %w", err)
	}
	return nil
}

func validateInsetView(v insetView) error {
	if !insetCorners[v.Corner] {
		return invalidf("corner must be top-left, top-right, bottom-left or bottom-right")
	}
	if v.ZoomOffset < -insetZoomOffsetMax || v.ZoomOffset > insetZoomOffsetMax {
		return invalidf("zoom_offset must be between -%d and %d", insetZoomOffsetMax, insetZoomOffsetMax)
	}
	return nil
}

// InsetView is the saved setting for the range, or the defaults.
func (s *passageStore) InsetView(fromMs, toMs int64) (insetView, error) {
	v := insetView{FromMs: fromMs, ToMs: toMs, Corner: insetDefaultCorner}
	err := s.db.QueryRow(`SELECT corner, zoom_offset FROM replay_inset_views WHERE from_ms = ? AND to_ms = ?`, fromMs, toMs).Scan(&v.Corner, &v.ZoomOffset)
	if err == sql.ErrNoRows {
		return v, nil
	}
	return v, err
}

func (s *passageStore) SetInsetView(in insetView) (insetView, error) {
	if err := validateInsetView(in); err != nil {
		return insetView{}, err
	}
	if in.ToMs < in.FromMs {
		return insetView{}, invalidf("to_ms must not be before from_ms")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return insetView{}, err
	}
	defer tx.Rollback()
	// Both ends must lie inside a passage; only "not inside" is the caller's
	// fault, a failing database is returned as it is.
	if err := timeInsideAPassage(tx, in.FromMs); err != nil {
		return insetView{}, rangeEndError("from_ms", err)
	}
	if err := timeInsideAPassage(tx, in.ToMs); err != nil {
		return insetView{}, rangeEndError("to_ms", err)
	}
	if _, err := tx.Exec(`INSERT INTO replay_inset_views (from_ms, to_ms, corner, zoom_offset, updated_ms) VALUES (?,?,?,?,?)
		ON CONFLICT(from_ms, to_ms) DO UPDATE SET corner=excluded.corner, zoom_offset=excluded.zoom_offset, updated_ms=excluded.updated_ms`,
		in.FromMs, in.ToMs, in.Corner, in.ZoomOffset, time.Now().UnixMilli()); err != nil {
		return insetView{}, fmt.Errorf("save inset view: %w", err)
	}
	return in, tx.Commit()
}

func rangeEndError(name string, err error) error {
	if errors.Is(err, errPassageInvalid) {
		return invalidf("%s is not inside any passage", name)
	}
	return err
}

// getInsetViewHandler is GET /api/passages/inset-view?from=&to= (Unix ms).
func getInsetViewHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	if c.QueryParam("from") == "" || c.QueryParam("to") == "" {
		return passageErrorResponse(c, invalidf("from and to are required"))
	}
	from, err := queryMs(c, "from")
	if err != nil {
		return passageErrorResponse(c, err)
	}
	to, err := queryMs(c, "to")
	if err != nil {
		return passageErrorResponse(c, err)
	}
	if to < from {
		return passageErrorResponse(c, invalidf("to must not be before from"))
	}
	v, err := store.InsetView(from, to)
	if err != nil {
		return passageErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"inset_view": v})
}

// putInsetViewHandler is PUT /api/passages/inset-view.
func putInsetViewHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	var body struct {
		FromMs     *int64  `json:"from_ms"`
		ToMs       *int64  `json:"to_ms"`
		Corner     *string `json:"corner"`
		ZoomOffset *int    `json:"zoom_offset"`
	}
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	if body.FromMs == nil || body.ToMs == nil || body.Corner == nil || body.ZoomOffset == nil {
		return passageErrorResponse(c, invalidf("from_ms, to_ms, corner and zoom_offset are required"))
	}
	v, err := store.SetInsetView(insetView{FromMs: *body.FromMs, ToMs: *body.ToMs, Corner: *body.Corner, ZoomOffset: *body.ZoomOffset})
	if err != nil {
		return passageErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"inset_view": v})
}
