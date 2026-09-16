package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/xeipuuv/gojsonschema"
)

//go:embed schemas/equipment-profile-v1.schema.json
var equipmentProfileSchemaFiles embed.FS

var (
	equipmentProfileSchemaOnce sync.Once
	equipmentProfileSchema     *gojsonschema.Schema
	equipmentProfileSchemaErr  error
)

type profileValidationError struct {
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

func compileEquipmentProfileSchema() (*gojsonschema.Schema, error) {
	data, err := equipmentProfileSchemaFiles.ReadFile("schemas/equipment-profile-v1.schema.json")
	if err != nil {
		return nil, err
	}
	return gojsonschema.NewSchema(gojsonschema.NewBytesLoader(data))
}

func equipmentSchema() (*gojsonschema.Schema, error) {
	equipmentProfileSchemaOnce.Do(func() {
		equipmentProfileSchema, equipmentProfileSchemaErr = compileEquipmentProfileSchema()
	})
	return equipmentProfileSchema, equipmentProfileSchemaErr
}

func canonicalizeProfileDocument(raw []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if _, ok := doc["kind"]; !ok {
		doc["kind"] = "engine"
	}
	if _, ok := doc["schema_version"]; !ok {
		doc["schema_version"] = 1
	}
	return json.Marshal(doc)
}

// validateProfileDocument checks a profile document against the schema. The
// caller must have already run it through canonicalizeProfileDocument — this
// validates the document it is given rather than re-deriving one, so every
// caller canonicalizes exactly once at the edge.
func validateProfileDocument(canonical []byte) ([]profileValidationError, error) {
	schema, err := equipmentSchema()
	if err != nil {
		return nil, fmt.Errorf("equipment profile schema unavailable: %w", err)
	}

	result, err := schema.Validate(gojsonschema.NewBytesLoader(canonical))
	if err != nil {
		return nil, err
	}
	if result.Valid() {
		return nil, nil
	}

	problems := make([]profileValidationError, 0, len(result.Errors()))
	for _, issue := range result.Errors() {
		problems = append(problems, profileValidationError{
			Path:    issue.Field(),
			Message: issue.Description(),
		})
	}
	return problems, nil
}
