package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Changesets (ADR 0158). A proposal is an ordered list of operations on
// records, applied together in one transaction or not at all. This file is
// the type-agnostic half: it checks an operation's shape against the type's
// registered fields, dry-runs the list in a transaction that always rolls
// back (so Mate's propose tool still writes nothing), and applies it for the
// operator's Apply tap through the very same run. Freshness is checked by
// version stamp for every type the same way.

const changesetMaxOps = 50

// changeOp is one operation. Type, Action, ID, Fields and BaseVersion are
// what Mate asked for; the rest is filled by the server when it prepares the
// changeset and is never taken from the model.
type changeOp struct {
	Type   string         `json:"type"`
	Action string         `json:"action"`
	ID     string         `json:"id,omitempty"`
	Fields map[string]any `json:"fields,omitempty"`
	// BaseVersion is the version of the target Mate read. Preparing fills it
	// when Mate left it out; Apply refuses (the changeset goes stale) if the
	// record has a different version by then.
	BaseVersion string `json:"base_version,omitempty"`

	// Label names the target for the operator. Description is the operator's
	// line for the operation, written once on the server: the card and Mate's
	// history both use it. Before and After hold the fields the operation
	// names, as they were and as the dry run left them (a delete's Before is
	// the whole record, and it has no After).
	Label       string         `json:"label,omitempty"`
	Description string         `json:"description"`
	Before      map[string]any `json:"before,omitempty"`
	After       map[string]any `json:"after,omitempty"`
	// Watches are records the operation depends on without targeting them.
	Watches []recordWatch `json:"watches,omitempty"`
}

// changeOpResult names what one operation wrote, for the card's links.
type changeOpResult struct {
	Type   string `json:"type"`
	Action string `json:"action"`
	ID     string `json:"id"`
	Label  string `json:"label,omitempty"`
}

// changeResult is what Apply stores: one entry per operation, in order.
type changeResult struct {
	Ops []changeOpResult `json:"ops"`
}

// changeOpError is a failure of one operation while a changeset ran.
type changeOpError struct {
	Index  int
	Type   string
	Action string
	Err    error
}

func (e *changeOpError) Error() string {
	return fmt.Sprintf("ops[%d] (%s %s): %s", e.Index, e.Type, e.Action, e.Err)
}
func (e *changeOpError) Unwrap() error { return e.Err }

// applyMessage is the failure as the operator reads it on the card.
func (e *changeOpError) applyMessage(total int) string {
	return fmt.Sprintf("change %d of %d (%s %s): %s", e.Index+1, total, e.Type, e.Action, e.Err)
}

// fieldedError is a problem with one field, reported as "field: message" so
// Mate can correct that field, keeping the original for classification.
type fieldedError struct {
	Field, Message string
	cause          error
}

func (e *fieldedError) Error() string { return e.Field + ": " + e.Message }
func (e *fieldedError) Unwrap() error { return e.cause }

// translateChangeError renames a command's field names to the ones Mate used
// and shapes a validation error as "field: message".
func translateChangeError(t *recordType, err error) error {
	var verr *inventoryValidationError
	if !errors.As(err, &verr) {
		return err
	}
	field, msg := verr.Field, verr.Message
	for old, renamed := range t.Renames {
		if field == old {
			field = renamed
		}
		if strings.HasPrefix(msg, old+" ") {
			msg = renamed + msg[len(old):]
		}
	}
	return &fieldedError{Field: field, Message: msg, cause: err}
}

var refPattern = regexp.MustCompile(`^\$(\d+)$`)

func isRef(s string) bool { return strings.HasPrefix(s, "$") }

// refIndex is the zero-based operation a reference names.
func refIndex(s string) (int, bool) {
	m := refPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 1 {
		return 0, false
	}
	return n - 1, true
}

// ── shape ───────────────────────────────────────────────────────────────

