package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// Edit routes for passages and cruises (tierWrite). Reads of the result come
// back as the same shapes the read routes use.

const passageEditMaxBody = 64 << 10

func passageErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, errPassageNotFound), errors.Is(err, errCruiseNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, errPassageInvalid):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	log.Printf("passages: %v", err)
	return c.JSON(http.StatusInternalServerError, map[string]string{"error": "passage request failed"})
}

func bindPassageBody(c echo.Context, v any) error {
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, passageEditMaxBody)
	dec := json.NewDecoder(c.Request().Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	return nil
}

// patchPassageHandler is PATCH /api/passages/:id.
func patchPassageHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	var body struct {
		Title    *string         `json:"title"`
		Notes    *string         `json:"notes"`
		FromName *string         `json:"from_name"`
		ToName   *string         `json:"to_name"`
		Hidden   *bool           `json:"hidden"`
		CruiseID json.RawMessage `json:"cruise_id"`
	}
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	patch := passagePatch{Title: body.Title, Notes: body.Notes, FromName: body.FromName, ToName: body.ToName, Hidden: body.Hidden}
	if body.CruiseID != nil {
		patch.CruiseIDSet = true
		if !bytes.Equal(bytes.TrimSpace(body.CruiseID), []byte("null")) {
			var id string
			if err := json.Unmarshal(body.CruiseID, &id); err != nil {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "cruise_id must be a string or null"})
			}
			patch.CruiseID = &id
		}
	}
	rec, err := store.PatchPassage(c.Param("id"), patch)
	if err != nil {
		return passageErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"passage": rec})
}

// splitPassageHandler is POST /api/passages/:id/split {at}.
func splitPassageHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	var body struct {
		At time.Time `json:"at"`
	}
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	a, b, err := store.SplitPassage(c.Param("id"), body.At.UTC())
	if err != nil {
		return passageErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"passages": []passageRecord{a, b}})
}

// mergePassagesHandler is POST /api/passages/merge {ids:[a,b,...]} with two or more ids.
func mergePassagesHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	rec, err := store.MergePassages(body.IDs...)
	if err != nil {
		return passageErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"passage": rec})
}

// trimPassageHandler is POST /api/passages/:id/trim {start,end}.
func trimPassageHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	var body struct {
		Start time.Time `json:"start"`
		End   time.Time `json:"end"`
	}
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	rec, err := store.TrimPassage(c.Param("id"), body.Start.UTC(), body.End.UTC())
	if err != nil {
		return passageErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"passage": rec})
}

// ── cruises ──────────────────────────────────────────────────────────────

func listCruisesHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	cruises, err := store.ListCruises()
	if err != nil {
		return passageErrorResponse(c, err)
	}
	if cruises == nil {
		cruises = []cruiseRecord{}
	}
	return c.JSON(http.StatusOK, map[string]any{"cruises": cruises})
}

func createCruiseHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	var body struct {
		Name  string `json:"name"`
		Notes string `json:"notes"`
	}
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	cr, err := store.CreateCruise(body.Name, body.Notes)
	if err != nil {
		return passageErrorResponse(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"cruise": cr})
}

func patchCruiseHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	var body struct {
		Name  *string `json:"name"`
		Notes *string `json:"notes"`
	}
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	cr, err := store.PatchCruise(c.Param("id"), cruisePatch{Name: body.Name, Notes: body.Notes})
	if err != nil {
		return passageErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"cruise": cr})
}

func deleteCruiseHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	if err := store.DeleteCruise(c.Param("id")); err != nil {
		return passageErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, map[string]bool{"deleted": true})
}

// getCruiseHandler is GET /api/cruises/:id: the cruise, its passages and the
// cruise-level figures.
func getCruiseHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	cr, passages, st, err := store.CruiseStats(c.Param("id"))
	if err != nil {
		return passageErrorResponse(c, err)
	}
	if passages == nil {
		passages = []passageRecord{}
	}
	return c.JSON(http.StatusOK, map[string]any{"cruise": cr, "passages": passages, "stats": st})
}

// putWaterRiseLabelHandler is PUT /api/cruises/:id/water-rises {at, label}:
// the operator's confirmation of a water rise as Watermaker or Dock fill. An
// empty label clears it.
func putWaterRiseLabelHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	var body struct {
		At    time.Time `json:"at"`
		Label string    `json:"label"`
	}
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	if body.At.IsZero() {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "at is required"})
	}
	if err := store.SetWaterRiseLabel(c.Param("id"), body.At.UTC(), body.Label); err != nil {
		return passageErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}
