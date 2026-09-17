package main

import (
	"strings"
	"unicode"
)

// maxFTSQueryTokens caps how many words of an operator's search box query
// ftsMatchQuery turns into FTS5 phrase terms. A query this long is already
// well past anything a human typed on purpose; capping it keeps the MATCH
// string bounded regardless of how much text a caller hands in.
const maxFTSQueryTokens = 16

// ftsMatchQuery turns free-typed operator input into a safe FTS5 MATCH
// string: every token becomes its own double-quoted phrase (so none of
// FTS5's own query syntax - AND/OR/NOT/NEAR, column filters, bare "*",
// parentheses - is ever interpreted from operator input), with any embedded
// double quote doubled per FTS5's own escaping rule. The last token gets a
// trailing "*" turned into a prefix match ("tok"*), so "impell" finds
// "impeller" while it's still being typed. Tokens beyond maxFTSQueryTokens
// are dropped rather than rejected. ok is false only for empty (or
// all-whitespace) input - the caller (Search) treats that as "no query", not
// an error, since FTS5 itself rejects an empty MATCH string outright.
func ftsMatchQuery(user string) (string, bool) {
	// Control characters split tokens like whitespace. SQLite reads a NUL as
	// the end of the MATCH string, which leaves the quoted phrase unterminated.
	tokens := strings.FieldsFunc(user, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	})
	if len(tokens) == 0 {
		return "", false
	}
	if len(tokens) > maxFTSQueryTokens {
		tokens = tokens[:maxFTSQueryTokens]
	}

	quoted := make([]string, len(tokens))
	for i, tok := range tokens {
		quoted[i] = `"` + strings.ReplaceAll(tok, `"`, `""`) + `"`
	}
	quoted[len(quoted)-1] += "*"

	return strings.Join(quoted, " "), true
}
