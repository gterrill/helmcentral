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
// (assistantStore.ApplyProposal, assistant_proposals.go).

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