func (r *recordRegistry) checkShape(i int, ops []changeOp) error {
	op := ops[i]
	t, ok := r.types[op.Type]
	if !ok {
		return fmt.Errorf("ops[%d]: type: unknown record type %q; the types are %s", i, op.Type, quoteList(r.names()))
	}
	if op.Action != changeCreate && op.Action != changeUpdate && op.Action != changeDelete {
		return fmt.Errorf("ops[%d] (%s): action: must be create, update or delete, got %q", i, op.Type, op.Action)
	}
	head := fmt.Sprintf("ops[%d] (%s %s)", i, op.Type, op.Action)
	fail := func(field, format string, a ...any) error {
		return fmt.Errorf("%s: %s: %s", head, field, fmt.Sprintf(format, a...))
	}
	if !t.allows(op.Action) {
		return fail("action", "%s records cannot be %sd by a changeset", t.Name, op.Action)
	}

	switch op.Action {
	case changeCreate:
		if op.ID != "" {
			return fail("id", "a create has no id yet; the new record gets one when applied, and a later operation can name it as $%d", i+1)
		}
		if op.BaseVersion != "" {
			return fail("base_version", "a create has no version to check")
		}
	default:
		if strings.TrimSpace(op.ID) == "" {
			return fail("id", "is required (an id from list_records or get_record)")
		}
		if isRef(op.ID) {
			if err := checkRef(ops, i, op.ID, t.Name); err != nil {
				return fail("id", "%s", err)
			}
			if op.BaseVersion != "" {
				return fail("base_version", "a record created in this changeset has no version")
			}
		}
	}
	if op.Action == changeDelete && len(op.Fields) > 0 {
		return fail("fields", "a delete takes no fields")
	}
	if op.Action == changeUpdate && len(op.Fields) == 0 {
		return fail("fields", "give at least one field to change")
	}

	names := make([]string, 0, len(op.Fields))
	for name := range op.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	norm := make(map[string]any, len(op.Fields))
	for _, name := range names {
		f, ok := t.field(name)
		if !ok {
			return fail(name, "%s has no such field; the fields are %s", t.Name, quoteList(t.writableNames()))
		}
		if !f.Writable {
			return fail(name, "cannot be changed; it is read-only")
		}
		v, err := normalizeFieldValue(f, op.Fields[name], ops, i)
		if err != nil {
			return fail(name, "%s", err)
		}
		norm[name] = v
	}
	if op.Fields != nil {
		ops[i].Fields = norm
	}
	return nil
}

// checkRef validates a local reference at operation i: it must name an
// earlier create, of the expected type when one is given.
func checkRef(ops []changeOp, i int, ref, wantType string) error {
	n, ok := refIndex(ref)
	if !ok {
		return fmt.Errorf("%q is not a reference; a reference is $ and the number of an earlier operation, such as $1", ref)
	}
	if n >= i {
		return fmt.Errorf("%q must name an earlier operation", ref)
	}
	if ops[n].Action != changeCreate {
		return fmt.Errorf("%q must name an operation that creates a record", ref)
	}
	if wantType != "" && ops[n].Type != wantType {
		return fmt.Errorf("%q creates a %s, but this field takes a %s", ref, ops[n].Type, wantType)
	}
	return nil
}

func normalizeFieldValue(f recordField, v any, ops []changeOp, i int) (any, error) {
	if v == nil {
		if !f.Nullable {
			return nil, errors.New("cannot be cleared")
		}
		return nil, nil
	}
	switch f.Kind {
	case kindString, kindDate:
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("must be text")
		}
		return s, nil
	case kindID:
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("must be text, an id or a reference such as $1")
		}
		if isRef(s) {
			if err := checkRef(ops, i, s, f.Ref); err != nil {
				return nil, err
			}
		}
		return s, nil
	case kindNumber:
		n, ok := v.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, errors.New("must be a number")
		}
		return n, nil
	case kindInteger:
		n, ok := v.(float64)
		if !ok || n != math.Trunc(n) || math.IsInf(n, 0) {
			return nil, errors.New("must be a whole number")
		}
		return n, nil
	case kindBoolean:
		b, ok := v.(bool)
		if !ok {
			return nil, errors.New("must be true or false")
		}
		return b, nil
	case kindStringList:
		list, ok := v.([]any)
		if !ok {
			if ss, isStrings := v.([]string); isStrings {
				return ss, nil
			}
			return nil, errors.New("must be a list of text")
		}
		out := make([]string, 0, len(list))
		for _, e := range list {
			s, ok := e.(string)
			if !ok {
				return nil, errors.New("must be a list of text")
			}
			out = append(out, s)
		}
		return out, nil
	case kindPolygon:
		list, ok := v.([]any)
		if !ok {
			if poly, isPoly := v.([][2]float64); isPoly {
				return poly, nil
			}
			return nil, errors.New("must be a list of [x, y] points")
		}
		out := make([][2]float64, 0, len(list))
		for _, e := range list {
			pt, ok := e.([]any)
			if !ok || len(pt) != 2 {
				return nil, errors.New("must be a list of [x, y] points")
			}
			x, okx := pt[0].(float64)
			y, oky := pt[1].(float64)
			if !okx || !oky {
				return nil, errors.New("must be a list of [x, y] points")
			}
			out = append(out, [2]float64{x, y})
		}
		return out, nil
	}
	return nil, fmt.Errorf("has an unknown kind %q", f.Kind)
}

