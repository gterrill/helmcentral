package main

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Record types (ADR 0158). Helmcentral's write surface is two things: records
// the operator keeps (equipment, locations, rules, documents) and vessel
// controls (the autopilot, the generator, CZone switches). Only records are
// registered here, and only types whose rows live in helmcentral.sqlite, so
// that one changeset can be one SQLite transaction. A type registers once:
// its fields, how to list and read it, and transaction-taking commands to
// create, update and delete it, which the type's own REST handlers call too,
// so a rule is checked in one place whoever makes the change.
//
// Never register: vessel controls, secrets and API keys, sessions and login,
// imports, map imagery caches, or Mate's own conversations. Reads through
// list_records return every registered field into a chat transcript, so a
// type registers only fields that are safe to show there.

// The three things a changeset operation can do to a record.
const (
	changeCreate = "create"
	changeUpdate = "update"
	changeDelete = "delete"
)

// fieldKind is the value shape of a registered field. The changeset layer
// checks it before a type's command sees the value.
type fieldKind string

const (
	kindString     fieldKind = "string"
	kindDate       fieldKind = "date" // text; the type's own validator checks YYYY-MM-DD
	kindInteger    fieldKind = "integer"
	kindNumber     fieldKind = "number"
	kindBoolean    fieldKind = "boolean"
	kindStringList fieldKind = "string_list"
	kindID         fieldKind = "id"      // the id of another record; Ref names its type
	kindPolygon    fieldKind = "polygon" // a list of [x, y] points
)

// recordField is one registered field.
type recordField struct {
	Name string
	Kind fieldKind
	// Writable false means the field is read-only: it appears in a read but a
	// changeset cannot set it.
	Writable bool
	// Nullable means an operation may set the field to null to clear it.
	Nullable bool
	// Ref names the record type an id field points at, so a local reference
	// ("$1") in it must name a create of that type.
	Ref         string
	Enum        []string
	Description string
}

// fieldSet is the fields an operation gives, decoded from JSON: strings,
// float64 numbers, bools, []string, [][2]float64 and nil for null.
type fieldSet map[string]any

// mergeFields is the record's snapshot fields with the given ones laid over.
func mergeFields(base map[string]any, given fieldSet) map[string]any {
	out := make(map[string]any, len(base)+len(given))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range given {
		out[k] = v
	}
	return out
}

// recordSnapshot is one record as a type shows it: the registered fields, a
// label for the operator ("Generator · Oil and filter") and a version stamp
// (its updated_at) that a changeset's freshness check compares.
type recordSnapshot struct {
	ID      string         `json:"id"`
	Label   string         `json:"label"`
	Version string         `json:"version"`
	Fields  map[string]any `json:"fields"`
}

// errRecordNotFound is what a type's Get answers for an id that is not there.
var errRecordNotFound = errors.New("record not found")

// changeEnv is what a command runs against: the open transaction, the store's
// clock and the operator's date. A command never takes the store's mutex,
// the echo context or the wall clock, so it is safe inside a transaction
// that already holds the write lock.
type changeEnv struct {
	tx    *sql.Tx
	now   time.Time
	today time.Time
}

// createdRecord is what a type's Create reports: the new id and anything its
// description wants from the run (a completion's new due date).
type createdRecord struct {
	ID    string
	Extra map[string]any
}

// recordWatch names a record an operation depends on without targeting it:
// if it changes before Apply, the changeset is stale.
type recordWatch struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Version string `json:"version"`
	Label   string `json:"label,omitempty"`
}

// describeInput is what a type's Describe writes the operator's line from.
type describeInput struct {
	Op      changeOp
	Type    *recordType
	Before  *recordSnapshot
	After   *recordSnapshot
	Effects []string
	Extra   map[string]any
}

// recordType is one registered type.
type recordType struct {
	Name    string
	Label   string // the singular noun for an operator: "equipment item", "location"
	Summary string
	Fields  []recordField
	Actions []string // which of create, update, delete a changeset may do
	Filters map[string]string

	// List and Get read through the type's own store code. Get takes a
	// queryer so a changeset reads the record inside its transaction.
	List func(s *documentStore, filter map[string]string) ([]recordSnapshot, error)
	Get  func(q sqlQueryer, id string) (recordSnapshot, error)

	// Create, Update and Delete are the same commands the type's REST handlers
	// call, taking the changeset's transaction. Update receives the record as
	// it stands and only the fields the operation gives.
	Create func(env changeEnv, fields fieldSet) (createdRecord, error)
	Update func(env changeEnv, before recordSnapshot, given fieldSet) error
	Delete func(env changeEnv, before recordSnapshot) error

	// Effects lists what else a delete takes with it, in the operator's words
	// ("removes the outlines of 3 locations"). Optional.
	Effects func(q sqlQueryer, before recordSnapshot) ([]string, error)
	// Watch lists other records an operation's fields depend on. Optional.
	Watch func(q sqlQueryer, fields fieldSet) ([]recordWatch, error)
	// Describe writes the operator's line for an operation. Optional: the
	// default names the action and the record.
	Describe func(d describeInput) string
	// Href is the page that shows a record, once it exists.
	Href func(r recordSnapshot) string
	// Renames maps a command's field name to the name Mate used for it (a
	// meter reading is what the operator's form calls hours).
	Renames map[string]string
	// AfterCommit runs once the changeset's transaction has committed, for
	// side effects a command cannot make inside it. Optional.
	AfterCommit func(res changeOpResult)
}

func (t *recordType) field(name string) (recordField, bool) {
	for _, f := range t.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return recordField{}, false
}

func (t *recordType) allows(action string) bool { return containsString(t.Actions, action) }

func (t *recordType) writableNames() []string {
	var out []string
	for _, f := range t.Fields {
		if f.Writable {
			out = append(out, f.Name)
		}
	}
	return out
}

// recordRegistry holds the registered types.
type recordRegistry struct {
	types map[string]*recordType
}

func newRecordRegistry() *recordRegistry {
	return &recordRegistry{types: map[string]*recordType{}}
}

func (r *recordRegistry) register(t *recordType) {
	if t.Name == "" {
		panic("record type registered with no name")
	}
	if _, dup := r.types[t.Name]; dup {
		panic(fmt.Sprintf("record type %q registered twice", t.Name))
	}
	r.types[t.Name] = t
}

func (r *recordRegistry) lookup(name string) (*recordType, bool) {
	t, ok := r.types[name]
	return t, ok
}

func (r *recordRegistry) names() []string {
	out := make([]string, 0, len(r.types))
	for n := range r.types {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// defaultRecordRegistry is the registry Mate's tools use. Types add
// themselves in the record_type_*.go files' init.
var defaultRecordRegistry = newRecordRegistry()

// quoteList renders names as "a, b, c".
func quoteList(names []string) string { return strings.Join(names, ", ") }

// recordTypeGuide lists every type's writable fields for a tool description.
func recordTypeGuide(r *recordRegistry) string {
	var b strings.Builder
	for _, name := range r.names() {
		t := r.types[name]
		fmt.Fprintf(&b, "- %s (%s): %s\n", t.Name, quoteList(t.Actions), t.Summary)
		for _, f := range t.Fields {
			if !f.Writable {
				continue
			}
			fmt.Fprintf(&b, "    %s (%s): %s\n", f.Name, f.Kind, f.Description)
		}
	}
	return b.String()
}
