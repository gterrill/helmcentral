package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// The overview map's corner and zoom for a replay (ADR 0209). Like camera
// shots (ADR 0206) the setting belongs to an instant of real time, the start of
// the replay's first passage, so it follows a passage through renames and
// edits that leave the start alone. Nothing saved means the defaults: top
// right, the automatic zoom.

const (
	insetDefaultCorner = "top-right"
	insetZoomOffsetMax = 3
)

var insetCorners = map[string]bool{"top-left": true, "top-right": true, "bottom-left": true, "bottom-right": true}

type insetView struct {
	AtMs       int64  `json:"at_ms"`
	Corner     string `json:"corner"`
	ZoomOffset int    `json:"zoom_offset"`
}

func createInsetViewSchema(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS replay_inset_views (
		at_ms INTEGER PRIMARY KEY,
		corner TEXT NOT NULL,
		zoom_offset INTEGER NOT NULL,
		updated_ms INTEGER NOT NULL
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

// InsetView is the saved setting at the instant, or the defaults.
func (s *passageStore) InsetView(atMs int64) (insetView, error) {
	v := insetView{AtMs: atMs, Corner: insetDefaultCorner}
	err := s.db.QueryRow(`SELECT corner, zoom_offset FROM replay_inset_views WHERE at_ms = ?`, atMs).Scan(&v.Corner, &v.ZoomOffset)
	if err == sql.ErrNoRows {
		return v, nil
	}
	return v, err
}

func (s *passageStore) SetInsetView(in insetView) (insetView, error) {
	if err := validateInsetView(in); err != nil {
		return insetView{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return insetView{}, err
	}
	defer tx.Rollback()
	if err := timeInsideAPassage(tx, in.AtMs); err != nil {
		return insetView{}, invalidf("at_ms is not inside any passage")
	}
	if _, err := tx.Exec(`INSERT INTO replay_inset_views (at_ms, corner, zoom_offset, updated_ms) VALUES (?,?,?,?)
		ON CONFLICT(at_ms) DO UPDATE SET corner=excluded.corner, zoom_offset=excluded.zoom_offset, updated_ms=excluded.updated_ms`,
		in.AtMs, in.Corner, in.ZoomOffset, time.Now().UnixMilli()); err != nil {
		return insetView{}, fmt.Errorf("save inset view: %w", err)
	}
	return in, tx.Commit()
}

// getInsetViewHandler is GET /api/passages/inset-view?at= (Unix ms).
func getInsetViewHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	if c.QueryParam("at") == "" {
		return passageErrorResponse(c, invalidf("at is required"))
	}
	at, err := queryMs(c, "at")
	if err != nil {
		return passageErrorResponse(c, err)
	}
	v, err := store.InsetView(at)
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
		AtMs       *int64  `json:"at_ms"`
		Corner     *string `json:"corner"`
		ZoomOffset *int    `json:"zoom_offset"`
	}
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	if body.AtMs == nil || body.Corner == nil || body.ZoomOffset == nil {
		return passageErrorResponse(c, invalidf("at_ms, corner and zoom_offset are required"))
	}
	v, err := store.SetInsetView(insetView{AtMs: *body.AtMs, Corner: *body.Corner, ZoomOffset: *body.ZoomOffset})
	if err != nil {
		return passageErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"inset_view": v})
}
