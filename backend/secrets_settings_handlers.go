package main

import (
	"fmt"
	"log"
	"net/http"
	"sort"

	"github.com/labstack/echo/v4"
)

// getSecretsSettingsHandler is GET /api/settings/secrets. It returns, for
// every knownSecretKeys name, a boolean reporting whether a value is
// currently stored - never plaintext or ciphertext. This exact shape is a
// fixed HTTP contract the frontend team builds against; do not change it
// without updating them.
func getSecretsSettingsHandler(c echo.Context) error {
	state, err := globalSecretsStore.All()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read secrets state"})
	}
	return c.JSON(http.StatusOK, state)
}

// updateSecretsSettingsHandler is POST /api/settings/secrets. The request
// body is a partial JSON object of knownSecretKeys names to string values;
// an empty string clears that key. Every key in the request is validated
// against knownSecretKeys BEFORE any write happens, so a request containing
// one bad key name never partially applies. On success, responds with the
// same shape as the GET handler, reflecting the new state.
func updateSecretsSettingsHandler(c echo.Context) error {
	var req map[string]string
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}

	for key := range req {
		if !isKnownSecretKey(key) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("unknown secret key: %s", key)})
		}
	}

	for key, value := range req {
		if err := globalSecretsStore.Set(key, value); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save secret"})
		}
	}

	state, err := globalSecretsStore.All()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read secrets state"})
	}
	return c.JSON(http.StatusOK, state)
}

// importEnvSecretsHandler is POST /api/settings/secrets/import-env, the
// one-time migration path from the old backend/.env-based configuration
// into the encrypted store. Always responds 200, even when nothing was
// imported (an empty process environment is not an error case).
func importEnvSecretsHandler(c echo.Context) error {
	imported, err := globalSecretsStore.ImportFromEnv()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to import secrets from environment"})
	}
	sort.Strings(imported)
	log.Printf("secrets: imported %d key(s) from process env: %v", len(imported), imported)

	if imported == nil {
		imported = []string{}
	}
	return c.JSON(http.StatusOK, map[string]any{"imported": imported})
}
