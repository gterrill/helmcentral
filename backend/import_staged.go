package main

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// This file holds the platform-neutral payload every import source emits
// (import_yachtwave.go is the first parser; a later source is another parser
// producing the same shapes) and the decisions an operator makes about it.
// A staged payload is what the review wizard reads: nothing in it has touched
// the registry yet. It is stored whole on an import run (import_store.go),
// which is why every record carries a stable key of its own rather than a
// position - the export has no ids, so the key is a hash of the record's
// natural key, and the same record in a later export hashes to the same key.
//
// No omitempty anywhere, ADR 0115 section 7: the browser binds these through
// a TypeScript interface that declares every field, and a key dropped for an
// empty value arrives as undefined in code that type-checked clean.

// Issue codes. A code is a stable, machine-readable name for one kind of data
// problem; the wizard groups on it and the message carries the detail.
const (
	issueDuplicate             = "duplicate"
	issueJunkName              = "junk_name"
	issueUnknownPartNumber     = "unknown_part_number"
	issueImplausibleValue      = "implausible_value"
	issueChecklistWithoutSteps = "checklist_without_steps"
	issuePasswordNoteSkipped   = "password_note_skipped"
	issueEmptySection          = "empty_section"
	issueSectionNotImported    = "section_not_imported"
	issueAmbiguousEquipment    = "ambiguous_equipment"
	issueUnparsedDate          = "unparsed_date"
	issueUnknownEquipmentInLog = "unknown_equipment"
	issueSeverityWarning       = "warning"
	issueSeverityInfo          = "info"
	importSourceYachtWave      = "yachtwave"
	stagedKindEngine           = "engine"
	stagedKindEquipment        = "equipment"
	stagedSpareItem            = "item"
	stagedSpareBin             = "bin"
	stagedNoteKindNote         = "note"
	stagedNoteKindTask         = "task"
	stagedFileKindPhoto        = "photo"
	stagedFileKindDocument     = "document"
	importTargetZone           = "zone"
	importTargetBin            = "bin"
	importTargetEquipment      = "equipment"
	importTargetLogEntry       = "log_entry"
	importTargetNote           = "note"
	importActionCreate         = "create"
	importActionMatch          = "match"
	importActionSkip           = "skip"
	importParticularApply      = "apply"
	importParticularSkip       = "skip"
	importStatusDraft          = "draft"
	importStatusCommitted      = "committed"
	importStatusAbandoned      = "abandoned"
)

// stagedImport is the whole parsed export.
type stagedImport struct {
	Source      string              `json:"source"`
	Vessel      stagedVessel        `json:"vessel"`
	Particulars []stagedParticular  `json:"particulars"`
	Locations   []stagedLocation    `json:"locations"`
	Equipment   []stagedEquipment   `json:"equipment"`
	Spares      []stagedSpare       `json:"spares"`
	LogEntries  []stagedLogEntry    `json:"log_entries"`
	Notes       []stagedNote        `json:"notes"`
	Files       []stagedFile        `json:"files"`
	Issues      []stagedIssue       `json:"issues"`
	Sections    []stagedSectionInfo `json:"sections"`
}

// stagedVessel identifies the export: which boat it describes and when the
// source generated it (the source's own text, not reformatted).
type stagedVessel struct {
	Name        string `json:"name"`
	GeneratedAt string `json:"generated_at"`
}

// stagedSectionInfo is one row of the review page's "what was in the file"
// table: the source's own section title, how many records it listed, and how
// many of those reach a staged list above.
type stagedSectionInfo struct {
	Name     string `json:"name"`
	Listed   int    `json:"listed"`
	Imported int    `json:"imported"`
}

// stagedParticular is one line of the vessel particulars. Field names the
// vessel_particulars column it would fill, or "" when Helmcentral has no
// column for it. SignalK is true for the values the boat's own data feed
// publishes (name, MMSI, call sign, length, beam, draft, air height): those
// are never written, the wizard only shows them next to the live value.
type stagedParticular struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Field   string `json:"field"`
	Value   string `json:"value"`
	SignalK bool   `json:"signalk"`
}

