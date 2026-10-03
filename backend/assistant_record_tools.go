package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// list_records, get_record and describe_record_type (ADR 0158): Mate's read
// side over every registered record type. The tuned read tools (find_equipment,
// list_maintenance and the rest) stay where they answer better; these reach
// the types that have no tool of their own. Reads return every registered
// field, which is why a type registers only fields safe to show in a chat
// transcript. describe_record_type exists so every schema does not ride along
// on every turn: Mate asks for a type's fields when it needs them.

const (
	assistantRecordsDefaultLimit = 25
	assistantRecordsMaxLimit     = 100
)

type assistantListRecordsArgs struct {
	Type   string            `json:"type"`
	Filter map[string]string `json:"filter"`
	Limit  int               `json:"limit"`
}

type assistantGetRecordArgs struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type assistantDescribeRecordTypeArgs struct {
	Type string `json:"type"`
}

type assistantListRecordsResult struct {
	Type    string           `json:"type"`
	Total   int              `json:"total"`
	Records []recordSnapshot `json:"records"`
	// Truncated says more records matched than were returned.
	Truncated bool `json:"truncated,omitempty"`
}

func strictDecode(toolName string, raw json.RawMessage, into any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("parse %s arguments: %w", toolName, err)
	}
	return nil
}

func recordTypeFor(toolName, name string) (*recordType, error) {
	t, ok := defaultRecordRegistry.lookup(name)
	if !ok {
		return nil, fmt.Errorf("%s: unknown record type %q; the types are %s", toolName, name, quoteList(defaultRecordRegistry.names()))
	}
	return t, nil
}

func (d assistantToolDeps) executeListRecords(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var args assistantListRecordsArgs
	if err := strictDecode("list_records", raw, &args); err != nil {
		return "", err
	}
	t, err := recordTypeFor("list_records", args.Type)
	if err != nil {
		return "", err
	}
	for k := range args.Filter {
		if _, ok := t.Filters[k]; !ok {
			names := make([]string, 0, len(t.Filters))
			for n := range t.Filters {
				names = append(names, n)
			}
			sort.Strings(names)
			return "", fmt.Errorf("list_records: %s has no filter %q; the filters are %s", t.Name, k, quoteList(names))
		}
	}
	store, err := d.recordStore("list_records")
	if err != nil {
		return "", err
	}
	all, err := t.List(store, args.Filter)
	if err != nil {
		return "", fmt.Errorf("list_records: %w", err)
	}
	limit := args.Limit
	if limit <= 0 {
		limit = assistantRecordsDefaultLimit
	}
	if limit > assistantRecordsMaxLimit {
		limit = assistantRecordsMaxLimit
	}
	result := assistantListRecordsResult{Type: t.Name, Total: len(all), Records: all}
	if len(all) > limit {
		result.Records, result.Truncated = all[:limit], true
	}
	if result.Records == nil {
		result.Records = []recordSnapshot{}
	}
	return capToolResultJSON(&result, func() bool {
		if len(result.Records) == 0 {
			return false
		}
		result.Records = result.Records[:len(result.Records)-1]
		result.Truncated = true
		return true
	})
}

func (d assistantToolDeps) executeGetRecord(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var args assistantGetRecordArgs
	if err := strictDecode("get_record", raw, &args); err != nil {
		return "", err
	}
	t, err := recordTypeFor("get_record", args.Type)
	if err != nil {
		return "", err
	}
	store, err := d.recordStore("get_record")
	if err != nil {
		return "", err
	}
	var snap recordSnapshot
	err = store.Read(func(q sqlQueryer) error {
		var gerr error
		snap, gerr = t.Get(q, strings.TrimSpace(args.ID))
		return gerr
	})
	if err != nil {
		if errors.Is(err, errRecordNotFound) {
			return "", fmt.Errorf("get_record: no %s with id %q (find one with list_records)", t.Label, args.ID)
		}
		return "", fmt.Errorf("get_record: %w", err)
	}
	return capToolResultJSON(map[string]any{"type": t.Name, "record": snap}, nil)
}

type assistantFieldDescription struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Writable    bool     `json:"writable"`
	Nullable    bool     `json:"nullable,omitempty"`
	RefersTo    string   `json:"refers_to,omitempty"`
	OneOf       []string `json:"one_of,omitempty"`
	Description string   `json:"description,omitempty"`
}

func (d assistantToolDeps) executeDescribeRecordType(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var args assistantDescribeRecordTypeArgs
	if err := strictDecode("describe_record_type", raw, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Type) == "" {
		var types []map[string]any
		for _, name := range defaultRecordRegistry.names() {
			t := defaultRecordRegistry.types[name]
			types = append(types, map[string]any{"type": t.Name, "actions": t.Actions, "summary": t.Summary})
		}
		return capToolResultJSON(map[string]any{"types": types}, nil)
	}
	t, err := recordTypeFor("describe_record_type", args.Type)
	if err != nil {
		return "", err
	}
	fields := make([]assistantFieldDescription, len(t.Fields))
	for i, f := range t.Fields {
		fields[i] = assistantFieldDescription{Name: f.Name, Kind: string(f.Kind), Writable: f.Writable, Nullable: f.Nullable, RefersTo: f.Ref, OneOf: f.Enum, Description: f.Description}
	}
	return capToolResultJSON(map[string]any{
		"type": t.Name, "summary": t.Summary, "actions": t.Actions, "filters": t.Filters, "fields": fields,
		"note": "Give fields by name in an operation's fields object; null clears a nullable field. A later operation can refer to a record an earlier create makes as $1, $2 (the operation's position).",
	}, nil)
}
