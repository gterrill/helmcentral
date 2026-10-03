package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Deck plans (ADR 0156): decks, zone outlines and bin pins. Coordinates are
// fractions of the plan image, 0 to 1, x across and y down.

var (
	errDeckNotFound    = errors.New("deck not found")
	errDeckNameInvalid = errors.New("deck name is required")
	errDeckNameTaken   = errors.New("a deck with this name already exists")

	errPolygonInvalid = errors.New("a zone outline needs 3 to 64 points, each between 0 and 1")
	errPinInvalid     = errors.New("a bin pin needs x and y between 0 and 1")

	// errLayoutBinZoneNotListed is a pin for a bin whose zone is not in the
	// same layout request (ADR 0156 §5).
	errLayoutBinZoneNotListed = errors.New("a pinned bin's zone must be on the plan in the same save")

	// errLayoutInvalid covers a layout naming the same zone or bin twice.
	errLayoutInvalid = errors.New("layout lists the same zone or bin more than once")
)

const (
	polygonMinPoints = 3
	polygonMaxPoints = 64
)

// inventoryDeck is one row of inventory_decks.
type inventoryDeck struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	SortIndex      int       `json:"sort_index"`
	PlanDocumentID *string   `json:"plan_document_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// deckLayoutZone and deckLayoutBin are the items of a SaveDeckLayout request.
type deckLayoutZone struct {
	ID      string       `json:"id"`
	Polygon [][2]float64 `json:"polygon"`
}

type deckLayoutBin struct {
	ID string  `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
}

func unitFraction(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1
}

func validatePolygon(p [][2]float64) error {
	if len(p) < polygonMinPoints || len(p) > polygonMaxPoints {
		return errPolygonInvalid
	}
	for _, pt := range p {
		if !unitFraction(pt[0]) || !unitFraction(pt[1]) {
			return errPolygonInvalid
		}
	}
	return nil
}

// zoneLayoutFromColumns decodes inventory_zones.deck_id/polygon. A polygon
// column that does not decode is a corrupt row and fails loudly.
func zoneLayoutFromColumns(deckID, polygon sql.NullString) (*string, [][2]float64, error) {
	var d *string
	if deckID.Valid {
		v := deckID.String
		d = &v
	}
	var poly [][2]float64
	if polygon.Valid {
		if err := json.Unmarshal([]byte(polygon.String), &poly); err != nil {
			return nil, nil, fmt.Errorf("decode zone polygon: %w", err)
		}
	}
	return d, poly, nil
}

func pinFromColumns(x, y sql.NullFloat64) *binPin {
	if !x.Valid || !y.Valid {
		return nil
	}
	return &binPin{X: x.Float64, Y: y.Float64}
}

const deckColumns = `id, name, sort_index, plan_document_id, created_at, updated_at`

func scanDeck(row rowScanner) (inventoryDeck, error) {
	var d inventoryDeck
	var plan sql.NullString
	var created, updated int64
	if err := row.Scan(&d.ID, &d.Name, &d.SortIndex, &plan, &created, &updated); err != nil {
		return inventoryDeck{}, err
	}
	if plan.Valid {
		v := plan.String
		d.PlanDocumentID = &v
	}
	d.CreatedAt = time.Unix(created, 0).UTC()
	d.UpdatedAt = time.Unix(updated, 0).UTC()
	return d, nil
}

