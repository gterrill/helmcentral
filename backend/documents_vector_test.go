package main

import (
	"math"
	"testing"
)

func TestEncodeDecodeEmbedding_RoundTrips(t *testing.T) {
	vec := []float32{0.5, -0.25, 1.0, 0.0, -3.75}
	blob := encodeEmbedding(vec)
	if len(blob) != len(vec)*4 {
		t.Fatalf("expected a %d-byte blob for %d float32s, got %d", len(vec)*4, len(vec), len(blob))
	}

	got, err := decodeEmbedding(blob)
	if err != nil {
		t.Fatalf("decodeEmbedding: %v", err)
	}
	if len(got) != len(vec) {
		t.Fatalf("expected %d components back, got %d", len(vec), len(got))
	}
	for i, v := range vec {
		if got[i] != v {
			t.Fatalf("component %d: expected %v, got %v", i, v, got[i])
		}
	}
}

func TestDecodeEmbedding_RejectsWrongLengthBlob(t *testing.T) {
	if _, err := decodeEmbedding(make([]byte, 7)); err == nil {
		t.Fatalf("expected an error for a 7-byte blob (not a multiple of 4)")
	}
}

func TestDecodeEmbedding_RejectsEmptyBlob(t *testing.T) {
	if _, err := decodeEmbedding(nil); err == nil {
		t.Fatalf("expected an error for an empty blob")
	}
	if _, err := decodeEmbedding([]byte{}); err == nil {
		t.Fatalf("expected an error for an empty (non-nil) blob")
	}
}

func TestNormaliseEmbedding_ProducesUnitVector(t *testing.T) {
	got, err := normaliseEmbedding([]float32{3, 4})
	if err != nil {
		t.Fatalf("normaliseEmbedding: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 components, got %d", len(got))
	}
	if math.Abs(float64(got[0])-0.6) > 1e-6 || math.Abs(float64(got[1])-0.8) > 1e-6 {
		t.Fatalf("expected [0.6, 0.8], got %v", got)
	}

	var sumSq float64
	for _, v := range got {
		sumSq += float64(v) * float64(v)
	}
	if math.Abs(math.Sqrt(sumSq)-1) > 1e-6 {
		t.Fatalf("expected unit length, got length %v", math.Sqrt(sumSq))
	}
}

func TestNormaliseEmbedding_RejectsZeroVector(t *testing.T) {
	if _, err := normaliseEmbedding([]float32{0, 0, 0}); err == nil {
		t.Fatalf("expected an error for a zero vector")
	}
	if _, err := normaliseEmbedding(nil); err == nil {
		t.Fatalf("expected an error for an empty vector (no direction either)")
	}
}

func TestNormaliseEmbedding_RejectsNonFiniteComponents(t *testing.T) {
	if _, err := normaliseEmbedding([]float32{1, float32(math.NaN())}); err == nil {
		t.Fatalf("expected an error for a NaN component")
	}
	if _, err := normaliseEmbedding([]float32{1, float32(math.Inf(1))}); err == nil {
		t.Fatalf("expected an error for a +Inf component")
	}
	if _, err := normaliseEmbedding([]float32{1, float32(math.Inf(-1))}); err == nil {
		t.Fatalf("expected an error for a -Inf component")
	}
}

func TestDotProduct_OfUnitVectors(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	if got := dotProduct(a, b); got != 0 {
		t.Fatalf("expected orthogonal unit vectors to score 0, got %v", got)
	}

	same := []float32{0, 0, 1}
	if got := dotProduct(same, same); got != 1 {
		t.Fatalf("expected an identical unit vector to score 1, got %v", got)
	}

	c, err := normaliseEmbedding([]float32{1, 1})
	if err != nil {
		t.Fatalf("normaliseEmbedding: %v", err)
	}
	d, err := normaliseEmbedding([]float32{1, -1})
	if err != nil {
		t.Fatalf("normaliseEmbedding: %v", err)
	}
	if got := dotProduct(c, d); math.Abs(float64(got)) > 1e-6 {
		t.Fatalf("expected perpendicular unit vectors to score ~0, got %v", got)
	}
}

// TestReciprocalRankFusion_ThirdAndFirstBeatsSecondAndSecond is the case
// the brief calls out explicitly: a document ranked 3rd by FTS and 1st by
// vector search must outrank one ranked 2nd by both, even though neither of
// its own ranks is as good as the other document's worse rank. Rank 1 in one
// list plus rank 3 in another (1/61 + 1/63 = 0.032266...) narrowly beats
// rank 2 in both (2/62 = 0.032258...) - RRF's whole point is that this
// margin is real but small, so two lists that mostly disagree still let a
// document either one is confident about surface near the top.
func TestReciprocalRankFusion_ThirdAndFirstBeatsSecondAndSecond(t *testing.T) {
	// "agree" is 3rd by FTS, 1st by vector search.
	// "middle" is 2nd by both.
	fts := []string{"other-a", "middle", "agree"}
	vec := []string{"agree", "middle", "other-b"}

	wantAgreeScore := rrfScore(3) + rrfScore(1)
	wantMiddleScore := rrfScore(2) + rrfScore(2)
	if wantAgreeScore <= wantMiddleScore {
		t.Fatalf("test setup is wrong: expected agree's score %v to exceed middle's %v", wantAgreeScore, wantMiddleScore)
	}

	got := reciprocalRankFusion(fts, vec)

	agreeIdx, middleIdx := -1, -1
	for i, id := range got {
		switch id {
		case "agree":
			agreeIdx = i
		case "middle":
			middleIdx = i
		}
	}
	if agreeIdx == -1 || middleIdx == -1 {
		t.Fatalf("expected both documents in the fused ranking, got %v", got)
	}
	if agreeIdx >= middleIdx {
		t.Fatalf("expected 'agree' (rank 3 + rank 1) ahead of 'middle' (rank 2 + rank 2), got order %v", got)
	}
}

// TestReciprocalRankFusion_TiesBreakDeterministically covers two documents
// landing on the exact same fused score - each appears only once, both at
// rank 1 of their own (single) list - so the merge has to fall back to the
// better single rank (a tie here too, both are rank 1) and then to the id,
// rather than depending on Go's randomised map iteration order.
func TestReciprocalRankFusion_TiesBreakDeterministically(t *testing.T) {
	fts := []string{"zzz-only-fts"}
	vec := []string{"aaa-only-vec"}

	want := []string{"aaa-only-vec", "zzz-only-fts"}
	for i := 0; i < 20; i++ {
		got := reciprocalRankFusion(fts, vec)
		if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("run %d: expected a stable tie-break order %v, got %v", i, want, got)
		}
	}
}
