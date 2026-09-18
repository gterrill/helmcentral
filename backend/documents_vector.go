package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
)

// encodeEmbedding packs vec as a little-endian float32 BLOB - the on-disk
// form document_chunk_embeddings.vector stores. Little-endian rather than
// native byte order because the blob has to read back identically wherever
// it's opened next: this boat's own box is armv7 today, a dev machine is
// amd64, and the sqlite file moves between them freely (ADR 0106's "the
// backup unit is one db file" - see newDocumentStore's doc comment).
func encodeEmbedding(vec []float32) []byte {
	out := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(out[i*4:i*4+4], math.Float32bits(v))
	}
	return out
}

// decodeEmbedding is encodeEmbedding's inverse. A blob whose length isn't a
// multiple of 4, or that is empty, is not a valid embedding - corruption or
// a programming error, not something to paper over. It's returned as an
// error rather than silently decoding whatever whole float32s happen to
// fit: a truncated vector would still score against a query, just wrongly,
// with nothing in the result to say so.
func decodeEmbedding(blob []byte) ([]float32, error) {
	if len(blob) == 0 {
		return nil, errors.New("decode embedding: empty blob")
	}
	if len(blob)%4 != 0 {
		return nil, fmt.Errorf("decode embedding: blob length %d is not a multiple of 4", len(blob))
	}
	out := make([]float32, len(blob)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4 : i*4+4]))
	}
	return out, nil
}

// normaliseEmbedding returns vec scaled to unit L2 length, as a new slice -
// vec itself is left untouched. The sum of squares accumulates in float64
// so a real embedding (hundreds of dimensions, each a small float32) keeps
// its precision through the sum instead of losing it the way accumulating
// in float32 would before the final sqrt. A zero vector has no direction to
// normalise to, and a NaN/Inf component means whatever produced vec was
// already broken upstream - both are errors here, never a silently-returned
// zero vector: AGENTS.md's fallback policy is fail fast and surface the
// problem, not mask it behind a plausible-looking result.
func normaliseEmbedding(vec []float32) ([]float32, error) {
	var sumSq float64
	for _, v := range vec {
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("normalise embedding: non-finite component %v", v)
		}
		sumSq += f * f
	}
	if sumSq == 0 {
		return nil, errors.New("normalise embedding: zero vector has no direction")
	}

	norm := math.Sqrt(sumSq)
	out := make([]float32, len(vec))
	for i, v := range vec {
		out[i] = float32(float64(v) / norm)
	}
	return out, nil
}

// dotProduct is the plain dot product of a and b. Both sides are assumed to
// already be unit vectors - normaliseEmbedding's output, on both the stored
// side (SetChunkEmbeddings) and the query side (SearchVector) - and the
// same length; callers guarantee both, so this IS cosine similarity, with
// no division or length check needed here.
func dotProduct(a, b []float32) float32 {
	var sum float32
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

// rrfK is the constant from Cormack, Clarke and Buettcher's 2009
// "Reciprocal Rank Fusion outperforms Condorcet and individual Rank
// Learning Methods" paper. It flattens the gap between a rank-1 hit and a
// rank-3 hit (1/61 vs 1/63 - about 3% apart) so neither retriever's own
// notion of "best" dominates the fused ranking: a document the keyword
// search is confident about and one the vector search is confident about
// start out nearly even, and a real edge only comes from also placing well
// in the other list.
const rrfK = 60

// rrfScore is one ranked list's contribution to a document's fused score at
// the given 1-based rank.
func rrfScore(rank int) float64 {
	return 1 / float64(rrfK+rank)
}

// reciprocalRankFusion merges two ranked lists of document ids - Search's
// FTS hits and SearchVector's hits, both already ordered best-first - into
// one fused ranking. Each id's score is the sum of rrfScore at its rank in
// whichever list(s) it appears in (0 from a list it's absent from), highest
// score first. A document appearing in both lists outranks one appearing
// in only one, even if the latter was 1st in its own list - that is RRF's
// entire point, favouring a hit two independent retrievers agree on over
// one only a single retriever is confident about.
//
// Ties are broken deterministically: first by the better (numerically
// lowest) of the id's own ranks, then by the id itself, so a caller (and
// its tests) can rely on a stable order rather than Go's randomised map
// iteration. A duplicate id within a single input list uses that id's
// first (best) occurrence - a caller handing in a list with duplicates has
// a bug, but this way it degrades to "ignore the repeat" rather than
// double-counting a phantom second hit.
func reciprocalRankFusion(a, b []string) []string {
	rankOf := func(list []string) map[string]int {
		ranks := make(map[string]int, len(list))
		for i, id := range list {
			if _, ok := ranks[id]; !ok {
				ranks[id] = i + 1
			}
		}
		return ranks
	}
	aRank := rankOf(a)
	bRank := rankOf(b)

	type fused struct {
		id       string
		score    float64
		bestRank int
	}
	seen := make(map[string]bool, len(a)+len(b))
	var entries []fused
	consider := func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true

		var score float64
		bestRank := 0
		if r, ok := aRank[id]; ok {
			score += rrfScore(r)
			bestRank = r
		}
		if r, ok := bRank[id]; ok {
			score += rrfScore(r)
			if bestRank == 0 || r < bestRank {
				bestRank = r
			}
		}
		entries = append(entries, fused{id: id, score: score, bestRank: bestRank})
	}
	for _, id := range a {
		consider(id)
	}
	for _, id := range b {
		consider(id)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].score != entries[j].score {
			return entries[i].score > entries[j].score
		}
		if entries[i].bestRank != entries[j].bestRank {
			return entries[i].bestRank < entries[j].bestRank
		}
		return entries[i].id < entries[j].id
	})

	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.id
	}
	return out
}
