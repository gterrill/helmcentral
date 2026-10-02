package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// list_records, get_record and describe_record_type (ADR 0158).

func runRecordTool(t *testing.T, deps assistantToolDeps, name, args string) (string, error) {
	t.Helper()
	return deps.execute(context.Background(), name, json.RawMessage(args))
}

func TestDescribeRecordType_ListsTypesAndDescribesFields(t *testing.T) {
	deps, _ := proposeDeps(t)
	raw, err := runRecordTool(t, deps, "describe_record_type", `{}`)
	if err != nil {
		t.Fatalf("describe_record_type: %v", err)
	}
	for _, want := range []string{"equipment", "location", "bin", "deck", "maintenance_rule", "maintenance_log"} {
		if !strings.Contains(raw, `"type":"`+want+`"`) {
			t.Errorf("expected type %q listed in %s", want, raw)
		}
	}
	raw, err = runRecordTool(t, deps, "describe_record_type", `{"type":"deck"}`)
	if err != nil {
		t.Fatalf("describe deck: %v", err)
	}
	var d struct {
		Actions []string                    `json:"actions"`
		Fields  []assistantFieldDescription `json:"fields"`
	}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(d.Actions) != 3 || len(d.Fields) != 2 || d.Fields[1].Name != "plan_document_id" || !d.Fields[1].Writable || d.Fields[1].RefersTo != "document" {
		t.Fatalf("unexpected deck description: %s", raw)
	}
	if _, err := runRecordTool(t, deps, "describe_record_type", `{"type":"anchor"}`); err == nil || !strings.Contains(err.Error(), "the types are") {
		t.Fatalf("an unknown type names the real ones, got %v", err)
	}
}

func TestListAndGetRecord_ReturnRegisteredFieldsAndVersions(t *testing.T) {
	deps, store := proposeDeps(t)
	z, _ := store.CreateZone("Salon")
	store.CreateBin(z.ID, "S-1", "Spares")
	deck, _ := store.CreateDeck("Main")

	raw, err := runRecordTool(t, deps, "list_records", `{"type":"location"}`)
	if err != nil {
		t.Fatalf("list_records: %v", err)
	}
	var list assistantListRecordsResult
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if list.Total != 1 || list.Records[0].ID != z.ID || list.Records[0].Version == "" || list.Records[0].Fields["name"] != "Salon" {
		t.Fatalf("unexpected list %s", raw)
	}
	if codes, _ := list.Records[0].Fields["bin_codes"].([]any); len(codes) != 1 || codes[0] != "S-1" {
		t.Fatalf("expected the location's bin codes, got %v", list.Records[0].Fields)
	}

	raw, err = runRecordTool(t, deps, "get_record", fmt.Sprintf(`{"type":"deck","id":%q}`, deck.ID))
	if err != nil || !strings.Contains(raw, `"name":"Main"`) || !strings.Contains(raw, `"version":`) {
		t.Fatalf("get_record: %v %s", err, raw)
	}
	if _, err := runRecordTool(t, deps, "get_record", `{"type":"deck","id":"nope"}`); err == nil || !strings.Contains(err.Error(), "no deck with id") {
		t.Fatalf("a missing record is named, got %v", err)
	}
}

func TestListRecords_FiltersLimitAndRefusals(t *testing.T) {
	deps, store := proposeDeps(t)
	z, _ := store.CreateZone("Salon")
	store.CreateBin(z.ID, "S-1", "")
	store.CreateBin(z.ID, "S-2", "")
	z2, _ := store.CreateZone("Galley")
	store.CreateBin(z2.ID, "G-1", "")

	raw, _ := runRecordTool(t, deps, "list_records", fmt.Sprintf(`{"type":"bin","filter":{"zone_id":%q}}`, z.ID))
	var list assistantListRecordsResult
	_ = json.Unmarshal([]byte(raw), &list)
	if list.Total != 2 {
		t.Fatalf("filter by location: %s", raw)
	}
	raw, _ = runRecordTool(t, deps, "list_records", `{"type":"bin","limit":1}`)
	_ = json.Unmarshal([]byte(raw), &list)
	if list.Total != 3 || len(list.Records) != 1 || !list.Truncated {
		t.Fatalf("limit should truncate and say so: %s", raw)
	}
	if _, err := runRecordTool(t, deps, "list_records", `{"type":"bin","filter":{"colour":"red"}}`); err == nil || !strings.Contains(err.Error(), "the filters are zone_id") {
		t.Fatalf("an unknown filter is named, got %v", err)
	}
	if _, err := runRecordTool(t, deps, "list_records", `{"type":"nope"}`); err == nil {
		t.Fatal("an unknown type is refused")
	}
}

func TestRegisteredTypes_NeverReachSecretsOrVesselControls(t *testing.T) {
	for _, name := range defaultRecordRegistry.names() {
		for _, banned := range []string{"secret", "session", "password", "api_key", "token", "autopilot", "generator", "czone", "anchor_watch", "import", "conversation", "tile_cache"} {
			if strings.Contains(name, banned) {
				t.Errorf("record type %q must not be registered (ADR 0158: %s)", name, banned)
			}
			for _, f := range defaultRecordRegistry.types[name].Fields {
				if strings.Contains(f.Name, banned) {
					t.Errorf("%s.%s must not be registered (ADR 0158: %s)", name, f.Name, banned)
				}
			}
		}
	}
}
