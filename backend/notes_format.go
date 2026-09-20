package main

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// This file is the note-on-disk serialisation format (plan §1): a note's
// bytes are a normal blob in DOCUMENTS_DIR, named by the sha256 of exactly
// what renderNoteFile produces - frontmatter plus body, nothing else. That
// makes the bytes themselves the note's identity (§1's "sha256 UNIQUE
// collisions" section), which is why rendering has to be byte-deterministic
// rather than merely "produces equivalent YAML": the same (meta, body)
// pair, rendered twice, five minutes apart, on two different machines, must
// come out bit-for-bit identical, or an edit that changed nothing would
// still mint a new sha256, a spurious reindex and (once Mate is on) an
// unnecessary embedding charge.

// noteFrontmatterDelimiter is the fence line every note file opens with,
// and (on its own line, later in the file) closes with. parseNoteFile
// fails loudly if a file doesn't begin with exactly this - a note file
// with no frontmatter at all is not a note this codebase ever wrote.
const noteFrontmatterDelimiter = "---\n"

// noteFrontmatter is the YAML shape rendered between the two "---" fences.
// Field order here IS the field order on disk - yaml.v3 marshals a struct's
// fields in declaration order, not alphabetically or by map iteration,
// which is what makes "explicit field order id/title/type/tags/created"
// (plan §1) a one-line guarantee rather than something renderNoteFile has
// to enforce by hand. Tags carries the `,flow` tag option so it serialises
// as `tags: [genset, engine-room]` (a single line, portable to read) rather
// than yaml.v3's default one-item-per-line block style. Created is a
// pre-formatted RFC3339 string, not time.Time: yaml.v3 has its own opinion
// about how to encode a time.Time (and quotes an RFC3339-shaped plain
// scalar to keep it unambiguously a string either way), and formatting it
// ourselves, once, in renderNoteFile, is one fewer place that opinion could
// silently change between yaml.v3 versions and break byte-stability.
type noteFrontmatter struct {
	ID      string   `yaml:"id"`
	Title   string   `yaml:"title"`
	Type    string   `yaml:"type"`
	Tags    []string `yaml:"tags,flow"`
	Created string   `yaml:"created"`
}

// noteFileMeta is renderNoteFile/parseNoteFile's shared, typed view of a
// note's frontmatter - noteFrontmatter with Created as an actual time.Time,
// for every caller that isn't the YAML encoder/decoder itself. ID IS
// documents.id (plan §1): a note's frontmatter carries the exact primary
// key its database row uses, so a note file handed to a next owner, or fed
// back through this same parser after a hand edit, always names the row it
// belongs to.
type noteFileMeta struct {
	ID      string
	Title   string
	Type    string
	Tags    []string
	Created time.Time
}

// renderNoteFile serialises meta and body into a note's on-disk bytes:
// "---\n", the YAML frontmatter, "---\n", then body right-trimmed of
// trailing whitespace plus exactly one trailing "\n". Tags are sorted here
// (not by the caller) so two notes carrying the same tag set in a different
// order render identically - "tags sorted on the way in" (plan §1)."
//
// LF only: this function never emits "\r\n" (yaml.v3 doesn't either, and
// body's own trailing whitespace is trimmed away by TrimRight below before
// the single "\n" is added back), so a note authored on this codebase's own
// Linux/armv7 targets round-trips identically regardless of what platform
// last touched it.
func renderNoteFile(meta noteFileMeta, body string) []byte {
	tags := append([]string{}, meta.Tags...)
	sort.Strings(tags)

	fm := noteFrontmatter{
		ID:    meta.ID,
		Title: meta.Title,
		Type:  meta.Type,
		Tags:  tags,
		// Truncate to whole seconds before formatting: documents.created_at
		// is stored as a Unix second count (documents_store.go), so a
		// caller that renders straight from a freshly-read database row
		// already has second precision. A caller that instead passes a
		// wall-clock time.Now() with sub-second precision (notes_handlers.go,
		// building the very first render before the row exists) gets
		// truncated here too, so the value it goes on to pass as the
		// document's own CreatedAt matches the frontmatter it just wrote,
		// rather than losing precision only on the database side and
		// silently drifting the two apart.
		Created: meta.Created.UTC().Truncate(time.Second).Format(time.RFC3339),
	}

	yamlBytes, err := yaml.Marshal(fm)
	if err != nil {
		// yaml.Marshal can only fail on a value it cannot encode at all
		// (a channel, a func, a cyclic map) - noteFrontmatter is nothing
		// but strings and a []string, which can never trigger that. A
		// panic here would be a bug in this function, not a runtime
		// condition either caller (notes_handlers.go, twice) could
		// meaningfully recover from - simpler for both to let
		// renderNoteFile return []byte outright than to thread an error
		// return through two call sites for a case that cannot happen.
		panic(fmt.Sprintf("notes: render note file: %v", err))
	}

	trimmedBody := strings.TrimRight(body, " \t\r\n")

	var buf bytes.Buffer
	buf.WriteString(noteFrontmatterDelimiter)
	buf.Write(yamlBytes)
	buf.WriteString("---\n")
	buf.WriteString(trimmedBody)
	buf.WriteString("\n")
	return buf.Bytes()
}

