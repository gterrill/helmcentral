package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// propose_changes (ADR 0158, replacing ADR 0146's propose_maintenance_changes).
// Mate never writes a record. This tool validates a changeset against the
// registered record types, dry-runs it in a transaction that is always rolled
// back, writes one operator-words line per operation and returns the whole
// thing as a proposal. It writes nothing: the runner saves the proposal with
// the assistant message that carries it, and the operator's Apply tap runs it
// (applyAssistantProposal, assistant_proposals.go).

const assistantProposalToolTag = "propose_changes"

type assistantProposeArgs struct {
	Operations []changeOp `json:"operations"`
}

// assistantProposeResult is the tool's answer: the proposal (which the runner
// also saves) and what to tell the operator.
type assistantProposeResult struct {
	Proposal assistantProposal `json:"proposal"`
	NextStep string            `json:"next_step"`
}

func (d assistantToolDeps) executeProposeChanges(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store, err := d.recordStore(assistantProposalToolTag)
	if err != nil {
		return "", err
	}
	if _, err := d.requireToday(assistantProposalToolTag); err != nil {
		return "", err
	}

	// Strict decode: an unknown field is a misspelt change Mate believes it
	// proposed, so it fails the call and names the field.
	var args assistantProposeArgs
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&args); err != nil {
		return "", fmt.Errorf("parse %s arguments: %w", assistantProposalToolTag, err)
	}
	ops, err := defaultRecordRegistry.prepare(store, d.today, args.Operations)
	if err != nil {
		return "", fmt.Errorf("%s: %w", assistantProposalToolTag, err)
	}

	result := assistantProposeResult{
		Proposal: assistantProposal{ID: uuid.NewString(), Ops: ops, Status: assistantProposalPending},
		NextStep: "This is a proposal, not a change. Nothing has been written. The operator sees it as a card under your " +
			"reply with Apply and Dismiss buttons. Tell them to tap Apply to make the change, and never say it is done.",
	}
	body, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("%s: marshal proposal: %w", assistantProposalToolTag, err)
	}
	return string(body), nil
}

// recordStore is the document store the record tools read and the propose
// dry run writes into, or an error saying the data is not available.
func (d assistantToolDeps) recordStore(toolName string) (*documentStore, error) {
	if d.documents == nil {
		return nil, fmt.Errorf("%s: the records are not available", toolName)
	}
	store := d.documents()
	if store == nil {
		return nil, fmt.Errorf("%s: the records are not available", toolName)
	}
	return store, nil
}

var mateToolProposeChanges = mateTool{
	name: assistantProposalToolTag,
	function: openRouterFunctionDef{
		Description: "Propose changes to Helmcentral's records. This changes NOTHING: the operator sees your " +
			"proposal as a card under your reply and taps Apply (or Dismiss). Use it only for a change the " +
			"operator asked for or agreed to, and read the records first (find_equipment, list_maintenance) so " +
			"you have real ids. Give every change in one call as a list of operations; the card applies them all " +
			"together or not at all. An operation is {type, action, id, fields}: action is create, update or " +
			"delete; update and delete name the record by id; fields holds only the fields that change, and null " +
			"removes a value where the type allows it. Call describe_record_type first to learn a type's fields, and " +
			"list_records or get_record for real ids and versions (pass the version you read as base_version). " +
			"A later operation can refer to a record an earlier create makes by its position, as $1, $2: " +
			"\"create the deck and move these locations onto it\" is one proposal. A call that fails names the " +
			"operation and the field to correct. Anything else, such as deleting rules or log entries, photos, " +
			"parts, meter replacements and procedure notes, cannot be proposed.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"operations": {
							"type": "array",
							"description": "The changes, in order (at most 50).",
							"items": {
								"type": "object",
								"properties": {
									"type": {"type": "string", "description": "The record type."},
									"action": {"type": "string", "description": "create, update or delete."},
									"id": {"type": "string", "description": "update and delete: the record's id."},
									"fields": {"type": "object", "description": "The fields to set, by name."},
									"base_version": {"type": "string", "description": "Optional: the version of the record you read, so the proposal fails if it has changed since."}
								},
								"required": ["type", "action"]
							}
						}
					},
					"required": ["operations"]
				}`),
	},
	execute: plainTool(assistantToolDeps.executeProposeChanges),
	describe: func(args json.RawMessage) string {
		return "Preparing the changes…"
	},
	collect: collectProposalFromToolResult,
}

// collectProposalFromToolResult is propose_changes' collect: a proposal in the
// result becomes a card under the reply.
func collectProposalFromToolResult(result string, reply *assistantReply) error {
	proposal, err := assistantProposalFromToolResult(result)
	if err != nil {
		return err
	}
	if proposal != nil {
		reply.Proposals = append(reply.Proposals, *proposal)
	}
	return nil
}