// ── running ─────────────────────────────────────────────────────────────

// opRun is what one operation did inside the transaction.
type opRun struct {
	id      string
	before  *recordSnapshot
	after   *recordSnapshot
	effects []string
	extra   map[string]any
}

// resolveFields substitutes the id each local reference stands for.
func resolveFields(t *recordType, fields map[string]any, created []string) fieldSet {
	out := make(fieldSet, len(fields))
	for k, v := range fields {
		if f, ok := t.field(k); ok && f.Kind == kindID {
			if s, isStr := v.(string); isStr && isRef(s) {
				n, _ := refIndex(s)
				v = created[n]
			}
		}
		out[k] = v
	}
	return out
}

// run executes every operation in order inside env's transaction. Each
// operation reads the records as the ones before it left them. The dry run
// and Apply both come through here, which is why a changeset that proposes
// cleanly applies the same way.
func (r *recordRegistry) run(env changeEnv, ops []changeOp) ([]opRun, error) {
	runs := make([]opRun, len(ops))
	created := make([]string, len(ops))
	for i, op := range ops {
		t := r.types[op.Type]
		fail := func(err error) error {
			return &changeOpError{Index: i, Type: op.Type, Action: op.Action, Err: translateChangeError(t, err)}
		}
		fields := resolveFields(t, op.Fields, created)
		id := op.ID
		if isRef(id) {
			n, _ := refIndex(id)
			id = created[n]
		}
		switch op.Action {
		case changeCreate:
			c, err := t.Create(env, fields)
			if err != nil {
				return nil, fail(err)
			}
			created[i] = c.ID
			after, err := t.Get(env.tx, c.ID)
			if err != nil {
				return nil, fail(fmt.Errorf("read back the new %s: %w", t.Label, err))
			}
			runs[i] = opRun{id: c.ID, after: &after, extra: c.Extra}
		case changeUpdate:
			before, err := t.Get(env.tx, id)
			if err != nil {
				return nil, fail(notFoundOr(t, id, err))
			}
			if err := t.Update(env, before, fields); err != nil {
				return nil, fail(err)
			}
			after, err := t.Get(env.tx, id)
			if err != nil {
				return nil, fail(fmt.Errorf("read back the %s: %w", t.Label, err))
			}
			runs[i] = opRun{id: id, before: &before, after: &after}
		case changeDelete:
			before, err := t.Get(env.tx, id)
			if err != nil {
				return nil, fail(notFoundOr(t, id, err))
			}
			var effects []string
			if t.Effects != nil {
				if effects, err = t.Effects(env.tx, before); err != nil {
					return nil, fail(err)
				}
			}
			if err := t.Delete(env, before); err != nil {
				return nil, fail(err)
			}
			runs[i] = opRun{id: id, before: &before, effects: effects}
		}
	}
	return runs, nil
}

// notFoundOr words errRecordNotFound with the record's type; any other error
// passes through.
func notFoundOr(t *recordType, id string, err error) error {
	if errors.Is(err, errRecordNotFound) {
		return &fieldedError{Field: "id", Message: fmt.Sprintf("no %s with id %q (find it with list_records)", t.Label, id), cause: err}
	}
	return err
}

// ── propose ─────────────────────────────────────────────────────────────

// errChangesetDryRunDone ends the dry run's transaction: RunTx commits only
// when its function returns nil, so returning this always rolls back. It
// never leaves prepare.
var errChangesetDryRunDone = errors.New("dry run finished")

// prepare validates a changeset and dry-runs it: shape against the registered
// fields, targets read and stamped with the version Mate must have seen, then
// every operation run, in order, in a transaction that is always rolled back.
// It returns the operations with Label, Description, Before, After, Watches
// and BaseVersion filled in, and writes nothing.
func (r *recordRegistry) prepare(store *documentStore, today time.Time, in []changeOp) ([]changeOp, error) {
	if len(in) == 0 {
		return nil, errors.New("ops is required and must list at least one change")
	}
	if len(in) > changesetMaxOps {
		return nil, fmt.Errorf("at most %d changes in one proposal, got %d", changesetMaxOps, len(in))
	}
	ops := make([]changeOp, len(in))
	for i, op := range in {
		// Output-only fields are never taken from the model.
		op.Label, op.Description, op.Before, op.After, op.Watches = "", "", nil, nil, nil
		ops[i] = op
	}
	for i := range ops {
		if err := r.checkShape(i, ops); err != nil {
			return nil, err
		}
	}

	err := store.RunTx(func(tx *sql.Tx, now time.Time) error {
		env := changeEnv{tx: tx, now: now, today: today}
		for i := range ops {
			if err := r.attach(env, i, &ops[i]); err != nil {
				return err
			}
		}
		runs, err := r.run(env, ops)
		if err != nil {
			return err
		}
		for i := range ops {
			if err := r.describe(i, &ops[i], runs[i]); err != nil {
				return err
			}
		}
		return errChangesetDryRunDone
	})
	if !errors.Is(err, errChangesetDryRunDone) {
		return nil, err
	}
	return ops, nil
}

