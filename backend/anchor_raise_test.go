package main

import (
	"fmt"
	"testing"
)

// TestAnchorRaiseFailureStage pins that anchorRaiseFailureStage tells apart
// the two ways raiseAnchorWatch can fail - the publish step (SignalK didn't
// confirm) and the local-file-removal step (SignalK confirmed, but the
// watch could not be cleared on disk) - which is what lets deleteAnchorWatch
// keep its two distinct, pre-existing error responses after being refactored
// onto the function shared with the auto-raise watcher (anchor_auto_raise.go).
func TestAnchorRaiseFailureStage(t *testing.T) {
	publishErr := &anchorRaiseError{Stage: anchorRaiseStagePublish, Err: fmt.Errorf("publish failed")}
	if got := anchorRaiseFailureStage(publishErr); got != anchorRaiseStagePublish {
		t.Fatalf("expected the publish stage, got %v", got)
	}

	removeErr := &anchorRaiseError{Stage: anchorRaiseStageRemoveFile, Err: fmt.Errorf("remove failed")}
	if got := anchorRaiseFailureStage(removeErr); got != anchorRaiseStageRemoveFile {
		t.Fatalf("expected the remove-file stage, got %v", got)
	}

	// A plain error (should never happen in practice, since raiseAnchorWatch
	// always wraps) must still resolve to a stage rather than panicking -
	// the publish stage, the more common and more serious failure.
	if got := anchorRaiseFailureStage(fmt.Errorf("unwrapped")); got != anchorRaiseStagePublish {
		t.Fatalf("expected the publish stage as the fallback for a bare error, got %v", got)
	}
}
