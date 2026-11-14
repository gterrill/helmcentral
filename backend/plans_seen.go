package main

import (
	"database/sql"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// Change marks (ADR 0288): the verdict each leg of a plan had when the operator
// last looked, kept on the box so any device can say "was Go, now Marginal". A
// leg is named by its key (planLegKey). Only the verdict level is kept.

func createPlanSeenSchema(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS plan_verdicts_seen (
		plan_id INTEGER NOT NULL,
		leg_key TEXT NOT NULL,
		verdict TEXT NOT NULL,
		PRIMARY KEY (plan_id, leg_key)
	)`); err != nil {
		return fmt.Errorf("create plan verdicts schema: %w", err)
	}
	return nil
}

// planSeenVerdict is one leg's verdict as last seen.
type planSeenVerdict struct {
	Key     string `json:"key"`
	Verdict string `json:"verdict"`
}

var planSeenVerdictLevels = []string{"go", "marginal", "no_go", "no_data"}

// PlanSeen returns the saved verdict of every leg of a plan, by key.
func (s *documentStore) PlanSeen(planID int64) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.planSeenLocked(planID)
}

func (s *documentStore) planSeenLocked(planID int64) (map[string]string, error) {
	rows, err := s.db.Query(`SELECT leg_key, verdict FROM plan_verdicts_seen WHERE plan_id = ?`, planID)
	if err != nil {
		return nil, fmt.Errorf("read plan verdicts seen: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("read plan verdicts seen: %w", err)
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SavePlanSeen replaces a plan's whole snapshot with the given verdicts, keeping
// only those whose key is one of the plan's current legs (currentKeys). A
// snapshot that is the one already saved writes nothing.
func (s *documentStore) SavePlanSeen(planID int64, currentKeys []string, seen []planSeenVerdict) error {
	want := map[string]string{}
	for _, v := range seen {
		if slices.Contains(currentKeys, v.Key) {
			want[v.Key] = v.Verdict
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	have, err := s.planSeenLocked(planID)
	if err != nil {
		return err
	}
	if maps.Equal(have, want) {
		return nil
	}
	return s.runTxLocked("save plan verdicts seen", func(tx *sql.Tx, _ time.Time) error {
		if _, err := tx.Exec(`DELETE FROM plan_verdicts_seen WHERE plan_id = ?`, planID); err != nil {
			return fmt.Errorf("save plan verdicts seen: %w", err)
		}
		for k, v := range want {
			if _, err := tx.Exec(`INSERT INTO plan_verdicts_seen (plan_id, leg_key, verdict) VALUES (?, ?, ?)`, planID, k, v); err != nil {
				return fmt.Errorf("save plan verdicts seen: %w", err)
			}
		}
		return nil
	})
}

// planLegKeys are the keys of the plan's current legs.
func planLegKeys(p plan) []string {
	keys := make([]string, 0, max(len(p.Stops)-1, 0))
	for i := 0; i < len(p.Stops)-1; i++ {
		keys = append(keys, planLegKey(p, i))
	}
	return keys
}

// planSeenGetHandler is GET /api/plans/:id/seen: the verdicts the operator last
// looked at, by leg key. The saving keeps only keys the plan had then; a leg the
// plan has since renamed or moved simply finds none.
func planSeenGetHandler(c echo.Context) error {
	p, ok, err := loadDecoratedPlan(c, planNow())
	if !ok {
		return err
	}
	seen, err := globalDocumentStore.PlanSeen(p.ID)
	if err != nil {
		return writeDocumentError(c, err)
	}
	legs := make([]planSeenVerdict, 0, len(seen))
	for k, v := range seen {
		legs = append(legs, planSeenVerdict{Key: k, Verdict: v})
	}
	slices.SortFunc(legs, func(a, b planSeenVerdict) int { return strings.Compare(a.Key, b.Key) })
	return c.JSON(http.StatusOK, map[string]any{"legs": legs})
}

// planSeenPutHandler is PUT /api/plans/:id/seen: save the verdicts now shown as
// the ones last looked at. A key the plan has no leg for is ignored.
func planSeenPutHandler(c echo.Context) error {
	p, ok, err := loadDecoratedPlan(c, planNow())
	if !ok {
		return err
	}
	limitNoteRequestBody(c)
	var req struct {
		Legs []planSeenVerdict `json:"legs"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	for _, v := range req.Legs {
		if !slices.Contains(planSeenVerdictLevels, v.Verdict) {
			return c.JSON(http.StatusBadRequest, map[string]string{"field": "verdict", "message": "verdict must be go, marginal, no_go or no_data"})
		}
	}
	if err := globalDocumentStore.SavePlanSeen(p.ID, planLegKeys(p), req.Legs); err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]bool{"saved": true})
}
