package main

import (
	"errors"
	"os"
)

// anchorRaiseStage identifies which step of raiseAnchorWatch failed, so a
// caller can pick its own error message/log line for each without
// duplicating the steps themselves.
type anchorRaiseStage int

const (
	anchorRaiseStagePublish anchorRaiseStage = iota
	anchorRaiseStageRemoveFile
)

// anchorRaiseError wraps a raiseAnchorWatch failure with the stage it
// happened at.
type anchorRaiseError struct {
	Stage anchorRaiseStage
	Err   error
}

func (e *anchorRaiseError) Error() string { return e.Err.Error() }
func (e *anchorRaiseError) Unwrap() error { return e.Err }

// anchorRaiseFailureStage reports which stage an error from raiseAnchorWatch
// failed at. Any error that isn't an *anchorRaiseError falls back to the
// publish stage - the more common and more serious of the two - which
// should never actually happen since raiseAnchorWatch always wraps, but a
// caller still needs one stage to pick rather than a panic.
func anchorRaiseFailureStage(err error) anchorRaiseStage {
	var raiseErr *anchorRaiseError
	if errors.As(err, &raiseErr) {
		return raiseErr.Stage
	}
	return anchorRaiseStagePublish
}

// raiseAnchorWatch performs the full anchor-raise lifecycle: publish an
// explicit null anchor position to SignalK, remove the local record, and
// clear the in-memory watch, its trail and its session placemarks. It is
// the one path both DELETE /api/anchor-watch (anchor.go's deleteAnchorWatch)
// and the server-side auto-raise watcher (anchor_auto_raise.go, ADR 0099)
// go through, so an operator's Raise and an automatic one leave SignalK,
// disk and memory in exactly the same state - and so a bug fixed in one is
// fixed in both.
//
// Callers must already hold anchorLifecycleMu: it serializes this against a
// concurrent Drop/reposition/PATCH, and, since the watcher calls this too,
// against a concurrent operator Raise from the HTTP handler.
//
// Runs the publish step even when there is no active watch: retrying Raise
// must repair an upstream Auto-state latch, not silently skip the explicit
// null event (docs/adr/0078).
func raiseAnchorWatch() error {
	if err := publishSignalKAnchorPosition(nil); err != nil {
		return &anchorRaiseError{Stage: anchorRaiseStagePublish, Err: err}
	}
	if err := os.Remove(anchorWatchFilePath()); err != nil && !os.IsNotExist(err) {
		return &anchorRaiseError{Stage: anchorRaiseStageRemoveFile, Err: err}
	}

	anchorWatchMu.Lock()
	anchorWatchState = nil
	anchorWatchMu.Unlock()

	trailMu.Lock()
	selfTrail = nil
	trailMu.Unlock()

	// Placemarks are bound to the anchoring session, so they end with it.
	clearPlacemarks()

	return nil
}
