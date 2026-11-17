//go:build !unix

package main

import (
	"errors"
	"time"
)

// processCPUTime is not available off unix; the CPU figure is left out.
func processCPUTime() (time.Duration, error) {
	return 0, errors.New("process CPU time is not available on this platform")
}