// attach reads an operation's target (and anything it watches) before any
// operation runs, so the version recorded is the one Mate saw, and refuses a
// base_version Mate supplied that is no longer current.
func (r *recordRegistry) attach(env changeEnv, i int, op *changeOp) error {
	t := r.types[op.Type]
	head := fmt.Sprintf("ops[%d] (%s %s)", i, op.Type, op.Action)
	if op.Action != changeCreate && !isRef(op.ID) {
		before, err := t.Get(env.tx, op.ID)
		if errors.Is(err, errRecordNotFound) {
			return fmt.Errorf("%s: id: no %s with id %q (find it with list_records)", head, t.Label, op.ID)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", head, err)
		}
		op.Label = before.Label
		if op.BaseVersion != "" && op.BaseVersion != before.Version {
			return fmt.Errorf("%s: base_version: %s has changed since you read it (you had %s, it is now %s); read it again with get_record",
				head, before.Label, op.BaseVersion, before.Version)
		}
		op.BaseVersion = before.Version
	}
	if t.Watch != nil {
		watches, err := t.Watch(env.tx, resolveWatchFields(t, op.Fields))
		if err != nil {
			return fmt.Errorf("%s: %w", head, translateChangeError(t, err))
		}
		op.Watches = watches
	}
	return nil
}

// resolveWatchFields is the fields with local references left out: a record
// created in the same changeset has no version to watch yet.
func resolveWatchFields(t *recordType, fields map[string]any) fieldSet {
	out := make(fieldSet, len(fields))
	for k, v := range fields {
		if f, ok := t.field(k); ok && f.Kind == kindID {
			if s, isStr := v.(string); isStr && isRef(s) {
				continue
			}
		}
		out[k] = v
	}
	return out
}

// sameFieldValue compares a given value with a snapshot's, ignoring the
// padding a command would trim and the int/float split JSON blurs.
func sameFieldValue(a, b any) bool {
	// A cleared text field is null in a read and "" when given blank.
	if a == nil {
		if bs, ok := b.(string); ok {
			return strings.TrimSpace(bs) == ""
		}
	}
	if b == nil {
		if as, ok := a.(string); ok {
			return strings.TrimSpace(as) == ""
		}
	}
	if as, ok := a.(string); ok {
		bs, ok := b.(string)
		return ok && strings.TrimSpace(as) == strings.TrimSpace(bs)
	}
	aj, aerr := json.Marshal(a)
	bj, berr := json.Marshal(b)
	return aerr == nil && berr == nil && string(aj) == string(bj)
}

// describe fills an operation's Label, Before, After and Description from its
// run. For an update it also refuses a change that leaves the record as it is.
func (r *recordRegistry) describe(i int, op *changeOp, run opRun) error {
	t := r.types[op.Type]
	head := fmt.Sprintf("ops[%d] (%s %s)", i, op.Type, op.Action)
	switch op.Action {
	case changeCreate:
		op.Label = run.after.Label
		op.After = pickAfter(run.after, op.Fields)
	case changeUpdate:
		changed := false
		for name, given := range op.Fields {
			cur, inSnapshot := run.before.Fields[name]
			if !inSnapshot || isRefValue(given) || !sameFieldValue(cur, given) {
				changed = true
				break
			}
		}
		if !changed {
			return fmt.Errorf("%s: fields: the change leaves %q exactly as it is; propose only what differs", head, run.before.Label)
		}
		op.Label = run.before.Label
		op.Before = pickFields(run.before.Fields, op.Fields)
		op.After = pickAfter(run.after, op.Fields)
	case changeDelete:
		op.Label = run.before.Label
		op.Before = mergeFields(run.before.Fields, nil)
	}
	d := describeInput{Op: *op, Type: t, Before: run.before, After: run.after, Effects: run.effects, Extra: run.extra}
	var line string
	if t.Describe != nil {
		line = t.Describe(d)
	} else {
		line = genericDescription(d)
	}
	if op.Action == changeDelete && len(run.effects) > 0 && t.Describe == nil {
		line += " (" + strings.Join(run.effects, "; ") + ")"
	}
	op.Description = line
	return nil
}

