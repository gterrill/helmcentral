package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// The REST side of routines (ADR 0227): the operator can see, switch off and
// delete a routine without Mate. Creating and rewording one goes through a
// Mate proposal, which keeps one writer for a routine's definition.

type mateRoutinesResponse struct {
	Routines        []mateRoutine `json:"routines"`
	Max             int           `json:"max"`
	RoutinesEnabled bool          `json:"routines_enabled"`
	DailyCapUSD     float64       `json:"daily_cap_usd"`
	Spent24hUSD     float64       `json:"spent_24h_usd"`
	// PausedUntil is set while a failed run's unknown cost holds routines back.
	PausedUntil *time.Time `json:"paused_until,omitempty"`
	// Problem says in full why routines are not running, "" when nothing is wrong.
	Problem string `json:"problem,omitempty"`
}

func routinesStores() (*documentStore, *assistantStore, error) {
	if globalDocumentStore == nil {
		return nil, nil, errors.New("the record store is not open")
	}
	if globalAssistantStore == nil {
		return nil, nil, errors.New("the assistant store is not open")
	}
	return globalDocumentStore, globalAssistantStore, nil
}

// GET /api/assistant/routines
func listMateRoutinesHandler(c echo.Context) error {
	docs, _, err := routinesStores()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	var routines []mateRoutine
	if err := docs.Read(func(q sqlQueryer) error {
		var err error
		routines, err = listMateRoutinesQ(q)
		return err
	}); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	settings, err := readSettings(assistantSettingsPath())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "read settings: " + err.Error()})
	}
	assistant := buildSettingsPayload(settings).Assistant
	spent, err := mateRoutineSpendSince(docs, time.Now().Add(-24*time.Hour))
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	var pausedUntil *time.Time
	if spent.UnknownAt != nil {
		t := spent.UnknownAt.Add(24 * time.Hour)
		pausedUntil = &t
	}
	if routines == nil {
		routines = []mateRoutine{}
	}
	problem := ""
	if host := activeMateRoutineHost; host != nil {
		problem = host.stopReason()
	}
	if problem == "" {
		if _, err := readMateRoutineConfig(assistantSettingsPath()); err != nil {
			problem = "Routines are not running: " + firstErrorLine(err)
		}
	}
	return c.JSON(http.StatusOK, mateRoutinesResponse{Problem: problem,
		Routines: routines, Max: maxMateRoutines, RoutinesEnabled: assistant.RoutinesEnabled,
		DailyCapUSD: assistant.RoutinesDailyCapUSD, Spent24hUSD: spent.USD, PausedUntil: pausedUntil,
	})
}

// PATCH /api/assistant/routines/:id with {"enabled": bool}
func patchMateRoutineHandler(c echo.Context) error {
	docs, _, err := routinesStores()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	dec := json.NewDecoder(c.Request().Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || body.Enabled == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "send {\"enabled\": true or false}; the rest of a routine is changed by asking Mate"})
	}
	var routine mateRoutine
	err = docs.RunTx("switch routine", func(tx *sql.Tx, now time.Time) error {
		r, err := mateRoutineByIDQ(tx, c.Param("id"))
		if err != nil {
			return err
		}
		r.Enabled, r.UpdatedAt = *body.Enabled, now
		if err := validateMateRoutine(&r); err != nil {
			return err
		}
		routine = r
		return updateMateRoutineTx(tx, r)
	})
	if errors.Is(err, errMateRoutineNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, routine)
}

// DELETE /api/assistant/routines/:id. The routine's conversation stays.
func deleteMateRoutineHandler(c echo.Context) error {
	docs, _, err := routinesStores()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	err = docs.RunTx("delete routine", func(tx *sql.Tx, _ time.Time) error {
		return deleteMateRoutineTx(tx, c.Param("id"))
	})
	if errors.Is(err, errMateRoutineNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}
