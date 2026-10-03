package main

import (
	"errors"
	"testing"
)

func TestMapInventoryError_NotFoundNamesRegisteredFields(t *testing.T) {
	for err, want := range map[error]string{errZoneNotFound: "zone_id", errBinNotFound: "bin_id"} {
		var fe *fieldedError
		if !errors.As(mapInventoryError(err), &fe) || fe.Field != want {
			t.Fatalf("%v: expected field %q, got %+v", err, want, fe)
		}
	}
}