func isRefValue(v any) bool {
	s, ok := v.(string)
	return ok && isRef(s)
}

// pickFields is the fields of snapshot named in given, those the snapshot has.
func pickFields(snapshot map[string]any, given map[string]any) map[string]any {
	out := make(map[string]any, len(given))
	for name := range given {
		if v, ok := snapshot[name]; ok {
			out[name] = v
		}
	}
	return out
}

// pickAfter is, for each field the operation names, what the record holds
// after the run, or the value given when the field is an input the record
// does not keep (a meter reading).
func pickAfter(after *recordSnapshot, given map[string]any) map[string]any {
	out := make(map[string]any, len(given))
	for name, v := range given {
		if cur, ok := after.Fields[name]; ok {
			out[name] = cur
		} else {
			out[name] = v
		}
	}
	return out
}

func genericDescription(d describeInput) string {
	label := d.Type.Label
	switch d.Op.Action {
	case changeCreate:
		return "Add " + label + " " + d.After.Label
	case changeDelete:
		return "Delete " + label + " " + d.Before.Label
	}
	var names []string
	for _, f := range d.Type.Fields {
		if _, ok := d.Op.Fields[f.Name]; ok {
			names = append(names, strings.ReplaceAll(f.Name, "_", " "))
		}
	}
	return "Change " + label + " " + d.Before.Label + ": " + strings.Join(names, ", ")
}

// ── apply ───────────────────────────────────────────────────────────────

// apply runs a prepared changeset inside the caller's transaction. Before the
// first write it checks every record the operations target or watch is still
// at the version Mate saw; a record that changed or went is a stale error
// naming it, and nothing is written. The caller commits only if apply returns
// nil. It returns what each operation wrote, in order.
func (r *recordRegistry) apply(tx *sql.Tx, now, today time.Time, ops []changeOp) ([]changeOpResult, error) {
	if err := r.checkFresh(tx, ops); err != nil {
		return nil, err
	}
	runs, err := r.run(changeEnv{tx: tx, now: now, today: today}, ops)
	if err != nil {
		return nil, err
	}
	results := make([]changeOpResult, len(ops))
	for i, op := range ops {
		label := op.Label
		if runs[i].after != nil {
			label = runs[i].after.Label
		}
		results[i] = changeOpResult{Type: op.Type, Action: op.Action, ID: runs[i].id, Label: label}
	}
	return results, nil
}

// afterCommit runs each type's post-commit hook for what its operations wrote.
func (r *recordRegistry) afterCommit(results []changeOpResult) {
	for _, res := range results {
		if t, ok := r.types[res.Type]; ok && t.AfterCommit != nil {
			t.AfterCommit(res)
		}
	}
}

// checkFresh compares every target and watch with its stored version. Two
// operations on one record are both checked against the same stored version
// up front, then run in order, the second seeing the first's writes.
func (r *recordRegistry) checkFresh(q sqlQueryer, ops []changeOp) error {
	// A card stored by an older Helmcentral may ask for something that is no
	// longer offered: stale, with a reason, rather than broken.
	for _, op := range ops {
		if _, ok := r.types[op.Type]; !ok || (op.Action != changeCreate && op.Action != changeUpdate && op.Action != changeDelete) {
			return &staleProposalError{reason: fmt.Sprintf("this card asks for a change Helmcentral no longer makes (%s), so it can no longer be applied", op.Description)}
		}
	}
	for _, op := range ops {
		if op.Action != changeCreate && !isRef(op.ID) {
			if err := r.checkOneFresh(q, op.Type, op.ID, op.BaseVersion, op.Label); err != nil {
				return err
			}
		}
		for _, w := range op.Watches {
			if err := r.checkOneFresh(q, w.Type, w.ID, w.Version, w.Label); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *recordRegistry) checkOneFresh(q sqlQueryer, typeName, id, version, label string) error {
	t, ok := r.types[typeName]
	if !ok {
		return &staleProposalError{reason: fmt.Sprintf("%s can no longer be changed by Mate, so nothing was applied", typeName)}
	}
	if label == "" {
		label = "a " + t.Label
	}
	cur, err := t.Get(q, id)
	if errors.Is(err, errRecordNotFound) {
		return &staleProposalError{reason: label + " was removed since Mate proposed this, so nothing was applied"}
	}
	if err != nil {
		return err
	}
	if cur.Version != version {
		return &staleProposalError{reason: label + " changed since Mate proposed this, so nothing was applied"}
	}
	return nil
}
