package main

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// chunkTargetChars is the size (in runes) chunkDocument aims for. It isn't a
// hard boundary: a chunk is flushed once it reaches this size, so most
// chunks land somewhere between this and chunkMaxChars.
const chunkTargetChars = 1500

// chunkMaxChars is the hard rune limit no chunk ever exceeds, enforced even
// when the input has no natural paragraph/line breaks to split on.
const chunkMaxChars = 2000

// paragraphSplitRe splits text on one or more blank lines - the first,
// coarsest level of chunkDocument's paragraph/line/rune fallback chain.
var paragraphSplitRe = regexp.MustCompile(`\r?\n\s*\r?\n+`)

// markdownHeadingRe matches a level 1-3 ATX heading line ("#", "##" or
// "###" followed by a space and the heading text). Four or more leading
// "#" characters never match here, so a #### line is left as ordinary body
// text of whatever section contains it, per the plan's "# through ###"
// scope.
var markdownHeadingRe = regexp.MustCompile(`(?m)^(#{1,3})[ \t]+(.+?)[ \t]*$`)

// chunkDocument splits an extracted document into document_chunks rows
// ready for documentStore.ReplaceChunks. Every chunk here is source "local"
// (B4's OCR pass uses its own "ocr" source) and seq starts at 1, since seq 0
// is reserved for the store's own meta chunk. PDFs are chunked per page so a
// chunk never spans two pages (PageStart/PageEnd carry the page number);
// markdown is chunked per # / ## / ### section; everything else is chunked
// by size alone.
func chunkDocument(ex extractedDocument, mime string) []documentChunk {
	switch mime {
	case "application/pdf":
		return chunkPDFPages(ex.Pages)
	case "text/markdown":
		return chunkMarkdown(ex.Markdown)
	default:
		return chunkPlainText(ex.Markdown)
	}
}

// chunkPDFPages emits one or more chunks per page (never fewer than the
// text needs, never spanning a page boundary), skipping pages with no
// extractable text at all - an empty page contributes nothing worth
// indexing.
func chunkPDFPages(pages []extractedPage) []documentChunk {
	var chunks []documentChunk
	seq := 1
	for _, page := range pages {
		text := strings.TrimSpace(page.Text)
		if text == "" {
			continue
		}
		for _, piece := range splitTextIntoChunks(text) {
			chunks = append(chunks, documentChunk{
				Seq:       seq,
				Source:    "local",
				PageStart: page.Number,
				PageEnd:   page.Number,
				Text:      piece,
			})
			seq++
		}
	}
	return chunks
}

// markdownSection is one heading-delimited span of a markdown document:
// Heading is "" for any text preceding the first heading (or for the whole
// document, if it has none at all).
type markdownSection struct {
	Heading string
	Body    string
}

// splitMarkdownSections splits md on its level 1-3 ATX headings.
func splitMarkdownSections(md string) []markdownSection {
	matches := markdownHeadingRe.FindAllStringSubmatchIndex(md, -1)
	if len(matches) == 0 {
		return []markdownSection{{Body: md}}
	}

	var sections []markdownSection
	if pre := strings.TrimSpace(md[:matches[0][0]]); pre != "" {
		sections = append(sections, markdownSection{Body: pre})
	}

	for i, m := range matches {
		heading := strings.TrimSpace(md[m[4]:m[5]])
		bodyStart := m[1]
		bodyEnd := len(md)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		sections = append(sections, markdownSection{
			Heading: heading,
			Body:    strings.TrimSpace(md[bodyStart:bodyEnd]),
		})
	}
	return sections
}

// chunkMarkdown emits chunks per heading-delimited section, splitting a
// section further by size if its body alone is larger than chunkMaxChars. A
// heading with no body still produces one chunk (the heading text itself),
// so it's not left unsearchable outside the store's own meta chunk.
func chunkMarkdown(md string) []documentChunk {
	var chunks []documentChunk
	seq := 1
	for _, section := range splitMarkdownSections(md) {
		if section.Heading == "" && section.Body == "" {
			continue
		}
		pieces := splitTextIntoChunks(section.Body)
		if len(pieces) == 0 {
			pieces = []string{section.Heading}
		}
		for _, piece := range pieces {
			chunks = append(chunks, documentChunk{
				Seq:     seq,
				Source:  "local",
				Heading: section.Heading,
				Text:    piece,
			})
			seq++
		}
	}
	return chunks
}

// chunkPlainText emits chunks split by size alone - text/plain, text/csv,
// application/json, or any other non-PDF, non-markdown text.
func chunkPlainText(text string) []documentChunk {
	var chunks []documentChunk
	seq := 1
	for _, piece := range splitTextIntoChunks(text) {
		chunks = append(chunks, documentChunk{Seq: seq, Source: "local", Text: piece})
		seq++
	}
	return chunks
}

// splitTextIntoChunks is the shared paragraph/line/rune fallback splitter
// behind every chunkDocument path. Paragraphs (blank-line-separated) are
// greedily packed into a chunk until it reaches chunkTargetChars, then
// flushed; a single paragraph already over chunkMaxChars is flushed on its
// own and broken down by splitOversizedText instead of being packed with
// anything else. No chunk this returns ever exceeds chunkMaxChars runes.
func splitTextIntoChunks(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	var out []string
	var buf strings.Builder
	bufLen := 0

	flush := func() {
		if bufLen > 0 {
			out = append(out, buf.String())
			buf.Reset()
			bufLen = 0
		}
	}

	for _, raw := range paragraphSplitRe.Split(text, -1) {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		pLen := utf8.RuneCountInString(p)

		if pLen > chunkMaxChars {
			flush()
			out = append(out, splitOversizedText(p, chunkMaxChars)...)
			continue
		}

		sep := 0
		if bufLen > 0 {
			sep = 2 // "\n\n"
		}
		if bufLen > 0 && bufLen+sep+pLen > chunkMaxChars {
			flush()
		}
		if bufLen > 0 {
			buf.WriteString("\n\n")
			bufLen += 2
		}
		buf.WriteString(p)
		bufLen += pLen

		if bufLen >= chunkTargetChars {
			flush()
		}
	}
	flush()
	return out
}

// splitOversizedText breaks a single paragraph that's already over max into
// line-sized pieces, falling back further to a hard rune cut for any
// individual line that's still too long on its own (e.g. one giant
// unbroken token). No returned piece exceeds max runes.
func splitOversizedText(text string, max int) []string {
	var out []string
	var buf strings.Builder
	bufLen := 0

	flush := func() {
		if bufLen > 0 {
			out = append(out, buf.String())
			buf.Reset()
			bufLen = 0
		}
	}

	for _, line := range strings.Split(text, "\n") {
		lineLen := utf8.RuneCountInString(line)

		if lineLen > max {
			flush()
			out = append(out, splitByRunes(line, max)...)
			continue
		}

		sep := 0
		if bufLen > 0 {
			sep = 1 // "\n"
		}
		if bufLen > 0 && bufLen+sep+lineLen > max {
			flush()
		}
		if bufLen > 0 {
			buf.WriteString("\n")
			bufLen++
		}
		buf.WriteString(line)
		bufLen += lineLen
	}
	flush()
	return out
}

// splitByRunes hard-cuts s every max runes - the last-resort fallback for a
// single line with no whitespace to split on at all.
func splitByRunes(s string, max int) []string {
	runes := []rune(s)
	if len(runes) == 0 {
		return nil
	}
	var out []string
	for i := 0; i < len(runes); i += max {
		end := i + max
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}