// stagedLocation is a zone the export names, with how many staged records
// point at it.
type stagedLocation struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// stagedEquipment is one engine or piece of fitted equipment. Kind is
// "engine" or "equipment". Detail is the location text beneath the name
// (for an engine, the whole descriptor line: position, power, fuel), Category
// the source's own grouping word, Hours the engine hour reading when the
// export carries one.
type stagedEquipment struct {
	Key          string `json:"key"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Category     string `json:"category"`
	Manufacturer string `json:"manufacturer"`
	Serial       string `json:"serial"`
	Installed    string `json:"installed"`
	Location     string `json:"location"`
	Detail       string `json:"detail"`
	Notes        string `json:"notes"`
	Hours        string `json:"hours"`
}

// stagedSpare is one stock row. Kind "item" is one stocked thing; Kind "bin"
// is a locker row whose detail lists many things one per line, which becomes
// a bin with an item per line (Items). Required is nil when the export gives
// only an on-hand count.
type stagedSpare struct {
	Key        string   `json:"key"`
	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	PartNumber string   `json:"part_number"`
	OnHand     int      `json:"on_hand"`
	Required   *int     `json:"required"`
	Location   string   `json:"location"`
	Detail     string   `json:"detail"`
	Notes      string   `json:"notes"`
	Category   string   `json:"category"`
	Items      []string `json:"items"`
}

// stagedLogEntry is one maintenance log row. EquipmentName is the source's
// own text; EquipmentKey is the staged equipment it names when exactly one
// matches, and Candidates lists every match when more than one does (the
// wizard asks the operator to pick).
type stagedLogEntry struct {
	Key           string   `json:"key"`
	Date          string   `json:"date"`
	Title         string   `json:"title"`
	Body          string   `json:"body"`
	Type          string   `json:"type"`
	EquipmentName string   `json:"equipment_name"`
	EquipmentKey  string   `json:"equipment_key"`
	Candidates    []string `json:"candidates"`
	Hours         *float64 `json:"hours"`
}

// stagedNote is a note to create. Kind "task" is a source task converted to
// a note. Skip is true for a note the parser refuses to carry (it looks like
// it holds a credential); SkipReason says why, and commit never writes a
// skipped note whatever the decisions say.
type stagedNote struct {
	Key        string `json:"key"`
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	Date       string `json:"date"`
	Body       string `json:"body"`
	Skip       bool   `json:"skip"`
	SkipReason string `json:"skip_reason"`
}

// stagedFile is a photo or document the export only links to. URL points at
// the source's own storage and is never fetched; the operator downloads it
// and hands the bytes over page by page. EquipmentKey is the staged
// equipment the file is attached to when its attachment text names exactly
// one.
type stagedFile struct {
	Key          string `json:"key"`
	Kind         string `json:"kind"`
	Label        string `json:"label"`
	Type         string `json:"type"`
	AttachedTo   string `json:"attached_to"`
	Date         string `json:"date"`
	Size         string `json:"size"`
	URL          string `json:"url"`
	EquipmentKey string `json:"equipment_key"`
}

// stagedIssue is one data-quality finding. Key is the record it concerns
// ("" for a whole-section finding), Section the source's own section name.
type stagedIssue struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Section  string `json:"section"`
	Key      string `json:"key"`
	Message  string `json:"message"`
}

// ── decisions ────────────────────────────────────────────────────────────

// importDecisions is everything the operator decides about a staged payload,
// saved page by page (PATCH) and read back at commit. Every map is keyed by a
// staged record key. Commit treats a missing entry as "skip", so a record the
// operator never saw is never written by accident; the defaults the server
// computes at upload fill every map so the wizard starts with a full set.
type importDecisions struct {
	// Particulars: key -> "apply" or "skip".
	Particulars map[string]string `json:"particulars"`
	// Zones: staged location key -> what to do with that location.
	Zones map[string]importZoneDecision `json:"zones"`
	// Records covers equipment, spares (items and bins), log entries and
	// notes, one namespace because the keys never collide.
	Records map[string]importRecordDecision `json:"records"`
	// LogEquipment: log entry key -> the equipment the operator picked when
	// the export's name was ambiguous.
	LogEquipment map[string]importEquipmentRef `json:"log_equipment"`
	// Files: staged file key -> the stored document, or skipped.
	Files map[string]importFileDecision `json:"files"`
}

// importZoneDecision: Action is create, match or skip. Match needs ZoneID.
type importZoneDecision struct {
	Action string `json:"action"`
	ZoneID string `json:"zone_id"`
}

// importRecordDecision: Action is create, match or skip. Match needs
// TargetID (the existing equipment or bin); nothing is written to the matched
// record, the import only remembers that they are the same thing.
type importRecordDecision struct {
	Action   string `json:"action"`
	TargetID string `json:"target_id"`
}

// importEquipmentRef names an equipment record either by the staged key of
// one being imported in this run or by the id of one that already exists.
// Exactly one is set.
type importEquipmentRef struct {
	EquipmentKey string `json:"equipment_key"`
	EquipmentID  string `json:"equipment_id"`
}

// importFileDecision: DocumentID is the stored document the operator handed
// over; Skipped true means the operator chose not to import this file.
// EquipmentKey optionally links the stored document to a staged equipment
// record at commit.
type importFileDecision struct {
	DocumentID   string `json:"document_id"`
	Skipped      bool   `json:"skipped"`
	EquipmentKey string `json:"equipment_key"`
}

// ── keys ─────────────────────────────────────────────────────────────────

// stagedKey hashes a record's natural key into its stable staged key. The
// first part names the record kind so a zone called "Salon" and a note
// called "Salon" never share a key. Twenty hex characters (80 bits) is far
// beyond any collision risk for one boat's export and short enough to read in
// a URL.
func stagedKey(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0x1f})
		}
		h.Write([]byte(strings.TrimSpace(p)))
	}
	return hex.EncodeToString(h.Sum(nil))[:20]
}

// keyAllocator hands out keys, adding an occurrence counter to a repeat of
// the same natural key so two identical rows (a log entry entered twice)
// still get distinct, repeatable keys. The second and later copies are the
// ones reported as duplicates.
type keyAllocator struct {
	seen map[string]int
}

func newKeyAllocator() *keyAllocator { return &keyAllocator{seen: map[string]int{}} }

// next returns the key and which occurrence of that natural key this is
// (0 for the first).
func (k *keyAllocator) next(parts ...string) (key string, occurrence int) {
	base := stagedKey(parts...)
	n := k.seen[base]
	k.seen[base] = n + 1
	if n == 0 {
		return base, 0
	}
	return stagedKey(append(append([]string{}, parts...), "#"+strconv.Itoa(n))...), n
}