func deckByID(q sqlQueryer, id string) (inventoryDeck, error) {
	d, err := scanDeck(q.QueryRow(`SELECT `+deckColumns+` FROM inventory_decks WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return inventoryDeck{}, errDeckNotFound
	}
	if err != nil {
		return inventoryDeck{}, fmt.Errorf("deck by id: %w", err)
	}
	return d, nil
}

// CreateDeck adds a deck at the next sort_index.
func (s *documentStore) CreateDeck(name string) (d inventoryDeck, err error) {
	err = s.RunTx(func(tx *sql.Tx, now time.Time) error {
		d, err = createDeckTx(tx, now, name)
		return err
	})
	return d, err
}

func createDeckTx(tx *sql.Tx, now time.Time, name string) (inventoryDeck, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return inventoryDeck{}, errDeckNameInvalid
	}
	taken, err := rowExists(tx, `SELECT 1 FROM inventory_decks WHERE lower(name) = lower(?)`, trimmed)
	if err != nil {
		return inventoryDeck{}, fmt.Errorf("create deck: check name: %w", err)
	}
	if taken {
		return inventoryDeck{}, errDeckNameTaken
	}
	var next int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(sort_index), -1) + 1 FROM inventory_decks`).Scan(&next); err != nil {
		return inventoryDeck{}, fmt.Errorf("create deck: next sort index: %w", err)
	}
	id := uuid.NewString()
	if _, err := tx.Exec(
		`INSERT INTO inventory_decks (id, name, sort_index, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		id, trimmed, next, now.Unix(), now.Unix(),
	); err != nil {
		return inventoryDeck{}, fmt.Errorf("create deck: %w", err)
	}
	return deckByID(tx, id)
}

// UpdateDeck renames a deck.
func (s *documentStore) UpdateDeck(id, name string) (d inventoryDeck, err error) {
	err = s.RunTx(func(tx *sql.Tx, now time.Time) error {
		d, err = updateDeckTx(tx, now, id, name)
		return err
	})
	return d, err
}

func updateDeckTx(tx *sql.Tx, now time.Time, id, name string) (inventoryDeck, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return inventoryDeck{}, errDeckNameInvalid
	}
	if _, err := deckByID(tx, id); err != nil {
		return inventoryDeck{}, err
	}
	taken, err := rowExists(tx, `SELECT 1 FROM inventory_decks WHERE lower(name) = lower(?) AND id != ?`, trimmed, id)
	if err != nil {
		return inventoryDeck{}, fmt.Errorf("update deck: check name: %w", err)
	}
	if taken {
		return inventoryDeck{}, errDeckNameTaken
	}
	if _, err := tx.Exec(`UPDATE inventory_decks SET name = ?, updated_at = ? WHERE id = ?`, trimmed, now.Unix(), id); err != nil {
		return inventoryDeck{}, fmt.Errorf("update deck: %w", err)
	}
	return deckByID(tx, id)
}

// SetDeckPlan points a deck at its plan image document. The previous plan
// document, if any, stays in Documents.
func (s *documentStore) SetDeckPlan(deckID, documentID string) (d inventoryDeck, err error) {
	err = s.RunTx(func(tx *sql.Tx, now time.Time) error {
		d, err = setDeckPlanTx(tx, now, deckID, documentID)
		return err
	})
	return d, err
}

// setDeckPlanTx points a deck at a plan document. The document must exist and
// be an image a plan can be drawn on (deckPlanRefusal); the upload handler
// applies the same rule to the file it has just received, so a plan chosen
// from Documents and a plan uploaded are held to one standard.
func setDeckPlanTx(tx *sql.Tx, now time.Time, deckID, documentID string) (inventoryDeck, error) {
	if _, err := deckByID(tx, deckID); err != nil {
		return inventoryDeck{}, err
	}
	var mimeType string
	err := tx.QueryRow(`SELECT mime FROM documents WHERE id = ?`, documentID).Scan(&mimeType)
	if errors.Is(err, sql.ErrNoRows) {
		return inventoryDeck{}, errDocumentNotFound
	}
	if err != nil {
		return inventoryDeck{}, fmt.Errorf("set deck plan: check document: %w", err)
	}
	if refusal := deckPlanRefusal(mimeType); refusal != "" {
		return inventoryDeck{}, &inventoryValidationError{Field: "plan_document_id", Message: refusal}
	}
	if _, err := tx.Exec(`UPDATE inventory_decks SET plan_document_id = ?, updated_at = ? WHERE id = ?`, documentID, now.Unix(), deckID); err != nil {
		return inventoryDeck{}, fmt.Errorf("set deck plan: %w", err)
	}
	return deckByID(tx, deckID)
}

// deckPlanRefusal is why a file of this MIME type cannot be a deck plan, in
// the operator's words, or "" when it can. Only JPEG and PNG pictures are
// accepted: a plan is drawn over as an image, and a page of a PDF is not one
// yet.
func deckPlanRefusal(mimeType string) string {
	switch mimeType {
	case "image/jpeg", "image/png":
		return ""
	case "image/heic":
		return documentHEICRejectionMessage
	case "application/pdf":
		return "a page of a PDF cannot be a deck plan yet; upload a picture of that page (JPEG or PNG) instead"
	default:
		return "a deck plan must be a JPEG or PNG image"
	}
}

// GetDeck returns one deck.
func (s *documentStore) GetDeck(id string) (inventoryDeck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return deckByID(s.db, id)
}

// ListDecks returns every deck by sort_index then name.
func (s *documentStore) ListDecks() ([]inventoryDeck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT ` + deckColumns + ` FROM inventory_decks ORDER BY sort_index, lower(name)`)
	if err != nil {
		return nil, fmt.Errorf("list decks: %w", err)
	}
	defer rows.Close()
	out := []inventoryDeck{}
	for rows.Next() {
		d, err := scanDeck(rows)
		if err != nil {
			return nil, fmt.Errorf("list decks: scan: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list decks: %w", err)
	}
	return out, nil
}

// DeleteDeck takes the deck's zones off the plan (outline and their bins'
// pins cleared) and deletes it, returning how many zones lost their outline.
// The plan document stays in Documents.
func (s *documentStore) DeleteDeck(id string) (cleared int, err error) {
	err = s.RunTx(func(tx *sql.Tx, now time.Time) error {
		cleared, err = deleteDeckTx(tx, now, id)
		return err
	})
	return cleared, err
}

func deleteDeckTx(tx *sql.Tx, now time.Time, id string) (int, error) {
	if _, err := deckByID(tx, id); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE inventory_bins SET pin_x = NULL, pin_y = NULL WHERE zone_id IN (SELECT id FROM inventory_zones WHERE deck_id = ?)`, id); err != nil {
		return 0, fmt.Errorf("delete deck: clear pins: %w", err)
	}
	res, err := tx.Exec(`UPDATE inventory_zones SET deck_id = NULL, polygon = NULL, updated_at = ? WHERE deck_id = ?`, now.Unix(), id)
	if err != nil {
		return 0, fmt.Errorf("delete deck: clear zones: %w", err)
	}
	cleared, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete deck: count zones: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM inventory_decks WHERE id = ?`, id); err != nil {
		return 0, fmt.Errorf("delete deck: %w", err)
	}
	return int(cleared), nil
}

// SaveDeckLayout replaces a deck's whole layout in one transaction (ADR 0156
// §5). Listed zones are placed on this deck with their outlines; zones that
// were on this deck and are not listed come off the plan; every bin in a
// listed or de-listed zone loses its pin, then the listed pins are set. Any
// validation failure changes nothing.
func (s *documentStore) SaveDeckLayout(deckID string, zones []deckLayoutZone, bins []deckLayoutBin) error {
	return s.RunTx(func(tx *sql.Tx, now time.Time) error { return saveDeckLayoutTx(tx, now, deckID, zones, bins) })
}

func saveDeckLayoutTx(tx *sql.Tx, now time.Time, deckID string, zones []deckLayoutZone, bins []deckLayoutBin) error {
	if _, err := deckByID(tx, deckID); err != nil {
		return err
	}

	listed := make(map[string]bool, len(zones))
	for _, z := range zones {
		if listed[z.ID] {
			return errLayoutInvalid
		}
		listed[z.ID] = true
		if err := validatePolygon(z.Polygon); err != nil {
			return err
		}
		ok, err := rowExists(tx, `SELECT 1 FROM inventory_zones WHERE id = ?`, z.ID)
		if err != nil {
			return fmt.Errorf("save deck layout: check zone: %w", err)
		}
		if !ok {
			return errZoneNotFound
		}
	}
	pinned := make(map[string]bool, len(bins))
	for _, b := range bins {
		if pinned[b.ID] {
			return errLayoutInvalid
		}
		pinned[b.ID] = true
		if !unitFraction(b.X) || !unitFraction(b.Y) {
			return errPinInvalid
		}
		var zoneID string
		err := tx.QueryRow(`SELECT zone_id FROM inventory_bins WHERE id = ?`, b.ID).Scan(&zoneID)
		if errors.Is(err, sql.ErrNoRows) {
			return errBinNotFound
		}
		if err != nil {
			return fmt.Errorf("save deck layout: check bin: %w", err)
		}
		if !listed[zoneID] {
			return errLayoutBinZoneNotListed
		}
	}

	nowUnix := now.Unix()

	// Zones leaving this deck: off the plan, pins cleared.
	rows, err := tx.Query(`SELECT id FROM inventory_zones WHERE deck_id = ?`, deckID)
	if err != nil {
		return fmt.Errorf("save deck layout: current zones: %w", err)
	}
	var leaving []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("save deck layout: scan: %w", err)
		}
		if !listed[id] {
			leaving = append(leaving, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("save deck layout: current zones: %w", err)
	}
	for _, id := range leaving {
		if _, err := tx.Exec(`UPDATE inventory_bins SET pin_x = NULL, pin_y = NULL WHERE zone_id = ?`, id); err != nil {
			return fmt.Errorf("save deck layout: clear pins: %w", err)
		}
		if _, err := tx.Exec(`UPDATE inventory_zones SET deck_id = NULL, polygon = NULL, updated_at = ? WHERE id = ?`, nowUnix, id); err != nil {
			return fmt.Errorf("save deck layout: take zone off plan: %w", err)
		}
	}

	for _, z := range zones {
		poly, err := json.Marshal(z.Polygon)
		if err != nil {
			return fmt.Errorf("save deck layout: encode polygon: %w", err)
		}
		if _, err := tx.Exec(`UPDATE inventory_bins SET pin_x = NULL, pin_y = NULL WHERE zone_id = ?`, z.ID); err != nil {
			return fmt.Errorf("save deck layout: clear pins: %w", err)
		}
		if _, err := tx.Exec(`UPDATE inventory_zones SET deck_id = ?, polygon = ?, updated_at = ? WHERE id = ?`, deckID, string(poly), nowUnix, z.ID); err != nil {
			return fmt.Errorf("save deck layout: place zone: %w", err)
		}
	}
	for _, b := range bins {
		if _, err := tx.Exec(`UPDATE inventory_bins SET pin_x = ?, pin_y = ? WHERE id = ?`, b.X, b.Y, b.ID); err != nil {
			return fmt.Errorf("save deck layout: pin bin: %w", err)
		}
	}
	return nil
}
