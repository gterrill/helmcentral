package main

import (
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
)

// Deck plan routes (ADR 0156).

type deckRequest struct {
	Name string `json:"name"`
}

// listDecksHandler is GET /api/inventory/decks.
func listDecksHandler(c echo.Context) error {
	decks, err := globalDocumentStore.ListDecks()
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"decks": decks})
}

// createDeckHandler is POST /api/inventory/decks: {name}.
func createDeckHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req deckRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	deck, err := globalDocumentStore.CreateDeck(req.Name)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"deck": deck})
}

// updateDeckHandler is PUT /api/inventory/decks/:id: {name}, a rename.
func updateDeckHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req deckRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	deck, err := globalDocumentStore.UpdateDeck(c.Param("id"), req.Name)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"deck": deck})
}

// deleteDeckHandler is DELETE /api/inventory/decks/:id. It takes the deck's
// zones off the plan rather than refusing, and says how many.
func deleteDeckHandler(c echo.Context) error {
	cleared, err := globalDocumentStore.DeleteDeck(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"zones_cleared": cleared})
}

type deckLayoutRequest struct {
	Zones []deckLayoutZone `json:"zones"`
	Bins  []deckLayoutBin  `json:"bins"`
}

// saveDeckLayoutHandler is PUT /api/inventory/decks/:id/layout. It answers
// with the refreshed zone list so the editor can reload in one round trip.
func saveDeckLayoutHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req deckLayoutRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if err := globalDocumentStore.SaveDeckLayout(c.Param("id"), req.Zones, req.Bins); err != nil {
		return writeDocumentError(c, err)
	}
	zones, err := globalDocumentStore.ListZones()
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"zones": zones})
}

// uploadDeckPlanHandler is POST /api/inventory/decks/:id/plan (multipart,
// one "file" part): JPEG or PNG only, stored as an ordinary document through
// the same intake as equipment photos, then set as the deck's plan. A
// replaced plan stays in Documents.
func uploadDeckPlanHandler(c echo.Context) error {
	id := c.Param("id")
	if _, err := globalDocumentStore.GetDeck(id); err != nil {
		return writeDocumentError(c, err)
	}

	dir := documentsDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to prepare document storage"})
	}
	up, err := receiveUploadedFile(c, dir, nil)
	if err != nil {
		return err
	}

	mimeType := detectDocumentMIME(up.head, up.filename)
	if refusal := deckPlanRefusal(mimeType); refusal != "" {
		os.Remove(up.tmpPath)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": refusal})
	}

	enrich, _, err := documentEnrichFlag("inventory: upload deck plan")
	if err != nil {
		os.Remove(up.tmpPath)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	doc := document{Filename: up.filename, MIME: mimeType, SizeBytes: up.size, Enrich: enrich}
	respond := func(documentID string, status int) error {
		deck, err := globalDocumentStore.SetDeckPlan(id, documentID)
		if err != nil {
			return writeDocumentError(c, err)
		}
		return c.JSON(status, map[string]any{"deck": deck})
	}
	return storeUploadedFile(dir, up, doc,
		func(existing document) error { return respond(existing.ID, http.StatusOK) },
		func(inserted document) error {
			wakeDocumentIndexer()
			return respond(inserted.ID, http.StatusOK)
		},
		func(err error) error {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		},
	)
}