// splitFrontmatterFence locates the frontmatter block at the start of s:
// ok is false unless s begins with exactly "---\n" AND a line consisting
// of exactly "---" appears somewhere after it. yamlPart is everything
// between the two fences (not including either fence line); body is
// everything after the closing fence's own line (its trailing "\n", if
// any, consumed). The closing fence is found structurally - as a whole
// line, not merely the substring "---" - so it works whether the
// frontmatter is empty (fences back to back), the very last thing in the
// file (no body at all), or the ordinary case with content on both sides.
//
// Shared by parseNoteFile (which treats ok=false as a hard error - a file
// this codebase wrote always has both fences) and
// stripLeadingYAMLFrontmatter in documents_extract.go (which treats
// ok=false as "not frontmatter at all", leaving the text alone - see that
// function's own comment on why an un-closed leading "---" is an ordinary
// Markdown horizontal rule, not a malformed note).
func splitFrontmatterFence(s string) (yamlPart, body string, ok bool) {
	if !strings.HasPrefix(s, noteFrontmatterDelimiter) {
		return "", "", false
	}
	rest := s[len(noteFrontmatterDelimiter):]

	// The closing fence is the very first line of rest: an empty
	// frontmatter block, either with nothing after it at all (rest is
	// exactly "---") or with body content following on the next line.
	if rest == "---" {
		return "", "", true
	}
	if strings.HasPrefix(rest, "---\n") {
		return "", rest[len("---\n"):], true
	}

	// The general case: the closing fence is a later line, preceded by the
	// previous line's own "\n".
	if idx := strings.Index(rest, "\n---\n"); idx >= 0 {
		return rest[:idx+1], rest[idx+len("\n---\n"):], true
	}
	// The closing fence is the last line of the file, with no trailing
	// newline after it (no body at all, and the file itself doesn't end
	// in a blank line).
	if strings.HasSuffix(rest, "\n---") {
		return rest[:len(rest)-len("---")], "", true
	}

	return "", "", false
}

// errNoteFileFrontmatterMissing and errNoteFileFrontmatterUnclosed are
// parseNoteFile's two "this isn't a note file this codebase wrote" errors -
// kept as sentinels (rather than ad hoc fmt.Errorf calls at each call site)
// so a caller that wants to distinguish them from a YAML/timestamp parse
// failure further down can, though today's callers (notes_handlers.go) just
// log-and-500 either way: a document row whose blob doesn't parse as a note
// is a disk/database drift the fallback policy says to surface, not to work
// around.
var (
	errNoteFileFrontmatterMissing  = errors.New(`note file does not begin with a "---" frontmatter delimiter`)
	errNoteFileFrontmatterUnclosed = errors.New(`note file frontmatter has no closing "---" delimiter`)
)

// parseNoteFile is renderNoteFile's inverse: given a note's on-disk bytes,
// it returns the frontmatter (Created parsed back to a time.Time) and the
// body exactly as splitFrontmatterFence found it - NOT re-trimmed, so a
// caller comparing parseNoteFile(renderNoteFile(meta, body)) against the
// original body sees precisely what rendering did to it (trailing
// whitespace collapsed to one "\n"), rather than a second independent trim
// masking a bug in renderNoteFile's own.
//
// Fails loudly (AGENTS.md's fallback policy) rather than returning a
// best-effort partial parse: a file that doesn't begin with "---\n", or
// whose frontmatter is never closed, or whose YAML or created timestamp
// doesn't parse, is not a note this codebase's own renderNoteFile ever
// produced - most likely a hand-edited blob gone wrong, which is exactly
// the case plan §1 says must surface rather than be quietly reinterpreted.
func parseNoteFile(b []byte) (noteFileMeta, string, error) {
	s := string(b)
	if !strings.HasPrefix(s, noteFrontmatterDelimiter) {
		return noteFileMeta{}, "", errNoteFileFrontmatterMissing
	}

	yamlPart, body, ok := splitFrontmatterFence(s)
	if !ok {
		return noteFileMeta{}, "", errNoteFileFrontmatterUnclosed
	}

	var fm noteFrontmatter
	if err := yaml.Unmarshal([]byte(yamlPart), &fm); err != nil {
		return noteFileMeta{}, "", fmt.Errorf("parse note frontmatter: %w", err)
	}

	created, err := time.Parse(time.RFC3339, fm.Created)
	if err != nil {
		return noteFileMeta{}, "", fmt.Errorf("parse note frontmatter created timestamp %q: %w", fm.Created, err)
	}

	return noteFileMeta{
		ID:      fm.ID,
		Title:   fm.Title,
		Type:    fm.Type,
		Tags:    fm.Tags,
		Created: created.UTC(),
	}, body, nil
}

// deriveNoteTitle picks a title for a note that didn't get one explicitly
// (POST /api/notes' "title" field is optional - plan §4, and §9's no-Mate
// path table: "first ATX heading, else first line to 80 chars"). body is
// assumed non-empty (notes_handlers.go rejects an empty body before this is
// ever called) but this is defensive about an all-whitespace body anyway,
// returning "" rather than panicking or indexing past the end - a caller
// that somehow reaches this with nothing usable gets an empty title, the
// same as an operator who left the title field blank on a document that
// has no fallback either.
func deriveNoteTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if heading, ok := strings.CutPrefix(trimmed, "#"); ok {
			// An ATX heading: strip every leading "#" (H1..H6 all count)
			// and the required space after them.
			heading = strings.TrimLeft(heading, "#")
			heading = strings.TrimSpace(heading)
			if heading != "" {
				return truncateNoteTitle(heading, 80)
			}
			// "#" with nothing after it isn't a usable heading - fall
			// through to the first-line rule below using this same line.
		}
		return truncateNoteTitle(trimmed, 80)
	}
	return ""
}

// truncateNoteTitle cuts s to at most n runes (not bytes - a title is
// operator-facing text that may well contain multi-byte characters, and
// cutting mid-rune would corrupt the last character rather than just
// shortening the string).
func truncateNoteTitle(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
